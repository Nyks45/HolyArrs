package stream

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// CachedSearchResult holds a scored stream search response with its private upstream metadata.
type CachedSearchResult struct {
	Candidates []UnifiedCandidate
	Sources    any
	Warnings   []string
	StoredAt   time.Time
}

// SearchCache caches recent search results per title/episode for sub-millisecond repeat loads.
type SearchCache struct {
	mu    sync.Mutex
	items map[string]CachedSearchResult
	ttl   time.Duration
	max   int
}

// NewSearchCache constructs a bounded TTL stream search cache.
func NewSearchCache() *SearchCache {
	return &SearchCache{
		items: map[string]CachedSearchResult{},
		ttl:   10 * time.Minute,
		max:   300,
	}
}

// SearchCacheKey builds a normalized cache lookup key for a title/episode/mode query.
func SearchCacheKey(kind, tmdb, imdb string, season, episode int, query, mode string) string {
	return strings.Join([]string{
		strings.ToLower(strings.TrimSpace(kind)),
		strings.TrimSpace(tmdb),
		strings.ToLower(strings.TrimSpace(imdb)),
		fmt.Sprintf("%d:%d", season, episode),
		strings.ToLower(strings.TrimSpace(query)),
		strings.ToLower(strings.TrimSpace(mode)),
	}, "|")
}

// Get returns a copy of the cached result if present and not expired.
func (c *SearchCache) Get(key string) (CachedSearchResult, bool) {
	if c == nil || key == "" {
		return CachedSearchResult{}, false
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.items[key]
	if !ok {
		return CachedSearchResult{}, false
	}
	if now.Sub(entry.StoredAt) > c.ttl {
		delete(c.items, key)
		return CachedSearchResult{}, false
	}
	entry.Candidates = slices.Clone(entry.Candidates)
	entry.Warnings = slices.Clone(entry.Warnings)
	return entry, true
}

// Set stores a search result copy under key, pruning expired entries when full.
func (c *SearchCache) Set(key string, res CachedSearchResult) {
	if c == nil || key == "" {
		return
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.items) >= c.max {
		var oldestKey string
		var oldestTime time.Time
		for k, v := range c.items {
			if now.Sub(v.StoredAt) > c.ttl {
				delete(c.items, k)
				continue
			}
			if oldestKey == "" || v.StoredAt.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.StoredAt
			}
		}
		if len(c.items) >= c.max && oldestKey != "" {
			delete(c.items, oldestKey)
		}
	}
	res.Candidates = slices.Clone(res.Candidates)
	res.Warnings = slices.Clone(res.Warnings)
	res.StoredAt = now
	c.items[key] = res
}

// InvalidateAll clears all cached searches (for example when plugins or debrid config change).
func (c *SearchCache) InvalidateAll() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.items = map[string]CachedSearchResult{}
	c.mu.Unlock()
}

type providerHealthEntry struct {
	emaMS         float64
	consecFail    int
	cooldownUntil time.Time
}

// ProviderHealth tracks moving-average response latency and temporary cooldowns per provider.
type ProviderHealth struct {
	mu    sync.Mutex
	stats map[string]*providerHealthEntry
}

// NewProviderHealth constructs a provider latency and failure tracker.
func NewProviderHealth() *ProviderHealth {
	return &ProviderHealth{
		stats: map[string]*providerHealthEntry{},
	}
}

func normProviderKey(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}

func (h *ProviderHealth) entryLocked(key string) *providerHealthEntry {
	e := h.stats[key]
	if e == nil {
		e = &providerHealthEntry{emaMS: 1500}
		h.stats[key] = e
	}
	return e
}

// ShouldSkip reports whether id is currently in a temporary failure cooldown window.
func (h *ProviderHealth) ShouldSkip(id string) bool {
	if h == nil {
		return false
	}
	key := normProviderKey(id)
	if key == "" {
		return false
	}
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.stats[key]
	if e == nil || e.cooldownUntil.IsZero() {
		return false
	}
	if !now.Before(e.cooldownUntil) {
		e.cooldownUntil = time.Time{}
		e.consecFail = 0
		return false
	}
	return true
}

