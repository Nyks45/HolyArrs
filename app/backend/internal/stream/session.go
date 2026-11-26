package stream

import (
	"context"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
)

// StartReq starts a playback session from a unified candidate, local file,
// remote URL, debrid provider, or torrent release link.
type StartReq struct {
	SourceID       int64             `json:"source_id"`
	SourceName     string            `json:"source_name"`
	Link           string            `json:"link"`
	Title          string            `json:"title"`
	ReleaseTitle   string            `json:"release_title"`
	Kind           string            `json:"kind"` // movie or show
	Season         int               `json:"season"`
	Episode        int               `json:"episode"`
	KeepFile       bool              `json:"keep_file"`
	CandidateID    string            `json:"candidate_id,omitempty"`
	LocalFileID    int64             `json:"local_file_id,omitempty"`
	StreamURL      string            `json:"stream_url,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	DebridProvider string            `json:"debrid_provider,omitempty"`
	TMDBID         int               `json:"tmdb,omitempty"`
	IMDbID         string            `json:"imdb,omitempty"`
}

// VideoFile represents one playable video file inside a session.
type VideoFile struct {
	Index    int    `json:"index"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Selected bool   `json:"selected"`
}

// SessionStatus is the JSON state polled by the streaming player UI.
type SessionStatus struct {
	ID              string        `json:"id"`
	Token           string        `json:"token"`
	Title           string        `json:"title"`
	ReleaseTitle    string        `json:"release_title"`
	SourceID        int64         `json:"source_id"`
	SourceName      string        `json:"source_name"`
	SourceProvider  string        `json:"source_provider,omitempty"`
	Transport       string        `json:"transport"` // torrent, local, direct, hls_remote
	DirectURL       string        `json:"direct_url,omitempty"`
	Kind            string        `json:"kind"`
	Season          int           `json:"season,omitempty"`
	Episode         int           `json:"episode,omitempty"`
	State           string        `json:"state"` // resolving, buffering, ready, error
	Error           string        `json:"error,omitempty"`
	Files           []VideoFile   `json:"files"`
	SelectedFile    int           `json:"selected_file"`
	Peers           int           `json:"peers"`
	Seeders         int           `json:"seeders"`
	DownloadBPS     int64         `json:"download_bps"`
	DownloadedBytes int64         `json:"downloaded_bytes"`
	FileSize        int64         `json:"file_size"`
	ProgressPct     float64       `json:"progress_pct"`
	KeepFile        bool          `json:"keep_file"`
	MPVRunning      bool          `json:"mpv_running"`
	MediaURL        string        `json:"media_url"`
	NeedsTranscode  bool          `json:"needs_transcode,omitempty"`
	Probe           *MediaProbe   `json:"probe,omitempty"`
	Chapters        []ChapterMark `json:"chapters,omitempty"`
}

// Session holds the state of one active playback stream.
type Session struct {
	id             string
	token          string
	userID         int64
	title          string
	releaseTitle   string
	sourceID       int64
	sourceName     string
	sourceProvider string
	transport      string
	kind           string
	season         int
	episode        int
	dir            string
	keepFile       bool
	createdAt      time.Time

	ctx    context.Context
	cancel context.CancelFunc

	mu            sync.Mutex
	state         string
	errMsg        string
	torrent       *torrent.Torrent
	files         []*torrent.File
	selected      int
	localFilePath string
	fileSize      int64
	proxyToken    string
	proxyIsHLS    bool
	needsTranscode bool
	probe         *MediaProbe
	chapters      []ChapterMark
	probing       bool
	probeFailedAt time.Time
	hls           *hlsState
	mpvCmd        *exec.Cmd
	mpvRunning    bool
	lastActivity  time.Time
	warmupAt      time.Time
	prevBytes     int64
	prevAt        time.Time
	downloadBPS   int64
	firstReadDone bool
	closed        bool
}

func (s *Session) touch() {
	s.mu.Lock()
	s.lastActivity = time.Now()
	s.mu.Unlock()
}

func (s *Session) mediaPath() string {
	s.mu.Lock()
	tok := s.proxyToken
	id := s.id
	stoken := s.token
	s.mu.Unlock()
	if tok != "" {
		return "/api/v1/streaming/proxy/" + tok
	}
	return "/api/v1/stream/" + id + "/media/" + stoken
}

