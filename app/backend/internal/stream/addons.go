package stream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/outbound"
)

// AddonCatalogDef describes one browseable Stremio catalog advertised by an addon.
type AddonCatalogDef struct {
	Type   string   `json:"type"`
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Genres []string `json:"genres,omitempty"`
	// Required says the catalog lists nothing without an input (a search);
	// Pages says it takes skip, so later pages can be read.
	Required bool `json:"required,omitempty"`
	Pages    bool `json:"pages,omitempty"`
}

// StremioAddon persists a configured Stremio manifest and its capabilities.
type StremioAddon struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Version     string            `json:"version,omitempty"`
	Description string            `json:"description,omitempty"`
	Logo        string            `json:"logo,omitempty"`
	ManifestURL string            `json:"manifest_url"`
	BaseURL     string            `json:"base_url"`
	Enabled     bool              `json:"enabled"`
	Order       int               `json:"order"`
	Resources   []string          `json:"resources"`
	Types       []string          `json:"types"`
	IDPrefixes  []string          `json:"id_prefixes,omitempty"`
	Catalogs    []AddonCatalogDef `json:"catalogs,omitempty"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// AddonCatalogItem is one poster card returned by a Stremio addon catalog.
type AddonCatalogItem struct {
	ID          string   `json:"id"`
	TMDBID      int      `json:"tmdb_id,omitempty"`
	IMDbID      string   `json:"imdb_id,omitempty"`
	Kind        string   `json:"kind"` // movie or show
	Name        string   `json:"name"`
	Poster      string   `json:"poster,omitempty"`
	Background  string   `json:"background,omitempty"`
	Year        string   `json:"year,omitempty"`
	Description string   `json:"description,omitempty"`
	Genres      []string `json:"genres,omitempty"`
}

// AddonCatalogShelf groups an addon catalog with its fetched items.
type AddonCatalogShelf struct {
	AddonID   string             `json:"addon_id"`
	AddonName string             `json:"addon_name"`
	CatalogID string             `json:"catalog_id"`
	Type      string             `json:"type"`
	Name      string             `json:"name"`
	// Pages says later pages can be read with skip.
	Pages bool               `json:"pages,omitempty"`
	Items []AddonCatalogItem `json:"items"`
}

// AddonSubCandidate represents one subtitle match returned by a Stremio addon.
type AddonSubCandidate struct {
	ID        string `json:"id"`
	SiteName  string `json:"site_name"`
	SiteKind  string `json:"site_kind"`
	Language  string `json:"language"`
	Format    string `json:"format,omitempty"`
	AddonName string `json:"addon_name"`
}

type cachedAddonSub struct {
	url       string
	lang      string
	addonName string
	expiresAt time.Time
}

type cachedAddonCatalog struct {
	items     []AddonCatalogItem
	expiresAt time.Time
}

// AddonStore manages Stremio addons stored in configDir/stream-addons.json.
type AddonStore struct {
	mu       sync.RWMutex
	path     string
	addons   []StremioAddon
	client   *http.Client
	subCache map[string]cachedAddonSub
	catCache map[string]cachedAddonCatalog
}

// NewAddonStore loads stream-addons.json if present and prepares SSRF-guarded HTTP access.
func NewAddonStore(configDir string) *AddonStore {
	tr := &http.Transport{
		DialContext:         outbound.PolicyDialer(outbound.NotMetadata).DialContext,
		MaxIdleConns:        24,
		IdleConnTimeout:     60 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	s := &AddonStore{
		client:   &http.Client{Transport: tr, Timeout: 10 * time.Second},
		subCache: map[string]cachedAddonSub{},
		catCache: map[string]cachedAddonCatalog{},
	}
	if configDir != "" {
		s.path = filepath.Join(configDir, "stream-addons.json")
		s.load()
	}
	return s
}

// SetHTTPClient overrides the outbound client (intended for isolated tests).
func (s *AddonStore) SetHTTPClient(hc *http.Client) {
	if s != nil && hc != nil {
		s.mu.Lock()
		s.client = hc
		s.mu.Unlock()
	}
}

func (s *AddonStore) httpClient() *http.Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.client
}

func (s *AddonStore) load() {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var list []StremioAddon
	if json.Unmarshal(raw, &list) == nil {
		s.addons = list
	}
}

func (s *AddonStore) saveLocked() error {
	if s.path == "" {
		return nil
	}
	return writeJSONAtomic(s.path, s.addons, 0o600)
}

// List returns all installed Stremio addons (logo proxied when external).
func (s *AddonStore) List() []StremioAddon {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]StremioAddon, len(s.addons))
	copy(out, s.addons)
	return out
}

// Add fetches and installs a Stremio addon from its manifest URL.
func (s *AddonStore) Add(ctx context.Context, rawURL string) (StremioAddon, error) {
	manifestURL, err := normalizeStremioManifestURL(rawURL)
	if err != nil {
		return StremioAddon{}, err
	}
	addon, err := s.fetchManifest(ctx, manifestURL)
	if err != nil {
		return StremioAddon{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.addons {
		if s.addons[i].ManifestURL == addon.ManifestURL || (addon.ID != "" && s.addons[i].ID == addon.ID) {
			addon.Order = s.addons[i].Order
			addon.Enabled = s.addons[i].Enabled
			s.addons[i] = addon
			if err := s.saveLocked(); err != nil {
				return StremioAddon{}, err
			}
			return addon, nil
		}
	}
	addon.Enabled = true
	addon.Order = len(s.addons)
	s.addons = append(s.addons, addon)
	if err := s.saveLocked(); err != nil {
		return StremioAddon{}, err
	}
	return addon, nil
}

// Refresh re-fetches manifest metadata for addon id.
func (s *AddonStore) Refresh(ctx context.Context, id string) (StremioAddon, error) {
	s.mu.RLock()
	var mURL string
	for _, a := range s.addons {
		if a.ID == id {
			mURL = a.ManifestURL
			break
		}
	}
	s.mu.RUnlock()
	if mURL == "" {
		return StremioAddon{}, errors.New("addon not found")
	}
	fresh, err := s.fetchManifest(ctx, mURL)
	if err != nil {
		return StremioAddon{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.addons {
		if s.addons[i].ID == id {
			fresh.Enabled = s.addons[i].Enabled
			fresh.Order = s.addons[i].Order
			s.addons[i] = fresh
			if err := s.saveLocked(); err != nil {
				return StremioAddon{}, err
			}
			return fresh, nil
		}
	}
	return StremioAddon{}, errors.New("addon not found")
}

// Toggle updates enabled state or display order for addon id.
func (s *AddonStore) Toggle(id string, enabled *bool, order *int) (StremioAddon, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.addons {
		if s.addons[i].ID == id {
			if enabled != nil {
				s.addons[i].Enabled = *enabled
			}
			if order != nil {
				s.addons[i].Order = *order
			}
			if err := s.saveLocked(); err != nil {
				return StremioAddon{}, err
			}
			return s.addons[i], nil
		}
	}
	return StremioAddon{}, errors.New("addon not found")
}

// Delete removes addon id.
func (s *AddonStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.addons[:0]
	found := false
	for _, a := range s.addons {
		if a.ID == id {
			found = true
			continue
		}
		next = append(next, a)
	}
	if !found {
		return errors.New("addon not found")
	}
	s.addons = next
	return s.saveLocked()
}

func normalizeStremioManifestURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("manifest URL is required")
	}
	if strings.HasPrefix(strings.ToLower(raw), "stremio://") {
		raw = "https://" + raw[len("stremio://"):]
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("invalid addon manifest URL")
	}
	if !strings.HasSuffix(strings.ToLower(u.Path), ".json") {
		u.Path = strings.TrimRight(u.Path, "/") + "/manifest.json"
	}
	return u.String(), nil
}

type stremioManifestRaw struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description"`
	Logo        string            `json:"logo"`
	Resources   []json.RawMessage `json:"resources"`
	Types       []string          `json:"types"`
	IDPrefixes  []string          `json:"idPrefixes"`
	Catalogs    []struct {
		Type  string   `json:"type"`
		ID    string   `json:"id"`
		Name  string   `json:"name"`
		Extra []struct {
			Name       string   `json:"name"`
			Options    []string `json:"options"`
			IsRequired bool     `json:"isRequired"`
		} `json:"extra"`
		// The older manifest form names the extras in two lists.
		ExtraRequired  []string `json:"extraRequired"`
		ExtraSupported []string `json:"extraSupported"`
	} `json:"catalogs"`
}