// LatencyMS returns the tracked exponential moving average latency in ms (default 1500).
func (h *ProviderHealth) LatencyMS(id string) float64 {
	if h == nil {
		return 1500
	}
	key := normProviderKey(id)
	if key == "" {
		return 1500
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if e := h.stats[key]; e != nil && e.emaMS > 0 {
		return e.emaMS
	}
	return 1500
}

// RecordSuccess resets consecutive failures for id and updates its latency EMA.
func (h *ProviderHealth) RecordSuccess(id string, dur time.Duration) {
	if h == nil {
		return
	}
	key := normProviderKey(id)
	if key == "" {
		return
	}
	ms := float64(dur.Milliseconds())
	if ms < 1 {
		ms = 1
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.entryLocked(key)
	e.consecFail = 0
	e.cooldownUntil = time.Time{}
	if e.emaMS <= 0 {
		e.emaMS = ms
	} else {
		e.emaMS = 0.35*ms + 0.65*e.emaMS
	}
}

// RecordFailure increments consecutive failures for id and triggers a 3-minute cooldown at 3 failures.
func (h *ProviderHealth) RecordFailure(id string) {
	if h == nil {
		return
	}
	key := normProviderKey(id)
	if key == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.entryLocked(key)
	e.consecFail++
	if e.consecFail >= 3 {
		e.cooldownUntil = time.Now().Add(3 * time.Minute)
	}
}

// PreflightAndMeasureCandidates deduplicates instant HTTP/HLS links, probes up to 24 links
// concurrently (round-robin across providers), drops HTML/error links, and records latency.
func PreflightAndMeasureCandidates(ctx context.Context, pm *ProxyManager, cands []UnifiedCandidate, health *ProviderHealth) []UnifiedCandidate {
	if pm == nil || len(cands) == 0 {
		return cands
	}

	// Deduplicate instant StreamURLs before probing and group candidate indices by provider.
	seenURLs := make(map[string]bool, len(cands))
	deduped := make([]UnifiedCandidate, 0, len(cands))
	byProvider := map[string][]int{}
	var provOrder []string
	for _, c := range cands {
		if (c.Kind == "http" || c.Kind == "hls") && c.StreamURL != "" {
			u := strings.TrimSpace(c.StreamURL)
			if seenURLs[u] {
				continue
			}
			seenURLs[u] = true
			idx := len(deduped)
			deduped = append(deduped, c)
			pk := normProviderKey(fallbackString(c.Provider, c.SourceName))
			if _, ok := byProvider[pk]; !ok {
				provOrder = append(provOrder, pk)
			}
			byProvider[pk] = append(byProvider[pk], idx)
			continue
		}
		deduped = append(deduped, c)
	}

	// Pick up to 32 instant candidates round-robin across providers so every healthy provider gets probed.
	const maxProbes = 32
	probeIndices := make([]int, 0, maxProbes)
	round := 0
	for len(probeIndices) < maxProbes {
		added := false
		for _, pk := range provOrder {
			list := byProvider[pk]
			if round < len(list) {
				probeIndices = append(probeIndices, list[round])
				added = true
				if len(probeIndices) >= maxProbes {
					break
				}
			}
		}
		if !added {
			break
		}
		round++
	}
	if len(probeIndices) == 0 {
		return deduped
	}

	dropped := make([]bool, len(deduped))
	pctx, cancelAll := context.WithTimeout(ctx, 4500*time.Millisecond)
	defer cancelAll()

	sem := make(chan struct{}, 16)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, idx := range probeIndices {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-pctx.Done():
				return
			}
			defer func() { <-sem }()

			c := deduped[i]
			ic, icCancel := context.WithTimeout(pctx, 3800*time.Millisecond)
			t0 := time.Now()
			ct, isHLS, err := pm.Preflight(ic, c.StreamURL, c.Headers)
			elapsed := time.Since(t0)
			icCancel()

			if err != nil {
				if health != nil {
					health.RecordFailure(fallbackString(c.Provider, c.SourceName))
				}
				mu.Lock()
				dropped[i] = true
				mu.Unlock()
				return
			}

			ms := int(elapsed.Milliseconds())
			if ms < 1 {
				ms = 1
			}
			if health != nil {
				health.RecordSuccess(fallbackString(c.Provider, c.SourceName), elapsed)
			}
			c.ProbeMS = ms
			c.Verified = true
			if isHLS || c.Kind == "hls" || looksLikeHLSURL(c.StreamURL) {
				c.Kind = "hls"
				c.FastStart = true
			} else {
				metaText := strings.TrimSpace(c.ReleaseName + " " + c.Title + " " + c.Filename)
				c.FastStart = !looksIncompatibleForDirectPlay(metaText, c.StreamURL, ct)
			}
			mu.Lock()
			deduped[i] = c
			mu.Unlock()
		}(idx)
	}
	wg.Wait()

	out := make([]UnifiedCandidate, 0, len(deduped))
	for i, c := range deduped {
		if dropped[i] {
			continue
		}
		out = append(out, c)
	}
	return out
}
