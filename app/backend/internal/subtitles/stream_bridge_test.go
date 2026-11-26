package subtitles

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"
)

func TestConvertToWebVTT(t *testing.T) {
	srt := "1\n00:00:01,500 --> 00:00:04,250\nHello world!\nSecond line.\n\n"
	vtt, err := ConvertToWebVTT([]byte(srt), "movie.srt", "en")
	if err != nil {
		t.Fatalf("SRT ConvertToWebVTT error: %v", err)
	}
	got := string(vtt)
	if !strings.HasPrefix(got, "WEBVTT\n\n") || !strings.Contains(got, "00:00:01.500 --> 00:00:04.250") || !strings.Contains(got, "Hello world!\nSecond line.") {
		t.Fatalf("unexpected WebVTT from SRT:\n%s", got)
	}

	// Windows-1254 Turkish SRT -> UTF-8 WebVTT
	trUTF8 := "1\n00:00:02,000 --> 00:00:05,000\nMerhaba dünya, akşam güneşi çok güzelmiş!\n\n"
	trBytes, err := charmap.Windows1254.NewEncoder().Bytes([]byte(trUTF8))
	if err != nil {
		t.Fatal(err)
	}
	vttTR, err := ConvertToWebVTT(trBytes, "tr.srt", "tr")
	if err != nil {
		t.Fatalf("TR CP1254 ConvertToWebVTT error: %v", err)
	}
	if !strings.Contains(string(vttTR), "akşam güneşi çok güzelmiş") {
		t.Fatalf("Turkish characters corrupted in WebVTT:\n%s", string(vttTR))
	}

	// ASS -> WebVTT
	ass := strings.Join([]string{
		"[Script Info]",
		"ScriptType: v4.00+",
		"[Events]",
		"Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text",
		"Dialogue: 0,0:00:10.50,0:00:13.00,Default,,0,0,0,,Line1\\N{\\i1}Italic{\\i0}",
		"",
	}, "\n")
	vttASS, err := ConvertToWebVTT([]byte(ass), "ep.ass", "en")
	if err != nil {
		t.Fatalf("ASS ConvertToWebVTT error: %v", err)
	}
	if !strings.Contains(string(vttASS), "00:00:10.500 --> 00:00:13.000") || !strings.Contains(string(vttASS), "Line1\n<i>Italic</i>") {
		t.Fatalf("unexpected WebVTT from ASS:\n%s", string(vttASS))
	}

	// ZIP containing SRT -> WebVTT
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("nested/sub.srt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(srt)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	vttZip, err := ConvertToWebVTT(buf.Bytes(), "archive.zip", "en")
	if err != nil {
		t.Fatalf("ZIP ConvertToWebVTT error: %v", err)
	}
	if !strings.Contains(string(vttZip), "Hello world!") {
		t.Fatalf("unexpected WebVTT from ZIP:\n%s", string(vttZip))
	}
}
