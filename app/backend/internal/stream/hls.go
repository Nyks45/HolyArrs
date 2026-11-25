package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// AudioTrack describes one audio stream discovered by ffprobe.
type AudioTrack struct {
	Index    int    `json:"index"`        // 0-based audio track index (0:a:<Index>)
	StreamID int    `json:"stream_index"` // global ffprobe stream index
	Language string `json:"language,omitempty"`
	Title    string `json:"title,omitempty"`
	Codec    string `json:"codec,omitempty"`
	Channels int    `json:"channels,omitempty"`
}

// SubtitleTrack describes one embedded subtitle stream discovered by ffprobe.
type SubtitleTrack struct {
	Index    int    `json:"index"`        // 0-based subtitle track index (0:s:<Index>)
	StreamID int    `json:"stream_index"` // global ffprobe stream index
	Language string `json:"language,omitempty"`
	Title    string `json:"title,omitempty"`
	Codec    string `json:"codec,omitempty"`
	Text     bool   `json:"text"`
}

// MediaProbe holds container/stream metadata extracted via ffprobe.
type MediaProbe struct {
	DurationSec    float64         `json:"duration_sec"`
	FormatName     string          `json:"format_name,omitempty"`
	VideoCodec     string          `json:"video_codec,omitempty"`
	Width          int             `json:"width,omitempty"`
	Height         int             `json:"height,omitempty"`
	PixFmt         string          `json:"pix_fmt,omitempty"`
	DirectPlayable bool            `json:"direct_playable"`
	AudioTracks    []AudioTrack    `json:"audio_tracks"`
	SubtitleTracks []SubtitleTrack `json:"subtitle_tracks"`
}

type hlsState struct {
	gen         int
	quality     string
	audioIdx    int
	startSec    float64
	dir         string
	playlistURL string
	cmd         *exec.Cmd
	done        chan struct{}
	errText     string
}

var textSubCodecs = map[string]bool{
	"subrip":   true,
	"srt":      true,
	"ass":      true,
	"ssa":      true,
	"webvtt":   true,
	"mov_text": true,
	"text":     true,
}

func isBrowserDirectPlayable(ext, formatName, vcodec, pixFmt string, audio []AudioTrack) bool {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext != "" {
		if ext != ".mp4" && ext != ".m4v" && ext != ".webm" {
			return false
		}
	} else {
		isMP4 := strings.Contains(formatName, "mp4")
		isPureWebM := formatName == "webm" || (strings.Contains(formatName, "webm") && (vcodec == "vp8" || vcodec == "vp9"))
		if !isMP4 && !isPureWebM {
			return false
		}
	}
	switch vcodec {
	case "h264", "vp8", "vp9":
	default:
		return false
	}
	if pixFmt != "" && pixFmt != "yuv420p" && pixFmt != "yuvj420p" {
		return false
	}
	for _, a := range audio {
		switch a.Codec {
		case "aac", "mp3", "opus", "vorbis":
		default:
			return false
		}
		if a.Channels > 2 {
			return false
		}
	}
	return true
}

