package stream

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/anacrolix/torrent"
)

func contentTypeForExt(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mkv":
		return "video/x-matroska"
	case ".avi":
		return "video/x-msvideo"
	case ".mov":
		return "video/quicktime"
	case ".ts", ".m2ts":
		return "video/mp2t"
	default:
		return "application/octet-stream"
	}
}

// ServeMedia streams the currently selected torrent video file over HTTP with full Range seeking support.
func (m *Manager) ServeMedia(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	tok := r.PathValue("token")
	sess, ok := m.getAuthorizedSession(id, tok)
	if !ok {
		http.NotFound(w, r)
		return
	}

	sess.mu.Lock()
	if sess.closed || sess.selected < 0 || sess.selected >= len(sess.files) {
		sess.mu.Unlock()
		http.Error(w, "stream not ready", http.StatusServiceUnavailable)
		return
	}
	file := sess.files[sess.selected]
	createdAt := sess.createdAt
	sess.mu.Unlock()

	sess.touch()
	tr := file.NewReader()
	defer tr.Close()
	tr.SetContext(r.Context())
	tr.SetResponsive()
	tr.SetReadahead(32 << 20)

	name := filepath.Base(file.DisplayPath())
	w.Header().Set("Content-Type", contentTypeForExt(name))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, name, createdAt, &activityReadSeeker{Reader: tr, sess: sess})
}

type activityReadSeeker struct {
	torrent.Reader
	sess *Session
}

func (a *activityReadSeeker) Read(p []byte) (int, error) {
	n, err := a.Reader.Read(p)
	if n > 0 {
		a.sess.touch()
	}
	return n, err
}

func (a *activityReadSeeker) Seek(offset int64, whence int) (int64, error) {
	a.sess.touch()
	return a.Reader.Seek(offset, whence)
}
