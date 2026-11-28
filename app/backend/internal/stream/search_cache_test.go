package stream

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStreamSearchResolvesMetadataOnlyForUncachedRemoteSearch(t *testing.T) {
	m := NewManager(nil, nil, t.TempDir(), nil)
	t.Cleanup(m.Close)
	calls := 0
	m.ResolveIMDb = func(context.Context, string, string) string {
		calls++
		return "tt0903747"
	}
	h := &Handlers{Mgr: m}
	req := func(body string) {
		t.Helper()
		w := httptest.NewRecorder()
		h.search(w, httptest.NewRequest(http.MethodPost, "/api/v1/stream/search", strings.NewReader(body)))
		if w.Code != http.StatusOK {
			t.Fatalf("search returned HTTP %d: %s", w.Code, w.Body.String())
		}
	}
	req(`{"kind":"show","tmdb":1396,"season":1,"episode":1,"local_only":true}`)
	key := SearchCacheKey("show", "1396", "", 1, 1, "", "full")
	m.SearchCache.Set(key, CachedSearchResult{})
	req(`{"kind":"show","tmdb":1396,"season":1,"episode":1}`)
	if calls != 0 {
		t.Fatalf("metadata lookups during local or cached search: %d", calls)
	}
	m.SearchCache.InvalidateAll()
	req(`{"kind":"show","tmdb":1396,"season":1,"episode":1}`)
	if calls != 1 {
		t.Fatalf("metadata lookups for uncached remote search: %d", calls)
	}
}

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

	// Hover prefetch (empty query/imdb, only tmdb) must match playback search (full query+imdb).
	hoverKey := SearchCacheKey("show", "1396", "", 1, 1, "", "fast")
	if hoverKey != k1 {
		t.Fatalf("expected hover prefetch key %q to match full search key %q", hoverKey, k1)
	}
	fullModeKey := SearchCacheKey("show", "1396", "tt0903747", 1, 1, "Breaking Bad", "full")
	if fullModeKey == k1 {
		t.Fatalf("expected fast vs full mode keys to stay distinct: %q", fullModeKey)
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
		case "/seg.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:5.0,\nseg0.ts\n"))
		case "/seg0.ts":
			w.Header().Set("Content-Type", "video/mp2t")
			_, _ = w.Write(bytes.Repeat([]byte{0x47, 0x01, 0x02}, 150))
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

func makeFakeTSSlice() []byte {
	ts := make([]byte, 188*3)
	for i := 0; i < 3; i++ {
		ts[i*188] = 0x47
		ts[i*188+1] = 0x40
	}
	return ts
}

