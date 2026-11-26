package stream

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/outbound"
)

//go:embed nuvio_runner.mjs
var nuvioRunnerScript string

// NuvioScraper describes one JS stream scraper script inside a Nuvio repo.
type NuvioScraper struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Version        string         `json:"version,omitempty"`
	Description    string         `json:"description,omitempty"`
	Filename       string         `json:"filename"`
	URL            string         `json:"url"`
	SupportedTypes []string       `json:"supported_types,omitempty"`
	Enabled        bool           `json:"enabled"`
	Settings       map[string]any `json:"settings,omitempty"`
	LocalFile      string         `json:"-"`
}

// NuvioRepo stores an installed Nuvio JS provider repository manifest and scrapers.
type NuvioRepo struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	ManifestURL string         `json:"manifest_url"`
	Enabled     bool           `json:"enabled"`
	Scrapers    []NuvioScraper `json:"scrapers"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// PluginStore manages Nuvio JS provider repos and executes enabled scrapers
// inside isolated short-lived Node VM workers.
type PluginStore struct {
	mu       sync.RWMutex
	path     string
	cacheDir string
	repos    []NuvioRepo
	client   *http.Client
	nodeBin  string
}

// NewPluginStore loads configDir/stream-plugins.json and locates the node binary.
func NewPluginStore(configDir string) *PluginStore {
	tr := &http.Transport{
		DialContext:         outbound.PolicyDialer(outbound.NotMetadata).DialContext,
		MaxIdleConns:        16,
		IdleConnTimeout:     60 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	nodePath, _ := exec.LookPath("node")
	s := &PluginStore{
		client:  &http.Client{Transport: tr, Timeout: 12 * time.Second},
		nodeBin: nodePath,
	}
	if configDir != "" {
		s.path = filepath.Join(configDir, "stream-plugins.json")
		s.cacheDir = filepath.Join(configDir, "stream-plugins")
		s.load()
	}
	return s
}

// SetHTTPClient overrides the HTTP client (intended for isolated tests).
func (p *PluginStore) SetHTTPClient(hc *http.Client) {
	if p != nil && hc != nil {
		p.mu.Lock()
		p.client = hc
		p.mu.Unlock()
	}
}

// NodeAvailable reports whether a Node.js runtime was found for JS scrapers.
func (p *PluginStore) NodeAvailable() bool {
	return p != nil && p.nodeBin != ""
}

func (p *PluginStore) load() {
	raw, err := os.ReadFile(p.path)
	if err != nil {
		return
	}
	var repos []NuvioRepo
	if json.Unmarshal(raw, &repos) == nil {
		for ri := range repos {
			for si := range repos[ri].Scrapers {
				if p.cacheDir != "" {
					repos[ri].Scrapers[si].LocalFile = filepath.Join(p.cacheDir, repos[ri].ID, repos[ri].Scrapers[si].ID+".js")
				}
			}
		}
		p.repos = repos
	}
}

func (p *PluginStore) saveLocked() error {
	if p.path == "" {
		return nil
	}
	return writeJSONAtomic(p.path, p.repos, 0o600)
}

// List returns all installed Nuvio plugin repos and their scrapers.
func (p *PluginStore) List() []NuvioRepo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]NuvioRepo, len(p.repos))
	copy(out, p.repos)
	return out
}

// AddRepo fetches a Nuvio manifest URL and caches all declared JS scraper files.
func (p *PluginStore) AddRepo(ctx context.Context, rawURL string) (NuvioRepo, error) {
	return p.AddRepoWithDefaults(ctx, rawURL, nil)
}

// AddRepoWithDefaults fetches a Nuvio manifest URL, caches all JS files, and when installing
// a repo for the first time with non-empty defaultEnabled, enables only matching scraper IDs.
func (p *PluginStore) AddRepoWithDefaults(ctx context.Context, rawURL string, defaultEnabled []string) (NuvioRepo, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return NuvioRepo{}, errors.New("invalid Nuvio repository URL")
	}
	repo, scripts, err := p.fetchRepo(ctx, u.String())
	if err != nil {
		return NuvioRepo{}, err
	}
	if p.cacheDir != "" {
		dir := filepath.Join(p.cacheDir, repo.ID)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return NuvioRepo{}, err
		}
		for sid, code := range scripts {
			fp := filepath.Join(dir, sid+".js")
			if err := os.WriteFile(fp, code, 0o600); err != nil {
				return NuvioRepo{}, err
			}
		}
	}
	allowSet := map[string]bool{}
	for _, id := range defaultEnabled {
		if sid := sanitizeID(id); sid != "" {
			allowSet[sid] = true
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.repos {
		if p.repos[i].ManifestURL == repo.ManifestURL || p.repos[i].ID == repo.ID {
			prevEnabled := map[string]bool{}
			prevSettings := map[string]map[string]any{}
			for _, sc := range p.repos[i].Scrapers {
				prevEnabled[sc.ID] = sc.Enabled
				prevSettings[sc.ID] = sc.Settings
			}
			repo.Enabled = p.repos[i].Enabled
			for si := range repo.Scrapers {
				sid := repo.Scrapers[si].ID
				if en, ok := prevEnabled[sid]; ok {
					repo.Scrapers[si].Enabled = en
				}
				if st, ok := prevSettings[sid]; ok {
					repo.Scrapers[si].Settings = st
				}
			}
			p.repos[i] = repo
			if err := p.saveLocked(); err != nil {
				return NuvioRepo{}, err
			}
			return repo, nil
		}
	}
	if len(allowSet) > 0 {
		for si := range repo.Scrapers {
			repo.Scrapers[si].Enabled = allowSet[repo.Scrapers[si].ID]
		}
	}
	p.repos = append(p.repos, repo)
	if err := p.saveLocked(); err != nil {
		return NuvioRepo{}, err
	}
	return repo, nil
}

// RefreshRepo re-downloads the manifest and JS files for repo id.
func (p *PluginStore) RefreshRepo(ctx context.Context, id string) (NuvioRepo, error) {
	p.mu.RLock()
	var mURL string
	for _, r := range p.repos {
		if r.ID == id {
			mURL = r.ManifestURL
			break
		}
	}
	p.mu.RUnlock()
	if mURL == "" {
		return NuvioRepo{}, errors.New("repository not found")
	}
	return p.AddRepo(ctx, mURL)
}

// UpdateRepo toggles a repo or individual scraper or updates scraper settings.
func (p *PluginStore) UpdateRepo(id string, enabled *bool, scraperID string, scraperEnabled *bool, settings map[string]any) (NuvioRepo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.repos {
		if p.repos[i].ID != id {
			continue
		}
		if enabled != nil {
			p.repos[i].Enabled = *enabled
		}
		if scraperID != "" {
			for si := range p.repos[i].Scrapers {
				if p.repos[i].Scrapers[si].ID == scraperID {
					if scraperEnabled != nil {
						p.repos[i].Scrapers[si].Enabled = *scraperEnabled
					}
					if settings != nil {
						p.repos[i].Scrapers[si].Settings = settings
					}
					break
				}
			}
		}
		if err := p.saveLocked(); err != nil {
			return NuvioRepo{}, err
		}
		return p.repos[i], nil
	}
	return NuvioRepo{}, errors.New("repository not found")
}

// DeleteRepo removes repo id and deletes its cached JS scripts.
func (p *PluginStore) DeleteRepo(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	next := p.repos[:0]
	found := false
	for _, r := range p.repos {
		if r.ID == id {
			found = true
			continue
		}
		next = append(next, r)
	}
	if !found {
		return errors.New("repository not found")
	}
	p.repos = next
	if p.cacheDir != "" {
		_ = os.RemoveAll(filepath.Join(p.cacheDir, sanitizeID(id)))
	}
	return p.saveLocked()
}

type rawNuvioScraper struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Version        string   `json:"version"`
	Description    string   `json:"description"`
	Filename       string   `json:"filename"`
	URL            string   `json:"url"`
	SupportedTypes []string `json:"supportedTypes"`
	Enabled        *bool    `json:"enabled"`
}

func (p *PluginStore) fetchRepo(ctx context.Context, manifestURL string) (NuvioRepo, map[string][]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	p.mu.RLock()
	hc := p.client
	p.mu.RUnlock()

	req, err := http.NewRequestWithContext(cctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return NuvioRepo{}, nil, err
	}
	req.Header.Set("User-Agent", defaultStreamUserAgent)
	resp, err := hc.Do(req)
	if err != nil {
		return NuvioRepo{}, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return NuvioRepo{}, nil, fmt.Errorf("repository manifest HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return NuvioRepo{}, nil, err
	}

	var rawScrapers []rawNuvioScraper
	repoName := ""
	repoID := ""
	if err := json.Unmarshal(body, &rawScrapers); err != nil {
		var obj struct {
			ID        string            `json:"id"`
			Name      string            `json:"name"`
			Scrapers  []rawNuvioScraper `json:"scrapers"`
			Providers []rawNuvioScraper `json:"providers"`
		}
		if jerr := json.Unmarshal(body, &obj); jerr != nil {
			return NuvioRepo{}, nil, errors.New("invalid Nuvio repository JSON")
		}
		repoID = sanitizeID(obj.ID)
		repoName = strings.TrimSpace(obj.Name)
		rawScrapers = append(obj.Scrapers, obj.Providers...)
	}
	if len(rawScrapers) == 0 {
		return NuvioRepo{}, nil, errors.New("repository declared no scrapers")
	}
	baseParsed, _ := url.Parse(manifestURL)
	if repoName == "" && baseParsed != nil {
		repoName = baseParsed.Host
	}
	if repoID == "" {
		repoID = sanitizeID(baseParsed.Host + "_" + baseParsed.Path)
	}
	if repoID == "" {
		repoID = newOpaqueID()
	}

	type fetchSlot struct {
		sid       string
		raw       rawNuvioScraper
		scriptURL string
		code      []byte
	}
	slots := make([]fetchSlot, len(rawScrapers))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for idx, rs := range rawScrapers {
		sid := sanitizeID(fallbackString(rs.ID, rs.Name))
		if sid == "" {
			continue
		}
		scriptURL := strings.TrimSpace(rs.URL)
		if scriptURL == "" && rs.Filename != "" && baseParsed != nil {
			ref, perr := url.Parse(strings.TrimLeft(rs.Filename, "/"))
			if perr == nil {
				scriptURL = baseParsed.ResolveReference(ref).String()
			}
		}
		if scriptURL == "" {
			continue
		}
		slots[idx] = fetchSlot{sid: sid, raw: rs, scriptURL: scriptURL}
		wg.Add(1)
		go func(i int, target string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-cctx.Done():
				return
			}
			defer func() { <-sem }()
			codeBytes, derr := p.downloadScript(cctx, hc, target)
			if derr == nil && len(codeBytes) > 0 {
				slots[i].code = codeBytes
			}
		}(idx, scriptURL)
	}
	wg.Wait()

	scripts := map[string][]byte{}
	scrapers := make([]NuvioScraper, 0, len(rawScrapers))
	for _, sl := range slots {
		if len(sl.code) == 0 {
			continue
		}
		rs := sl.raw
		sid := sl.sid
		scripts[sid] = sl.code
		en := true
		if rs.Enabled != nil {
			en = *rs.Enabled
		}
		localFP := ""
		if p.cacheDir != "" {
			localFP = filepath.Join(p.cacheDir, repoID, sid+".js")
		}
		scrapers = append(scrapers, NuvioScraper{
			ID:             sid,
			Name:           fallbackString(rs.Name, sid),
			Version:        rs.Version,
			Description:    rs.Description,
			Filename:       rs.Filename,
			URL:            sl.scriptURL,
			SupportedTypes: rs.SupportedTypes,
			Enabled:        en,
			LocalFile:      localFP,
		})
	}
	if len(scrapers) == 0 {
		return NuvioRepo{}, nil, errors.New("no valid scrapers could be downloaded")
	}
	return NuvioRepo{
		ID:          repoID,
		Name:        repoName,
		ManifestURL: manifestURL,
		Enabled:     true,
		Scrapers:    scrapers,
		UpdatedAt:   time.Now().UTC(),
	}, scripts, nil
}

func (p *PluginStore) downloadScript(ctx context.Context, hc *http.Client, targetURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", defaultStreamUserAgent)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 2<<20))
}

// SearchStreams runs enabled Nuvio JS scrapers matching kind concurrently in isolated Node workers.
func (p *PluginStore) SearchStreams(ctx context.Context, kind, tmdbID string, season, episode int, tmdbAPIKey string) ([]UnifiedCandidate, []string) {
	if !p.NodeAvailable() || strings.TrimSpace(tmdbID) == "" {
		return nil, nil
	}
	type job struct {
		repoName string
		scraper  NuvioScraper
		code     string
	}
	p.mu.RLock()
	var jobs []job
	for _, r := range p.repos {
		if !r.Enabled {
			continue
		}
		for _, sc := range r.Scrapers {
			if !sc.Enabled || !scraperSupportsKind(sc.SupportedTypes, kind) || sc.LocalFile == "" {
				continue
			}
			raw, err := os.ReadFile(sc.LocalFile)
			if err != nil || len(raw) == 0 {
				continue
			}
			jobs = append(jobs, job{repoName: r.Name, scraper: sc, code: string(raw)})
		}
	}
	p.mu.RUnlock()
	if len(jobs) == 0 {
		return nil, nil
	}

	mediaType := "movie"
	if kind == "show" {
		mediaType = "tv"
	}

	sctx, cancelAll := context.WithTimeout(ctx, 12*time.Second)
	defer cancelAll()

	sem := make(chan struct{}, 8)
	var mu sync.Mutex
	var out []UnifiedCandidate
	var warnings []string
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func(jb job) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-sctx.Done():
				return
			}
			defer func() { <-sem }()

			cands, err := p.runScraperCode(sctx, jb.scraper.ID, jb.scraper.Name, jb.code, tmdbID, mediaType, season, episode, jb.scraper.Settings, tmdbAPIKey)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				warnings = append(warnings, jb.scraper.Name+": "+err.Error())
				return
			}
			out = append(out, cands...)
		}(j)
	}
	wg.Wait()
	return out, warnings
}

func (p *PluginStore) runScraperCode(ctx context.Context, scraperID, scraperName, code, tmdbID, mediaType string, season, episode int, settings map[string]any, tmdbKey string) ([]UnifiedCandidate, error) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	payload := map[string]any{
		"code":       code,
		"tmdbId":     tmdbID,
		"mediaType":  mediaType,
		"season":     season,
		"episode":    episode,
		"scraperId":  scraperID,
		"settings":   settings,
		"tmdbApiKey": tmdbKey,
	}
	inBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(cctx, p.nodeBin, "--no-warnings", "--max-old-space-size=512", "--input-type=module", "-e", nuvioRunnerScript)
	cmd.Stdin = bytes.NewReader(inBytes)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil && stdout.Len() == 0 {
		return nil, fmt.Errorf("plugin runner failed: %v", err)
	}

	var parsed struct {
		Streams []struct {
			Name    string            `json:"name"`
			Title   string            `json:"title"`
			URL     string            `json:"url"`
			Quality string            `json:"quality"`
			Size    float64           `json:"size"`
			Headers map[string]string `json:"headers"`
		} `json:"streams"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &parsed); err != nil {
		return nil, errors.New("invalid plugin runner JSON")
	}
	if parsed.Error != "" && len(parsed.Streams) == 0 {
		return nil, errors.New(parsed.Error)
	}
	var out []UnifiedCandidate
	for _, st := range parsed.Streams {
		u, perr := url.Parse(strings.TrimSpace(st.URL))
		if perr != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			continue
		}
		kind := "http"
		if looksLikeHLSURL(u.String()) {
			kind = "hls"
		}
		relTitle := fallbackString(st.Title, st.Name)
		if relTitle == "" {
			relTitle = scraperName + " stream"
		}
		q := normalizeQualityTag(st.Quality)
		out = append(out, UnifiedCandidate{
			Kind:         kind,
			Instant:      true,
			Title:        relTitle,
			ReleaseName:  relTitle,
			Provider:     scraperName,
			ProviderType: "nuvio",
			SourceName:   scraperName,
			Quality:      q,
			Size:         int64(st.Size),
			StreamURL:    u.String(),
			Headers:      cloneHeaderMap(st.Headers),
		})
	}
	return out, nil
}

func scraperSupportsKind(types []string, kind string) bool {
	if len(types) == 0 {
		return true
	}
	want := "movie"
	if kind == "show" {
		want = "tv"
	}
	for _, t := range types {
		low := strings.ToLower(strings.TrimSpace(t))
		if low == want || low == kind || (want == "tv" && (low == "show" || low == "anime")) {
			return true
		}
	}
	return false
}

func normalizeQualityTag(q string) string {
	low := strings.ToLower(strings.TrimSpace(q))
	switch {
	case strings.Contains(low, "2160") || strings.Contains(low, "4k"):
		return "2160p"
	case strings.Contains(low, "1080"):
		return "1080p"
	case strings.Contains(low, "720"):
		return "720p"
	case strings.Contains(low, "480"):
		return "480p"
	}
	return strings.TrimSpace(q)
}
