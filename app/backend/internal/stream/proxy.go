package stream

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/outbound"
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/webserver"
)

const defaultStreamUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

type proxyEntry struct {
	target    string
	headers   map[string]string
	userID    int64
	expiresAt time.Time
}

// ProxyManager terminates authenticated remote HTTP/HLS streams behind local
// token URLs so upstream headers (Referer, Origin, User-Agent, Cookie) stay on
// the backend and all outbound requests pass SSRF policy checks.
type ProxyManager struct {
	mu      sync.RWMutex
	entries map[string]proxyEntry
	client  *http.Client
	hls     *http.Client
}

// NewProxyManager builds an SSRF-guarded HTTP/HLS reverse proxy.
func NewProxyManager() *ProxyManager {
	tr := &http.Transport{
		DialContext:           outbound.PolicyDialer(outbound.NotMetadata).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
	}
	redirPolicy := func(req *http.Request, via []*http.Request) error {
		if len(via) >= 6 {
			return errors.New("too many redirects")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return errors.New("disallowed redirect scheme")
		}
		return nil
	}
	return &ProxyManager{
		entries: map[string]proxyEntry{},
		client:  &http.Client{Transport: tr, CheckRedirect: redirPolicy},
		hls:     &http.Client{Transport: tr, Timeout: 25 * time.Second, CheckRedirect: redirPolicy},
	}
}

// SetHTTPClient overrides upstream HTTP clients (intended for unit tests).
func (p *ProxyManager) SetHTTPClient(hc *http.Client) {
	if p == nil || hc == nil {
		return
	}
	p.mu.Lock()
	p.client = hc
	p.hls = hc
	p.mu.Unlock()
}

// Register creates a short-lived local proxy token for targetURL and headers.
func (p *ProxyManager) Register(targetURL string, headers map[string]string, userID int64) (string, time.Time, error) {
	u, err := url.Parse(strings.TrimSpace(targetURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", time.Time{}, errors.New("invalid stream URL")
	}
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", time.Time{}, err
	}
	tok := hex.EncodeToString(b[:])
	exp := time.Now().Add(6 * time.Hour)
	hdrCopy := cloneHeaderMap(headers)

	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.entries) > 512 {
		now := time.Now()
		for k, v := range p.entries {
			if now.After(v.expiresAt) {
				delete(p.entries, k)
			}
		}
	}
	p.entries[tok] = proxyEntry{
		target:    u.String(),
		headers:   hdrCopy,
		userID:    userID,
		expiresAt: exp,
	}
	return tok, exp, nil
}

// Revoke deletes a registered proxy token.
func (p *ProxyManager) Revoke(token string) {
	if p == nil || token == "" {
		return
	}
	p.mu.Lock()
	delete(p.entries, token)
	p.mu.Unlock()
}

func (p *ProxyManager) lookup(token string) (proxyEntry, bool) {
	if p == nil || token == "" || token == "poster" {
		return proxyEntry{}, false
	}
	now := time.Now()
	p.mu.RLock()
	e, ok := p.entries[token]
	p.mu.RUnlock()
	if !ok {
		return proxyEntry{}, false
	}
	if now.After(e.expiresAt) {
		p.Revoke(token)
		return proxyEntry{}, false
	}
	return e, true
}

// LookupTarget returns the resolved target URL and stored headers for a valid token.
func (p *ProxyManager) LookupTarget(token string) (string, map[string]string, bool) {
	e, ok := p.lookup(token)
	if !ok {
		return "", nil, false
	}
	return e.target, cloneHeaderMap(e.headers), true
}

// Preflight checks that a remote stream URL (plus any master->variant->segment HLS chain)
// returns playable non-HTML media using the stored request headers before starting a session.
func (p *ProxyManager) Preflight(ctx context.Context, targetURL string, storedHeaders map[string]string) (contentType string, isHLS bool, err error) {
	pr, err := p.PreflightDetailed(ctx, targetURL, storedHeaders)
	return pr.ContentType, pr.IsHLS, err
}