func TestProxyManagerUnwrapsDisguisedTSManifestSanitizeAndDeepPreflight(t *testing.T) {
	tsPayload := makeFakeTSSlice()
	pngWrapped := append(bytes.Repeat([]byte{0x89, 'P', 'N', 'G'}, 16), tsPayload...) // 64-byte PNG header before 0x47

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(w, strings.Join([]string{
				"#EXTM3U",
				`#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aud",NAME="Français",DEFAULT=YES,AUTOSELECT=YES,LANGUAGE="fr",URI="aud_fr.m3u8"`,
				`#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aud",NAME="English",DEFAULT=NO,AUTOSELECT=NO,LANGUAGE="en",URI="aud_en.m3u8"`,
				`#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",NAME="French",DEFAULT=YES,LANGUAGE="fre",URI="subs_fre.vtt"`,
				`#EXT-X-STREAM-INF:BANDWIDTH=2000000,AUDIO="aud",SUBTITLES="subs"`,
				"var.m3u8",
			}, "\n"))
		case "/var.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(w, "#EXTM3U\n#EXTINF:4.0,\nseg_png.png\n#EXTINF:4.0,\nseg_jpg.jpg\n")
		case "/seg_png.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngWrapped)
		case "/seg_jpg.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(tsPayload)
		case "/cf_master.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=2000000\ncf_var.m3u8\n")
		case "/cf_var.m3u8":
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, "<!doctype html><html><title>Attention Required!</title></html>")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	pm := NewProxyManager()
	pm.SetHTTPClient(srv.Client())
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/streaming/proxy/{token}", pm.ServeProxy)
	mux.HandleFunc("GET /api/v1/streaming/proxy/{token}/hls", pm.ServeProxyHLS)

	// 1. Deep preflight must reject Cloudflare 403 HTML variant even when master is 200 #EXTM3U.
	if _, _, err := pm.Preflight(context.Background(), srv.URL+"/cf_master.m3u8", nil); err == nil {
		t.Fatal("expected deep Preflight to reject master whose variant returns 403 HTML")
	}

	// 2. Valid chain passes deep Preflight, and rewritten master strips .vtt subtitles + picks English audio default.
	if _, isHLS, err := pm.Preflight(context.Background(), srv.URL+"/master.m3u8", nil); err != nil || !isHLS {
		t.Fatalf("expected valid HLS chain to pass Preflight, got isHLS=%v err=%v", isHLS, err)
	}
	tok, _, err := pm.Register(srv.URL+"/master.m3u8", nil, 1)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/streaming/proxy/"+tok, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	manifest := rec.Body.String()
	if strings.Contains(manifest, ".vtt") || strings.Contains(manifest, `SUBTITLES="subs"`) {
		t.Fatalf("expected raw .vtt subtitle group stripped from master manifest, got:\n%s", manifest)
	}
	if !strings.Contains(manifest, `LANGUAGE="en",URI=`) || !strings.Contains(manifest, `NAME="English",DEFAULT=YES,AUTOSELECT=YES`) {
		t.Fatalf("expected English audio promoted to DEFAULT=YES, got:\n%s", manifest)
	}

	// 3. Proxied child segments: 64-byte PNG prefix stripped & Content-Type fixed to video/mp2t.
	var urls []string
	varRec := httptest.NewRecorder()
	mux.ServeHTTP(varRec, httptest.NewRequest(http.MethodGet, strings.Split(manifest, "\n")[len(strings.Split(manifest, "\n"))-1], nil))
	for _, ln := range strings.Split(varRec.Body.String(), "\n") {
		if strings.HasPrefix(ln, "/api/v1/streaming/proxy/") {
			urls = append(urls, ln)
		}
	}
	if len(urls) != 2 {
		t.Fatalf("expected 2 rewritten segment URLs, got %d: %s", len(urls), varRec.Body.String())
	}
	for idx, u := range urls {
		srec := httptest.NewRecorder()
		mux.ServeHTTP(srec, httptest.NewRequest(http.MethodGet, u, nil))
		body := srec.Body.Bytes()
		if ct := srec.Header().Get("Content-Type"); ct != "video/mp2t" {
			t.Fatalf("seg %d Content-Type = %q, want video/mp2t", idx, ct)
		}
		if len(body) != len(tsPayload) || body[0] != 0x47 || body[188] != 0x47 {
			t.Fatalf("seg %d expected clean MPEG-TS payload len=%d starting 0x47, got len=%d first=%x", idx, len(tsPayload), len(body), body[:min(8, len(body))])
		}
	}
}

func TestPreflightAggregatesProviderHealthPerSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok.mp4" {
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("0000ftypmp42"))
			return
		}
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	pm := NewProxyManager()
	pm.SetHTTPClient(srv.Client())
	health := NewProviderHealth()

	rawMixed := []UnifiedCandidate{
		{Kind: "http", Provider: "4khdhub", Title: "Dead1", StreamURL: srv.URL + "/bad1"},
		{Kind: "http", Provider: "4khdhub", Title: "Dead2", StreamURL: srv.URL + "/bad2"},
		{Kind: "http", Provider: "4khdhub", Title: "Dead3", StreamURL: srv.URL + "/bad3"},
		{Kind: "http", Provider: "4khdhub", Title: "Dead4", StreamURL: srv.URL + "/bad4"},
		{Kind: "http", Provider: "4khdhub", Title: "Good1", StreamURL: srv.URL + "/ok.mp4"},
	}
	kept := PreflightAndMeasureCandidates(context.Background(), pm, rawMixed, health)
	if len(kept) != 1 || kept[0].Title != "Good1" {
		t.Fatalf("expected 1 kept candidate Good1, got %+v", kept)
	}
	if health.ShouldSkip("4khdhub") {
		t.Fatal("expected mixed provider with a working mirror not to enter cooldown")
	}

	allDead := []UnifiedCandidate{
		{Kind: "http", Provider: "deadprov", Title: "DeadA", StreamURL: srv.URL + "/dead1"},
		{Kind: "http", Provider: "deadprov", Title: "DeadB", StreamURL: srv.URL + "/dead2"},
		{Kind: "http", Provider: "deadprov", Title: "DeadC", StreamURL: srv.URL + "/dead3"},
	}
	for i := 0; i < 3; i++ {
		PreflightAndMeasureCandidates(context.Background(), pm, allDead, health)
	}
	if !health.ShouldSkip("deadprov") {
		t.Fatal("expected provider failing all mirrors across 3 searches to enter cooldown")
	}
}
