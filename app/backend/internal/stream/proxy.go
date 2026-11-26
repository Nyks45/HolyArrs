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

// Preflight checks that a remote stream URL returns a non-HTML playable response
// using the stored request headers before starting a session.
func (p *ProxyManager) Preflight(ctx context.Context, targetURL string, storedHeaders map[string]string) (contentType string, isHLS bool, err error) {
	if p == nil {
		return "", false, errors.New("stream proxy unavailable")
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(cctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return "", false, errors.New("invalid stream URL")
	}
	var reqHdr http.Header
	if !looksLikeHLSURL(targetURL) {
		reqHdr = http.Header{"Range": []string{"bytes=0-1023"}}
	}
	applyProxyHeaders(req, storedHeaders, reqHdr)

	resp, err := p.hls.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("source unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", false, fmt.Errorf("source link unavailable (HTTP %d)", resp.StatusCode)
	}
	ct := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	peek, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	if strings.Contains(ct, "mpegurl") || bytes.Contains(peek, []byte("#EXTM3U")) {
		return ct, true, nil
	}
	sample := strings.ToLower(strings.TrimSpace(string(peek)))
	if strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xml") ||
		strings.Contains(ct, "text/xml") || strings.Contains(ct, "application/json") ||
		strings.Contains(ct, "text/plain") ||
		strings.HasPrefix(sample, "<!doctype") || strings.HasPrefix(sample, "<html") ||
		strings.HasPrefix(sample, "<head") || strings.HasPrefix(sample, "<?xml") ||
		strings.HasPrefix(sample, `{"error"`) || strings.HasPrefix(sample, `{"code"`) ||
		strings.HasPrefix(sample, "unable to get") {
		return ct, false, errors.New("source returned a web page instead of a playable stream")
	}
	return ct, false, nil
}

// ServeProxy streams the primary proxied URL (rewriting .m3u8 manifests automatically).
func (p *ProxyManager) ServeProxy(w http.ResponseWriter, r *http.Request) {
	tok := r.PathValue("token")
	entry, ok := p.lookup(tok)
	if !ok {
		http.NotFound(w, r)
		return
	}
	p.proxyUpstream(w, r, tok, entry.target, entry.headers)
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
	p.proxyUpstream(w, r, tok, u.String(), entry.headers)
}

func (p *ProxyManager) proxyUpstream(w http.ResponseWriter, r *http.Request, tok, targetURL string, storedHeaders map[string]string) {
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

var reHLSAttrURI = regexp.MustCompile(`URI="([^"]+)"`)

func rewriteHLSManifest(body string, base *url.URL, token string) string {
	var out strings.Builder
	out.Grow(len(body) + len(body)/4)
	sc := bufio.NewScanner(strings.NewReader(body))
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1<<20)

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

	first := true
	for sc.Scan() {
		if !first {
			out.WriteByte('\n')
		}
		first = false
		line := strings.TrimRight(sc.Text(), "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
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
		case "Host", "Content-Length", "Connection", "Transfer-Encoding":
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
