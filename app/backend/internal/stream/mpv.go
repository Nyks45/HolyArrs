package stream

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// MPVOpts customizes how desktop mpv is launched for a streaming session.
type MPVOpts struct {
	AudioLang string
	SubLang   string
	StartSec  float64
	SubFile   string
	Watch     *WatchEntry
}

func buildMPVArgs(mediaURL, title string, opts MPVOpts, ipcSock string) []string {
	audioLang := strings.TrimSpace(opts.AudioLang)
	if audioLang == "" {
		audioLang = "eng,en,tur,tr"
	}
	subLang := strings.TrimSpace(opts.SubLang)
	if subLang == "" {
		subLang = "tur,tr,eng,en"
	}
	args := []string{
		"--no-terminal",
		"--cache=yes",
		"--demuxer-max-bytes=150MiB",
		"--demuxer-readahead-secs=30",
		"--alang=" + audioLang,
		"--slang=" + subLang,
	}
	if opts.StartSec > 5 {
		args = append(args, fmt.Sprintf("--start=%.1f", opts.StartSec))
	}
	if sf := strings.TrimSpace(opts.SubFile); sf != "" {
		args = append(args, "--sub-file="+sf)
	}
	if strings.TrimSpace(ipcSock) != "" {
		args = append(args, "--input-ipc-server="+ipcSock)
	}
	if strings.TrimSpace(title) != "" {
		args = append(args, "--force-media-title="+strings.TrimSpace(title))
	}
	args = append(args, mediaURL)
	return args
}

func isSafeSessionSubFile(sessDir, subFile string) bool {
	if strings.TrimSpace(sessDir) == "" || strings.TrimSpace(subFile) == "" {
		return false
	}
	cleanSess := filepath.Clean(sessDir)
	cleanSub := filepath.Clean(subFile)
	rel, err := filepath.Rel(cleanSess, cleanSub)
	if err != nil || rel == "" || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	return strings.EqualFold(filepath.Ext(cleanSub), ".vtt")
}

// LaunchMPV starts desktop mpv pointing at the active stream's local HTTP range URL,
// optionally resuming at StartSec, attaching a session subtitle file, and tracking progress via IPC.
func (m *Manager) LaunchMPV(id string, opts MPVOpts) error {
	mpvBin, err := exec.LookPath("mpv")
	if err != nil {
		return errors.New("mpv is not installed")
	}
	sess, ok := m.getSession(id)
	if !ok {
		return errors.New("stream session not found")
	}

	sess.mu.Lock()
	mediaURL, _, _, targetOK := sess.localOrMediaTargetLocked(m.localBaseURL())
	if sess.closed || !targetOK {
		sess.mu.Unlock()
		return errors.New("stream file is not ready yet")
	}
	if opts.SubFile != "" && !isSafeSessionSubFile(sess.dir, opts.SubFile) {
		sess.mu.Unlock()
		return errors.New("invalid session subtitle file path")
	}
	if sess.mpvCmd != nil && sess.mpvCmd.Process != nil {
		_ = sess.mpvCmd.Process.Kill()
	}
	title := sess.title
	if title == "" {
		title = sess.releaseTitle
	}
	ipcSock := filepath.Join(sess.dir, fmt.Sprintf("mpv-%d.sock", time.Now().UnixNano()))
	args := buildMPVArgs(mediaURL, title, opts, ipcSock)
	cmd := exec.Command(mpvBin, args...)
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		sess.mu.Unlock()
		return err
	}
	sess.mpvCmd = cmd
	sess.mpvRunning = true
	sess.lastActivity = time.Now()
	sess.mu.Unlock()

	exited := make(chan struct{})
	go func(c *exec.Cmd, sock string) {
		_ = c.Wait()
		close(exited)
		_ = os.Remove(sock)
		sess.mu.Lock()
		if sess.mpvCmd == c {
			sess.mpvCmd = nil
			sess.mpvRunning = false
			sess.lastActivity = time.Now()
		}
		sess.mu.Unlock()
	}(cmd, ipcSock)

	if opts.Watch != nil && strings.TrimSpace(opts.Watch.TMDB) != "" && m.History != nil {
		watchCopy := *opts.Watch
		go m.pollMPVProgress(sess, exited, ipcSock, watchCopy)
	}

	return nil
}

func (m *Manager) pollMPVProgress(sess *Session, exited <-chan struct{}, ipcSock string, watch WatchEntry) {
	// Wait briefly for mpv to create the Unix IPC socket.
	var conn net.Conn
	for range 20 {
		select {
		case <-exited:
			return
		case <-sess.ctx.Done():
			return
		case <-time.After(250 * time.Millisecond):
		}
		c, err := net.DialTimeout("unix", ipcSock, 500*time.Millisecond)
		if err == nil {
			conn = c
			break
		}
	}
	if conn == nil {
		return
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	reqID := 1
	tk := time.NewTicker(3 * time.Second)
	defer tk.Stop()

	for {
		select {
		case <-exited:
			return
		case <-sess.ctx.Done():
			return
		case <-tk.C:
			pos, okPos := mpvGetFloat(conn, reader, reqID, "time-pos")
			reqID++
			if !okPos || pos < 1 {
				continue
			}
			dur, okDur := mpvGetFloat(conn, reader, reqID, "duration")
			reqID++

			entry := watch
			entry.PositionSec = pos
			if okDur && dur > 0 {
				entry.DurationSec = dur
			}
			sess.touch()
			_, _ = m.History.Upsert(sess.userID, entry)
		}
	}
}

func mpvGetFloat(conn net.Conn, reader *bufio.Reader, reqID int, prop string) (float64, bool) {
	_ = conn.SetDeadline(time.Now().Add(1200 * time.Millisecond))
	payload := fmt.Sprintf("{\"command\":[\"get_property\",%q],\"request_id\":%d}\n", prop, reqID)
	if _, err := conn.Write([]byte(payload)); err != nil {
		return 0, false
	}
	for range 12 {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return 0, false
		}
		var resp struct {
			RequestID int     `json:"request_id"`
			Error     string  `json:"error"`
			Data      float64 `json:"data"`
		}
		if json.Unmarshal(line, &resp) != nil {
			continue
		}
		if resp.RequestID == reqID {
			return resp.Data, resp.Error == "success"
		}
	}
	return 0, false
}