// PreflightDetailed inspects remote stream headers, disguised container magic bytes,
// HLS variant chains, and stream duration without downloading the full asset.
func (p *ProxyManager) PreflightDetailed(ctx context.Context, targetURL string, storedHeaders map[string]string) (PreflightResult, error) {
	var res PreflightResult
	if p == nil {
		return res, errors.New("stream proxy unavailable")
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(cctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return res, errors.New("invalid stream URL")
	}
	var reqHdr http.Header
	if !looksLikeHLSURL(targetURL) {
		reqHdr = http.Header{"Range": []string{"bytes=0-16383"}}
	}
	applyProxyHeaders(req, storedHeaders, reqHdr)

	resp, err := p.hls.Do(req)
	if err != nil {
		return res, fmt.Errorf("source unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return res, fmt.Errorf("source link unavailable (HTTP %d)", resp.StatusCode)
	}
	ct := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	res.ContentType = ct
	peek, _ := io.ReadAll(io.LimitReader(resp.Body, 16384))
	headSample := peek[:min(512, len(peek))]
	isPlaylist := strings.Contains(ct, "mpegurl") || bytes.Contains(headSample, []byte("#EXTM3U"))
	if isPlaylist {
		const maxList = 1 << 20
		rest, _ := io.ReadAll(io.LimitReader(resp.Body, maxList))
		body := string(append(peek, rest...))
		if !strings.Contains(body, "#EXTM3U") {
			return res, errors.New("source returned an invalid playlist")
		}
		durSec, err := p.preflightHLSChain(cctx, resp.Request.URL, body, len(rest) == maxList, storedHeaders)
		if err != nil {
			return res, err
		}
		res.IsHLS = true
		res.DurationSec = durSec
		return res, nil
	}
	if looksDisallowedMediaBody(ct, headSample) {
		return res, errors.New("source returned a web page instead of a playable stream")
	}
	ct = detectDisguisedContainer(ct, peek)
	res.ContentType = ct
	res.TotalSize = parseContentRangeTotal(resp.StatusCode, resp.Header.Get("Content-Range"), resp.Header.Get("Content-Length"))
	if strings.Contains(ct, "matroska") || strings.Contains(ct, "webm") {
		res.DurationSec = parseEBMLDuration(peek)
	} else if len(peek) >= 8 && string(peek[4:8]) == "ftyp" {
		res.DurationSec = parseMP4Duration(peek)
	}
	return res, nil
}

func looksDisallowedMediaBody(ct string, peek []byte) bool {
	sample := strings.ToLower(strings.TrimSpace(string(peek)))
	return strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xml") ||
		strings.Contains(ct, "text/xml") || strings.Contains(ct, "application/json") ||
		strings.Contains(ct, "text/plain") ||
		strings.HasPrefix(sample, "<!doctype") || strings.HasPrefix(sample, "<html") ||
		strings.HasPrefix(sample, "<head") || strings.HasPrefix(sample, "<?xml") ||
		strings.HasPrefix(sample, `{"error"`) || strings.HasPrefix(sample, `{"code"`) ||
		strings.HasPrefix(sample, "unable to get")
}

func firstHLSDataLine(body string) string {
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			return line
		}
	}
	return ""
}

func resolveRelativeHLSURL(base *url.URL, raw string) string {
	ref, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || base == nil {
		return ""
	}
	abs := base.ResolveReference(ref)
	if abs.Scheme != "http" && abs.Scheme != "https" {
		return ""
	}
	return abs.String()
}

