package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/outbound"
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/secrets"
)

const debridSecretGroup = "links"

func debridSecretKey(provider string) string {
	return "stream_debrid_" + provider
}

// DebridProviderStatus is the safe, masked API view of one debrid provider.
type DebridProviderStatus struct {
	Provider   string `json:"provider"`
	Name       string `json:"name"`
	Configured bool   `json:"configured"`
	MaskedKey  string `json:"masked_key,omitempty"`
	Username   string `json:"username,omitempty"`
}

// DebridService stores encrypted API keys in Vault, checks cached torrent
// hashes, and resolves magnets into direct HTTP stream URLs.
type DebridService struct {
	mu             sync.RWMutex
	vault          *secrets.Vault
	client         *http.Client
	torboxBase     string
	premiumizeBase string
	rdBase         string
}

// NewDebridService constructs a DebridService backed by Vault and SSRF-safe HTTP.
func NewDebridService(vault *secrets.Vault) *DebridService {
	tr := &http.Transport{
		DialContext:         outbound.PolicyDialer(outbound.NotMetadata).DialContext,
		MaxIdleConns:        16,
		IdleConnTimeout:     60 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	return &DebridService{
		vault:          vault,
		client:         &http.Client{Transport: tr, Timeout: 12 * time.Second},
		torboxBase:     "https://api.torbox.app",
		premiumizeBase: "https://www.premiumize.me",
		rdBase:         "https://api.real-debrid.com/rest/1.0",
	}
}

// SetVault wires secrets storage onto DebridService.
func (d *DebridService) SetVault(v *secrets.Vault) {
	if d != nil {
		d.mu.Lock()
		d.vault = v
		d.mu.Unlock()
	}
}

// SetTestEndpoints overrides provider base URLs and HTTP client for isolated tests.
func (d *DebridService) SetTestEndpoints(hc *http.Client, torbox, premiumize, rd string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if hc != nil {
		d.client = hc
	}
	if torbox != "" {
		d.torboxBase = strings.TrimRight(torbox, "/")
	}
	if premiumize != "" {
		d.premiumizeBase = strings.TrimRight(premiumize, "/")
	}
	if rd != "" {
		d.rdBase = strings.TrimRight(rd, "/")
	}
}

func (d *DebridService) getVault() *secrets.Vault {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.vault
}

func (d *DebridService) httpClient() *http.Client {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.client
}

func (d *DebridService) getKey(ctx context.Context, provider string) string {
	v := d.getVault()
	if v == nil {
		return ""
	}
	key, err := v.Setting(ctx, debridSecretGroup, debridSecretKey(provider))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(key)
}

// Status lists supported debrid providers with masked configuration state.
func (d *DebridService) Status(ctx context.Context) []DebridProviderStatus {
	defs := []struct{ id, name string }{
		{"torbox", "TorBox"},
		{"premiumize", "Premiumize"},
		{"realdebrid", "Real-Debrid"},
	}
	out := make([]DebridProviderStatus, 0, len(defs))
	for _, p := range defs {
		key := d.getKey(ctx, p.id)
		st := DebridProviderStatus{
			Provider:   p.id,
			Name:       p.name,
			Configured: key != "",
		}
		if key != "" {
			st.MaskedKey = maskSecretKey(key)
		}
		out = append(out, st)
	}
	return out
}

// FirstConfigured returns the highest-priority configured debrid provider id.
func (d *DebridService) FirstConfigured(ctx context.Context) string {
	for _, id := range []string{"torbox", "premiumize", "realdebrid"} {
		if d.getKey(ctx, id) != "" {
			return id
		}
	}
	return ""
}

// SaveKey verifies apiKey against the provider API and stores it encrypted in Vault.
func (d *DebridService) SaveKey(ctx context.Context, provider, apiKey string, by database.Actor) (DebridProviderStatus, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return DebridProviderStatus{}, errors.New("API key is required")
	}
	if !isSupportedDebrid(provider) {
		return DebridProviderStatus{}, errors.New("unsupported debrid provider")
	}
	username, err := d.verifyKey(ctx, provider, apiKey)
	if err != nil {
		return DebridProviderStatus{}, err
	}
	v := d.getVault()
	if v == nil {
		return DebridProviderStatus{}, errors.New("secret vault unavailable")
	}
	if by.Kind == "" {
		by = database.System
	}
	if err := v.SetSetting(ctx, debridSecretGroup, debridSecretKey(provider), apiKey, by); err != nil {
		return DebridProviderStatus{}, err
	}
	return DebridProviderStatus{
		Provider:   provider,
		Name:       debridDisplayName(provider),
		Configured: true,
		MaskedKey:  maskSecretKey(apiKey),
		Username:   username,
	}, nil
}

