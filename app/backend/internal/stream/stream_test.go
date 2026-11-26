package stream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestTokenRejection(t *testing.T) {
	mgr := NewManager(nil, nil, t.TempDir(), nil)
	defer mgr.Close()

	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	sessDir := filepath.Join(t.TempDir(), "s1")
	if err := os.MkdirAll(sessDir, 0o750); err != nil {
		t.Fatal(err)
	}
	mgr.mu.Lock()
	mgr.sessions["s1"] = &Session{
		id:        "s1",
		token:     "valid-secret-token",
		dir:       sessDir,
		ctx:       context.Background(),
		cancel:    cancel,
		createdAt: time.Now(),
	}
	mgr.mu.Unlock()

	mux := http.NewServeMux()
	(&Handlers{Mgr: mgr}).Routes(mux)

	paths := []string{
		"/api/v1/stream/s1/media/wrong-token",
		"/api/v1/stream/unknown/media/valid-secret-token",
		"/api/v1/stream/s1/hls/wrong-token/1/index.m3u8",
		"/api/v1/stream/s1/sub/wrong-token/0",
	}
	for _, p := range paths {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, rec.Code)
		}
	}

	// Empty token directly into handler must also return 404.
	for _, fn := range []http.HandlerFunc{mgr.ServeMedia, mgr.ServeHLSFile, mgr.ServeSubtitleVTT} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/stream/s1/media/", nil)
		req.SetPathValue("id", "s1")
		req.SetPathValue("token", "")
		req.SetPathValue("gen", "1")
		req.SetPathValue("file", "index.m3u8")
		req.SetPathValue("idx", "0")
		rec := httptest.NewRecorder()
		fn(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("empty token handler status = %d, want 404", rec.Code)
		}
	}
}

func TestExtractEpisodeNumsAndPick(t *testing.T) {
	epCases := []struct {
		name string
		s, e int
		ok   bool
	}{
		{"Show.S01E05.1080p.WEB-DL.mkv", 1, 5, true},
		{"show.s2e12.720p.mp4", 2, 12, true},
		{"Another.Show.1x04.HDTV.mkv", 1, 4, true},
		{"Random.Movie.2024.1080p.BluRay.mkv", 0, 0, false},
	}
	for _, tc := range epCases {
		s, e, ok := extractEpisodeNums(tc.name)
		if s != tc.s || e != tc.e || ok != tc.ok {
			t.Errorf("extractEpisodeNums(%q) = (%d,%d,%v), want (%d,%d,%v)", tc.name, s, e, ok, tc.s, tc.e, tc.ok)
		}
	}

	directCases := []struct {
		name       string
		ext, fmt   string
		vcodec     string
		pixFmt     string
		audio      []AudioTrack
		wantDirect bool
	}{
		{
			name:       "mp4 h264 yuv420p stereo aac",
			ext:        ".mp4",
			fmt:        "mov,mp4,m4a,3gp,3g2,mj2",
			vcodec:     "h264",
			pixFmt:     "yuv420p",
			audio:      []AudioTrack{{Codec: "aac", Channels: 2}},
			wantDirect: true,
		},
		{
			name:       "mkv rejected",
			ext:        ".mkv",
			fmt:        "matroska,webm",
			vcodec:     "h264",
			pixFmt:     "yuv420p",
			audio:      []AudioTrack{{Codec: "aac", Channels: 2}},
			wantDirect: false,
		},
		{
			name:       "matroska without ext rejected for h264",
			ext:        "",
			fmt:        "matroska,webm",
			vcodec:     "h264",
			pixFmt:     "yuv420p",
			audio:      []AudioTrack{{Codec: "aac", Channels: 2}},
			wantDirect: false,
		},
		{
			name:       "hevc rejected",
			ext:        ".mp4",
			fmt:        "mp4",
			vcodec:     "hevc",
			pixFmt:     "yuv420p",
			audio:      []AudioTrack{{Codec: "aac", Channels: 2}},
			wantDirect: false,
		},
		{
			name:       "6ch audio rejected",
			ext:        ".mp4",
			fmt:        "mp4",
			vcodec:     "h264",
			pixFmt:     "yuv420p",
			audio:      []AudioTrack{{Codec: "aac", Channels: 6}},
			wantDirect: false,
		},
		{
			name:       "10-bit pixfmt rejected",
			ext:        ".mp4",
			fmt:        "mp4",
			vcodec:     "h264",
			pixFmt:     "yuv420p10le",
			audio:      []AudioTrack{{Codec: "aac", Channels: 2}},
			wantDirect: false,
		},
	}
	for _, tc := range directCases {
		got := isBrowserDirectPlayable(tc.ext, tc.fmt, tc.vcodec, tc.pixFmt, tc.audio)
		if got != tc.wantDirect {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.wantDirect)
		}
	}
}

