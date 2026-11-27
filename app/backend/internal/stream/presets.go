package stream

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// PresetAddon describes a verified Stremio addon offered for quick installation.
type PresetAddon struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	ManifestURL string `json:"manifest_url"`
	Default     bool   `json:"default"`
	Installed   bool   `json:"installed"`
}

// PresetPluginRepo describes a verified Nuvio JS provider repository offered for quick installation.
type PresetPluginRepo struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Category        string   `json:"category"`
	Description     string   `json:"description"`
	ManifestURL     string   `json:"manifest_url"`
	Default         bool     `json:"default"`
	DefaultScrapers []string `json:"default_scrapers,omitempty"`
	Installed       bool     `json:"installed"`
}

// PresetCatalog groups curated Stremio addons and Nuvio plugin repos with live install state.
type PresetCatalog struct {
	Addons  []PresetAddon      `json:"addons"`
	Plugins []PresetPluginRepo `json:"plugins"`
}

// PresetSetupResult summarizes a preset installation pass.
type PresetSetupResult struct {
	Installed []string      `json:"installed"`
	Skipped   []string      `json:"skipped"`
	Errors    []string      `json:"errors,omitempty"`
	Catalog   PresetCatalog `json:"catalog"`
}

var builtinAddonPresets = []PresetAddon{
	{
		ID:          "opensubtitles-v3",
		Name:        "OpenSubtitles v3",
		Category:    "Subtitles",
		Description: "Official multilingual Stremio subtitle feed for movies and shows.",
		ManifestURL: "https://opensubtitles-v3.strem.io/manifest.json",
		Default:     true,
	},
	{
		ID:          "opensubtitles-pro",
		Name:        "OpenSubtitles PRO",
		Category:    "Subtitles",
		Description: "Ad-free community OpenSubtitles mirror for extra subtitle coverage.",
		ManifestURL: "https://opensubtitlesv3-pro.dexter21767.com/manifest.json",
		Default:     true,
	},
	{
		ID:          "anime-kitsu",
		Name:        "Anime Kitsu",
		Category:    "Anime",
		Description: "Kitsu-powered anime catalogs, metadata and episode subtitles.",
		ManifestURL: "https://anime-kitsu.strem.fun/manifest.json",
		Default:     true,
	},
	{
		ID:          "torrentio",
		Name:        "Torrentio",
		Category:    "Streams",
		Description: "Fast movie, show and anime torrent & Debrid stream provider.",
		ManifestURL: "https://torrentio.strem.fun/manifest.json",
		Default:     true,
	},
	{
		ID:          "streaming-catalogs",
		Name:        "Streaming Catalogs",
		Category:    "Catalogs",
		Description: "Curated platform trending shelves for Explore.",
		ManifestURL: "https://7a82163c306e-stremio-netflix-catalog-addon.baby-beamup.club/manifest.json",
		Default:     false,
	},
	{
		ID:          "mediafusion",
		Name:        "MediaFusion | ElfHosted",
		Category:    "Streams",
		Description: "Optional ElfHosted multi-source stream and Debrid indexer.",
		ManifestURL: "https://mediafusion.elfhosted.com/manifest.json",
		Default:     false,
	},
	{
		ID:          "comet",
		Name:        "Comet | ElfHosted",
		Category:    "Streams",
		Description: "Optional ElfHosted Stremio torrent and Debrid scraper addon.",
		ManifestURL: "https://comet.elfhosted.com/manifest.json",
		Default:     false,
	},
}

var builtinPluginPresets = []PresetPluginRepo{
	{
		ID:              "yoruix",
		Name:            "Yoru's Nuvio Repo",
		Category:        "Movies, Shows & Anime",
		Description:     "29 JS scrapers (all scrapers enabled by default and toggleable).",
		ManifestURL:     "https://raw.githubusercontent.com/yoruix/nuvio-providers/refs/heads/main/manifest.json",
		Default:         true,
		DefaultScrapers: nil,
	},
	{
		ID:              "all-in-one-nuvio",
		Name:            "All-in-One Nuvio",
		Category:        "Anime, K-Drama & Global",
		Description:     "66 JS scrapers across anime, K-Drama and global hosts (all scrapers enabled by default).",
		ManifestURL:     "https://raw.githubusercontent.com/NuvioPlugin/All-in-One-Nuvio/refs/heads/main/manifest.json",
		Default:         true,
		DefaultScrapers: nil,
	},
	{
		ID:              "cb-providers",
		Name:            "CB Provider Repo",
		Category:        "Movies, Shows & Anime",
		Description:     "Optional 28-scraper community repo (all scrapers enabled by default).",
		ManifestURL:     "https://raw.githubusercontent.com/SA7ANI/cb-providers/main/manifest.json",
		Default:         false,
		DefaultScrapers: nil,
	},
	{
		ID:              "nuviotr",
		Name:            "MoOnCrOwN-NUVİO",
		Category:        "Turkish & Global",
		Description:     "Optional 18-scraper community repo (enabled manually in Settings).",
		ManifestURL:     "https://raw.githubusercontent.com/mooncrown04/nuviotr/refs/heads/main/manifest.json",
		Default:         false,
		DefaultScrapers: nil,
	},
}

