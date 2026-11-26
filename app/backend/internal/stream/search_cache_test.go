package stream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSearchCacheHitExpiryAndInvalidate(t *testing.T) {
	c := NewSearchCache()
	k1 := SearchCacheKey("show", "1396", "TT0903747", 1, 1, " Breaking Bad ", "Fast")
	k2 := SearchCacheKey("show", "1396", "tt0903747", 1, 2, "Breaking Bad", "fast")
	if k1 == k2 {
		t.Fatalf("expected distinct keys across episodes: %q vs %q", k1, k2)
	}
	if _, ok := c.Get(k1); ok {
		t.Fatal("expected cache miss on empty cache")
	}

	c.Set(k1, CachedSearchResult{
		Candidates: []UnifiedCandidate{{Provider: "vidrock", Quality: "1080p", Kind: "hls"}},
		Warnings:   []string{"w1"},
	})
	got, ok := c.Get(k1)
	if !ok || len(got.Candidates) != 1 || got.Candidates[0].Provider != "vidrock" {
		t.Fatalf("expected cache hit with cloned candidate, got ok=%v res=%+v", ok, got)
	}
	// Mutating caller slice must not corrupt cached copy.
	got.Candidates[0].Provider = "tampered"
	again, ok := c.Get(k1)
	if !ok || again.Candidates[0].Provider != "vidrock" {
		t.Fatalf("cache should clone slices on Get, got %+v", again)
	}

	// Expire entry past TTL.
	c.mu.Lock()
	entry := c.items[k1]
	entry.StoredAt = time.Now().Add(-15 * time.Minute)
	c.items[k1] = entry
	c.mu.Unlock()
	if _, ok := c.Get(k1); ok {
		t.Fatal("expected expired entry to miss and be evicted")
	}

	c.Set(k2, CachedSearchResult{Candidates: []UnifiedCandidate{{Provider: "castle"}}})
	c.InvalidateAll()
	if _, ok := c.Get(k2); ok {
		t.Fatal("expected InvalidateAll to clear cached entries")
	}
}

func TestProviderHealthCooldownAndLatencyOrder(t *testing.T) {
	h := NewProviderHealth()
	if h.LatencyMS("vidrock") != 1500 {
		t.Fatalf("expected default 1500ms EMA, got %v", h.LatencyMS("vidrock"))
	}
	if h.ShouldSkip("vidrock") {
		t.Fatal("fresh provider must not be skipped")
	}

	h.RecordSuccess("vidrock", 120*time.Millisecond)
	h.RecordSuccess("slowprov", 2600*time.Millisecond)
	if h.LatencyMS("vidrock") >= h.LatencyMS("slowprov") {
		t.Fatalf("expected fast provider lower latency than slow: fast=%v slow=%v",
			h.LatencyMS("vidrock"), h.LatencyMS("slowprov"))
	}

	h.RecordFailure("flaky")
	h.RecordFailure("flaky")
	if h.ShouldSkip("flaky") {
		t.Fatal("expected fewer than 3 failures not to trigger cooldown")
	}
	h.RecordFailure("flaky")
	if !h.ShouldSkip("flaky") {
		t.Fatal("expected 3 consecutive failures to trigger cooldown")
	}

	h.RecordSuccess("flaky", 200*time.Millisecond)
	if h.ShouldSkip("flaky") {
		t.Fatal("expected RecordSuccess to clear failure cooldown")
	}
}

func TestPreflightAndMeasureFiltersHTMLAndRanksFastestForQuality(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<!doctype html><html><body>Please wait 5s...</body></html>"))
		case "/xmlerr":
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("<Error><Code>NoSuchKey</Code></Error>"))
		case "/jsonerr":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"quota exceeded"}`))
		case "/plainerr":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("Unable to Get Download Link from upstream"))
		case "/slow.m3u8":
			time.Sleep(85 * time.Millisecond)
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=2000000\nseg.m3u8\n"))
		case "/fast.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=2000000\nseg.m3u8\n"))
		case "/mkv":
			w.Header().Set("Content-Type", "video/x-matroska")
			_, _ = w.Write([]byte{0x1a, 0x45, 0xdf, 0xa3, 0x00, 0x01, 0x02, 0x03})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	pm := NewProxyManager()
	pm.SetHTTPClient(srv.Client())
	health := NewProviderHealth()

	raw := []UnifiedCandidate{
		{Kind: "http", Provider: "uhdmovies", Title: "HTML trap", StreamURL: srv.URL + "/page", Quality: "1080p"},
		{Kind: "http", Provider: "streamflix", Title: "XML 404", StreamURL: srv.URL + "/xmlerr", Quality: "1080p"},
		{Kind: "http", Provider: "moviesdrive", Title: "JSON 403", StreamURL: srv.URL + "/jsonerr", Quality: "1080p"},
		{Kind: "http", Provider: "4khdhub", Title: "Plain err", StreamURL: srv.URL + "/plainerr", Quality: "1080p"},
		{Kind: "http", Provider: "reanime", Title: "Movie.1080p.mkv", StreamURL: srv.URL + "/mkv", Filename: "Movie.1080p.mkv", Quality: "1080p"},
		{Kind: "http", Provider: "slowhls", Title: "Slow HLS", StreamURL: srv.URL + "/slow.m3u8", Quality: "1080p"},
		{Kind: "http", Provider: "fasthls", Title: "Fast HLS", StreamURL: srv.URL + "/fast.m3u8", Quality: "1080p"},
	}

	verified := PreflightAndMeasureCandidates(context.Background(), pm, raw, health)
	if len(verified) != 3 {
		t.Fatalf("expected 3 surviving playable candidates (4 dead/HTML dropped), got %d: %+v", len(verified), verified)
	}
	for _, c := range verified {
		if !c.Verified || c.ProbeMS <= 0 {
			t.Fatalf("expected Verified=true and ProbeMS>0, got %+v", c)
		}
		if c.Provider == "reanime" && c.FastStart {
			t.Fatalf("expected MKV candidate not to mark FastStart=true: %+v", c)
		}
		if (c.Provider == "slowhls" || c.Provider == "fasthls") && (!c.FastStart || c.Kind != "hls") {
			t.Fatalf("expected HLS stream upgraded to Kind=hls FastStart=true, got %+v", c)
		}
	}

	ranked := FilterAndRankCandidates(verified, StreamPrefs{PreferredQuality: "1080p"})
	if len(ranked) != 3 {
		t.Fatalf("expected 3 ranked candidates, got %d", len(ranked))
	}
	if ranked[0].Provider != "fasthls" {
		t.Fatalf("expected fastest verified 1080p HLS (fasthls) first, got order: %s, %s, %s (scores: %d, %d, %d)",
			ranked[0].Provider, ranked[1].Provider, ranked[2].Provider,
			ranked[0].Score, ranked[1].Score, ranked[2].Score)
	}
	if ranked[1].Provider != "slowhls" || ranked[2].Provider != "reanime" {
		t.Fatalf("expected fast-start HLS before non-fast-start MKV, got %+v", ranked)
	}
}
