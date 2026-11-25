package stream

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	"github.com/HolyArrs/holyarrs/app/backend/internal/sources"
)

var defaultTrackers = []string{
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://open.stealth.si:80/announce",
	"udp://tracker.torrent.eu.org:451/announce",
	"udp://exodus.desync.com:6969/announce",
	"udp://tracker.openbittorrent.com:6969/announce",
}

var videoExts = map[string]bool{
	".mkv":  true,
	".mp4":  true,
	".m4v":  true,
	".webm": true,
	".avi":  true,
	".mov":  true,
	".ts":   true,
	".m2ts": true,
}

// StartReq starts a direct torrent streaming session from a release candidate.
type StartReq struct {
	SourceID     int64  `json:"source_id"`
	SourceName   string `json:"source_name"`
	Link         string `json:"link"`
	Title        string `json:"title"`
	ReleaseTitle string `json:"release_title"`
	Kind         string `json:"kind"` // movie or show
	Season       int    `json:"season"`
	Episode      int    `json:"episode"`
	KeepFile     bool   `json:"keep_file"`
}

// VideoFile represents one playable video file inside a torrent.
type VideoFile struct {
	Index    int    `json:"index"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Selected bool   `json:"selected"`
}

// SessionStatus is the JSON state polled by the streaming player UI.
type SessionStatus struct {
	ID              string      `json:"id"`
	Token           string      `json:"token"`
	Title           string      `json:"title"`
	ReleaseTitle    string      `json:"release_title"`
	SourceID        int64       `json:"source_id"`
	SourceName      string      `json:"source_name"`
	Kind            string      `json:"kind"`
	Season          int         `json:"season,omitempty"`
	Episode         int         `json:"episode,omitempty"`
	State           string      `json:"state"` // resolving, buffering, ready, error
	Error           string      `json:"error,omitempty"`
	Files           []VideoFile `json:"files"`
	SelectedFile    int         `json:"selected_file"`
	Peers           int         `json:"peers"`
	Seeders         int         `json:"seeders"`
	DownloadBPS     int64       `json:"download_bps"`
	DownloadedBytes int64       `json:"downloaded_bytes"`
	FileSize        int64       `json:"file_size"`
	ProgressPct     float64     `json:"progress_pct"`
	KeepFile        bool        `json:"keep_file"`
	MPVRunning      bool        `json:"mpv_running"`
	MediaURL        string      `json:"media_url"`
	Probe           *MediaProbe `json:"probe,omitempty"`
}

// Manager owns the shared BitTorrent client and active streaming sessions.
type Manager struct {
	Store      *database.Store
	Secrets    sources.SecretReader
	ConfigDir  string
	KeepDir    string
	CacheRoot  string
	ListenAddr func() string

	clientOnce sync.Once
	client     *torrent.Client
	clientErr  error

	mu       sync.Mutex
	sessions map[string]*Session
	stopCh   chan struct{}
	closed   bool
}

// Session holds the state of one active torrent stream.
type Session struct {
	id           string
	token        string
	userID       int64
	title        string
	releaseTitle string
	sourceID     int64
	sourceName   string
	kind         string
	season       int
	episode      int
	dir          string
	keepFile     bool
	createdAt    time.Time

	ctx    context.Context
	cancel context.CancelFunc

	mu              sync.Mutex
	state           string
	errMsg          string
	torrent         *torrent.Torrent
	files           []*torrent.File
	selected        int
	probe           *MediaProbe
	probing         bool
	probeFailedAt   time.Time
	hls             *hlsState
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

// NewManager creates a stream Manager and starts its idle-session reaper.
func NewManager(store *database.Store, secrets sources.SecretReader, configDir string, listenAddr func() string) *Manager {
	m := &Manager{
		Store:      store,
		Secrets:    secrets,
		ConfigDir:  configDir,
		ListenAddr: listenAddr,
		sessions:   map[string]*Session{},
		stopCh:     make(chan struct{}),
	}
	go m.reapLoop()
	return m
}

func (m *Manager) resolveCacheRoot() string {
	if env := strings.TrimSpace(os.Getenv("HOLYARRS_STREAM_CACHE")); env != "" {
		return env
	}
	if m.CacheRoot != "" {
		return m.CacheRoot
	}
	// Prefer the library disk (e.g. /mnt/D/Media/.stream-cache) so large 4K streams
	// never exhaust RAM-backed /tmp tmpfs.
	if m.Store != nil {
		var path string
		err := m.Store.Read().QueryRow("SELECT path FROM library_folders ORDER BY id LIMIT 1").Scan(&path)
		if err == nil && path != "" {
			parent := filepath.Dir(filepath.Clean(path))
			cand := filepath.Join(parent, ".stream-cache")
			if os.MkdirAll(cand, 0o750) == nil {
				return cand
			}
		}
	}
	if info, err := os.Stat("/mnt/D/Media"); err == nil && info.IsDir() {
		cand := "/mnt/D/Media/.stream-cache"
		if os.MkdirAll(cand, 0o750) == nil {
			return cand
		}
	}
	if m.ConfigDir != "" {
		return filepath.Join(m.ConfigDir, "stream-cache")
	}
	return filepath.Join(os.TempDir(), "holyarrs-stream-cache")
}

func (m *Manager) resolveKeepDir() string {
	if m.KeepDir != "" {
		return m.KeepDir
	}
	if info, err := os.Stat("/mnt/D/Media/downloads"); err == nil && info.IsDir() {
		return "/mnt/D/Media/downloads"
	}
	if m.Store != nil {
		var path string
		err := m.Store.Read().QueryRow("SELECT path FROM library_folders ORDER BY id LIMIT 1").Scan(&path)
		if err == nil && path != "" {
			return filepath.Join(filepath.Dir(filepath.Clean(path)), "downloads")
		}
	}
	return filepath.Join(m.ConfigDir, "stream-saved")
}

func (m *Manager) ensureClient() (*torrent.Client, error) {
	m.clientOnce.Do(func() {
		base := m.resolveCacheRoot()
		m.CacheRoot = base
		clientDir := filepath.Join(base, "_client")
		if err := os.MkdirAll(clientDir, 0o750); err != nil {
			m.clientErr = err
			return
		}
		cfg := torrent.NewDefaultClientConfig()
		cfg.DataDir = clientDir
		cfg.DefaultStorage = storage.NewFileOpts(storage.NewFileClientOpts{
			ClientBaseDir:   clientDir,
			PieceCompletion: storage.NewMapPieceCompletion(),
			DisableMmap:     true,
		})
		cfg.ListenPort = 0
		cfg.NoUpload = false
		cfg.Seed = false
		cfg.Slogger = slog.New(slog.NewTextHandler(io.Discard, nil))
		cl, err := torrent.NewClient(cfg)
		if err != nil {
			m.clientErr = err
			return
		}
		m.client = cl
	})
	return m.client, m.clientErr
}

func (m *Manager) localBaseURL() string {
	addr := ""
	if m.ListenAddr != nil {
		addr = strings.TrimSpace(m.ListenAddr())
	}
	if addr == "" {
		addr = "127.0.0.1:6262"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://127.0.0.1:6262"
	}
	if host == "" || host == "::" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func randomHex(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Start begins a new torrent stream session, stopping any prior session owned by userID.
func (m *Manager) Start(ctx context.Context, userID int64, req StartReq) (SessionStatus, error) {
	if strings.TrimSpace(req.Link) == "" {
		return SessionStatus{}, errors.New("torrent link is required")
	}
	cl, err := m.ensureClient()
	if err != nil {
		return SessionStatus{}, fmt.Errorf("torrent engine: %w", err)
	}

	var dbReader *sql.DB
	if m.Store != nil {
		dbReader = m.Store.Read()
	}
	magnet, rawTorrent, err := sources.FetchTorrentPayload(ctx, dbReader, m.Secrets, req.SourceID, req.Link)
	if err != nil {
		return SessionStatus{}, fmt.Errorf("fetching torrent: %w", err)
	}

	// Stop existing sessions for this user so abandoned streams never pile up.
	m.stopUserSessions(userID)

	id := randomHex(8)
	token := randomHex(16)
	sessDir := filepath.Join(m.resolveCacheRoot(), id)
	dataDir := filepath.Join(sessDir, "data")
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return SessionStatus{}, err
	}

	var spec *torrent.TorrentSpec
	if magnet != "" {
		spec, err = torrent.TorrentSpecFromMagnetUri(magnet)
		if err != nil {
			_ = os.RemoveAll(sessDir)
			return SessionStatus{}, fmt.Errorf("invalid magnet link: %w", err)
		}
		if len(spec.Trackers) == 0 {
			spec.Trackers = [][]string{slicesClone(defaultTrackers)}
		} else {
			spec.Trackers = append(spec.Trackers, slicesClone(defaultTrackers))
		}
	} else {
		mi, err := metainfo.Load(bytes.NewReader(rawTorrent))
		if err != nil {
			_ = os.RemoveAll(sessDir)
			return SessionStatus{}, fmt.Errorf("invalid torrent file: %w", err)
		}
		spec, err = torrent.TorrentSpecFromMetaInfoErr(mi)
		if err != nil {
			_ = os.RemoveAll(sessDir)
			return SessionStatus{}, fmt.Errorf("reading torrent info: %w", err)
		}
		if len(spec.Trackers) == 0 {
			spec.Trackers = [][]string{slicesClone(defaultTrackers)}
		}
	}

	spec.Storage = storage.NewFileOpts(storage.NewFileClientOpts{
		ClientBaseDir:   dataDir,
		PieceCompletion: storage.NewMapPieceCompletion(),
		DisableMmap:     true,
	})

	// Drop any leftover torrent with the same infohash so storage stays isolated.
	if prev, ok := cl.Torrent(spec.InfoHash); ok {
		prev.Drop()
	}

	t, _, err := cl.AddTorrentSpec(spec)
	if err != nil {
		_ = os.RemoveAll(sessDir)
		return SessionStatus{}, fmt.Errorf("adding torrent: %w", err)
	}

	sctx, cancel := context.WithCancel(context.Background())
	now := time.Now()
	kind := req.Kind
	if kind != "show" {
		kind = "movie"
	}
	sess := &Session{
		id:           id,
		token:        token,
		userID:       userID,
		title:        strings.TrimSpace(req.Title),
		releaseTitle: strings.TrimSpace(req.ReleaseTitle),
		sourceID:     req.SourceID,
		sourceName:   strings.TrimSpace(req.SourceName),
		kind:         kind,
		season:       req.Season,
		episode:      req.Episode,
		dir:          sessDir,
		keepFile:     req.KeepFile,
		createdAt:    now,
		ctx:          sctx,
		cancel:       cancel,
		state:        "resolving",
		torrent:      t,
		lastActivity: now,
		prevAt:       now,
	}
	if sess.title == "" {
		sess.title = sess.releaseTitle
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		cancel()
		t.Drop()
		_ = os.RemoveAll(sessDir)
		return SessionStatus{}, errors.New("stream manager is closed")
	}
	m.sessions[id] = sess
	m.mu.Unlock()

	go m.resolveAndWarm(sess)
	return sess.status(), nil
}

func (m *Manager) stopUserSessions(userID int64) {
	m.mu.Lock()
	var ids []string
	for id, s := range m.sessions {
		if s.userID == userID {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Stop(id)
	}
}

func (m *Manager) resolveAndWarm(sess *Session) {
	t := sess.torrent
	select {
	case <-t.GotInfo():
	case <-sess.ctx.Done():
		return
	case <-time.After(60 * time.Second):
		sess.mu.Lock()
		if sess.state == "resolving" {
			sess.state = "error"
			sess.errMsg = "timed out waiting for torrent metadata from peers"
		}
		sess.mu.Unlock()
		return
	}

	vfiles := filterVideoFiles(t.Files())
	if len(vfiles) == 0 {
		sess.mu.Lock()
		sess.state = "error"
		sess.errMsg = "no playable video files found in this torrent"
		sess.mu.Unlock()
		return
	}

	sel := pickBestVideoFile(vfiles, sess.kind, sess.season, sess.episode)
	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		return
	}
	sess.files = vfiles
	sess.selected = sel
	sess.state = "buffering"
	sess.warmupAt = time.Now()
	sess.mu.Unlock()

	prioritizeFile(t, vfiles, sel)
	m.monitorWarmup(sess)
}

func (m *Manager) monitorWarmup(sess *Session) {
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-sess.ctx.Done():
			return
		case <-tick.C:
			sess.mu.Lock()
			if sess.closed || sess.selected < 0 || sess.selected >= len(sess.files) {
				sess.mu.Unlock()
				return
			}
			f := sess.files[sess.selected]
			comp := f.BytesCompleted()
			fLen := f.Length()
			needProbe := sess.probe == nil && !sess.probing && time.Since(sess.probeFailedAt) > 4*time.Second
			readyThreshold := minInt64(2<<20, fLen)
			canProbe := needProbe && comp >= minInt64(1500*1024, fLen)
			if canProbe {
				sess.probing = true
			}
			// Wait briefly for ffprobe so track lists and DirectPlayable arrive with ready state.
			if comp >= readyThreshold && sess.state == "buffering" && time.Since(sess.warmupAt) >= 8*time.Second {
				sess.state = "ready"
			}
			sess.mu.Unlock()

			if canProbe {
				m.runProbeAsync(sess)
			}
		}
	}
}

func (m *Manager) runProbeAsync(sess *Session) {
	sess.mu.Lock()
	var fileExt string
	if sess.selected >= 0 && sess.selected < len(sess.files) {
		fileExt = filepath.Ext(sess.files[sess.selected].DisplayPath())
	}
	sess.mu.Unlock()
	mediaURL := m.localBaseURL() + sess.mediaPath()
	ctx, cancel := context.WithTimeout(sess.ctx, 18*time.Second)
	defer cancel()
	p, err := probeMedia(ctx, mediaURL, fileExt)
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.probing = false
	if err != nil {
		sess.probeFailedAt = time.Now()
		return
	}
	sess.probe = p
	if sess.state == "buffering" {
		sess.state = "ready"
	}
}

func prioritizeFile(t *torrent.Torrent, files []*torrent.File, selectedIdx int) {
	for _, f := range t.Files() {
		f.SetPriority(torrent.PiecePriorityNone)
	}
	if selectedIdx < 0 || selectedIdx >= len(files) {
		return
	}
	f := files[selectedIdx]
	f.SetPriority(torrent.PiecePriorityNormal)
	info := t.Info()
	if info == nil || info.PieceLength <= 0 {
		f.Download()
		return
	}
	beginIdx := f.BeginPieceIndex()
	endIdx := f.EndPieceIndex()
	// Prioritize first ~6 MiB and final ~1 MiB (for MP4 moov atoms / MKV Cues).
	headPieces := int((6<<20)/info.PieceLength) + 1
	for i := beginIdx; i < endIdx && i < beginIdx+headPieces; i++ {
		if i == beginIdx {
			t.Piece(i).SetPriority(torrent.PiecePriorityNow)
		} else {
			t.Piece(i).SetPriority(torrent.PiecePriorityHigh)
		}
	}
	if endIdx-1 > beginIdx {
		t.Piece(endIdx - 1).SetPriority(torrent.PiecePriorityHigh)
	}
}

// filterVideoFiles keeps video files, sorting largest/natural order and dropping tiny samples when real videos exist.
func filterVideoFiles(all []*torrent.File) []*torrent.File {
	var vids []*torrent.File
	for _, f := range all {
		ext := strings.ToLower(filepath.Ext(f.DisplayPath()))
		if videoExts[ext] {
			vids = append(vids, f)
		}
	}
	if len(vids) <= 1 {
		return vids
	}
	var nonSample []*torrent.File
	for _, f := range vids {
		low := strings.ToLower(f.DisplayPath())
		if strings.Contains(low, "sample") && f.Length() < 80<<20 {
			continue
		}
		nonSample = append(nonSample, f)
	}
	if len(nonSample) > 0 {
		vids = nonSample
	}
	sort.Slice(vids, func(i, j int) bool {
		return vids[i].DisplayPath() < vids[j].DisplayPath()
	})
	return vids
}

var (
	reSeasonEp = regexp.MustCompile(`(?i)S(\d{1,2})\s*[.\-_ ]*E(\d{1,3})`)
	reAltEp    = regexp.MustCompile(`(?i)\b(\d{1,2})x(\d{1,3})\b`)
)

// pickBestVideoFile selects the matching episode (for shows) or largest video file.
func pickBestVideoFile(files []*torrent.File, kind string, season, episode int) int {
	if len(files) == 0 {
		return -1
	}
	if kind == "show" && season > 0 && episode > 0 {
		best := -1
		var bestSize int64
		for i, f := range files {
			name := filepath.Base(f.DisplayPath())
			s, e, ok := extractEpisodeNums(name)
			if ok && s == season && e == episode && f.Length() > bestSize {
				best = i
				bestSize = f.Length()
			}
		}
		if best >= 0 {
			return best
		}
	}
	best := 0
	for i := 1; i < len(files); i++ {
		if files[i].Length() > files[best].Length() {
			best = i
		}
	}
	return best
}

func extractEpisodeNums(name string) (season, episode int, ok bool) {
	if m := reSeasonEp.FindStringSubmatch(name); len(m) == 3 {
		s, _ := strconv.Atoi(m[1])
		e, _ := strconv.Atoi(m[2])
		return s, e, true
	}
	if m := reAltEp.FindStringSubmatch(name); len(m) == 3 {
		s, _ := strconv.Atoi(m[1])
		e, _ := strconv.Atoi(m[2])
		return s, e, true
	}
	return 0, 0, false
}

func (m *Manager) getSession(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

func (m *Manager) getAuthorizedSession(id, token string) (*Session, bool) {
	s, ok := m.getSession(id)
	if !ok || token == "" {
		return nil, false
	}
	if subtle.ConstantTimeCompare([]byte(s.token), []byte(token)) != 1 {
		return nil, false
	}
	return s, true
}

// ListSessions returns active sessions for userID.
func (m *Manager) ListSessions(userID int64) []SessionStatus {
	m.mu.Lock()
	var list []*Session
	for _, s := range m.sessions {
		if userID == 0 || s.userID == userID {
			list = append(list, s)
		}
	}
	m.mu.Unlock()
	out := make([]SessionStatus, 0, len(list))
	for _, s := range list {
		out = append(out, s.status())
	}
	return out
}

// Status returns the current telemetry for session id.
func (m *Manager) Status(id string) (SessionStatus, bool) {
	s, ok := m.getSession(id)
	if !ok {
		return SessionStatus{}, false
	}
	return s.status(), true
}

// SelectFile switches the active file index inside a multi-file torrent.
func (m *Manager) SelectFile(id string, idx int) (SessionStatus, error) {
	s, ok := m.getSession(id)
	if !ok {
		return SessionStatus{}, errors.New("stream session not found")
	}
	s.mu.Lock()
	if idx < 0 || idx >= len(s.files) {
		s.mu.Unlock()
		return SessionStatus{}, errors.New("file index out of range")
	}
	s.stopHLSLocked()
	s.selected = idx
	s.probe = nil
	s.probing = false
	s.probeFailedAt = time.Time{}
	s.state = "buffering"
	s.warmupAt = time.Now()
	s.lastActivity = time.Now()
	t := s.torrent
	files := s.files
	s.mu.Unlock()

	if t != nil {
		prioritizeFile(t, files, idx)
	}
	return s.status(), nil
}

// SetKeep updates whether the downloaded video file should be saved to KeepDir on stop.
func (m *Manager) SetKeep(id string, keep bool) (SessionStatus, error) {
	s, ok := m.getSession(id)
	if !ok {
		return SessionStatus{}, errors.New("stream session not found")
	}
	s.mu.Lock()
	s.keepFile = keep
	s.lastActivity = time.Now()
	s.mu.Unlock()
	return s.status(), nil
}

// Stop terminates session id, kills child processes, and cleans up temp storage unless keepFile saved a completed file.
func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if !ok {
		return nil
	}

	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.stopHLSLocked()
	if s.mpvCmd != nil && s.mpvCmd.Process != nil {
		_ = s.mpvCmd.Process.Kill()
	}
	t := s.torrent
	var keepSrc string
	if s.keepFile && s.selected >= 0 && s.selected < len(s.files) {
		f := s.files[s.selected]
		if f.Length() > 0 && f.BytesCompleted() >= f.Length() {
			keepSrc = filepath.Join(s.dir, "data", f.Path())
		}
	}
	sessDir := s.dir
	s.mu.Unlock()

	if t != nil {
		t.Drop()
	}
	if keepSrc != "" {
		keepDir := m.resolveKeepDir()
		if err := os.MkdirAll(keepDir, 0o755); err == nil {
			dst := filepath.Join(keepDir, filepath.Base(keepSrc))
			if err := os.Rename(keepSrc, dst); err != nil {
				log.Printf("stream: could not move kept file %s -> %s: %v", keepSrc, dst, err)
			}
		}
	}
	if sessDir != "" {
		_ = os.RemoveAll(sessDir)
	}
	return nil
}

// Close shuts down all active sessions and the shared torrent client.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	close(m.stopCh)
	var ids []string
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	for _, id := range ids {
		_ = m.Stop(id)
	}
	if m.client != nil {
		m.client.Close()
	}
	if m.CacheRoot != "" {
		_ = os.RemoveAll(filepath.Join(m.CacheRoot, "_client"))
		_ = os.Remove(m.CacheRoot) // removes only if empty
	}
}

func (m *Manager) reapLoop() {
	tk := time.NewTicker(time.Minute)
	defer tk.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-tk.C:
			m.mu.Lock()
			var stale []string
			now := time.Now()
			for id, s := range m.sessions {
				s.mu.Lock()
				idle := !s.mpvRunning && now.Sub(s.lastActivity) > 15*time.Minute
				s.mu.Unlock()
				if idle {
					stale = append(stale, id)
				}
			}
			m.mu.Unlock()
			for _, id := range stale {
				_ = m.Stop(id)
			}
		}
	}
}

func (s *Session) touch() {
	s.mu.Lock()
	s.lastActivity = time.Now()
	s.mu.Unlock()
}

func (s *Session) mediaPath() string {
	return "/api/v1/stream/" + s.id + "/media/" + s.token
}

func (s *Session) status() SessionStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := SessionStatus{
		ID:           s.id,
		Token:        s.token,
		Title:        s.title,
		ReleaseTitle: s.releaseTitle,
		SourceID:     s.sourceID,
		SourceName:   s.sourceName,
		Kind:         s.kind,
		Season:       s.season,
		Episode:      s.episode,
		State:        s.state,
		Error:        s.errMsg,
		SelectedFile: s.selected,
		KeepFile:     s.keepFile,
		MPVRunning:   s.mpvRunning,
		MediaURL:     s.mediaPath(),
		Probe:        s.probe,
		Files:        make([]VideoFile, 0, len(s.files)),
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

func slicesClone(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
