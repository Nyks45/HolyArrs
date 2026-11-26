package stream

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	customRepoID          = "local-custom"
	customRepoManifestURL = "local://custom"
	maxCustomScraperBytes = 512 << 10
)

// CustomScraperInput creates or updates a locally authored Nuvio JS scraper.
type CustomScraperInput struct {
	ID             string   `json:"id,omitempty"`
	Name           string   `json:"name"`
	Description    string   `json:"description,omitempty"`
	SupportedTypes []string `json:"supported_types,omitempty"`
	Code           string   `json:"code"`
}

// CustomScraperDetail returns a custom Nuvio scraper definition including its JS source.
type CustomScraperDetail struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description,omitempty"`
	SupportedTypes []string `json:"supported_types,omitempty"`
	Code           string   `json:"code"`
}

// SaveCustomScraper validates, writes, and registers a local Nuvio JS plugin scraper.
func (p *PluginStore) SaveCustomScraper(in CustomScraperInput) (NuvioRepo, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return NuvioRepo{}, errors.New("plugin name is required")
	}
	code := strings.TrimSpace(in.Code)
	if code == "" {
		return NuvioRepo{}, errors.New("plugin code is required")
	}
	if len(code) > maxCustomScraperBytes {
		return NuvioRepo{}, errors.New("plugin code exceeds 512 KB limit")
	}
	if !strings.Contains(code, "getStreams") {
		return NuvioRepo{}, errors.New("plugin script must define getStreams(tmdbId, mediaType, season, episode)")
	}
	sid := sanitizeID(fallbackString(strings.TrimSpace(in.ID), name))
	if sid == "" {
		return NuvioRepo{}, errors.New("valid plugin identifier is required")
	}
	types := normalizeCustomTypes(in.SupportedTypes)

	localFP := ""
	if p.cacheDir != "" {
		dir := filepath.Join(p.cacheDir, customRepoID)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return NuvioRepo{}, err
		}
		localFP = filepath.Join(dir, sid+".js")
		if err := os.WriteFile(localFP, []byte(code+"\n"), 0o600); err != nil {
			return NuvioRepo{}, err
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now().UTC()
	idx := -1
	for i := range p.repos {
		if p.repos[i].ID == customRepoID || p.repos[i].ManifestURL == customRepoManifestURL {
			idx = i
			break
		}
	}
	if idx < 0 {
		p.repos = append(p.repos, NuvioRepo{
			ID:          customRepoID,
			Name:        "Personal Custom Plugins",
			ManifestURL: customRepoManifestURL,
			Enabled:     true,
			UpdatedAt:   now,
		})
		idx = len(p.repos) - 1
	}

	scraper := NuvioScraper{
		ID:             sid,
		Name:           name,
		Version:        "1.0.0",
		Description:    strings.TrimSpace(in.Description),
		Filename:       sid + ".js",
		URL:            customRepoManifestURL + "/" + sid + ".js",
		SupportedTypes: types,
		Enabled:        true,
		LocalFile:      localFP,
	}
	replaced := false
	for si, cur := range p.repos[idx].Scrapers {
		if cur.ID == sid {
			scraper.Enabled = cur.Enabled
			scraper.Settings = cur.Settings
			p.repos[idx].Scrapers[si] = scraper
			replaced = true
			break
		}
	}
	if !replaced {
		p.repos[idx].Scrapers = append(p.repos[idx].Scrapers, scraper)
	}
	p.repos[idx].UpdatedAt = now
	if err := p.saveLocked(); err != nil {
		return NuvioRepo{}, err
	}
	return p.repos[idx], nil
}

// GetCustomScraper loads a locally authored Nuvio scraper and its JS source code.
func (p *PluginStore) GetCustomScraper(id string) (CustomScraperDetail, error) {
	sid := sanitizeID(strings.TrimSpace(id))
	if sid == "" {
		return CustomScraperDetail{}, errors.New("plugin not found")
	}
	p.mu.RLock()
	var found *NuvioScraper
	for _, r := range p.repos {
		if r.ID != customRepoID && r.ManifestURL != customRepoManifestURL {
			continue
		}
		for i := range r.Scrapers {
			if r.Scrapers[i].ID == sid {
				cp := r.Scrapers[i]
				found = &cp
				break
			}
		}
	}
	p.mu.RUnlock()
	if found == nil {
		return CustomScraperDetail{}, errors.New("custom plugin not found")
	}
	fp := found.LocalFile
	if fp == "" && p.cacheDir != "" {
		fp = filepath.Join(p.cacheDir, customRepoID, sid+".js")
	}
	raw, err := os.ReadFile(fp)
	if err != nil {
		return CustomScraperDetail{}, err
	}
	return CustomScraperDetail{
		ID:             found.ID,
		Name:           found.Name,
		Description:    found.Description,
		SupportedTypes: found.SupportedTypes,
		Code:           string(raw),
	}, nil
}

// DeleteCustomScraper deletes one locally authored plugin from Personal Custom Plugins.
func (p *PluginStore) DeleteCustomScraper(id string) error {
	sid := sanitizeID(strings.TrimSpace(id))
	if sid == "" {
		return errors.New("custom plugin not found")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	found := false
	nextRepos := make([]NuvioRepo, 0, len(p.repos))
	for _, r := range p.repos {
		if r.ID != customRepoID && r.ManifestURL != customRepoManifestURL {
			nextRepos = append(nextRepos, r)
			continue
		}
		kept := make([]NuvioScraper, 0, len(r.Scrapers))
		for _, sc := range r.Scrapers {
			if sc.ID == sid {
				found = true
				if sc.LocalFile != "" {
					_ = os.Remove(sc.LocalFile)
				}
				continue
			}
			kept = append(kept, sc)
		}
		if len(kept) > 0 {
			r.Scrapers = kept
			r.UpdatedAt = time.Now().UTC()
			nextRepos = append(nextRepos, r)
		}
	}
	if !found {
		return errors.New("custom plugin not found")
	}
	if p.cacheDir != "" {
		_ = os.Remove(filepath.Join(p.cacheDir, customRepoID, sid+".js"))
	}
	p.repos = nextRepos
	return p.saveLocked()
}

func normalizeCustomTypes(raw []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range raw {
		low := strings.ToLower(strings.TrimSpace(t))
		if low == "show" {
			low = "tv"
		}
		if low == "movie" || low == "tv" || low == "anime" {
			if !seen[low] {
				seen[low] = true
				out = append(out, low)
			}
		}
	}
	if len(out) == 0 {
		return []string{"movie", "tv"}
	}
	return out
}
