package stream

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
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
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/secrets"
	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"
	"github.com/HolyArrs/holyarrs/app/backend/internal/metadata"
	"github.com/HolyArrs/holyarrs/app/backend/internal/sources"
	"github.com/HolyArrs/holyarrs/app/backend/internal/subtitles"
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

// Manager owns the shared BitTorrent client, active streaming sessions, and
// external source/debrid/plugin/sync subsystems.
type Manager struct {
	Store      *database.Store
	Secrets    sources.SecretReader
	Vault       *secrets.Vault
	Trakt       *metadata.TraktAuth
	ResolveIMDb func(ctx context.Context, kind, tmdbID string) string
	Subtitles   *subtitles.Service
	History    *HistoryStore
	Prefs      *PrefsStore
	Addons     *AddonStore
	Plugins    *PluginStore
	Debrid     *DebridService
	Proxy       *ProxyManager
	CandCache   *CandidateCache
	SearchCache *SearchCache
	Health      *ProviderHealth
	Skip        *SkipService
	Scrobble   *ScrobbleService
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

// NewManager creates a stream Manager and starts its idle-session reaper.
func NewManager(store *database.Store, sr sources.SecretReader, configDir string, listenAddr func() string) *Manager {
	prefs := NewPrefsStore(configDir)
	var vault *secrets.Vault
	if v, ok := sr.(*secrets.Vault); ok {
		vault = v
	}
	m := &Manager{
		Store:      store,
		Secrets:    sr,
		Vault:      vault,
		History:    NewHistoryStore(configDir),
		Prefs:      prefs,
		Addons:     NewAddonStore(configDir),
		Plugins:    NewPluginStore(configDir),
		Debrid:     NewDebridService(vault),
		Proxy:       NewProxyManager(),
		CandCache:   NewCandidateCache(),
		SearchCache: NewSearchCache(),
		Health:      NewProviderHealth(),
		Skip:        NewSkipService(),
		Scrobble:   NewScrobbleService(vault, nil, prefs),
		ConfigDir:  configDir,
		ListenAddr: listenAddr,
		sessions:   map[string]*Session{},
		stopCh:     make(chan struct{}),
	}
	go m.reapLoop()
	return m
}

// SetExtras attaches Vault and TraktAuth references constructed during server startup.
func (m *Manager) SetExtras(vault *secrets.Vault, trakt *metadata.TraktAuth) {
	if m == nil {
		return
	}
	if vault != nil {
		m.Vault = vault
	}
	if trakt != nil {
		m.Trakt = trakt
	}
	if m.Debrid != nil && vault != nil {
		m.Debrid.SetVault(vault)
	}
	if m.Scrobble != nil {
		m.Scrobble.SetExtras(vault, trakt)
	}
}

func (m *Manager) resolveCacheRoot() string {
	if m.CacheRoot != "" {
		return m.CacheRoot
	}
	if m.Store != nil {
		if path, err := mediafiles.FirstLibraryFolderPath(context.Background(), m.Store.Read()); err == nil && path != "" {
			parent := filepath.Dir(filepath.Clean(path))
			cand := filepath.Join(parent, ".stream-cache")
			if err := os.MkdirAll(cand, 0o750); err == nil {
				probe := filepath.Join(cand, ".write-probe")
				if f, err := os.Create(probe); err == nil {
					_ = f.Close()
					_ = os.Remove(probe)
					m.CacheRoot = cand
					return cand
				}
			}
		}
	}
	if m.ConfigDir != "" {
		cand := filepath.Join(m.ConfigDir, "stream-cache")
		if err := os.MkdirAll(cand, 0o750); err == nil {
			m.CacheRoot = cand
			return cand
		}
	}
	cand := filepath.Join(os.TempDir(), "holyarrs-stream")
	_ = os.MkdirAll(cand, 0o750)
	m.CacheRoot = cand
	return cand
}

func (m *Manager) resolveKeepDir() string {
	if m.KeepDir != "" {
		return m.KeepDir
	}
	if m.Store != nil {
		if path, err := mediafiles.FirstLibraryFolderPath(context.Background(), m.Store.Read()); err == nil && path != "" {
			return filepath.Join(filepath.Clean(path), "_Streamed")
		}
	}
	return filepath.Join(m.resolveCacheRoot(), "saved")
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
	srcName := strings.TrimSpace(req.SourceName)
	sess := &Session{
		id:             id,
		token:          token,
		userID:         userID,
		title:          strings.TrimSpace(req.Title),
		releaseTitle:   strings.TrimSpace(req.ReleaseTitle),
		sourceID:       req.SourceID,
		sourceName:     srcName,
		sourceProvider: fallbackString(srcName, "Torrent"),
		transport:      "torrent",
		kind:           kind,
		season:         req.Season,
		episode:        req.Episode,
		dir:            sessDir,
		keepFile:       req.KeepFile,
		createdAt:      now,
		ctx:            sctx,
		cancel:         cancel,
		state:          "resolving",
		selected:       -1,
		torrent:        t,
		lastActivity:   now,
		prevAt:         now,
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

// SaveSessionSubtitle saves a converted WebVTT subtitle inside session id's cache directory
// so desktop mpv can load it securely via --sub-file.
func (m *Manager) SaveSessionSubtitle(id string, vtt []byte) (string, error) {
	if len(vtt) == 0 {
		return "", errors.New("empty subtitle data")
	}
	s, ok := m.getSession(id)
	if !ok {
		return "", errors.New("stream session not found")
	}
	s.mu.Lock()
	sessDir := s.dir
	closed := s.closed
	s.mu.Unlock()
	if closed || sessDir == "" {
		return "", errors.New("stream session closed")
	}
	subsDir := filepath.Join(sessDir, "subs")
	if err := os.MkdirAll(subsDir, 0o750); err != nil {
		return "", err
	}
	sum := sha256.Sum256(vtt)
	outPath := filepath.Join(subsDir, "ext_"+hex.EncodeToString(sum[:8])+".vtt")
	if err := os.WriteFile(outPath, vtt, 0o640); err != nil {
		return "", err
	}
	return outPath, nil
}

// Stop terminates session id, kills child processes, revokes any proxy token,
// and cleans up temp storage unless keepFile saved a completed file.
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
	proxyTok := s.proxyToken
	s.proxyToken = ""
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

	if proxyTok != "" && m.Proxy != nil {
		m.Proxy.Revoke(proxyTok)
	}
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
		_ = os.Remove(m.CacheRoot)
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
