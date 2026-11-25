package stream

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

func buildMPVArgs(mediaURL, title, audioLang, subLang string) []string {
	if strings.TrimSpace(audioLang) == "" {
		audioLang = "eng,en,tur,tr"
	}
	if strings.TrimSpace(subLang) == "" {
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
	if strings.TrimSpace(title) != "" {
		args = append(args, "--force-media-title="+strings.TrimSpace(title))
	}
	args = append(args, mediaURL)
	return args
}

// LaunchMPV starts desktop mpv pointing at the active stream's local HTTP range URL.
func (m *Manager) LaunchMPV(id, audioLang, subLang string) error {
	mpvBin, err := exec.LookPath("mpv")
	if err != nil {
		return errors.New("mpv is not installed")
	}
	sess, ok := m.getSession(id)
	if !ok {
		return errors.New("stream session not found")
	}

	sess.mu.Lock()
	if sess.closed || sess.selected < 0 || sess.selected >= len(sess.files) {
		sess.mu.Unlock()
		return errors.New("stream file is not ready yet")
	}
	if sess.mpvCmd != nil && sess.mpvCmd.Process != nil {
		_ = sess.mpvCmd.Process.Kill()
	}
	title := sess.title
	if title == "" {
		title = sess.releaseTitle
	}
	mediaURL := m.localBaseURL() + sess.mediaPath()
	args := buildMPVArgs(mediaURL, title, audioLang, subLang)
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

	go func(c *exec.Cmd) {
		_ = c.Wait()
		sess.mu.Lock()
		if sess.mpvCmd == c {
			sess.mpvCmd = nil
			sess.mpvRunning = false
			sess.lastActivity = time.Now()
		}
		sess.mu.Unlock()
	}(cmd)

	return nil
}
