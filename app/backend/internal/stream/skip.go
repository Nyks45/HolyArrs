package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/outbound"
)

// SkipSegment defines an intro, recap, outro, or credits interval in seconds.
type SkipSegment struct {
	Kind   string  `json:"kind"` // intro, recap, outro, credits
	Start  float64 `json:"start"`
	End    float64 `json:"end"`
	Source string  `json:"source"` // introdb, aniskip, animeskip, chapters
	Label  string  `json:"label"`
}

type cachedSkipResult struct {
	segments  []SkipSegment
	expiresAt time.Time
}

type cachedMALMapping struct {
	malID     int
	expiresAt time.Time
}

// SkipService resolves skip segments across IntroDB, AniSkip, Anime-Skip, and
// embedded MKV chapter marks with 24-hour in-memory caching.
type SkipService struct {
	mu            sync.RWMutex
	client        *http.Client
	introDBBase   string
	yunaBase      string
	aniSkipBase   string
	animeSkipBase string
	cache         map[string]cachedSkipResult
	malCache      map[string]cachedMALMapping
}

// NewSkipService constructs a SkipService backed by an SSRF-safe HTTP client.
func NewSkipService() *SkipService {
	tr := &http.Transport{
		DialContext:         outbound.PolicyDialer(outbound.NotMetadata).DialContext,
		MaxIdleConns:        16,
		IdleConnTimeout:     60 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	return &SkipService{
		client:        &http.Client{Transport: tr, Timeout: 6 * time.Second},
		introDBBase:   "https://api.introdb.app",
		yunaBase:      "https://relations.yuna.moe",
		aniSkipBase:   "https://api.aniskip.com",
		animeSkipBase: "https://api.anime-skip.com",
		cache:         map[string]cachedSkipResult{},
		malCache:      map[string]cachedMALMapping{},
	}
}

// SetTestEndpoints overrides upstream API bases for unit tests.
func (s *SkipService) SetTestEndpoints(hc *http.Client, introDB, yuna, aniSkip, animeSkip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if hc != nil {
		s.client = hc
	}
	if introDB != "" {
		s.introDBBase = strings.TrimRight(introDB, "/")
	}
	if yuna != "" {
		s.yunaBase = strings.TrimRight(yuna, "/")
	}
	if aniSkip != "" {
		s.aniSkipBase = strings.TrimRight(aniSkip, "/")
	}
	if animeSkip != "" {
		s.animeSkipBase = strings.TrimRight(animeSkip, "/")
	}
}

// GetSegments fetches skip segments for a show episode or movie and merges
// any chapter-derived markers as lower-priority fallbacks.
func (s *SkipService) GetSegments(ctx context.Context, kind, imdbID, tmdbID string, season, episode int, animeHint bool, title string, animeSkipClientID string, chapters []ChapterMark) []SkipSegment {
	cacheKey := fmt.Sprintf("%s|%s|%s|%d|%d|%v", kind, imdbID, tmdbID, season, episode, animeHint)
	var remote []SkipSegment
	haveCached := false
	now := time.Now()
	s.mu.RLock()
	if c, ok := s.cache[cacheKey]; ok && now.Before(c.expiresAt) {
		remote = c.segments
		haveCached = true
	}
	s.mu.RUnlock()

	if !haveCached {
		remote = s.fetchRemoteSegments(ctx, kind, imdbID, tmdbID, season, episode, animeHint, title, animeSkipClientID)
		s.mu.Lock()
		if len(s.cache) > 1000 {
			s.cache = map[string]cachedSkipResult{}
		}
		s.cache[cacheKey] = cachedSkipResult{segments: remote, expiresAt: now.Add(24 * time.Hour)}
		s.mu.Unlock()
	}

	return mergeSkipSegments(remote, chaptersToSegments(chapters))
}

func (s *SkipService) fetchRemoteSegments(ctx context.Context, kind, imdbID, tmdbID string, season, episode int, animeHint bool, title string, animeSkipClientID string) []SkipSegment {
	var mu sync.Mutex
	var out []SkipSegment
	var wg sync.WaitGroup

	if imdbID != "" && ((kind == "show" && season > 0 && episode > 0) || kind == "movie") {
		wg.Add(1)
		go func() {
			defer wg.Done()
			segs := s.queryIntroDB(ctx, imdbID, season, episode)
			if len(segs) > 0 {
				mu.Lock()
				out = append(out, segs...)
				mu.Unlock()
			}
		}()
	}

	if kind == "show" && episode > 0 && (animeHint || tmdbID != "" || imdbID != "") {
		wg.Add(1)
		go func() {
			defer wg.Done()
			malID := s.resolveMALID(ctx, imdbID, tmdbID)
			if malID <= 0 {
				return
			}
			segs := s.queryAniSkip(ctx, malID, episode)
			if len(segs) > 0 {
				mu.Lock()
				out = append(out, segs...)
				mu.Unlock()
			}
		}()
	}

	if animeSkipClientID != "" && episode > 0 && title != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			segs := s.queryAnimeSkip(ctx, animeSkipClientID, title, season, episode)
			if len(segs) > 0 {
				mu.Lock()
				out = append(out, segs...)
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	return out
}

func (s *SkipService) queryIntroDB(ctx context.Context, imdbID string, season, episode int) []SkipSegment {
	s.mu.RLock()
	base, hc := s.introDBBase, s.client
	s.mu.RUnlock()

	u := fmt.Sprintf("%s/segments?imdb_id=%s", base, url.QueryEscape(imdbID))
	if season > 0 && episode > 0 {
		u += fmt.Sprintf("&season=%d&episode=%d", season, episode)
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, u, nil)
	if err != nil {
		return nil
	}
	resp, err := hc.Do(req)
	if err != nil || resp.StatusCode >= 300 {
		if resp != nil {
			resp.Body.Close()
		}
		return nil
	}
	defer resp.Body.Close()

	type rawSeg struct {
		StartSec *float64 `json:"start_sec"`
		EndSec   *float64 `json:"end_sec"`
		StartMS  *float64 `json:"start_ms"`
		EndMS    *float64 `json:"end_ms"`
		Kind     string   `json:"kind"`
	}
	var obj struct {
		Intro       *rawSeg  `json:"intro"`
		Recap       *rawSeg  `json:"recap"`
		Outro       *rawSeg  `json:"outro"`
		PostCredits *rawSeg  `json:"post_credits"`
		Credits     *rawSeg  `json:"credits"`
		Segments    []rawSeg `json:"segments"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&obj) != nil {
		return nil
	}
	var out []SkipSegment
	addRaw := func(k string, rs *rawSeg) {
		if rs == nil {
			return
		}
		var st, en float64
		if rs.StartSec != nil && rs.EndSec != nil {
			st, en = *rs.StartSec, *rs.EndSec
		} else if rs.StartMS != nil && rs.EndMS != nil {
			st, en = *rs.StartMS/1000.0, *rs.EndMS/1000.0
		}
		if en > st && en > 0 {
			out = append(out, SkipSegment{
				Kind:   k,
				Start:  st,
				End:    en,
				Source: "introdb",
				Label:  skipLabelForKind(k),
			})
		}
	}
	addRaw("intro", obj.Intro)
	addRaw("recap", obj.Recap)
	addRaw("outro", obj.Outro)
	if obj.PostCredits != nil {
		addRaw("credits", obj.PostCredits)
	} else {
		addRaw("credits", obj.Credits)
	}
	for i := range obj.Segments {
		k := strings.ToLower(strings.TrimSpace(obj.Segments[i].Kind))
		if k != "" {
			addRaw(k, &obj.Segments[i])
		}
	}
	return out
}

func (s *SkipService) resolveMALID(ctx context.Context, imdbID, tmdbID string) int {
	lookup := func(source, id string) int {
		if id == "" {
			return 0
		}
		key := source + "|" + id
		now := time.Now()
		s.mu.RLock()
		c, ok := s.malCache[key]
		base, hc := s.yunaBase, s.client
		s.mu.RUnlock()
		if ok && now.Before(c.expiresAt) {
			return c.malID
		}
		u := fmt.Sprintf("%s/api/v2/ids?source=%s&id=%s", base, url.QueryEscape(source), url.QueryEscape(id))
		cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(cctx, http.MethodGet, u, nil)
		if err != nil {
			return 0
		}
		resp, err := hc.Do(req)
		if err != nil || resp.StatusCode >= 300 {
			if resp != nil {
				resp.Body.Close()
			}
			return 0
		}
		var parsed struct {
			MyAnimeList int `json:"myanimelist"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 256<<10)).Decode(&parsed)
		resp.Body.Close()
		s.mu.Lock()
		s.malCache[key] = cachedMALMapping{malID: parsed.MyAnimeList, expiresAt: now.Add(24 * time.Hour)}
		s.mu.Unlock()
		return parsed.MyAnimeList
	}
	if tmdbID != "" {
		if id := lookup("themoviedb", tmdbID); id > 0 {
			return id
		}
	}
	if imdbID != "" {
		if id := lookup("imdb", imdbID); id > 0 {
			return id
		}
	}
	return 0
}