// DeleteKey clears the stored key for provider.
func (d *DebridService) DeleteKey(ctx context.Context, provider string, by database.Actor) error {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if !isSupportedDebrid(provider) {
		return errors.New("unsupported debrid provider")
	}
	v := d.getVault()
	if v == nil {
		return errors.New("secret vault unavailable")
	}
	if by.Kind == "" {
		by = database.System
	}
	return v.SetSetting(ctx, debridSecretGroup, debridSecretKey(provider), "", by)
}

func (d *DebridService) verifyKey(ctx context.Context, provider, apiKey string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	d.mu.RLock()
	hc := d.client
	tbBase, pmBase, rdBase := d.torboxBase, d.premiumizeBase, d.rdBase
	d.mu.RUnlock()

	switch provider {
	case "torbox":
		req, err := http.NewRequestWithContext(cctx, http.MethodGet, tbBase+"/v1/api/user/me", nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		resp, err := hc.Do(req)
		if err != nil {
			return "", errors.New("could not reach TorBox")
		}
		defer resp.Body.Close()
		var parsed struct {
			Success bool `json:"success"`
			Data    struct {
				Email string `json:"email"`
			} `json:"data"`
			Detail string `json:"detail"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&parsed)
		if resp.StatusCode >= 300 || !parsed.Success {
			return "", errors.New(fallbackString(parsed.Detail, "TorBox rejected the API key"))
		}
		return parsed.Data.Email, nil

	case "premiumize":
		u := pmBase + "/api/account/info?apikey=" + url.QueryEscape(apiKey)
		req, err := http.NewRequestWithContext(cctx, http.MethodGet, u, nil)
		if err != nil {
			return "", err
		}
		resp, err := hc.Do(req)
		if err != nil {
			return "", errors.New("could not reach Premiumize")
		}
		defer resp.Body.Close()
		var parsed struct {
			Status     string `json:"status"`
			CustomerID any    `json:"customer_id"`
			Message    string `json:"message"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&parsed)
		if resp.StatusCode >= 300 || parsed.Status != "success" {
			return "", errors.New(fallbackString(parsed.Message, "Premiumize rejected the API key"))
		}
		if parsed.CustomerID != nil {
			return fmt.Sprintf("ID %v", parsed.CustomerID), nil
		}
		return "Premiumize", nil

	case "realdebrid":
		req, err := http.NewRequestWithContext(cctx, http.MethodGet, rdBase+"/user", nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		resp, err := hc.Do(req)
		if err != nil {
			return "", errors.New("could not reach Real-Debrid")
		}
		defer resp.Body.Close()
		var parsed struct {
			Username string `json:"username"`
			Error    string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&parsed)
		if resp.StatusCode >= 300 || parsed.Username == "" {
			return "", errors.New(fallbackString(parsed.Error, "Real-Debrid rejected the API key"))
		}
		return parsed.Username, nil
	}
	return "", errors.New("unsupported debrid provider")
}

// CheckCached checks which infoHashes are instantly cached on TorBox or Premiumize.
func (d *DebridService) CheckCached(ctx context.Context, hashes []string) map[string]string {
	out := map[string]string{}
	seen := map[string]bool{}
	var uniq []string
	for _, h := range hashes {
		lh := strings.ToLower(strings.TrimSpace(h))
		if len(lh) == 40 && !seen[lh] {
			seen[lh] = true
			uniq = append(uniq, lh)
			if len(uniq) >= 40 {
				break
			}
		}
	}
	if len(uniq) == 0 {
		return out
	}

	tbKey := d.getKey(ctx, "torbox")
	pmKey := d.getKey(ctx, "premiumize")
	var mu sync.Mutex
	var wg sync.WaitGroup

	if tbKey != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, h := range d.checkTorBoxCached(ctx, tbKey, uniq) {
				mu.Lock()
				out[h] = "torbox"
				mu.Unlock()
			}
		}()
	}
	if pmKey != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, h := range d.checkPremiumizeCached(ctx, pmKey, uniq) {
				mu.Lock()
				if _, exists := out[h]; !exists {
					out[h] = "premiumize"
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return out
}

func (d *DebridService) checkTorBoxCached(ctx context.Context, apiKey string, hashes []string) []string {
	cctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	d.mu.RLock()
	base, hc := d.torboxBase, d.client
	d.mu.RUnlock()

	payload, _ := json.Marshal(map[string]any{"hashes": hashes})
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, base+"/v1/api/torrents/checkcached?format=object", bytes.NewReader(payload))
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil
	}
	var parsed struct {
		Success bool                       `json:"success"`
		Data    map[string]json.RawMessage `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&parsed) != nil || !parsed.Success {
		return nil
	}
	var cached []string
	for h, raw := range parsed.Data {
		tr := strings.TrimSpace(string(raw))
		if tr != "" && tr != "null" && tr != "false" {
			cached = append(cached, strings.ToLower(h))
		}
	}
	return cached
}

func (d *DebridService) checkPremiumizeCached(ctx context.Context, apiKey string, hashes []string) []string {
	cctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	d.mu.RLock()
	base, hc := d.premiumizeBase, d.client
	d.mu.RUnlock()

	form := url.Values{}
	form.Set("apikey", apiKey)
	for _, h := range hashes {
		form.Add("items[]", "magnet:?xt=urn:btih:"+h)
	}
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, base+"/api/cache/check", strings.NewReader(form.Encode()))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := hc.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil
	}
	var parsed struct {
		Status   string `json:"status"`
		Response []bool `json:"response"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&parsed) != nil || parsed.Status != "success" {
		return nil
	}
	var cached []string
	for i, ok := range parsed.Response {
		if ok && i < len(hashes) {
			cached = append(cached, hashes[i])
		}
	}
	return cached
}

