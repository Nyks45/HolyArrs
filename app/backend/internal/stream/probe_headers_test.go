package stream

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func buildSyntheticMP4Header(ver byte, timescale uint32, duration uint64) []byte {
	var buf []byte
	buf = append(buf, 0, 0, 0, 20)
	buf = append(buf, "ftypisom"...)
	buf = append(buf, 0, 0, 2, 0, 'i', 's', 'o', 'm')
	buf = append(buf, 0, 0, 0, 8, 'm', 'o', 'o', 'v')
	mvhd := make([]byte, 40)
	binary.BigEndian.PutUint32(mvhd[0:4], 40)
	copy(mvhd[4:8], "mvhd")
	mvhd[8] = ver
	if ver == 0 {
		binary.BigEndian.PutUint32(mvhd[20:24], timescale)
		binary.BigEndian.PutUint32(mvhd[24:28], uint32(duration))
	} else {
		binary.BigEndian.PutUint32(mvhd[28:32], timescale)
		binary.BigEndian.PutUint64(mvhd[32:40], duration)
	}
	return append(buf, mvhd...)
}

func buildSyntheticEBMLHeader(doctype string, scale uint32, durSec float64, float8 bool) []byte {
	var buf []byte
	buf = append(buf, 0x1a, 0x45, 0xdf, 0xa3, 0x8c)
	buf = append(buf, doctype...)
	// SeekHead reference with 0x53 0xac that must be skipped.
	buf = append(buf, 0x15, 0x49, 0xa9, 0x66, 0x53, 0xac, 0x81, 0x10)
	buf = append(buf, bytes.Repeat([]byte{0x00}, 64)...)
	// Real SegmentInfo element.
	buf = append(buf, 0x15, 0x49, 0xa9, 0x66, 0xa0)
	buf = append(buf, 0x2a, 0xd7, 0xb1, 0x83, byte(scale>>16), byte(scale>>8), byte(scale))
	buf = append(buf, 0x4d, 0x80, 0x84, 't', 'e', 's', 't')
	rawVal := durSec * 1e9 / float64(scale)
	if float8 {
		buf = append(buf, 0x44, 0x89, 0x88)
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], math.Float64bits(rawVal))
		buf = append(buf, b[:]...)
	} else {
		buf = append(buf, 0x44, 0x89, 0x84)
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], math.Float32bits(float32(rawVal)))
		buf = append(buf, b[:]...)
	}
	return buf
}

func TestParseHeadersAndHLSPlaylistDuration(t *testing.T) {
	if got := parseMP4Duration(buildSyntheticMP4Header(0, 1000, 22708)); math.Abs(got-22.708) > 0.01 {
		t.Fatalf("mp4 v0 duration = %v, want ~22.708", got)
	}
	if got := parseMP4Duration(buildSyntheticMP4Header(1, 90000, 90000*2561)); math.Abs(got-2561.0) > 0.1 {
		t.Fatalf("mp4 v1 duration = %v, want 2561", got)
	}

	ebmlMKV := buildSyntheticEBMLHeader("matroska", 1_000_000, 2561.525, true)
	if ct := detectDisguisedContainer("application/octet-stream", ebmlMKV); ct != "video/x-matroska" {
		t.Fatalf("detectDisguisedContainer MKV = %q", ct)
	}
	if got := parseEBMLDuration(ebmlMKV); math.Abs(got-2561.525) > 0.01 {
		t.Fatalf("ebml f64 duration = %v, want ~2561.525", got)
	}
	ebmlWebM := buildSyntheticEBMLHeader("webm", 1_000_000, 1800.0, false)
	if ct := detectDisguisedContainer("application/octet-stream", ebmlWebM); ct != "video/webm" {
		t.Fatalf("detectDisguisedContainer WebM = %q", ct)
	}
	if got := parseEBMLDuration(ebmlWebM); math.Abs(got-1800.0) > 0.5 {
		t.Fatalf("ebml f32 duration = %v, want ~1800", got)
	}

	// Simulate >64 KB HLS playlist (~72 KB, matching real long episode playlists).
	var pl strings.Builder
	pl.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n")
	for i := 0; i < 600; i++ {
		pl.WriteString("#EXTINF:4.25,\nhttps://cdn.example.com/very/long/segment/path/number/")
		pl.WriteString(strings.Repeat("x", 60))
		pl.WriteString(".ts\n")
	}
	pl.WriteString("#EXT-X-ENDLIST\n")
	if len(pl.String()) <= 65536 {
		t.Fatalf("test playlist expected > 64KB, got %d", len(pl.String()))
	}
	if got := parseHLSPlaylistDuration(pl.String(), false); math.Abs(got-2550.0) > 0.1 {
		t.Fatalf("hls playlist duration = %v, want 2550", got)
	}
	if got := parseHLSPlaylistDuration(pl.String(), true); got != 0 {
		t.Fatalf("truncated playlist duration should be 0, got %v", got)
	}
}