func probeMedia(ctx context.Context, mediaURL, fileExt string) (*MediaProbe, error) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		mediaURL,
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var raw struct {
		Format struct {
			FormatName string `json:"format_name"`
			Duration   string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			Index     int               `json:"index"`
			CodecType string            `json:"codec_type"`
			CodecName string            `json:"codec_name"`
			Width     int               `json:"width"`
			Height    int               `json:"height"`
			PixFmt    string            `json:"pix_fmt"`
			Channels  int               `json:"channels"`
			Tags      map[string]string `json:"tags"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	dur, _ := strconv.ParseFloat(strings.TrimSpace(raw.Format.Duration), 64)
	p := &MediaProbe{
		DurationSec:    dur,
		FormatName:     raw.Format.FormatName,
		AudioTracks:    []AudioTrack{},
		SubtitleTracks: []SubtitleTrack{},
	}
	aIdx, sIdx := 0, 0
	for _, st := range raw.Streams {
		lang := tagCI(st.Tags, "language")
		title := tagCI(st.Tags, "title")
		switch st.CodecType {
		case "video":
			if p.VideoCodec == "" {
				p.VideoCodec = strings.ToLower(st.CodecName)
				p.Width = st.Width
				p.Height = st.Height
				p.PixFmt = strings.ToLower(st.PixFmt)
			}
		case "audio":
			p.AudioTracks = append(p.AudioTracks, AudioTrack{
				Index:    aIdx,
				StreamID: st.Index,
				Language: lang,
				Title:    title,
				Codec:    strings.ToLower(st.CodecName),
				Channels: st.Channels,
			})
			aIdx++
		case "subtitle":
			codec := strings.ToLower(st.CodecName)
			if textSubCodecs[codec] {
				p.SubtitleTracks = append(p.SubtitleTracks, SubtitleTrack{
					Index:    sIdx,
					StreamID: st.Index,
					Language: lang,
					Title:    title,
					Codec:    codec,
					Text:     true,
				})
			}
			sIdx++
		}
	}
	p.DirectPlayable = isBrowserDirectPlayable(fileExt, p.FormatName, p.VideoCodec, p.PixFmt, p.AudioTracks)
	return p, nil
}

func tagCI(tags map[string]string, key string) string {
	for k, v := range tags {
		if strings.EqualFold(k, key) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func buildFFmpegArgs(mediaURL, outDir string, probe *MediaProbe, quality string, audioIdx int, startSec float64) []string {
	switch quality {
	case "original", "1080p", "720p", "480p":
	default:
		quality = "original"
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-y"}
	if startSec > 0.5 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", startSec))
	}
	args = append(args, "-i", mediaURL)

	audioMap := "0:a:0?"
	if probe != nil && len(probe.AudioTracks) > 0 {
		if audioIdx < 0 || audioIdx >= len(probe.AudioTracks) {
			audioIdx = 0
		}
		audioMap = fmt.Sprintf("0:a:%d?", audioIdx)
	}
	args = append(args, "-map", "0:v:0", "-map", audioMap, "-sn")

	copyVideo := quality == "original" && probe != nil && probe.VideoCodec == "h264" &&
		(probe.PixFmt == "" || probe.PixFmt == "yuv420p" || probe.PixFmt == "yuvj420p")
	if copyVideo {
		args = append(args, "-c:v", "copy")
	} else {
		args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-crf", "23", "-pix_fmt", "yuv420p", "-g", "48", "-sc_threshold", "0")
		switch quality {
		case "1080p":
			args = append(args, "-vf", "scale=-2:'min(1080,ih)'")
		case "720p":
			args = append(args, "-vf", "scale=-2:'min(720,ih)'")
		case "480p":
			args = append(args, "-vf", "scale=-2:'min(480,ih)'")
		}
	}

	args = append(args,
		"-c:a", "aac", "-ac", "2", "-b:a", "192k",
		"-f", "hls",
		"-hls_time", "4",
		"-hls_list_size", "0",
		"-hls_playlist_type", "event",
		"-hls_flags", "independent_segments",
		"-hls_segment_filename", filepath.Join(outDir, "seg_%05d.ts"),
		filepath.Join(outDir, "index.m3u8"),
	)
	return args
}

func (s *Session) stopHLSLocked() {
	if s.hls == nil {
		return
	}
	if s.hls.cmd != nil && s.hls.cmd.Process != nil {
		_ = s.hls.cmd.Process.Kill()
	}
	if s.hls.dir != "" {
		_ = os.RemoveAll(s.hls.dir)
	}
	s.hls = nil
}

// EnsureHLS starts or switches the ffmpeg HLS pipeline for browser playback.
func (m *Manager) EnsureHLS(ctx context.Context, id, quality string, audioIdx int, startSec float64) (playlistURL string, actualStart float64, err error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return "", 0, errors.New("ffmpeg is not installed")
	}
	sess, ok := m.getSession(id)
	if !ok {
		return "", 0, errors.New("stream session not found")
	}
	if quality == "" {
		quality = "original"
	}
	if startSec < 0 {
		startSec = 0
	}

	sess.mu.Lock()
	if sess.closed || sess.selected < 0 || sess.selected >= len(sess.files) {
		sess.mu.Unlock()
		return "", 0, errors.New("stream file not ready yet")
	}
	// Reuse running pipeline if parameters match.
	if sess.hls != nil && sess.hls.quality == quality && sess.hls.audioIdx == audioIdx &&
		absFloat(sess.hls.startSec-startSec) < 0.5 && sess.hls.errText == "" {
		url := sess.hls.playlistURL
		st := sess.hls.startSec
		sess.lastActivity = time.Now()
		sess.mu.Unlock()
		return url, st, nil
	}
	gen := 1
	if sess.hls != nil {
		gen = sess.hls.gen + 1
	}
	sess.stopHLSLocked()

	outDir := filepath.Join(sess.dir, "hls", strconv.Itoa(gen))
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		sess.mu.Unlock()
		return "", 0, err
	}
	probe := sess.probe
	mediaURL := m.localBaseURL() + sess.mediaPath()
	playlistPath := fmt.Sprintf("/api/v1/stream/%s/hls/%s/%d/index.m3u8", sess.id, sess.token, gen)

	args := buildFFmpegArgs(mediaURL, outDir, probe, quality, audioIdx, startSec)
	cmd := exec.CommandContext(sess.ctx, "ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(outDir)
		sess.mu.Unlock()
		return "", 0, fmt.Errorf("starting ffmpeg: %w", err)
	}

	done := make(chan struct{})
	st := &hlsState{
		gen:         gen,
		quality:     quality,
		audioIdx:    audioIdx,
		startSec:    startSec,
		dir:         outDir,
		playlistURL: playlistPath,
		cmd:         cmd,
		done:        done,
	}
	sess.hls = st
	sess.lastActivity = time.Now()
	sess.mu.Unlock()

	go func() {
		waitErr := cmd.Wait()
		if waitErr != nil {
			sess.mu.Lock()
			if sess.hls == st && !sess.closed {
				msg := strings.TrimSpace(stderr.String())
				if msg == "" {
					msg = waitErr.Error()
				}
				st.errText = msg
			}
			sess.mu.Unlock()
		}
		close(done)
	}()

	// Wait until ffmpeg writes the first segment so hls.js starts smoothly.
	m3u8Path := filepath.Join(outDir, "index.m3u8")
	deadline := time.NewTimer(25 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		if b, rerr := os.ReadFile(m3u8Path); rerr == nil {
			s := string(b)
			if strings.Contains(s, ".ts") || strings.Contains(s, "#EXT-X-ENDLIST") {
				return playlistPath, startSec, nil
			}
		}
		select {
		case <-done:
			sess.mu.Lock()
			msg := st.errText
			sess.mu.Unlock()
			if msg == "" {
				if b, rerr := os.ReadFile(m3u8Path); rerr == nil && strings.Contains(string(b), ".ts") {
					return playlistPath, startSec, nil
				}
				msg = "ffmpeg exited before creating stream segments"
			}
			return "", 0, errors.New(msg)
		case <-ctx.Done():
			return "", 0, ctx.Err()
		case <-deadline.C:
			return "", 0, errors.New("timed out waiting for HLS buffer")
		case <-tick.C:
		}
	}
}

var reHLSSegment = regexp.MustCompile(`^seg_\d{5}\.ts$`)

// ServeHLSFile serves index.m3u8 or seg_NNNNN.ts for an active HLS pipeline.
func (m *Manager) ServeHLSFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	tok := r.PathValue("token")
	genStr := r.PathValue("gen")
	file := r.PathValue("file")

	sess, ok := m.getAuthorizedSession(id, tok)
	if !ok {
		http.NotFound(w, r)
		return
	}
	gen, err := strconv.Atoi(genStr)
	if err != nil || gen <= 0 {
		http.NotFound(w, r)
		return
	}
	if file != "index.m3u8" && !reHLSSegment.MatchString(file) {
		http.NotFound(w, r)
		return
	}

	sess.mu.Lock()
	var dir string
	if sess.hls != nil && sess.hls.gen == gen {
		dir = sess.hls.dir
		sess.lastActivity = time.Now()
	}
	sess.mu.Unlock()
	if dir == "" {
		http.NotFound(w, r)
		return
	}

	fullPath := filepath.Join(dir, file)
	if file == "index.m3u8" {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-store")
	} else {
		w.Header().Set("Content-Type", "video/mp2t")
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeFile(w, r, fullPath)
}

// ServeSubtitleVTT extracts an embedded subtitle stream as WebVTT for browser <track>.
func (m *Manager) ServeSubtitleVTT(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	tok := r.PathValue("token")
	idxStr := r.PathValue("idx")

	sess, ok := m.getAuthorizedSession(id, tok)
	if !ok {
		http.NotFound(w, r)
		return
	}
	idx, err := strconv.Atoi(idxStr)
	if err != nil || idx < 0 || idx > 64 {
		http.NotFound(w, r)
		return
	}

	sess.mu.Lock()
	if sess.closed || sess.selected < 0 || sess.selected >= len(sess.files) {
		sess.mu.Unlock()
		http.Error(w, "stream not ready", http.StatusServiceUnavailable)
		return
	}
	fileIdx := sess.selected
	subsDir := filepath.Join(sess.dir, "subs")
	sess.lastActivity = time.Now()
	sess.mu.Unlock()

	vttPath := filepath.Join(subsDir, fmt.Sprintf("%d_%d.vtt", fileIdx, idx))
	if _, statErr := os.Stat(vttPath); statErr != nil {
		if err := os.MkdirAll(subsDir, 0o750); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		mediaURL := m.localBaseURL() + sess.mediaPath()
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		tmpPath := vttPath + ".tmp"
		cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-y", "-i", mediaURL, "-map", fmt.Sprintf("0:s:%d", idx), "-f", "webvtt", tmpPath)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if runErr := cmd.Run(); runErr != nil {
			_ = os.Remove(tmpPath)
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = runErr.Error()
			}
			http.Error(w, "subtitle extraction failed: "+msg, http.StatusBadGateway)
			return
		}
		if renErr := os.Rename(tmpPath, vttPath); renErr != nil {
			_ = os.Remove(tmpPath)
			http.Error(w, renErr.Error(), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if offsetSec, err := strconv.ParseFloat(strings.TrimSpace(r.URL.Query().Get("offset")), 64); err == nil && offsetSec > 0.5 {
		raw, rerr := os.ReadFile(vttPath)
		if rerr != nil {
			http.Error(w, rerr.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(shiftWebVTT(raw, offsetSec))
		return
	}
	http.ServeFile(w, r, vttPath)
}

func shiftWebVTT(data []byte, offsetSec float64) []byte {
	if offsetSec <= 0 {
		return data
	}
	norm := strings.ReplaceAll(string(data), "\r\n", "\n")
	blocks := strings.Split(norm, "\n\n")
	out := make([]string, 0, len(blocks))
	for _, blk := range blocks {
		trimmed := strings.TrimSpace(blk)
		if trimmed == "" {
			continue
		}
		lines := strings.Split(blk, "\n")
		timingIdx := -1
		for i, line := range lines {
			if strings.Contains(line, "-->") {
				timingIdx = i
				break
			}
		}
		if timingIdx < 0 {
			out = append(out, blk)
			continue
		}
		shiftedLine, keep := shiftVTTCueTimingLine(lines[timingIdx], offsetSec)
		if !keep {
			continue
		}
		lines[timingIdx] = shiftedLine
		out = append(out, strings.Join(lines, "\n"))
	}
	if len(out) == 0 {
		return []byte("WEBVTT\n\n")
	}
	return []byte(strings.Join(out, "\n\n") + "\n")
}

func shiftVTTCueTimingLine(line string, offsetSec float64) (string, bool) {
	arrow := strings.Index(line, "-->")
	if arrow < 0 {
		return line, true
	}
	startStr := strings.TrimSpace(line[:arrow])
	rest := strings.TrimSpace(line[arrow+3:])
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return line, true
	}
	endStr := fields[0]
	trailing := ""
	if len(fields) > 1 {
		trailing = " " + strings.Join(fields[1:], " ")
	}
	startSec, ok1 := parseVTTTimestamp(startStr)
	endSec, ok2 := parseVTTTimestamp(endStr)
	if !ok1 || !ok2 {
		return line, true
	}
	newEnd := endSec - offsetSec
	if newEnd <= 0 {
		return "", false
	}
	newStart := startSec - offsetSec
	if newStart < 0 {
		newStart = 0
	}
	return formatVTTTimestamp(newStart) + " --> " + formatVTTTimestamp(newEnd) + trailing, true
}

func parseVTTTimestamp(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	secPart := s
	frac := 0.0
	if dot := strings.IndexAny(s, ".,"); dot >= 0 {
		secPart = s[:dot]
		msStr := s[dot+1:]
		for len(msStr) < 3 {
			msStr += "0"
		}
		if len(msStr) > 3 {
			msStr = msStr[:3]
		}
		msVal, err := strconv.Atoi(msStr)
		if err != nil {
			return 0, false
		}
		frac = float64(msVal) / 1000.0
	}
	parts := strings.Split(secPart, ":")
	switch len(parts) {
	case 2:
		m, err1 := strconv.Atoi(parts[0])
		sec, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			return 0, false
		}
		return float64(m*60+sec) + frac, true
	case 3:
		h, err1 := strconv.Atoi(parts[0])
		m, err2 := strconv.Atoi(parts[1])
		sec, err3 := strconv.Atoi(parts[2])
		if err1 != nil || err2 != nil || err3 != nil {
			return 0, false
		}
		return float64(h*3600+m*60+sec) + frac, true
	default:
		return 0, false
	}
}

func formatVTTTimestamp(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	totalMS := int64(sec*1000 + 0.5)
	ms := totalMS % 1000
	totalSec := totalMS / 1000
	s := totalSec % 60
	totalMin := totalSec / 60
	m := totalMin % 60
	h := totalMin / 60
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