func (s *SkipService) queryAniSkip(ctx context.Context, malID, episode int) []SkipSegment {
	s.mu.RLock()
	base, hc := s.aniSkipBase, s.client
	s.mu.RUnlock()

	u := fmt.Sprintf("%s/v2/skip-times/%d/%d?types[]=op&types[]=ed&types[]=recap&types[]=mixed-op&types[]=mixed-ed&episodeLength=0",
		base, malID, episode)
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, u, nil)
	if err != nil {
		return nil
	}
	resp, err := hc.Do(req)
	if err != nil || resp.StatusCode >= 300 {
		if resp != nil {
			resp.Body.Close()
		}
		return nil
	}
	defer resp.Body.Close()
	var parsed struct {
		Found   bool `json:"found"`
		Results []struct {
			SkipType string `json:"skipType"`
			Interval struct {
				StartTime float64 `json:"startTime"`
				EndTime   float64 `json:"endTime"`
			} `json:"interval"`
		} `json:"results"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&parsed) != nil || !parsed.Found {
		return nil
	}
	var out []SkipSegment
	for _, r := range parsed.Results {
		if r.Interval.EndTime <= r.Interval.StartTime {
			continue
		}
		kind := "intro"
		switch r.SkipType {
		case "op", "mixed-op":
			kind = "intro"
		case "ed", "mixed-ed":
			kind = "outro"
		case "recap":
			kind = "recap"
		}
		out = append(out, SkipSegment{
			Kind:   kind,
			Start:  r.Interval.StartTime,
			End:    r.Interval.EndTime,
			Source: "aniskip",
			Label:  skipLabelForKind(kind),
		})
	}
	return out
}

func (s *SkipService) queryAnimeSkip(ctx context.Context, clientID, title string, season, episode int) []SkipSegment {
	s.mu.RLock()
	base, hc := s.animeSkipBase, s.client
	s.mu.RUnlock()

	query := `query($search: String!, $offset: Int) { searchShows(search: $search, offset: $offset) { id name episodes { number season timestamps { at type { name } } } } }`
	payload, _ := json.Marshal(map[string]any{
		"query":     query,
		"variables": map[string]any{"search": title, "offset": 0},
	})
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, base+"/graphql", bytes.NewReader(payload))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-ID", clientID)
	resp, err := hc.Do(req)
	if err != nil || resp.StatusCode >= 300 {
		if resp != nil {
			resp.Body.Close()
		}
		return nil
	}
	defer resp.Body.Close()
	var parsed struct {
		Data struct {
			SearchShows []struct {
				Episodes []struct {
					Number     string `json:"number"`
					Season     string `json:"season"`
					Timestamps []struct {
						At   float64 `json:"at"`
						Type struct {
							Name string `json:"name"`
						} `json:"type"`
					} `json:"timestamps"`
				} `json:"episodes"`
			} `json:"searchShows"`
		} `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&parsed) != nil || len(parsed.Data.SearchShows) == 0 {
		return nil
	}
	epStr := strconv.Itoa(episode)
	sStr := strconv.Itoa(season)
	for _, ep := range parsed.Data.SearchShows[0].Episodes {
		if ep.Number != epStr {
			continue
		}
		if season > 0 && ep.Season != "" && ep.Season != sStr {
			continue
		}
		var out []SkipSegment
		for i, ts := range ep.Timestamps {
			k := classifySkipTitle(ts.Type.Name)
			if k == "" {
				continue
			}
			end := ts.At + 85
			if i+1 < len(ep.Timestamps) && ep.Timestamps[i+1].At > ts.At {
				end = ep.Timestamps[i+1].At
			}
			if end > ts.At {
				out = append(out, SkipSegment{
					Kind:   k,
					Start:  ts.At,
					End:    end,
					Source: "animeskip",
					Label:  skipLabelForKind(k),
				})
			}
		}
		return out
	}
	return nil
}

