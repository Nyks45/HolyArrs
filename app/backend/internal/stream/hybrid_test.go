package stream

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/secrets"
	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"
)

func openTestVault(t *testing.T) (*database.Store, *secrets.Vault) {
	t.Helper()
	dir := t.TempDir()
	st, err := database.Open(dir, database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	key, err := secrets.LoadKey(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	v, err := secrets.Open(context.Background(), st, key)
	if err != nil {
		t.Fatal(err)
	}
	return st, v
}

func TestProxyManagerHeaderInjectionAndHLSRewrite(t *testing.T) {
	var sawSegRef atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "https://player.example.com/" {
			t.Errorf("Referer = %q, want https://player.example.com/", r.Header.Get("Referer"))
		}
		if r.Header.Get("Origin") != "https://player.example.com" {
			t.Errorf("Origin = %q, want https://player.example.com", r.Header.Get("Origin"))
		}
		switch r.URL.Path {
		case "/video.bin":
			if r.Header.Get("Range") == "bytes=0-4" {
				w.Header().Set("Content-Type", "video/mp4")
				w.Header().Set("Content-Range", "bytes 0-4/10")
				w.WriteHeader(http.StatusPartialContent)
				_, _ = io.WriteString(w, "HELLO")
				return
			}
			_, _ = io.WriteString(w, "HELLOWORLD")
		case "/master.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(w, strings.Join([]string{
				"#EXTM3U",
				`#EXT-X-KEY:METHOD=AES-128,URI="key.bin"`,
				`#EXT-X-MAP:URI="init.mp4"`,
				"seg0.ts",
			}, "\n"))
		case "/seg0.ts":
			sawSegRef.Add(1)
			w.Header().Set("Content-Type", "video/mp2t")
			_, _ = io.WriteString(w, "TSDATA")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	pm := NewProxyManager()
	pm.SetHTTPClient(srv.Client())
	headers := map[string]string{
		"Referer": "https://player.example.com/",
		"Origin":  "https://player.example.com",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/streaming/proxy/{token}", pm.ServeProxy)
	mux.HandleFunc("GET /api/v1/streaming/proxy/{token}/hls", pm.ServeProxyHLS)

	// 1. Direct Range proxy + headers.
	tokVid, _, err := pm.Register(srv.URL+"/video.bin", headers, 1)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/streaming/proxy/"+tokVid, nil)
	req.Header.Set("Range", "bytes=0-4")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "HELLO" {
		t.Fatalf("proxy range got %d %q", rec.Code, rec.Body.String())
	}

	// 2. HLS manifest rewrite + nested segment header injection.
	tokHLS, _, err := pm.Register(srv.URL+"/master.m3u8", headers, 1)
	if err != nil {
		t.Fatal(err)
	}
	hReq := httptest.NewRequest(http.MethodGet, "/api/v1/streaming/proxy/"+tokHLS, nil)
	hRec := httptest.NewRecorder()
	mux.ServeHTTP(hRec, hReq)
	if hRec.Code != http.StatusOK {
		t.Fatalf("hls manifest code = %d", hRec.Code)
	}
	body := hRec.Body.String()
	if !strings.Contains(body, "/api/v1/streaming/proxy/"+tokHLS+"/hls?u=") {
		t.Fatalf("rewritten playlist missing proxy /hls URL:\n%s", body)
	}
	lines := strings.Split(strings.TrimSpace(body), "\n")
	segLine := lines[len(lines)-1]
	segReq := httptest.NewRequest(http.MethodGet, segLine, nil)
	segRec := httptest.NewRecorder()
	mux.ServeHTTP(segRec, segReq)
	if segRec.Code != http.StatusOK || segRec.Body.String() != "TSDATA" || sawSegRef.Load() == 0 {
		t.Fatalf("nested segment proxy failed: code=%d body=%q ref=%d", segRec.Code, segRec.Body.String(), sawSegRef.Load())
	}

	// 3. Default SSRF-guarded proxy rejects cloud metadata IP.
	guarded := NewProxyManager()
	tokSSRF, _, err := guarded.Register("http://169.254.169.254/latest/meta-data/", nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	ssrfMux := http.NewServeMux()
	ssrfMux.HandleFunc("GET /api/v1/streaming/proxy/{token}", guarded.ServeProxy)
	sReq := httptest.NewRequest(http.MethodGet, "/api/v1/streaming/proxy/"+tokSSRF, nil)
	sRec := httptest.NewRecorder()
	ssrfMux.ServeHTTP(sRec, sReq)
	if sRec.Code == http.StatusOK {
		t.Fatalf("expected SSRF metadata fetch to fail, got 200")
	}
}

func TestStremioAddonStore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/manifest.json":
			_, _ = io.WriteString(w, `{
				"id": "org.test.addon",
				"name": "Test Stremio",
				"version": "1.0.0",
				"logo": "https://cdn.example.com/logo.png",
				"resources": ["stream", "subtitles", "catalog"],
				"types": ["movie"],
				"idPrefixes": ["tt"],
				"catalogs": [{"type": "movie", "id": "top", "name": "Addon Top"}]
			}`)
		case "/stream/movie/tt1234567.json":
			_, _ = io.WriteString(w, `{
				"streams": [
					{
						"name": "Direct",
						"title": "Test.Movie.2025.1080p.WEB.mp4\n2.4 GB",
						"url": "https://stream.example.com/movie.mp4",
						"behaviorHints": {"proxyHeaders": {"request": {"Referer": "https://example.com"}}}
					},
					{
						"name": "Torrent",
						"title": "Test.Movie.2025.2160p.HEVC.mkv\n👤 45",
						"infoHash": "0123456789abcdef0123456789abcdef01234567",
						"fileIdx": 0
					}
				]
			}`)
		case "/subtitles/movie/tt1234567.json":
			_, _ = io.WriteString(w, `{"subtitles":[{"id":"s1","url":"`+"http://"+r.Host+`/sub.srt","lang":"eng"}]}`)
		case "/sub.srt":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, "1\n00:00:01,000 --> 00:00:03,000\nHello Addon\n")
		case "/catalog/movie/top.json":
			_, _ = io.WriteString(w, `{"metas":[{"id":"tmdb:550","type":"movie","name":"Fight Club","poster":"https://img.example.com/p.jpg","releaseInfo":"1999"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	store := NewAddonStore(t.TempDir())
	store.SetHTTPClient(srv.Client())

	addon, err := store.Add(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(addon.Logo, "/api/v1/streaming/proxy/poster?url=") {
		t.Fatalf("expected proxied logo URL, got %q", addon.Logo)
	}

	cands, warns := store.SearchStreams(context.Background(), "movie", "tt1234567", "550", 0, 0)
	if len(warns) != 0 || len(cands) != 2 {
		t.Fatalf("got %d candidates, warns=%v", len(cands), warns)
	}
	if !cands[0].Instant || cands[0].Headers["Referer"] != "https://example.com" {
		t.Fatalf("unexpected HTTP stream candidate: %+v", cands[0])
	}
	if cands[1].InfoHash != "0123456789abcdef0123456789abcdef01234567" || cands[1].Seeders != 45 {
		t.Fatalf("unexpected torrent candidate: %+v", cands[1])
	}

	subs := store.SearchSubtitles(context.Background(), "movie", "tt1234567", "550", 0, 0)
	if len(subs) != 1 || !strings.HasPrefix(subs[0].ID, "stremio:") {
		t.Fatalf("unexpected subtitle candidates: %+v", subs)
	}
	rawSub, _, lang, err := store.FetchSubtitle(context.Background(), subs[0].ID)
	if err != nil || lang != "eng" || !strings.Contains(string(rawSub), "Hello Addon") {
		t.Fatalf("FetchSubtitle failed: err=%v lang=%q", err, lang)
	}

	shelves := store.BrowseCatalogs(context.Background(), "movie")
	if len(shelves) != 1 || len(shelves[0].Items) != 1 || shelves[0].Items[0].TMDBID != 550 {
		t.Fatalf("unexpected catalog shelves: %+v", shelves)
	}
}

func TestNuvioPluginNodeWorkerAndSSRF(t *testing.T) {
	ps := NewPluginStore(t.TempDir())
	if !ps.NodeAvailable() {
		t.Skip("node runtime unavailable")
	}

	scraperCode := `
	const cheerio = require('cheerio');
	const CryptoJS = require('crypto-js');
	async function getStreams(tmdbId, mediaType, season, episode) {
		const $ = cheerio.load('<div id="wrap"><a class="src" data-url="https://cdn.example.com/hls/master.m3u8">Play</a></div>');
		const u = $('div#wrap a.src').attr('data-url');
		const sig = CryptoJS.MD5(tmdbId).toString();
		return [{
			name: 'NuvioMock',
			title: 'Mock 1080p ' + sig.slice(0, 6),
			url: u,
			quality: '1080p',
			headers: { Referer: 'https://embed.example.com/' }
		}];
	}
	`
	cands, err := ps.runScraperCode(context.Background(), "mock", "NuvioMock", scraperCode, "550", "movie", 0, 0, nil, "")
	if err != nil || len(cands) != 1 {
		t.Fatalf("runScraperCode failed: err=%v cands=%+v", err, cands)
	}
	if cands[0].Kind != "hls" || !cands[0].Instant || cands[0].Headers["Referer"] != "https://embed.example.com/" {
		t.Fatalf("unexpected nuvio candidate: %+v", cands[0])
	}

	ssrfCode := `
	async function getStreams() {
		const r = await fetch('http://127.0.0.1:1/secret');
		return [{ name: 'Bad', url: 'https://example.com/' + await r.text() }];
	}
	`
	badCands, badErr := ps.runScraperCode(context.Background(), "bad", "Bad", ssrfCode, "550", "movie", 0, 0, nil, "")
	if badErr == nil || len(badCands) != 0 {
		t.Fatalf("expected SSRF fetch error inside Node sandbox, got err=%v cands=%+v", badErr, badCands)
	}
}

func TestLocalLibraryPlaybackAndRange(t *testing.T) {
	dir := t.TempDir()
	vidPath := filepath.Join(dir, "Test.Movie.2024.1080p.mp4")
	if err := os.WriteFile(vidPath, []byte("0123456789ABCDEF"), 0o640); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(nil, nil, t.TempDir(), nil)
	defer mgr.Close()

	st, err := mgr.StartLocal(context.Background(), 1, mediafiles.LocalStreamFile{
		FileID:   99,
		Kind:     "movie",
		Name:     "Test Movie",
		RelPath:  "Test.Movie.2024.1080p.mp4",
		RootPath: dir,
		FullPath: vidPath,
		Size:     16,
		Quality:  "1080p",
	}, StartReq{Title: "Test Movie", Kind: "movie"})
	if err != nil {
		t.Fatal(err)
	}
	if st.Transport != "local" || st.State != "ready" || st.DirectURL == "" {
		t.Fatalf("unexpected local session status: %+v", st)
	}

	mux := http.NewServeMux()
	(&Handlers{Mgr: mgr}).Routes(mux)
	req := httptest.NewRequest(http.MethodGet, st.MediaURL, nil)
	req.Header.Set("Range", "bytes=4-9")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "456789" {
		t.Fatalf("local ServeMedia range = %d %q", rec.Code, rec.Body.String())
	}
}

func TestDebridMockFlows(t *testing.T) {
	_, vault := openTestVault(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/api/user/me":
			_, _ = io.WriteString(w, `{"success":true,"data":{"email":"tb@example.com"}}`)
		case "/v1/api/torrents/checkcached":
			_, _ = io.WriteString(w, `{"success":true,"data":{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa":{"name":"hit"}}}`)
		case "/v1/api/torrents/createtorrent":
			_, _ = io.WriteString(w, `{"success":true,"data":{"torrent_id":42}}`)
		case "/v1/api/torrents/mylist":
			_, _ = io.WriteString(w, `{"success":true,"data":{"files":[{"id":7,"name":"Movie.1080p.mkv","size":5000000}]}}`)
		case "/v1/api/torrents/requestdl":
			_, _ = io.WriteString(w, `{"success":true,"data":"https://dl.torbox.example/movie.mkv"}`)
		case "/api/account/info":
			_, _ = io.WriteString(w, `{"status":"success","customer_id":"pm99"}`)
		case "/api/cache/check":
			_, _ = io.WriteString(w, `{"status":"success","response":[false,true]}`)
		case "/api/transfer/directdl":
			_, _ = io.WriteString(w, `{"status":"success","content":[{"path":"/Show.mkv","link":"https://dl.pm.example/show.mkv","size":1000}]}`)
		case "/user":
			_, _ = io.WriteString(w, `{"username":"rduser"}`)
		case "/torrents/addMagnet":
			_, _ = io.WriteString(w, `{"id":"rd1"}`)
		case "/torrents/info/rd1":
			_, _ = io.WriteString(w, `{"status":"downloaded","links":["https://rd.example/l/1"],"files":[{"id":1,"path":"/Movie.mkv","bytes":2000}]}`)
		case "/torrents/selectFiles/rd1":
			w.WriteHeader(http.StatusNoContent)
		case "/unrestrict/link":
			_, _ = io.WriteString(w, `{"download":"https://dl.rd.example/unrestricted.mkv","filename":"Movie.mkv"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ds := NewDebridService(vault)
	ds.SetTestEndpoints(srv.Client(), srv.URL, srv.URL, srv.URL)
	ctx := context.Background()

	for _, p := range []string{"torbox", "premiumize", "realdebrid"} {
		st, err := ds.SaveKey(ctx, p, "secret-key-"+p, database.Actor{Kind: "system"})
		if err != nil || !st.Configured {
			t.Fatalf("SaveKey(%s) failed: err=%v st=%+v", p, err, st)
		}
	}

	h1 := strings.Repeat("a", 40)
	h2 := strings.Repeat("b", 40)
	cached := ds.CheckCached(ctx, []string{h1, h2})
	if cached[h1] != "torbox" || cached[h2] != "premiumize" {
		t.Fatalf("unexpected cache map: %+v", cached)
	}

	for _, tc := range []struct{ prov, want string }{
		{"torbox", "https://dl.torbox.example/movie.mkv"},
		{"premiumize", "https://dl.pm.example/show.mkv"},
		{"realdebrid", "https://dl.rd.example/unrestricted.mkv"},
	} {
		u, _, err := ds.ResolveMagnet(ctx, tc.prov, "magnet:?xt=urn:btih:"+h1, h1, -1)
		if err != nil || u != tc.want {
			t.Fatalf("ResolveMagnet(%s) = (%q, %v), want %q", tc.prov, u, err, tc.want)
		}
	}
}

func TestSkipServiceMergeAndScrobble(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/segments":
			_, _ = io.WriteString(w, `{"intro":{"start_sec":12,"end_sec":92}}`)
		case r.URL.Path == "/api/v2/ids":
			_, _ = io.WriteString(w, `{"myanimelist":16498}`)
		case strings.HasPrefix(r.URL.Path, "/v2/skip-times/"):
			_, _ = io.WriteString(w, `{"found":true,"results":[{"skipType":"ed","interval":{"startTime":1320,"endTime":1410}}]}`)
		case strings.HasPrefix(r.URL.Path, "/oauth/pin") && r.URL.Path == "/oauth/pin":
			_, _ = io.WriteString(w, `{"user_code":"ABCDE","verification_url":"https://simkl.com/pin","expires_in":300,"interval":5}`)
		case strings.HasPrefix(r.URL.Path, "/oauth/pin/ABCDE"):
			_, _ = io.WriteString(w, `{"result":"OK","access_token":"simkl-tok"}`)
		case r.URL.Path == "/users/settings":
			_, _ = io.WriteString(w, `{"user":{"name":"hakan"}}`)
		case strings.HasPrefix(r.URL.Path, "/scrobble/"):
			_, _ = io.WriteString(w, `{"action":"ok"}`)
		case r.URL.Path == "/sync/playback":
			_, _ = io.WriteString(w, `[
				{"type":"movie","progress":55.0,"paused_at":"2020-01-01T00:00:00Z","movie":{"title":"Old Local Wins","ids":{"tmdb":"101"}}},
				{"type":"movie","progress":70.0,"paused_at":"2030-01-01T00:00:00Z","movie":{"title":"Remote Wins","ids":{"tmdb":"202"}}}
			]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	skipSvc := NewSkipService()
	skipSvc.SetTestEndpoints(srv.Client(), srv.URL, srv.URL, srv.URL, srv.URL)
	segs := skipSvc.GetSegments(context.Background(), "show", "tt2560140", "1429", 1, 1, true, "Attack on Titan", "", []ChapterMark{
		{Start: 5, End: 80, Title: "Opening Theme", Kind: "intro"},
		{Start: 1410, End: 1440, Title: "Credits", Kind: "credits"},
	})
	byKind := map[string]SkipSegment{}
	for _, s := range segs {
		byKind[s.Kind] = s
	}
	if byKind["intro"].Source != "introdb" || byKind["outro"].Source != "aniskip" || byKind["credits"].Source != "chapters" {
		t.Fatalf("unexpected merged skip segments: %+v", byKind)
	}

	_, vault := openTestVault(t)
	prefs := NewPrefsStore(t.TempDir())
	hist := NewHistoryStore(t.TempDir())
	_, _ = hist.Upsert(1, WatchEntry{
		Key:         WatchKey("movie", "101", 0, 0),
		Kind:        "movie",
		TMDB:        "101",
		Title:       "Local",
		ProgressPct: 88,
		UpdatedAt:   time.Now().UTC(),
	})

	scrob := NewScrobbleService(vault, nil, prefs)
	scrob.SetTestEndpoints(srv.Client(), srv.URL)
	ctx := context.Background()
	ch, err := scrob.StartSimklPIN(ctx, "cid1", database.System)
	if err != nil || ch.UserCode != "ABCDE" {
		t.Fatalf("StartSimklPIN failed: %+v %v", ch, err)
	}
	ok, uname, err := scrob.PollSimklPIN(ctx, "ABCDE", database.System)
	if err != nil || !ok || uname != "hakan" {
		t.Fatalf("PollSimklPIN failed: ok=%v u=%q err=%v", ok, uname, err)
	}
	sent := scrob.Scrobble(ctx, "start", "movie", 202, "", 0, 0, 12.5)
	if len(sent) != 1 || sent[0] != "simkl" {
		t.Fatalf("unexpected scrobble targets: %v", sent)
	}
	merged, err := scrob.PullProgress(ctx, hist, 1)
	if err != nil || merged != 1 {
		t.Fatalf("PullProgress merged=%d err=%v (want 1)", merged, err)
	}
	got101, _ := hist.ForTitle(1, "movie", "101")
	if got101 == nil || got101.ProgressPct != 88 {
		t.Fatalf("expected newer local entry 101 to remain 88, got %+v", got101)
	}
	got202, _ := hist.ForTitle(1, "movie", "202")
	if got202 == nil || got202.ProgressPct != 70 {
		t.Fatalf("expected synced remote entry 202 to have progress 70, got %+v", got202)
	}
	var unused json.RawMessage
	_ = unused
}