func (s *AddonStore) fetchManifest(ctx context.Context, manifestURL string) (StremioAddon, error) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return StremioAddon{}, err
	}
	req.Header.Set("User-Agent", defaultStreamUserAgent)
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return StremioAddon{}, fmt.Errorf("failed to reach addon manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return StremioAddon{}, fmt.Errorf("addon manifest returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return StremioAddon{}, err
	}
	var m stremioManifestRaw
	if err := json.Unmarshal(body, &m); err != nil {
		return StremioAddon{}, errors.New("invalid Stremio manifest JSON")
	}
	if strings.TrimSpace(m.Name) == "" && strings.TrimSpace(m.ID) == "" {
		return StremioAddon{}, errors.New("manifest missing id and name")
	}
	resList, idPrefixes := parseManifestResources(m.Resources, m.IDPrefixes)
	if len(resList) == 0 {
		resList = []string{"stream"}
	}
	baseURL := strings.TrimSuffix(manifestURL, "/manifest.json")
	if baseURL == manifestURL {
		if idx := strings.LastIndex(manifestURL, "/"); idx > 0 {
			baseURL = manifestURL[:idx]
		}
	}
	var cats []AddonCatalogDef
	for _, c := range m.Catalogs {
		if c.Type == "" || c.ID == "" {
			continue
		}
		var genres []string
		required := len(c.ExtraRequired) > 0
		pages := false
		for _, ex := range c.Extra {
			if ex.Name == "genre" && len(ex.Options) > 0 {
				genres = ex.Options
			}
			required = required || ex.IsRequired
			pages = pages || ex.Name == "skip"
		}
		for _, n := range c.ExtraSupported {
			pages = pages || n == "skip"
		}
		cats = append(cats, AddonCatalogDef{
			Type:     c.Type,
			ID:       c.ID,
			Name:     fallbackString(c.Name, c.ID),
			Genres:   genres,
			Required: required,
			Pages:    pages,
		})
	}
	addonID := sanitizeID(m.ID)
	if addonID == "" {
		addonID = newOpaqueID()
	}
	logo := strings.TrimSpace(m.Logo)
	if strings.HasPrefix(logo, "http://") || strings.HasPrefix(logo, "https://") {
		logo = "/api/v1/streaming/proxy/poster?url=" + url.QueryEscape(logo)
	}
	return StremioAddon{
		ID:          addonID,
		Name:        fallbackString(m.Name, addonID),
		Version:     m.Version,
		Description: m.Description,
		Logo:        logo,
		ManifestURL: manifestURL,
		BaseURL:     strings.TrimRight(baseURL, "/"),
		Enabled:     true,
		Resources:   resList,
		Types:       m.Types,
		IDPrefixes:  idPrefixes,
		Catalogs:    cats,
		UpdatedAt:   time.Now().UTC(),
	}, nil
}