func TestBuildFFmpegArgs(t *testing.T) {
	probeH264 := &MediaProbe{
		VideoCodec: "h264",
		PixFmt:     "yuv420p",
		AudioTracks: []AudioTrack{
			{Index: 0, Codec: "aac", Channels: 2},
			{Index: 1, Codec: "ac3", Channels: 6},
		},
	}
	argsCopy := buildFFmpegArgs("http://127.0.0.1/media", "/tmp/hls", probeH264, "original", 0, 0)
	if !hasPair(argsCopy, "-c:v", "copy") {
		t.Fatalf("expected -c:v copy in %v", argsCopy)
	}
	if slices.Contains(argsCopy, "-ss") {
		t.Fatalf("did not expect -ss for startSec=0: %v", argsCopy)
	}

	args720 := buildFFmpegArgs("http://127.0.0.1/media", "/tmp/hls", probeH264, "720p", 1, 125.5)
	if !hasPair(args720, "-c:v", "libx264") {
		t.Fatalf("expected -c:v libx264 for 720p: %v", args720)
	}
	if !hasPair(args720, "-vf", "scale=-2:'min(720,ih)'") {
		t.Fatalf("expected 720p scale filter: %v", args720)
	}
	if !hasPair(args720, "-ss", "125.500") {
		t.Fatalf("expected -ss 125.500: %v", args720)
	}
	if !hasPair(args720, "-map", "0:a:1?") {
		t.Fatalf("expected audio track 1 map: %v", args720)
	}

	probeHEVC := &MediaProbe{VideoCodec: "hevc", PixFmt: "yuv420p10le"}
	argsHEVC := buildFFmpegArgs("http://127.0.0.1/media", "/tmp/hls", probeHEVC, "original", 0, 0)
	if !hasPair(argsHEVC, "-c:v", "libx264") {
		t.Fatalf("expected libx264 transcode for hevc original: %v", argsHEVC)
	}
}

func TestBuildMPVArgs(t *testing.T) {
	args := buildMPVArgs(
		"http://127.0.0.1:6262/api/v1/stream/s1/media/tok",
		"Dune (2021)",
		MPVOpts{
			AudioLang: "eng,en",
			SubLang:   "tur,tr",
			StartSec:  1234.5,
			SubFile:   "/tmp/sub.vtt",
		},
		"/tmp/mpv.sock",
	)
	for _, want := range []string{
		"--cache=yes",
		"--alang=eng,en",
		"--slang=tur,tr",
		"--start=1234.5",
		"--sub-file=/tmp/sub.vtt",
		"--input-ipc-server=/tmp/mpv.sock",
		"--force-media-title=Dune (2021)",
	} {
		if !slices.Contains(args, want) {
			t.Errorf("missing %q in mpv args %v", want, args)
		}
	}
	if args[len(args)-1] != "http://127.0.0.1:6262/api/v1/stream/s1/media/tok" {
		t.Errorf("last arg = %q, want media URL", args[len(args)-1])
	}
}