func (p *ProxyManager) preflightHLSChain(ctx context.Context, baseURL *url.URL, playlist string, truncated bool, storedHeaders map[string]string) (float64, error) {
	mediaBody := playlist
	mediaBase := baseURL
	mediaTruncated := truncated
	if strings.Contains(playlist, "#EXT-X-STREAM-INF") {
		subLine := firstHLSDataLine(playlist)
		if subLine == "" {
			return 0, errors.New("master playlist contains no variants")
		}
		subURL := resolveRelativeHLSURL(baseURL, subLine)
		if subURL == "" {
			return 0, errors.New("invalid variant playlist URL")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, subURL, nil)
		if err != nil {
			return 0, errors.New("invalid variant playlist URL")
		}
		applyProxyHeaders(req, storedHeaders, nil)
		resp, err := p.hls.Do(req)
		if err != nil {
			return 0, fmt.Errorf("variant playlist unreachable: %w", err)
		}
		ct := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
		const maxList = 1 << 20
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxList))
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 || !bytes.Contains(raw, []byte("#EXTM3U")) || looksDisallowedMediaBody(ct, raw[:min(256, len(raw))]) {
			return 0, errors.New("source variant returned a web page instead of a playable stream")
		}
		mediaBody = string(raw)
		mediaBase = resp.Request.URL
		mediaTruncated = len(raw) == maxList
	}

	durSec := parseHLSPlaylistDuration(mediaBody, mediaTruncated)
	segLine := firstHLSDataLine(mediaBody)
	if segLine == "" {
		return durSec, nil
	}
	segURL := resolveRelativeHLSURL(mediaBase, segLine)
	if segURL == "" {
		return durSec, nil
	}
	sreq, err := http.NewRequestWithContext(ctx, http.MethodGet, segURL, nil)
	if err != nil {
		return 0, errors.New("invalid segment URL")
	}
	applyProxyHeaders(sreq, storedHeaders, http.Header{"Range": []string{"bytes=0-1023"}})
	sresp, err := p.hls.Do(sreq)
	if err != nil {
		return 0, fmt.Errorf("media segment unreachable: %w", err)
	}
	defer sresp.Body.Close()
	if sresp.StatusCode < 200 || sresp.StatusCode >= 300 {
		return 0, fmt.Errorf("media segment unavailable (HTTP %d)", sresp.StatusCode)
	}
	sct := strings.ToLower(strings.TrimSpace(sresp.Header.Get("Content-Type")))
	speek, _ := io.ReadAll(io.LimitReader(sresp.Body, 1024))
	if looksDisallowedMediaBody(sct, speek) {
		return 0, errors.New("source segment returned a web page instead of media")
	}
	return durSec, nil
}

// ServeProxy streams the primary proxied URL (rewriting .m3u8 manifests automatically).
func (p *ProxyManager) ServeProxy(w http.ResponseWriter, r *http.Request) {
	tok := r.PathValue("token")
	entry, ok := p.lookup(tok)
	if !ok {
		http.NotFound(w, r)
		return
	}
	p.proxyUpstream(w, r, tok, entry.target, entry.headers, false)
}

// ServeProxyHLS handles rewritten nested playlists, keys, init maps, and media segments.
func (p *ProxyManager) ServeProxyHLS(w http.ResponseWriter, r *http.Request) {
	tok := r.PathValue("token")
	entry, ok := p.lookup(tok)
	if !ok {
		http.NotFound(w, r)
		return
	}
	rawU := strings.TrimSpace(r.URL.Query().Get("u"))
	if rawU == "" {
		http.NotFound(w, r)
		return
	}
	decoded, err := base64.RawURLEncoding.DecodeString(rawU)
	if err != nil {
		http.Error(w, "invalid target encoding", http.StatusBadRequest)
		return
	}
	target := string(decoded)
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		http.Error(w, "invalid upstream target", http.StatusBadRequest)
		return
	}
	p.proxyUpstream(w, r, tok, u.String(), entry.headers, true)
}