func normPresetURL(raw string) string {
	u := strings.TrimSpace(raw)
	if norm, err := normalizeStremioManifestURL(u); err == nil {
		u = norm
	}
	return strings.TrimRight(strings.ToLower(u), "/")
}

// Presets returns all built-in Stremio addon and Nuvio repo presets annotated with install state.
func (m *Manager) Presets() PresetCatalog {
	addonURLs := map[string]bool{}
	addonIDs := map[string]bool{}
	if m != nil && m.Addons != nil {
		for _, a := range m.Addons.List() {
			addonURLs[normPresetURL(a.ManifestURL)] = true
			if a.ID != "" {
				addonIDs[strings.ToLower(a.ID)] = true
			}
		}
	}
	pluginURLs := map[string]bool{}
	pluginIDs := map[string]bool{}
	if m != nil && m.Plugins != nil {
		for _, r := range m.Plugins.List() {
			pluginURLs[normPresetURL(r.ManifestURL)] = true
			if r.ID != "" {
				pluginIDs[strings.ToLower(r.ID)] = true
			}
		}
	}

	addons := make([]PresetAddon, len(builtinAddonPresets))
	for i, p := range builtinAddonPresets {
		p.Installed = addonURLs[normPresetURL(p.ManifestURL)] || addonIDs[strings.ToLower(p.ID)]
		addons[i] = p
	}
	plugins := make([]PresetPluginRepo, len(builtinPluginPresets))
	for i, p := range builtinPluginPresets {
		p.Installed = pluginURLs[normPresetURL(p.ManifestURL)] || pluginIDs[strings.ToLower(p.ID)]
		plugins[i] = p
	}
	return PresetCatalog{Addons: addons, Plugins: plugins}
}

// ApplyPresets installs either a specific preset (kind+id) or the recommended default pack.
func (m *Manager) ApplyPresets(ctx context.Context, kind, id string, recommendedOnly bool) PresetSetupResult {
	res := PresetSetupResult{
		Installed: []string{},
		Skipped:   []string{},
	}
	if m == nil {
		return res
	}
	cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	kind = strings.ToLower(strings.TrimSpace(kind))
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" && kind == "" {
		recommendedOnly = true
	}

	cur := m.Presets()
	var targetAddons []PresetAddon
	var targetPlugins []PresetPluginRepo
	for _, a := range cur.Addons {
		if id != "" {
			if (kind == "" || kind == "addon") && strings.ToLower(a.ID) == id {
				targetAddons = append(targetAddons, a)
			}
		} else if (kind == "" || kind == "addon") && (!recommendedOnly || a.Default) {
			targetAddons = append(targetAddons, a)
		}
	}
	for _, p := range cur.Plugins {
		if id != "" {
			if (kind == "" || kind == "plugin") && strings.ToLower(p.ID) == id {
				targetPlugins = append(targetPlugins, p)
			}
		} else if (kind == "" || kind == "plugin") && (!recommendedOnly || p.Default) {
			targetPlugins = append(targetPlugins, p)
		}
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, a := range targetAddons {
		if a.Installed {
			res.Skipped = append(res.Skipped, a.Name)
			continue
		}
		if m.Addons == nil {
			continue
		}
		wg.Add(1)
		go func(item PresetAddon) {
			defer wg.Done()
			_, err := m.Addons.Add(cctx, item.ManifestURL)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				res.Errors = append(res.Errors, item.Name+": "+err.Error())
			} else {
				res.Installed = append(res.Installed, item.Name)
			}
		}(a)
	}
	for _, p := range targetPlugins {
		if p.Installed {
			res.Skipped = append(res.Skipped, p.Name)
			continue
		}
		if m.Plugins == nil {
			continue
		}
		wg.Add(1)
		go func(item PresetPluginRepo) {
			defer wg.Done()
			_, err := m.Plugins.AddRepoWithDefaults(cctx, item.ManifestURL, item.DefaultScrapers)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				res.Errors = append(res.Errors, item.Name+": "+err.Error())
			} else {
				res.Installed = append(res.Installed, item.Name)
			}
		}(p)
	}
	wg.Wait()
	res.Catalog = m.Presets()
	return res
}

// BootstrapDefaultsIfEmpty automatically sets up the curated addons and Nuvio provider repos
// once on first launch when no streaming config files exist yet.
func (m *Manager) BootstrapDefaultsIfEmpty(ctx context.Context) {
	if m == nil || m.ConfigDir == "" || os.Getenv("HOLYARRS_CONFIG_IN_MEMORY") != "" {
		return
	}
	addonsPath := filepath.Join(m.ConfigDir, "stream-addons.json")
	pluginsPath := filepath.Join(m.ConfigDir, "stream-plugins.json")
	if _, err := os.Stat(addonsPath); err == nil {
		return
	}
	if _, err := os.Stat(pluginsPath); err == nil {
		return
	}
	if (m.Addons != nil && len(m.Addons.List()) > 0) || (m.Plugins != nil && len(m.Plugins.List()) > 0) {
		return
	}

	res := m.ApplyPresets(ctx, "", "", true)
	log.Printf("stream presets: initial setup installed %d (%v), skipped %d, errors %d",
		len(res.Installed), res.Installed, len(res.Skipped), len(res.Errors))
}