func parseManifestResources(raw []json.RawMessage, topPrefixes []string) ([]string, []string) {
	seenRes := map[string]bool{}
	var resources []string
	seenPre := map[string]bool{}
	var prefixes []string
	addPre := func(p string) {
		p = strings.TrimSpace(p)
		if p != "" && !seenPre[p] {
			seenPre[p] = true
			prefixes = append(prefixes, p)
		}
	}
	for _, p := range topPrefixes {
		addPre(p)
	}
	for _, r := range raw {
		var s string
		if json.Unmarshal(r, &s) == nil && s != "" {
			if !seenRes[s] {
				seenRes[s] = true
				resources = append(resources, s)
			}
			continue
		}
		var obj struct {
			Name       string   `json:"name"`
			IDPrefixes []string `json:"idPrefixes"`
		}
		if json.Unmarshal(r, &obj) == nil && obj.Name != "" {
			if !seenRes[obj.Name] {
				seenRes[obj.Name] = true
				resources = append(resources, obj.Name)
			}
			for _, p := range obj.IDPrefixes {
				addPre(p)
			}
		}
	}
	return resources, prefixes
}

// SearchStreams queries enabled Stremio addons that provide streams concurrently.
func (s *AddonStore) SearchStreams(ctx context.Context, kind, imdbID, tmdbID string, season, episode int) ([]UnifiedCandidate, []string) {
	addons := s.enabledFor("stream", kind)
	if len(addons) == 0 {
		return nil, nil
	}
	stremioType := "movie"
	if kind == "show" {
		stremioType = "series"
	}

	var mu sync.Mutex
	var out []UnifiedCandidate
	var warnings []string
	var wg sync.WaitGroup
	for _, addon := range addons {
		targetID := pickAddonMediaID(addon, kind, imdbID, tmdbID, season, episode)
		if targetID == "" {
			continue
		}
		wg.Add(1)
		go func(a StremioAddon, id string) {
			defer wg.Done()
			cands, err := s.queryStreamAddon(ctx, a, stremioType, id)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				warnings = append(warnings, a.Name+": "+err.Error())
				return
			}
			out = append(out, cands...)
		}(addon, targetID)
	}
	wg.Wait()
	return out, warnings
}