func (p *ProxyManager) proxyUpstream(w http.ResponseWriter, r *http.Request, tok, targetURL string, storedHeaders map[string]string, isHLSChild bool) {
	method := r.Method
	if method != http.MethodGet && method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), method, targetURL, nil)
	if err != nil {
		http.Error(w, "invalid target URL", http.StatusBadRequest)
		return
	}
	applyProxyHeaders(req, storedHeaders, r.Header)

	isMaybeHLS := looksLikeHLSURL(targetURL)
	hc := p.client
	if isMaybeHLS {
		hc = p.hls
	}
	resp, err := hc.Do(req)
	if err != nil {
		webserver.Fail(w, http.StatusBadGateway, "upstream stream request failed")
		return
	}
	defer resp.Body.Close()

	setStreamCORS(w)
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	isManifest := method == http.MethodGet && resp.StatusCode >= 200 && resp.StatusCode < 300 &&
		(strings.Contains(ct, "mpegurl") || isMaybeHLS)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 && !isManifest &&
		(strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xml") || strings.Contains(ct, "text/xml")) {
		webserver.Fail(w, http.StatusBadGateway, "upstream returned web page instead of media")
		return
	}

	if isManifest {
		const maxManifest = 4 << 20
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxManifest+1))
		if err != nil {
			webserver.Fail(w, http.StatusBadGateway, "failed to read HLS manifest")
			return
		}
		if len(body) > maxManifest {
			webserver.Fail(w, http.StatusBadGateway, "HLS manifest exceeds limit")
			return
		}
		trimmed := bytes.TrimSpace(body)
		if bytes.HasPrefix(trimmed, []byte("#EXTM3U")) || strings.Contains(ct, "mpegurl") {
			finalBase := resp.Request.URL
			rewritten := rewriteHLSManifest(string(body), finalBase, tok)
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(resp.StatusCode)
			_, _ = io.WriteString(w, rewritten)
			return
		}
		copyUpstreamHeaders(w.Header(), resp.Header)
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(body)
		return
	}

	if isHLSChild && method == http.MethodGet && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		peek := make([]byte, 8192)
		n, rerr := io.ReadFull(resp.Body, peek)
		if rerr != nil && !errors.Is(rerr, io.ErrUnexpectedEOF) && !errors.Is(rerr, io.EOF) {
			webserver.Fail(w, http.StatusBadGateway, "upstream stream read failed")
			return
		}
		peek = peek[:n]
		if bytes.HasPrefix(bytes.TrimSpace(peek), []byte("#EXTM3U")) {
			const maxManifest = 4 << 20
			body := peek
			if rerr == nil {
				rest, rErr := io.ReadAll(io.LimitReader(resp.Body, int64(maxManifest-len(peek)+1)))
				if rErr != nil {
					webserver.Fail(w, http.StatusBadGateway, "failed to read HLS manifest")
					return
				}
				body = append(body, rest...)
			}
			if len(body) > maxManifest {
				webserver.Fail(w, http.StatusBadGateway, "HLS manifest exceeds limit")
				return
			}
			rewritten := rewriteHLSManifest(string(body), resp.Request.URL, tok)
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(resp.StatusCode)
			_, _ = io.WriteString(w, rewritten)
			return
		}
		if off := disguisedTSOffset(ct, peek); off >= 0 && (off > 0 || strings.HasPrefix(ct, "image/")) {
			copyUpstreamHeaders(w.Header(), resp.Header)
			w.Header().Set("Content-Type", "video/mp2t")
			if off > 0 {
				if cl, perr := strconv.ParseInt(w.Header().Get("Content-Length"), 10, 64); perr == nil && cl > int64(off) {
					w.Header().Set("Content-Length", strconv.FormatInt(cl-int64(off), 10))
				} else {
					w.Header().Del("Content-Length")
				}
			}
			w.Header().Set("Cache-Control", "no-store")
			if w.Header().Get("Accept-Ranges") == "" {
				w.Header().Set("Accept-Ranges", "bytes")
			}
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write(peek[off:])
			if rerr == nil {
				_, _ = io.Copy(w, resp.Body)
			}
			return
		}
		copyUpstreamHeaders(w.Header(), resp.Header)
		w.Header().Set("Cache-Control", "no-store")
		if w.Header().Get("Accept-Ranges") == "" {
			w.Header().Set("Accept-Ranges", "bytes")
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(peek)
		if rerr == nil {
			_, _ = io.Copy(w, resp.Body)
		}
		return
	}

	copyUpstreamHeaders(w.Header(), resp.Header)
	w.Header().Set("Cache-Control", "no-store")
	if w.Header().Get("Accept-Ranges") == "" {
		w.Header().Set("Accept-Ranges", "bytes")
	}
	w.WriteHeader(resp.StatusCode)
	if method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, resp.Body)
}

