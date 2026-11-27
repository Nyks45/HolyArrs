package stream

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"
)

func looksIncompatibleForDirectPlay(releaseTitle, streamURL, contentType string) bool {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "matroska") || strings.Contains(ct, "x-msvideo") ||
		strings.Contains(ct, "mpegts") || strings.Contains(ct, "mp2t") {
		return true
	}
	hay := strings.ToLower(releaseTitle + " " + streamURL)
	for _, tok := range []string{
		".mkv", ".avi", ".ts", ".m2ts",
		"hevc", "x265", "h265", "h.265",
		"hi10", "main10", "av01", ".av1", " av1", "[av1", "(av1", "-av1",
		"10bit", "10-bit", "dovi", "dolby vision", "hdr10",
		"ddp", "eac3", "e-ac-3", "ac3", "dts", "truehd", "atmos", "flac",
		" dv ", ".dv.", "[dv", "(dv",
	} {
		if strings.Contains(hay, tok) {
			return true
		}
	}
	return false
}

// StartLocal starts an instant playback session backed directly by a present
// file inside the user's Library folders.
func (m *Manager) StartLocal(ctx context.Context, userID int64, file mediafiles.LocalStreamFile, req StartReq) (SessionStatus, error) {
	if file.FullPath == "" {
		return SessionStatus{}, errors.New("local library file path is empty")
	}
	st, err := os.Stat(file.FullPath)
	if err != nil || st.IsDir() {
		return SessionStatus{}, errors.New("local library file could not be read")
	}

	m.stopUserSessions(userID)

	id := randomHex(8)
	tok := randomHex(16)
	cacheRoot := m.resolveCacheRoot()
	sessDir := filepath.Join(cacheRoot, id)
	if err := os.MkdirAll(sessDir, 0o750); err != nil {
		return SessionStatus{}, err
	}

	sctx, cancel := context.WithCancel(context.Background())
	now := time.Now()
	kind := req.Kind
	if kind != "show" {
		kind = "movie"
	}
	relTitle := req.ReleaseTitle
	if relTitle == "" {
		relTitle = filepath.Base(file.FullPath)
	}
	title := req.Title
	if title == "" {
		title = file.Name
	}

	sess := &Session{
		id:             id,
		token:          tok,
		userID:         userID,
		title:          title,
		releaseTitle:   relTitle,
		sourceName:     "Local Library",
		sourceProvider: "Local Library",
		transport:      "local",
		kind:           kind,
		season:         req.Season,
		episode:        req.Episode,
		dir:            sessDir,
		createdAt:      now,
		ctx:            sctx,
		cancel:         cancel,
		state:          "ready",
		selected:       0,
		localFilePath:  file.FullPath,
		fileSize:       st.Size(),
		needsTranscode: looksIncompatibleForDirectPlay(relTitle, file.FullPath, ""),
		lastActivity:   now,
		warmupAt:       now,
		prevAt:         now,
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		cancel()
		_ = os.RemoveAll(sessDir)
		return SessionStatus{}, errors.New("stream manager closed")
	}
	m.sessions[id] = sess
	m.mu.Unlock()

	go m.probeLocalOrRemoteAsync(sess)
	return sess.status(), nil
}

// StartRemote starts an instant playback session for an HTTP or HLS stream,
// terminating upstream headers cleanly through ProxyManager.
func (m *Manager) StartRemote(ctx context.Context, userID int64, streamURL string, headers map[string]string, isHLS bool, provider string, req StartReq) (SessionStatus, error) {
	return m.startRemote(ctx, userID, streamURL, headers, isHLS, provider, req, false, false)
}

// StartVerifiedRemote starts a cached remote candidate session without repeating
// upstream preflight when the candidate was already verified during search.
func (m *Manager) StartVerifiedRemote(ctx context.Context, userID int64, cand UnifiedCandidate, req StartReq) (SessionStatus, error) {
	isHLS := cand.Kind == "hls" || looksLikeHLSURL(cand.StreamURL)
	return m.startRemote(ctx, userID, cand.StreamURL, cand.Headers, isHLS, cand.Provider, req, cand.Verified, cand.FastStart)
}