func (s *Session) mediaPathLocked() string {
	if s.proxyToken != "" {
		return "/api/v1/streaming/proxy/" + s.proxyToken
	}
	return "/api/v1/stream/" + s.id + "/media/" + s.token
}

func (s *Session) hasPlayableTargetLocked() bool {
	if s.closed {
		return false
	}
	if s.localFilePath != "" || s.proxyToken != "" {
		return true
	}
	return s.selected >= 0 && s.selected < len(s.files)
}

func (s *Session) localOrMediaTargetLocked(baseURL string) (target, ext string, fileIdx int, ok bool) {
	if !s.hasPlayableTargetLocked() {
		return "", "", 0, false
	}
	if s.localFilePath != "" {
		return s.localFilePath, filepath.Ext(s.localFilePath), 0, true
	}
	if s.proxyToken != "" {
		ext = ""
		if s.proxyIsHLS {
			ext = ".m3u8"
		}
		return baseURL + s.mediaPathLocked(), ext, 0, true
	}
	return baseURL + s.mediaPathLocked(), filepath.Ext(s.files[s.selected].DisplayPath()), s.selected, true
}

func (s *Session) status() SessionStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	tr := s.transport
	if tr == "" {
		tr = "torrent"
	}
	mediaPath := s.mediaPathLocked()
	out := SessionStatus{
		ID:             s.id,
		Token:          s.token,
		Title:          s.title,
		ReleaseTitle:   s.releaseTitle,
		SourceID:       s.sourceID,
		SourceName:     s.sourceName,
		SourceProvider: fallbackString(s.sourceProvider, s.sourceName),
		Transport:      tr,
		Kind:           s.kind,
		Season:         s.season,
		Episode:        s.episode,
		State:          s.state,
		Error:          s.errMsg,
		SelectedFile:   s.selected,
		KeepFile:       s.keepFile,
		MPVRunning:     s.mpvRunning,
		MediaURL:       mediaPath,
		NeedsTranscode: s.needsTranscode,
		Probe:          s.probe,
		Chapters:       s.chapters,
		Files:          make([]VideoFile, 0, len(s.files)),
	}
	if s.probe != nil {
		out.NeedsTranscode = !s.probe.DirectPlayable
	}
	if tr == "local" || tr == "direct" || tr == "hls_remote" {
		out.DirectURL = mediaPath
	}

	if tr == "local" && s.localFilePath != "" {
		baseName := filepath.Base(s.localFilePath)
		out.SelectedFile = 0
		out.FileSize = s.fileSize
		out.DownloadedBytes = s.fileSize
		out.ProgressPct = 100
		out.Files = []VideoFile{{
			Index:    0,
			Name:     baseName,
			Path:     baseName,
			Size:     s.fileSize,
			Selected: true,
		}}
		return out
	}

	for i, f := range s.files {
		out.Files = append(out.Files, VideoFile{
			Index:    i,
			Name:     filepath.Base(f.DisplayPath()),
			Path:     f.DisplayPath(),
			Size:     f.Length(),
			Selected: i == s.selected,
		})
	}
	if s.selected >= 0 && s.selected < len(s.files) {
		f := s.files[s.selected]
		out.FileSize = f.Length()
		out.DownloadedBytes = f.BytesCompleted()
		if out.FileSize > 0 {
			out.ProgressPct = float64(out.DownloadedBytes) * 100 / float64(out.FileSize)
		}
	}
	if s.torrent != nil {
		st := s.torrent.Stats()
		out.Peers = st.ActivePeers
		out.Seeders = st.ConnectedSeeders
		curBytes := st.BytesReadUsefulData.Int64()
		now := time.Now()
		dt := now.Sub(s.prevAt).Seconds()
		if dt >= 0.4 {
			if curBytes >= s.prevBytes {
				s.downloadBPS = int64(float64(curBytes-s.prevBytes) / dt)
			}
			s.prevBytes = curBytes
			s.prevAt = now
		}
		out.DownloadBPS = s.downloadBPS
	}
	return out
}