func isMP4OrAACPrefix(b []byte) bool {
	if len(b) >= 8 {
		box := string(b[4:8])
		if box == "ftyp" || box == "styp" || box == "sidx" || box == "moof" {
			return true
		}
	}
	return bytes.HasPrefix(b, []byte("ID3")) || bytes.HasPrefix(b, []byte("WEBVTT"))
}

func findMPEGTSOffset(buf []byte) int {
	if len(buf) < 377 {
		return -1
	}
	lim := len(buf) - 377
	for i := 0; i <= lim; i++ {
		if buf[i] == 0x47 && buf[i+188] == 0x47 && buf[i+376] == 0x47 {
			return i
		}
	}
	return -1
}

func disguisedTSOffset(ct string, peek []byte) int {
	if len(peek) < 377 || isMP4OrAACPrefix(peek) {
		return -1
	}
	if !strings.HasPrefix(ct, "image/") && peek[0] == 0x47 {
		return -1
	}
	return findMPEGTSOffset(peek)
}

// ServePosterProxy fetches remote addon logos/posters through SSRF checks so
// strict image CSP rules ('self') allow addon artwork without opening arbitrary origins.
func (p *ProxyManager) ServePosterProxy(w http.ResponseWriter, r *http.Request) {
	rawURL := strings.TrimSpace(r.URL.Query().Get("url"))
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	req.Header.Set("User-Agent", defaultStreamUserAgent)
	resp, err := p.hls.Do(req)
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp != nil {
			resp.Body.Close()
		}
		http.NotFound(w, r)
		return
	}
	defer resp.Body.Close()
	ct := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0]))
	if !strings.HasPrefix(ct, "image/") {
		http.NotFound(w, r)
		return
	}
	const maxImage = 8 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImage))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}

var (
	reHLSAttrURI      = regexp.MustCompile(`URI="([^"]+)"`)
	reHLSGroupID      = regexp.MustCompile(`GROUP-ID="([^"]+)"`)
	reHLSLang         = regexp.MustCompile(`(?i)LANGUAGE="([^"]+)"`)
	reHLSStreamSubGrp = regexp.MustCompile(`,?SUBTITLES="([^"]+)"`)
)

func sanitizeHLSManifestLines(body string) []string {
	var rawLines []string
	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	hasEnglishAudio := false
	hasNonEngDefaultAudio := false
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "#EXT-X-MEDIA:") && strings.Contains(line, "TYPE=AUDIO") {
			lang := ""
			if m := reHLSLang.FindStringSubmatch(line); len(m) == 2 {
				lang = strings.ToLower(strings.TrimSpace(m[1]))
			}
			if lang == "en" || lang == "eng" {
				hasEnglishAudio = true
			} else if strings.Contains(strings.ToUpper(line), "DEFAULT=YES") {
				hasNonEngDefaultAudio = true
			}
		}
		rawLines = append(rawLines, line)
	}

	droppedSubGroups := map[string]bool{}
	keptSubGroups := map[string]bool{}
	filtered := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		tr := strings.TrimSpace(line)
		if strings.HasPrefix(tr, "#EXT-X-MEDIA:") && strings.Contains(tr, "TYPE=SUBTITLES") {
			uri := ""
			if m := reHLSAttrURI.FindStringSubmatch(line); len(m) == 2 {
				uri = strings.ToLower(strings.Split(m[1], "?")[0])
			}
			grp := ""
			if m := reHLSGroupID.FindStringSubmatch(line); len(m) == 2 {
				grp = m[1]
			}
			if strings.HasSuffix(uri, ".vtt") || strings.HasSuffix(uri, ".srt") {
				if grp != "" {
					droppedSubGroups[grp] = true
				}
				continue
			}
			if grp != "" {
				keptSubGroups[grp] = true
			}
		}
		if hasEnglishAudio && hasNonEngDefaultAudio && strings.HasPrefix(tr, "#EXT-X-MEDIA:") && strings.Contains(tr, "TYPE=AUDIO") {
			lang := ""
			if m := reHLSLang.FindStringSubmatch(line); len(m) == 2 {
				lang = strings.ToLower(strings.TrimSpace(m[1]))
			}
			if lang == "en" || lang == "eng" {
				line = strings.ReplaceAll(line, "DEFAULT=NO", "DEFAULT=YES")
				line = strings.ReplaceAll(line, "AUTOSELECT=NO", "AUTOSELECT=YES")
			} else {
				line = strings.ReplaceAll(line, "DEFAULT=YES", "DEFAULT=NO")
				line = strings.ReplaceAll(line, "AUTOSELECT=YES", "AUTOSELECT=NO")
			}
		}
		filtered = append(filtered, line)
	}

	for i, line := range filtered {
		if strings.HasPrefix(strings.TrimSpace(line), "#EXT-X-STREAM-INF:") {
			filtered[i] = reHLSStreamSubGrp.ReplaceAllStringFunc(line, func(m string) string {
				sub := reHLSStreamSubGrp.FindStringSubmatch(m)
				if len(sub) == 2 && droppedSubGroups[sub[1]] && !keptSubGroups[sub[1]] {
					return ""
				}
				return m
			})
		}
	}
	return filtered
}