func TestProxyManagerLargeDisguisedTSAndNestedExtM3UChild(t *testing.T) {
	tsPayload := makeFakeTSSlice()
	pngHead := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	pad := append(pngHead, bytes.Repeat([]byte{0x00, 0x11, 0x22, 0x33}, 1022)...)
	largePNGWrapped := append(pad, tsPayload...) // 4096-byte fake PNG header without 0x47 sync collisions

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=2000000\nnested_list.bin\n")
		case "/nested_list.bin":
			// Non-.m3u8 extension and generic content-type containing #EXTM3U.
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = io.WriteString(w, "#EXTM3U\n#EXTINF:5.0,\nseg0.png\n#EXTINF:5.0,\nseg1.png\n#EXT-X-ENDLIST\n")
		case "/seg0.png", "/seg1.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(largePNGWrapped)
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

	pr, err := pm.PreflightDetailed(context.Background(), srv.URL+"/master.m3u8", nil)
	if err != nil || !pr.IsHLS {
		t.Fatalf("expected valid chain, got pr=%+v err=%v", pr, err)
	}
	tok, _, err := pm.Register(srv.URL+"/master.m3u8", nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/streaming/proxy/"+tok, nil))
	if !strings.Contains(rec.Body.String(), "/hls?u=") {
		t.Fatalf("expected rewritten master playlist, got:\n%s", rec.Body.String())
	}

	// Extract proxied child playlist URL and fetch it without .m3u8 extension.
	lines := strings.Split(rec.Body.String(), "\n")
	childPath := ""
	for _, ln := range lines {
		if strings.Contains(ln, "/hls?u=") {
			childPath = strings.TrimSpace(ln)
			break
		}
	}
	childRec := httptest.NewRecorder()
	mux.ServeHTTP(childRec, httptest.NewRequest(http.MethodGet, childPath, nil))
	if !strings.Contains(childRec.Body.String(), "/hls?u=") || childRec.Header().Get("Content-Type") != "application/vnd.apple.mpegurl" {
		t.Fatalf("expected nested non-.m3u8 #EXTM3U child rewritten, ct=%q body:\n%s", childRec.Header().Get("Content-Type"), childRec.Body.String())
	}

	segPath := ""
	for _, ln := range strings.Split(childRec.Body.String(), "\n") {
		if strings.Contains(ln, "/hls?u=") {
			segPath = strings.TrimSpace(ln)
			break
		}
	}
	segRec := httptest.NewRecorder()
	mux.ServeHTTP(segRec, httptest.NewRequest(http.MethodGet, segPath, nil))
	if segRec.Header().Get("Content-Type") != "video/mp2t" || !bytes.Equal(segRec.Body.Bytes(), tsPayload) {
		t.Fatalf("expected 4096-byte PNG prefix stripped from disguised TS, ct=%q len=%d want=%d", segRec.Header().Get("Content-Type"), segRec.Body.Len(), len(tsPayload))
	}
}

func TestFilterAndRankCandidatesWithRuntimeAndMovieReferer(t *testing.T) {
	mcShow := NewSearchMatchContext("show", "The Rainmaker", "", 2025, 2, 6, nil, "en", []string{"US"}, false)
	movieBoxWrongShow := UnifiedCandidate{
		Kind:         "http",
		Provider:     "MovieBox",
		ProviderType: "nuvio",
		ReleaseName:  "The Rainmaker S02E06",
		StreamURL:    "https://cdn.example.com/clip.mp4",
		Headers: map[string]string{
			"Referer": "https://movie-box.co/movies/detail/the-rainmaker?id=123&type=%2Fmovie%2Fdetail&detailSe=&detailEp=",
		},
	}
	if VerifyCandidate(movieBoxWrongShow, mcShow) {
		t.Fatal("expected VerifyCandidate to reject MovieBox movie-detail Referer on TV episode search")
	}
	movieBoxValidShow := movieBoxWrongShow
	movieBoxValidShow.Headers = map[string]string{
		"Referer": "https://movie-box.co/movies/detail/the-rainmaker?id=123&detailSe=2&detailEp=6",
	}
	if !VerifyCandidate(movieBoxValidShow, mcShow) {
		t.Fatal("expected VerifyCandidate to allow MovieBox Referer when detailSe/detailEp match")
	}

	cands := []UnifiedCandidate{
		{ID: "netmirror-fake", Kind: "hls", Provider: "NetMirror", Quality: "1080p", FastStart: true, Verified: true, ProbeMS: 200, DurationSec: 599.7},
		{ID: "vegamovies-movie", Kind: "http", Provider: "VegaMovies", Quality: "1080p", FastStart: true, Verified: true, ProbeMS: 300, DurationSec: 8137},
		{ID: "moviebox-short", Kind: "http", Provider: "MovieBox", Quality: "1080p", FastStart: true, Verified: true, ProbeMS: 20, DurationSec: 22.7},
		{ID: "torrent-ep", Kind: "torrent", Provider: "Torrentio", Quality: "1080p", Seeders: 80},
		{ID: "warn-ep", Kind: "hls", Provider: "WarnSrc", Quality: "1080p", FastStart: true, Verified: true, ProbeMS: 120, DurationSec: 1800},
		{ID: "vidrock-ep", Kind: "hls", Provider: "VidRock", Quality: "480p", FastStart: true, Verified: true, ProbeMS: 450, DurationSec: 2561.5},
		{ID: "dahmer-ep", Kind: "http", Provider: "Dahmermovies-TV", Quality: "1080p", FastStart: false, Verified: true, ProbeMS: 1100, DurationSec: 2561.5},
	}
	ranked := FilterAndRankCandidatesWithRuntime(cands, StreamPrefs{PreferredQuality: "1080p"}, "show", 43*60)
	order := make([]string, len(ranked))
	for i, c := range ranked {
		order[i] = c.ID
	}
	// Matching-duration instant sources come first, then warn instant, then valid torrent, then severe mismatched sources.
	wantPrefix := []string{"dahmer-ep", "vidrock-ep", "warn-ep", "torrent-ep"}
	for i, id := range wantPrefix {
		if order[i] != id {
			t.Fatalf("show rank[%d] = %q, full order = %v (ranked=%+v)", i, order[i], order, ranked)
		}
	}
	if ranked[0].DurationMismatch != "" || ranked[2].DurationMismatch != "warn" || ranked[4].DurationMismatch != "severe" {
		t.Fatalf("unexpected mismatch labels: %+v", ranked)
	}

	// Re-ranking cached candidates without runtime clears DurationMismatch cleanly.
	noRuntime := FilterAndRankCandidatesWithRuntime(ranked, StreamPrefs{PreferredQuality: "1080p"}, "", 0)
	for _, c := range noRuntime {
		if c.DurationMismatch != "" {
			t.Fatalf("expected DurationMismatch cleared when expectedSec=0, got %+v", c)
		}
	}

	// Movie threshold test (166 min movie): 165m ok, 140m (26m diff) warn, 10m severe.
	mCands := []UnifiedCandidate{
		{ID: "m-fake", Kind: "hls", Quality: "1080p", FastStart: true, Verified: true, ProbeMS: 100, DurationSec: 600},
		{ID: "m-tor", Kind: "torrent", Quality: "1080p", Seeders: 120},
		{ID: "m-warn", Kind: "hls", Quality: "1080p", FastStart: true, Verified: true, ProbeMS: 150, DurationSec: 140 * 60},
		{ID: "m-good", Kind: "hls", Quality: "720p", FastStart: true, Verified: true, ProbeMS: 250, DurationSec: 165 * 60},
	}
	mRanked := FilterAndRankCandidatesWithRuntime(mCands, StreamPrefs{PreferredQuality: "1080p"}, "movie", 166*60)
	mOrder := []string{mRanked[0].ID, mRanked[1].ID, mRanked[2].ID, mRanked[3].ID}
	if strings.Join(mOrder, ",") != "m-good,m-warn,m-tor,m-fake" {
		t.Fatalf("movie rank order = %v, want [m-good m-warn m-tor m-fake]", mOrder)
	}
}