type stremioStreamResponse struct {
	Streams []struct {
		Name          string   `json:"name"`
		Title         string   `json:"title"`
		Description   string   `json:"description"`
		URL           string   `json:"url"`
		InfoHash      string   `json:"infoHash"`
		FileIdx       *int     `json:"fileIdx"`
		Sources       []string `json:"sources"`
		BehaviorHints struct {
			Filename     string `json:"filename"`
			VideoSize    int64  `json:"videoSize"`
			ProxyHeaders struct {
				Request map[string]string `json:"request"`
			} `json:"proxyHeaders"`
		} `json:"behaviorHints"`
	} `json:"streams"`
}

func (s *AddonStore) queryStreamAddon(ctx context.Context, addon StremioAddon, stremioType, mediaID string) ([]UnifiedCandidate, error) {
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	endpoint := addon.BaseURL + "/stream/" + url.PathEscape(stremioType) + "/" + url.PathEscape(mediaID) + ".json"
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", defaultStreamUserAgent)
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var parsed stremioStreamResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	var out []UnifiedCandidate
	for _, st := range parsed.Streams {
		desc := strings.TrimSpace(fallbackString(st.Title, st.Description))
		relName := st.BehaviorHints.Filename
		if relName == "" {
			relName = firstLine(desc)
		}
		if relName == "" {
			relName = st.Name
		}
		size := st.BehaviorHints.VideoSize
		if size <= 0 {
			size = parseHumanSizeBytes(desc)
		}
		seeders := parseSeedersHint(st.Name + "\n" + desc)

		if st.URL != "" {
			u, perr := url.Parse(strings.TrimSpace(st.URL))
			if perr != nil || (u.Scheme != "http" && u.Scheme != "https") {
				continue
			}
			kind := "http"
			if looksLikeHLSURL(u.String()) {
				kind = "hls"
			}
			out = append(out, UnifiedCandidate{
				Kind:         kind,
				Instant:      true,
				Title:        fallbackString(firstLine(desc), st.Name),
				ReleaseName:  relName,
				Provider:     addon.Name,
				ProviderType: "stremio",
				SourceName:   addon.Name,
				Size:         size,
				Seeders:      seeders,
				StreamURL:    u.String(),
				Headers:      cloneHeaderMap(st.BehaviorHints.ProxyHeaders.Request),
				Filename:     st.BehaviorHints.Filename,
			})
			continue
		}
		if st.InfoHash != "" {
			hash := strings.ToLower(strings.TrimSpace(st.InfoHash))
			magnet := buildMagnetFromHash(hash, relName, st.Sources)
			fileIdx := -1
			if st.FileIdx != nil {
				fileIdx = *st.FileIdx
			}
			out = append(out, UnifiedCandidate{
				Kind:         "torrent",
				Instant:      false,
				Title:        fallbackString(firstLine(desc), st.Name),
				ReleaseName:  relName,
				Provider:     addon.Name,
				ProviderType: "stremio",
				SourceName:   addon.Name,
				Size:         size,
				Seeders:      seeders,
				InfoHash:     hash,
				Magnet:       magnet,
				Link:         magnet,
				FileIdx:      fileIdx,
				Filename:     st.BehaviorHints.Filename,
			})
		}
	}
	return out, nil
}

