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
		case "/page.html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, "<html><body>Not a video</body></html>")
		case "/bad.mp4":
			http.Error(w, "expired", http.StatusBadRequest)
		case "/movie.mkv":
			w.Header().Set("Content-Type", "video/x-matroska")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = io.WriteString(w, "MKV")
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

	// 4. Preflight & ServeProxy reject 200 text/html and 400 while allowing MKV and HLS.
	if _, _, err := pm.Preflight(context.Background(), srv.URL+"/page.html", headers); err == nil {
		t.Fatal("expected Preflight to reject text/html page")
	}
	if _, _, err := pm.Preflight(context.Background(), srv.URL+"/bad.mp4", headers); err == nil {
		t.Fatal("expected Preflight to reject HTTP 400 link")
	}
	if ct, isHLS, err := pm.Preflight(context.Background(), srv.URL+"/movie.mkv", headers); err != nil || isHLS || !strings.Contains(ct, "matroska") {
		t.Fatalf("expected Preflight MKV success, got ct=%q isHLS=%v err=%v", ct, isHLS, err)
	}
	if _, isHLS, err := pm.Preflight(context.Background(), srv.URL+"/master.m3u8", headers); err != nil || !isHLS {
		t.Fatalf("expected Preflight HLS detection, got isHLS=%v err=%v", isHLS, err)
	}
	tokHTML, _, err := pm.Register(srv.URL+"/page.html", headers, 1)
	if err != nil {
		t.Fatal(err)
	}
	pReq := httptest.NewRequest(http.MethodGet, "/api/v1/streaming/proxy/"+tokHTML, nil)
	pRec := httptest.NewRecorder()
	mux.ServeHTTP(pRec, pReq)
	if pRec.Code != http.StatusBadGateway {
		t.Fatalf("expected ServeProxy to reject text/html with 502, got %d", pRec.Code)
	}

	// 5. Direct play compatibility checks.
	if !looksIncompatibleForDirectPlay("Dune - Part Two (2024) 1080p UHD DV HDR 10bit HEVC DDP 7.1.mkv", "", "video/x-matroska") {
		t.Fatal("expected 10bit HEVC MKV DDP to require transcode")
	}
	if looksIncompatibleForDirectPlay("Movie.2024.1080p.WEB.mp4", "https://cdn.example.com/movie.mp4", "video/mp4") {
		t.Fatal("expected plain H.264 mp4 to allow direct play")
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

func TestNuvioExtendedCheerioCryptoAndRepoDefaults(t *testing.T) {
	ps := NewPluginStore(t.TempDir())
	if !ps.NodeAvailable() {
		t.Skip("node runtime unavailable")
	}

	// Verify cheerio-without-node-native, combinators (>), filter(), next(), [href*=], .html(),
	// WordArray AES encrypt/decrypt + HmacMD5, Buffer, and Headers.get().
	code := `
	const cheerio = require('cheerio-without-node-native');
	const CryptoJS = require('crypto-js');
	async function getStreams() {
		const doc = '<div class="box"><p class="label">Source</p><div class="links"><a href="https://drive.example.com/dl/1">Bad</a><a class="keep" href="https://cdn.example.com/driveleech/ok.mp4">Go</a></div></div>';
		const $ = cheerio.load(doc);
		const href = $('p.label').next('.links').find('> a[href*="driveleech"]').filter((i, el) => $(el).hasClass('keep')).attr('href');
		const inner = $('div.box').html();
		if (!inner || !inner.includes('<a class="keep"')) throw new Error('bad html(): ' + inner);

		const h = new Headers({ 'X-Nuvio-Token': 'abc123' });
		if (h.get('x-nuvio-token') !== 'abc123') throw new Error('headers.get failed');

		const key = CryptoJS.enc.Utf8.parse('0123456789abcdef0123456789abcdef');
		const iv = CryptoJS.enc.Utf8.parse('abcdef0123456789');
		const enc = CryptoJS.AES.encrypt(href, key, { iv, mode: CryptoJS.mode.CBC, padding: CryptoJS.pad.Pkcs7 });
		const dec = CryptoJS.AES.decrypt(enc.toString(), key, { iv, mode: CryptoJS.mode.CBC, padding: CryptoJS.pad.Pkcs7 }).toString(CryptoJS.enc.Utf8);
		const mac = CryptoJS.HmacMD5(Buffer.from(dec).toString('utf8'), CryptoJS.enc.Utf8.parse('secret')).toString(CryptoJS.enc.Hex);
		if (dec !== href || mac.length !== 32) throw new Error('crypto mismatch');

		return [{ name: 'ShimOK', title: mac, url: dec, quality: '1080p' }];
	}
	`
	cands, err := ps.runScraperCode(context.Background(), "shim", "ShimOK", code, "550", "movie", 0, 0, nil, "")
	if err != nil || len(cands) != 1 || cands[0].StreamURL != "https://cdn.example.com/driveleech/ok.mp4" {
		t.Fatalf("extended shim test failed: err=%v cands=%+v", err, cands)
	}

	// Verify AddRepoWithDefaults concurrently downloads, ignores duplicate providers/scrapers entries,
	// and applies defaultEnabled only on first install.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			_, _ = io.WriteString(w, `{
				"name": "Repo",
				"scrapers": [{"id":"one","name":"One","filename":"a.js","enabled":true},{"id":"two","name":"Two","filename":"b.js","enabled":true},{"id":"two","name":"Two Dup","filename":"b.js","enabled":true}],
				"providers": [{"id":"one","name":"One","filename":"a.js","enabled":true},{"id":"two","name":"Two","filename":"b.js","enabled":true}]
			}`)
		case "/a.js", "/b.js":
			_, _ = io.WriteString(w, `function getStreams(){ return []; }`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfgDir := t.TempDir()
	ps2 := NewPluginStore(cfgDir)
	ps2.SetHTTPClient(srv.Client())
	repo, err := ps2.AddRepoWithDefaults(context.Background(), srv.URL+"/manifest.json", []string{"two"})
	if err != nil {
		t.Fatalf("AddRepoWithDefaults failed: %v", err)
	}
	if len(repo.Scrapers) != 2 || !repo.Scrapers[0].Enabled || !repo.Scrapers[1].Enabled {
		t.Fatalf("expected all scrapers enabled by default on install, got: %+v", repo.Scrapers)
	}

	// Verify load() heals pre-existing duplicate scraper IDs persisted on disk.
	healDir := t.TempDir()
	dupJSON := `[{"id":"r1","name":"R1","manifest_url":"https://example.com/m.json","enabled":true,"scrapers":[{"id":"dup","name":"Dup1","enabled":true},{"id":"dup","name":"Dup2","enabled":false}]}]`
	if err := os.WriteFile(filepath.Join(healDir, "stream-plugins.json"), []byte(dupJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	ps3 := NewPluginStore(healDir)
	reloaded := ps3.List()
	if len(reloaded) != 1 || len(reloaded[0].Scrapers) != 1 || reloaded[0].Scrapers[0].ID != "dup" {
		t.Fatalf("expected load() to heal duplicate scrapers, got %+v", reloaded)
	}
	if rawAfter, err := os.ReadFile(filepath.Join(healDir, "stream-plugins.json")); err != nil || strings.Contains(string(rawAfter), "Dup2") {
		t.Fatalf("expected healed JSON written to disk, got err=%v body=%q", err, string(rawAfter))
	}
}

func TestFilterExcludeTermsTokenBoundaryAndInfoHashDedupe(t *testing.T) {
	prefs := DefaultPrefs()
	prefs.PreferredQuality = "1080p"

	cands := []UnifiedCandidate{
		{ID: "keep-kimetsu", Kind: "http", Provider: "Castle", Title: "Demon Slayer: Kimetsu no Yaiba Infinity Castle (2025) 1080p WEB-DL DTS-HD", StreamURL: "https://cdn.example/kimetsu.mp4"},
		{ID: "keep-pirates", Kind: "http", Provider: "P2", Title: "Pirates of the Caribbean 1080p DTS", StreamURL: "https://cdn.example/pirates.mp4"},
		{ID: "keep-camp", Kind: "http", Provider: "P3", Title: "Camp Rock 2008 1080p", StreamURL: "https://cdn.example/camp.mp4"},
		{ID: "keep-camera", Kind: "http", Provider: "P4", Title: "Camera Obscura 2017 1080p", StreamURL: "https://cdn.example/camera.mp4"},
		{ID: "drop-ts", Kind: "http", Provider: "B1", Title: "Movie 2025 TS 1080p", StreamURL: "https://cdn.example/ts.mp4"},
		{ID: "drop-hdts", Kind: "http", Provider: "B2", Title: "Movie 2025 HDTS 1080p", StreamURL: "https://cdn.example/hdts.mp4"},
		{ID: "drop-telesync", Kind: "http", Provider: "B3", Title: "Movie 2025 TELESYNC 1080p", StreamURL: "https://cdn.example/telesync.mp4"},
		{ID: "drop-camrip", Kind: "http", Provider: "B4", Title: "Movie 2025 CAMRip 1080p", StreamURL: "https://cdn.example/camrip.mp4"},
		{ID: "drop-hdcam", Kind: "http", Provider: "B5", Title: "Movie 2025 HDCAM 1080p", StreamURL: "https://cdn.example/hdcam.mp4"},
		{ID: "tor-low", Kind: "torrent", Provider: "A", ReleaseName: "Kimetsu no Yaiba 1080p", Quality: "1080p", Seeders: 12, InfoHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Link: "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{ID: "tor-high", Kind: "torrent", Provider: "B", ReleaseName: "Kimetsu no Yaiba 1080p", Quality: "1080p", Seeders: 250, InfoHash: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Link: "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{ID: "tor-debrid", Kind: "debrid_cached", Provider: "RD", ReleaseName: "Kimetsu no Yaiba 1080p", Quality: "1080p", Seeders: 250, InfoHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Link: "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}

	got := FilterAndRankCandidates(cands, prefs)
	kept := map[string]bool{}
	torCount := 0
	for _, c := range got {
		kept[c.ID] = true
		if c.Kind == "torrent" {
			torCount++
		}
	}
	for _, wantID := range []string{"keep-kimetsu", "keep-pirates", "keep-camp", "keep-camera", "tor-high", "tor-debrid"} {
		if !kept[wantID] {
			t.Fatalf("expected candidate %q to be kept; got %+v", wantID, kept)
		}
	}
	for _, dropID := range []string{"drop-ts", "drop-hdts", "drop-telesync", "drop-camrip", "drop-hdcam", "tor-low"} {
		if kept[dropID] {
			t.Fatalf("expected candidate %q to be dropped; got %+v", dropID, kept)
		}
	}
	if torCount != 1 {
		t.Fatalf("expected duplicate torrent infohash to dedupe to 1 torrent entry, got %d", torCount)
	}
}

func TestFastScraperPriorityAndMagnetOutput(t *testing.T) {
	if fastScraperPriority("castle") != 0 || fastScraperPriority("movix-vf") != 0 || fastScraperPriority("torrentio") != 0 {
		t.Fatalf("expected core reliable scrapers to have priority 0")
	}
	if fastScraperPriority("unknown-random-site") != 1 {
		t.Fatalf("expected unknown scraper to have priority 1")
	}

	ps := NewPluginStore(t.TempDir())
	if !ps.NodeAvailable() {
		t.Skip("node runtime unavailable")
	}
	code := `
	async function getStreams() {
		return [
			{ name: 'MagnetOne', title: 'Demon Slayer 1080p', url: 'magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Test', quality: '1080p', size: 1234567 }
		];
	}`
	cands, err := ps.runScraperCodeWithBudget(context.Background(), "mag", "MagnetOne", code, "1311031", "movie", 0, 0, nil, "", 5*time.Second)
	if err != nil || len(cands) != 1 {
		t.Fatalf("expected 1 magnet candidate, got err=%v cands=%+v", err, cands)
	}
	if cands[0].Kind != "torrent" || cands[0].Instant || cands[0].InfoHash != "0123456789abcdef0123456789abcdef01234567" || cands[0].Magnet == "" {
		t.Fatalf("unexpected magnet candidate fields: %+v", cands[0])
	}
}
