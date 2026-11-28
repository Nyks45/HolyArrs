package stream

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const styledFixture = `[Script Info]
ScriptType: v4.00+
[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:03.00,Default,,0,0,0,,{\pos(15,25)}Hello
`

func TestPlayerStyledSubtitleUpload(t *testing.T) {
	for _, filename := range []string{"title.ass", "title.ssa", "title.txt"} {
		t.Run(filename, func(t *testing.T) {
			body, err := json.Marshal(map[string]string{"filename": filename, "content": styledFixture, "language": "en"})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/subtitles/upload", bytes.NewReader(body))
			rec := httptest.NewRecorder()
			new(Handlers).uploadSubtitle(rec, req)
			var result map[string]string
			if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &result) != nil {
				t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
			}
			if result["ass"] != styledFixture || !strings.Contains(result["vtt"], "00:00:01.000 --> 00:00:03.000\nHello") {
				t.Fatalf("unexpected subtitle response: %v", result)
			}
		})
	}
}

func TestPlayerMediaHeadersAndSubtitleFormats(t *testing.T) {
	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "video.mp4")
	fixtures := map[string]string{
		mediaPath:                             "sample media",
		filepath.Join(dir, "index.m3u8"):      "#EXTM3U\n",
		filepath.Join(dir, "subs", "0_0.ass"): styledFixture,
		filepath.Join(dir, "subs", "0_0.vtt"): "WEBVTT\n\n00:00:01.000 --> 00:00:03.000\nHello\n",
	}
	for path, content := range fixtures {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m := &Manager{sessions: map[string]*Session{"session": {
		token: "test", dir: dir, localFilePath: mediaPath, hls: &hlsState{gen: 1, dir: dir},
	}}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{id}/media/{token}", m.ServeMedia)
	mux.HandleFunc("GET /{id}/hls/{token}/{gen}/{file}", m.ServeHLSFile)
	mux.HandleFunc("GET /{id}/sub/{token}/{idx}", m.ServeSubtitleVTT)
	for path, expected := range map[string]string{
		"/session/media/test":                     "sample media",
		"/session/hls/test/1/index.m3u8":          "#EXTM3U",
		"/session/sub/test/0?format=ass&offset=1": "{\\pos(15,25)}Hello",
		"/session/sub/test/0?offset=1":            "00:00:00.000 --> 00:00:02.000",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "*" || !strings.Contains(rec.Body.String(), expected) {
			t.Errorf("%s: %d %v %q", path, rec.Code, rec.Header(), rec.Body.String())
		}
	}
	refused := httptest.NewRecorder()
	mux.ServeHTTP(refused, httptest.NewRequest(http.MethodGet, "/session/sub/invalid/0?format=ass", nil))
	if refused.Code != http.StatusNotFound || refused.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("unauthorized response exposed subtitle access")
	}
}

func TestPlayerH264Quality(t *testing.T) {
	for profile, expected := range map[string]string{"": "-preset superfast -crf 23", "balanced": "-preset superfast -crf 23", "high": "-preset veryfast -crf 18"} {
		got := buildFFmpegArgs("test.mkv", "test", nil, "720p", 0, 0, true, profile)
		if !strings.Contains(strings.Join(got, " "), expected) {
			t.Errorf("encoding profile %q: %v", profile, got)
		}
	}
}