func chaptersToSegments(chapters []ChapterMark) []SkipSegment {
	var out []SkipSegment
	for _, ch := range chapters {
		if ch.Kind == "" || ch.End <= ch.Start {
			continue
		}
		out = append(out, SkipSegment{
			Kind:   ch.Kind,
			Start:  ch.Start,
			End:    ch.End,
			Source: "chapters",
			Label:  skipLabelForKind(ch.Kind),
		})
	}
	return out
}

func mergeSkipSegments(primary, fallback []SkipSegment) []SkipSegment {
	byKind := map[string]SkipSegment{}
	prio := func(src string) int {
		switch src {
		case "introdb", "aniskip":
			return 3
		case "animeskip":
			return 2
		case "chapters":
			return 1
		}
		return 0
	}
	for _, seg := range append(append([]SkipSegment{}, fallback...), primary...) {
		if seg.End <= seg.Start {
			continue
		}
		cur, exists := byKind[seg.Kind]
		if !exists || prio(seg.Source) > prio(cur.Source) {
			byKind[seg.Kind] = seg
		}
	}
	out := make([]SkipSegment, 0, len(byKind))
	for _, seg := range byKind {
		out = append(out, seg)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

func skipLabelForKind(k string) string {
	switch k {
	case "intro":
		return "Skip Intro"
	case "recap":
		return "Skip Recap"
	case "outro":
		return "Skip Outro"
	case "credits":
		return "Skip Credits"
	}
	return "Skip"
}

func classifySkipTitle(title string) string {
	low := strings.ToLower(strings.TrimSpace(title))
	switch {
	case strings.Contains(low, "recap") || strings.Contains(low, "previously"):
		return "recap"
	case strings.Contains(low, "intro") || strings.Contains(low, "opening") || low == "op":
		return "intro"
	case strings.Contains(low, "outro") || strings.Contains(low, "ending") || low == "ed":
		return "outro"
	case strings.Contains(low, "credit"):
		return "credits"
	}
	return ""
}