func TestHistoryStorePersistenceAndContinueWatching(t *testing.T) {
	cfgDir := t.TempDir()
	hs := NewHistoryStore(cfgDir)

	// Movie in progress: should retain metadata across periodic progress ticks and appear in continue_watching.
	if _, err := hs.Upsert(1, WatchEntry{
		Kind:         "movie",
		TMDB:         "27205",
		Title:        "Inception",
		Year:         2010,
		Backdrop:     "/back.jpg",
		PositionSec:  1200,
		DurationSec:  8880,
		SourceID:     2,
		SourceName:   "Knaben",
		ReleaseTitle: "Inception.2010.1080p",
		ReleaseLink:  "magnet:?xt=urn:btih:1234",
		Quality:      "1080p",
		Subtitle:     &SubPref{Mode: "site", Language: "tr", DelayMS: -250},
	}); err != nil {
		t.Fatal(err)
	}
	saved, err := hs.Upsert(1, WatchEntry{
		Kind:        "movie",
		TMDB:        "27205",
		PositionSec: 1500,
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Backdrop != "/back.jpg" || saved.SourceName != "Knaben" || saved.Subtitle == nil || saved.Subtitle.Language != "tr" {
		t.Fatalf("metadata/prefs lost across partial progress upsert: %+v", saved)
	}
	if saved.Completed {
		t.Fatalf("expected movie at 1500/8880 not completed")
	}

	// Show episodes: E1 completed, E2 in progress -> continue_watching deduplicates to latest episode (E2).
	if _, err := hs.Upsert(1, WatchEntry{
		Kind:        "show",
		TMDB:        "1396",
		Title:       "Breaking Bad",
		Season:      1,
		Episode:     1,
		EpisodeName: "Pilot",
		PositionSec: 3400,
		DurationSec: 3480,
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := hs.Upsert(1, WatchEntry{
		Kind:        "show",
		TMDB:        "1396",
		Title:       "Breaking Bad",
		Season:      1,
		Episode:     2,
		EpisodeName: "Cat's in the Bag...",
		PositionSec: 600,
		DurationSec: 2880,
	}); err != nil {
		t.Fatal(err)
	}

	// Finished movie (>92%) must be excluded from continue_watching.
	if _, err := hs.Upsert(1, WatchEntry{
		Kind:        "movie",
		TMDB:        "603",
		Title:       "The Matrix",
		PositionSec: 7900,
		DurationSec: 8100,
	}); err != nil {
		t.Fatal(err)
	}

	// Reload from disk into fresh HistoryStore.
	reloaded := NewHistoryStore(cfgDir)
	items, cw := reloaded.List(1, 50)
	if len(items) != 4 {
		t.Fatalf("expected 4 history items after reload, got %d", len(items))
	}
	if len(cw) != 2 {
		t.Fatalf("expected 2 continue_watching entries (Inception + Breaking Bad E2), got %d: %+v", len(cw), cw)
	}

	latest, eps := reloaded.ForTitle(1, "show", "1396")
	if latest == nil || latest.Episode != 2 || len(eps) != 2 {
		t.Fatalf("ForTitle show mismatch: latest=%+v eps=%d", latest, len(eps))
	}

	if err := reloaded.Delete(1, "movie:27205"); err != nil {
		t.Fatal(err)
	}
	_, cwAfter := reloaded.List(1, 50)
	if len(cwAfter) != 1 {
		t.Fatalf("expected 1 continue_watching entry after delete, got %d", len(cwAfter))
	}
}

func TestShiftWebVTT(t *testing.T) {
	src := strings.Join([]string{
		"WEBVTT",
		"",
		"1",
		"00:00:10.000 --> 00:00:15.000",
		"Before seek",
		"",
		"2",
		"00:00:55.000 --> 00:01:05.500 align:middle",
		"Spanning seek",
		"",
		"3",
		"00:02:00.000 --> 00:02:04.250",
		"After seek",
		"",
	}, "\n")

	out := string(shiftWebVTT([]byte(src), 60))
	if strings.Contains(out, "Before seek") {
		t.Fatalf("cue before offset should have been dropped:\n%s", out)
	}
	if !strings.Contains(out, "00:00:00.000 --> 00:00:05.500 align:middle") {
		t.Fatalf("spanning cue was not clamped/preserved:\n%s", out)
	}
	if !strings.Contains(out, "00:01:00.000 --> 00:01:04.250") {
		t.Fatalf("later cue was not shifted by 60s:\n%s", out)
	}
}

func TestStopCleansSessionDir(t *testing.T) {
	cacheRoot := t.TempDir()
	mgr := NewManager(nil, nil, t.TempDir(), nil)
	mgr.CacheRoot = cacheRoot
	defer mgr.Close()

	sessDir := filepath.Join(cacheRoot, "sess1")
	dataFile := filepath.Join(sessDir, "data", "movie.mkv")
	if err := os.MkdirAll(filepath.Dir(dataFile), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataFile, []byte("test"), 0o640); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	mgr.mu.Lock()
	mgr.sessions["sess1"] = &Session{
		id:        "sess1",
		token:     "tok",
		dir:       sessDir,
		keepFile:  false,
		ctx:       ctx,
		cancel:    cancel,
		createdAt: time.Now(),
	}
	mgr.mu.Unlock()

	if err := mgr.Stop("sess1"); err != nil {
		t.Fatalf("Stop error: %v", err)
	}
	if _, ok := mgr.Status("sess1"); ok {
		t.Fatal("session still present after Stop")
	}
	if _, err := os.Stat(sessDir); !os.IsNotExist(err) {
		t.Fatalf("session directory still exists after Stop: err=%v", err)
	}
}

func hasPair(args []string, flag, val string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == val {
			return true
		}
	}
	return false
}