// ResolveMagnet converts magnet/infoHash into an instant direct HTTP stream URL via provider.
func (d *DebridService) ResolveMagnet(ctx context.Context, provider, magnet, infoHash string, fileIdx int) (string, string, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		provider = d.FirstConfigured(ctx)
	}
	if !isSupportedDebrid(provider) {
		return "", "", errors.New("no configured debrid provider")
	}
	key := d.getKey(ctx, provider)
	if key == "" {
		return "", "", fmt.Errorf("%s is not configured", debridDisplayName(provider))
	}
	if magnet == "" && infoHash != "" {
		magnet = buildMagnetFromHash(infoHash, "", nil)
	}
	if magnet == "" {
		return "", "", errors.New("magnet link is required for debrid resolution")
	}

	switch provider {
	case "torbox":
		return d.resolveTorBox(ctx, key, magnet, fileIdx)
	case "premiumize":
		return d.resolvePremiumize(ctx, key, magnet, fileIdx)
	case "realdebrid":
		return d.resolveRealDebrid(ctx, key, magnet, fileIdx)
	default:
		return "", "", errors.New("unsupported debrid provider")
	}
}

func (d *DebridService) resolveTorBox(ctx context.Context, apiKey, magnet string, fileIdx int) (string, string, error) {
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	d.mu.RLock()
	base, hc := d.torboxBase, d.client
	d.mu.RUnlock()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("magnet", magnet)
	_ = mw.WriteField("add_only_if_cached", "true")
	_ = mw.WriteField("allow_zip", "false")
	_ = mw.Close()

	req, err := http.NewRequestWithContext(cctx, http.MethodPost, base+"/v1/api/torrents/createtorrent", &body)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := hc.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	var createRes struct {
		Success bool   `json:"success"`
		Detail  string `json:"detail"`
		Data    struct {
			TorrentID int64 `json:"torrent_id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&createRes); err != nil {
		return "", "", err
	}
	if !createRes.Success || createRes.Data.TorrentID == 0 {
		return "", "", errors.New(fallbackString(createRes.Detail, "release is not cached on TorBox"))
	}

	listReq, err := http.NewRequestWithContext(cctx, http.MethodGet, fmt.Sprintf("%s/v1/api/torrents/mylist?id=%d&bypass_cache=true", base, createRes.Data.TorrentID), nil)
	if err != nil {
		return "", "", err
	}
	listReq.Header.Set("Authorization", "Bearer "+apiKey)
	listResp, err := hc.Do(listReq)
	if err != nil {
		return "", "", err
	}
	defer listResp.Body.Close()

	var listParsed struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(listResp.Body, 2<<20)).Decode(&listParsed); err != nil || !listParsed.Success {
		return "", "", errors.New("failed to query TorBox torrent info")
	}

	type tbFile struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		ShortName string `json:"short_name"`
		Size      int64  `json:"size"`
	}
	type tbItem struct {
		Files []tbFile `json:"files"`
	}
	var item tbItem
	if json.Unmarshal(listParsed.Data, &item) != nil || len(item.Files) == 0 {
		var items []tbItem
		if json.Unmarshal(listParsed.Data, &items) == nil && len(items) > 0 {
			item = items[0]
		}
	}
	if len(item.Files) == 0 {
		return "", "", errors.New("no files found inside TorBox release")
	}
	chosen := pickDebridFile(item.Files, fileIdx, func(f tbFile) string { return fallbackString(f.Name, f.ShortName) }, func(f tbFile) int64 { return f.Size })

	dlURL := fmt.Sprintf("%s/v1/api/torrents/requestdl?token=%s&torrent_id=%d&file_id=%d&zip_link=false&redirect=false&append_name=false",
		base, url.QueryEscape(apiKey), createRes.Data.TorrentID, chosen.ID)
	dlReq, err := http.NewRequestWithContext(cctx, http.MethodGet, dlURL, nil)
	if err != nil {
		return "", "", err
	}
	dlResp, err := hc.Do(dlReq)
	if err != nil {
		return "", "", err
	}
	defer dlResp.Body.Close()
	var dlParsed struct {
		Success bool   `json:"success"`
		Data    string `json:"data"`
		Detail  string `json:"detail"`
	}
	if err := json.NewDecoder(io.LimitReader(dlResp.Body, 1<<20)).Decode(&dlParsed); err != nil {
		return "", "", err
	}
	if !dlParsed.Success || dlParsed.Data == "" {
		return "", "", errors.New(fallbackString(dlParsed.Detail, "TorBox download URL request failed"))
	}
	return dlParsed.Data, filepath.Base(fallbackString(chosen.ShortName, chosen.Name)), nil
}

func (d *DebridService) resolvePremiumize(ctx context.Context, apiKey, magnet string, fileIdx int) (string, string, error) {
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	d.mu.RLock()
	base, hc := d.premiumizeBase, d.client
	d.mu.RUnlock()

	form := url.Values{}
	form.Set("apikey", apiKey)
	form.Set("src", magnet)
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, base+"/api/transfer/directdl", strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := hc.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	type pmContent struct {
		Path string `json:"path"`
		Link string `json:"link"`
		Size int64  `json:"size"`
	}
	var parsed struct {
		Status  string      `json:"status"`
		Message string      `json:"message"`
		Content []pmContent `json:"content"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&parsed); err != nil {
		return "", "", err
	}
	if parsed.Status != "success" || len(parsed.Content) == 0 {
		return "", "", errors.New(fallbackString(parsed.Message, "release unavailable on Premiumize"))
	}
	chosen := pickDebridFile(parsed.Content, fileIdx, func(f pmContent) string { return f.Path }, func(f pmContent) int64 { return f.Size })
	if chosen.Link == "" {
		return "", "", errors.New("Premiumize returned no direct link")
	}
	return chosen.Link, filepath.Base(chosen.Path), nil
}

func (d *DebridService) resolveRealDebrid(ctx context.Context, apiKey, magnet string, fileIdx int) (string, string, error) {
	cctx, cancel := context.WithTimeout(ctx, 22*time.Second)
	defer cancel()
	d.mu.RLock()
	base, hc := d.rdBase, d.client
	d.mu.RUnlock()

	cleanupID := ""
	deleteTorrent := func() {
		if cleanupID == "" {
			return
		}
		dctx, dcancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer dcancel()
		dreq, err := http.NewRequestWithContext(dctx, http.MethodDelete, base+"/torrents/delete/"+url.PathEscape(cleanupID), nil)
		if err == nil {
			dreq.Header.Set("Authorization", "Bearer "+apiKey)
			if r, err := hc.Do(dreq); err == nil {
				r.Body.Close()
			}
		}
	}

	form := url.Values{}
	form.Set("magnet", magnet)
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, base+"/torrents/addMagnet", strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := hc.Do(req)
	if err != nil {
		return "", "", err
	}
	var added struct {
		ID    string `json:"id"`
		Error string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&added)
	resp.Body.Close()
	if resp.StatusCode >= 300 || added.ID == "" {
		return "", "", errors.New(fallbackString(added.Error, "Real-Debrid failed to add magnet"))
	}
	cleanupID = added.ID

	type rdFile struct {
		ID    int    `json:"id"`
		Path  string `json:"path"`
		Bytes int64  `json:"bytes"`
	}
	type rdInfo struct {
		Status string   `json:"status"`
		Links  []string `json:"links"`
		Files  []rdFile `json:"files"`
	}
	getInfo := func() (rdInfo, error) {
		ireq, err := http.NewRequestWithContext(cctx, http.MethodGet, base+"/torrents/info/"+url.PathEscape(added.ID), nil)
		if err != nil {
			return rdInfo{}, err
		}
		ireq.Header.Set("Authorization", "Bearer "+apiKey)
		iresp, err := hc.Do(ireq)
		if err != nil {
			return rdInfo{}, err
		}
		defer iresp.Body.Close()
		var inf rdInfo
		err = json.NewDecoder(io.LimitReader(iresp.Body, 2<<20)).Decode(&inf)
		return inf, err
	}

	info, err := getInfo()
	if err != nil || len(info.Files) == 0 {
		deleteTorrent()
		return "", "", errors.New("Real-Debrid did not expose release files")
	}
	chosen := pickDebridFile(info.Files, fileIdx, func(f rdFile) string { return f.Path }, func(f rdFile) int64 { return f.Bytes })
	selForm := url.Values{}
	selForm.Set("files", strconv.Itoa(chosen.ID))
	sreq, err := http.NewRequestWithContext(cctx, http.MethodPost, base+"/torrents/selectFiles/"+url.PathEscape(added.ID), strings.NewReader(selForm.Encode()))
	if err != nil {
		deleteTorrent()
		return "", "", err
	}
	sreq.Header.Set("Authorization", "Bearer "+apiKey)
	sreq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sresp, err := hc.Do(sreq)
	if err != nil {
		deleteTorrent()
		return "", "", err
	}
	sresp.Body.Close()

	deadline := time.Now().Add(8 * time.Second)
	for {
		info, err = getInfo()
		if err == nil && info.Status == "downloaded" && len(info.Links) > 0 {
			break
		}
		if time.Now().After(deadline) || cctx.Err() != nil {
			deleteTorrent()
			return "", "", errors.New("release is not cached on Real-Debrid")
		}
		select {
		case <-time.After(750 * time.Millisecond):
		case <-cctx.Done():
			deleteTorrent()
			return "", "", errors.New("Real-Debrid resolution cancelled")
		}
	}

	uform := url.Values{}
	uform.Set("link", info.Links[0])
	ureq, err := http.NewRequestWithContext(cctx, http.MethodPost, base+"/unrestrict/link", strings.NewReader(uform.Encode()))
	if err != nil {
		return "", "", err
	}
	ureq.Header.Set("Authorization", "Bearer "+apiKey)
	ureq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	uresp, err := hc.Do(ureq)
	if err != nil {
		return "", "", err
	}
	defer uresp.Body.Close()
	var unres struct {
		Download string `json:"download"`
		Filename string `json:"filename"`
		Error    string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(uresp.Body, 1<<20)).Decode(&unres); err != nil {
		return "", "", err
	}
	if unres.Download == "" {
		return "", "", errors.New(fallbackString(unres.Error, "Real-Debrid unrestrict failed"))
	}
	return unres.Download, fallbackString(unres.Filename, filepath.Base(chosen.Path)), nil
}

func pickDebridFile[T any](files []T, preferredIdx int, pathOf func(T) string, sizeOf func(T) int64) T {
	if preferredIdx >= 0 && preferredIdx < len(files) {
		return files[preferredIdx]
	}
	bestIdx := 0
	bestScore := int64(-1)
	for i, f := range files {
		ext := strings.ToLower(filepath.Ext(pathOf(f)))
		sz := sizeOf(f)
		bonus := int64(0)
		if videoExts[ext] {
			bonus = 1 << 50
		}
		if sz+bonus > bestScore {
			bestScore = sz + bonus
			bestIdx = i
		}
	}
	return files[bestIdx]
}

func isSupportedDebrid(p string) bool {
	return p == "torbox" || p == "premiumize" || p == "realdebrid"
}

func debridDisplayName(p string) string {
	switch p {
	case "torbox":
		return "TorBox"
	case "premiumize":
		return "Premiumize"
	case "realdebrid":
		return "Real-Debrid"
	}
	return p
}

func maskSecretKey(k string) string {
	n := len(k)
	if n <= 6 {
		return strings.Repeat("*", n)
	}
	return k[:3] + "..." + k[n-3:]
}