func rewriteHLSManifest(body string, base *url.URL, token string) string {
	lines := sanitizeHLSManifestLines(body)
	var out strings.Builder
	out.Grow(len(body) + len(body)/4)

	rewriteURL := func(raw string) string {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.HasPrefix(raw, "data:") {
			return raw
		}
		ref, err := url.Parse(raw)
		if err != nil {
			return raw
		}
		abs := base.ResolveReference(ref)
		if abs.Scheme != "http" && abs.Scheme != "https" {
			return raw
		}
		enc := base64.RawURLEncoding.EncodeToString([]byte(abs.String()))
		return "/api/v1/streaming/proxy/" + token + "/hls?u=" + enc
	}

	for i, line := range lines {
		if i > 0 {
			out.WriteByte('\n')
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			if strings.Contains(line, `URI="`) {
				line = reHLSAttrURI.ReplaceAllStringFunc(line, func(m string) string {
					sub := reHLSAttrURI.FindStringSubmatch(m)
					if len(sub) != 2 {
						return m
					}
					return `URI="` + rewriteURL(sub[1]) + `"`
				})
			}
			out.WriteString(line)
			continue
		}
		out.WriteString(rewriteURL(trimmed))
	}
	return out.String()
}

func applyProxyHeaders(dst *http.Request, stored map[string]string, clientHdr http.Header) {
	uaSet := false
	for k, v := range stored {
		canon := http.CanonicalHeaderKey(strings.TrimSpace(k))
		if canon == "" || v == "" {
			continue
		}
		switch canon {
		case "Host", "Content-Length", "Connection", "Transfer-Encoding", "Accept-Encoding":
			continue
		case "User-Agent":
			uaSet = true
		}
		dst.Header.Set(canon, v)
	}
	if !uaSet {
		dst.Header.Set("User-Agent", defaultStreamUserAgent)
	}
	if rng := clientHdr.Get("Range"); rng != "" {
		dst.Header.Set("Range", rng)
	}
	if ifr := clientHdr.Get("If-Range"); ifr != "" {
		dst.Header.Set("If-Range", ifr)
	}
}

func copyUpstreamHeaders(dst, src http.Header) {
	for _, key := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if v := src.Get(key); v != "" {
			dst.Set(key, v)
		}
	}
}

func cloneHeaderMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
			out[k] = v
		}
	}
	return out
}

func looksLikeHLSURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return strings.Contains(strings.ToLower(raw), ".m3u8")
	}
	return strings.HasSuffix(strings.ToLower(u.Path), ".m3u8")
}