func (m *Manager) startRemote(ctx context.Context, userID int64, streamURL string, headers map[string]string, isHLS bool, provider string, req StartReq, verified bool, fastStart bool) (SessionStatus, error) {
	if m.Proxy == nil {
		return SessionStatus{}, errors.New("stream proxy unavailable")
	}
	if !isHLS && looksLikeHLSURL(streamURL) {
		isHLS = true
	}
	relTitle := fallbackString(req.ReleaseTitle, req.Title)
	var ct string
	if !verified {
		t0 := time.Now()
		pr, err := m.Proxy.PreflightDetailed(ctx, streamURL, headers)
		elapsed := time.Since(t0)
		provKey := fallbackString(provider, req.SourceName)
		if err != nil {
			if m.Health != nil {
				m.Health.RecordFailure(provKey)
			}
			return SessionStatus{}, err
		}
		if m.Health != nil {
			m.Health.RecordSuccess(provKey, elapsed)
		}
		ct = pr.ContentType
		if pr.IsHLS {
			isHLS = true
		}
	}
	proxyTok, _, err := m.Proxy.Register(streamURL, headers, userID)
	if err != nil {
		return SessionStatus{}, err
	}

	m.stopUserSessions(userID)

	id := randomHex(8)
	tok := randomHex(16)
	cacheRoot := m.resolveCacheRoot()
	sessDir := filepath.Join(cacheRoot, id)
	if err := os.MkdirAll(sessDir, 0o750); err != nil {
		m.Proxy.Revoke(proxyTok)
		return SessionStatus{}, err
	}

	transport := "direct"
	if isHLS {
		transport = "hls_remote"
	}
	if provider == "" {
		provider = fallbackString(req.SourceName, "Instant Stream")
	}
	kind := req.Kind
	if kind != "show" {
		kind = "movie"
	}
	needsTranscode := !isHLS && looksIncompatibleForDirectPlay(relTitle, streamURL, ct)
	if verified {
		needsTranscode = !isHLS && (!fastStart || looksIncompatibleForDirectPlay(relTitle, streamURL, ""))
	}
	sctx, cancel := context.WithCancel(context.Background())
	now := time.Now()
	sess := &Session{
		id:             id,
		token:          tok,
		userID:         userID,
		title:          req.Title,
		releaseTitle:   relTitle,
		sourceID:       req.SourceID,
		sourceName:     provider,
		sourceProvider: provider,
		transport:      transport,
		kind:           kind,
		season:         req.Season,
		episode:        req.Episode,
		dir:            sessDir,
		createdAt:      now,
		ctx:            sctx,
		cancel:         cancel,
		state:          "ready",
		selected:       -1,
		proxyToken:     proxyTok,
		proxyIsHLS:     isHLS,
		needsTranscode: needsTranscode,
		lastActivity:   now,
		warmupAt:       now,
		prevAt:         now,
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		cancel()
		m.Proxy.Revoke(proxyTok)
		_ = os.RemoveAll(sessDir)
		return SessionStatus{}, errors.New("stream manager closed")
	}
	m.sessions[id] = sess
	m.mu.Unlock()

	if !isHLS {
		go m.probeLocalOrRemoteAsync(sess)
	}
	return sess.status(), nil
}

func (m *Manager) probeLocalOrRemoteAsync(sess *Session) {
	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		return
	}
	target, fileExt, _, ok := sess.localOrMediaTargetLocked(m.localBaseURL())
	if !ok {
		sess.mu.Unlock()
		return
	}
	sess.probing = true
	sess.mu.Unlock()

	pctx, cancel := context.WithTimeout(sess.ctx, 18*time.Second)
	defer cancel()
	p, err := probeMedia(pctx, target, fileExt)

	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.probing = false
	if err != nil {
		sess.probeFailedAt = time.Now()
		return
	}
	sess.probe = p
	if p != nil && len(p.Chapters) > 0 {
		sess.chapters = p.Chapters
	}
}
