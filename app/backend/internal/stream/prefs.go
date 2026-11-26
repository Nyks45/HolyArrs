package stream

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// SubStylePrefs controls web player subtitle styling, including HDR dimming
// and SDH hearing-impaired cue filtering.
type SubStylePrefs struct {
	Color         string  `json:"color"`
	BgColor       string  `json:"bg_color"`
	BgOpacity     float64 `json:"bg_opacity"`
	Size          float64 `json:"size"`
	OffsetPct     float64 `json:"offset_pct"`
	HDRDimmed     bool    `json:"hdr_dimmed"`
	HDRBrightness int     `json:"hdr_brightness"`
	StripSDH      bool    `json:"strip_sdh"`
	FontFamily    string  `json:"font_family"`
}

// StreamPrefs holds user playback, source filtering, skip, and sync preferences.
type StreamPrefs struct {
	ExcludeTerms      []string      `json:"exclude_terms"`
	Exclude3D         bool          `json:"exclude_3d"`
	PreferredQuality  string        `json:"preferred_quality"`
	MaxSizeGB         float64       `json:"max_size_gb"`
	AutoPlayFastest   bool          `json:"auto_play_fastest"`
	AutoFallback      bool          `json:"auto_fallback"`
	AutoResolveDebrid bool          `json:"auto_resolve_debrid"`
	AutoSkipIntro     bool          `json:"auto_skip_intro"`
	AutoSkipRecap     bool          `json:"auto_skip_recap"`
	AutoSkipOutro     bool          `json:"auto_skip_outro"`
	AnimeSkipClientID string        `json:"anime_skip_client_id,omitempty"`
	AutoFindSubtitles bool          `json:"auto_find_subtitles"`
	AutoSyncSubtitles bool          `json:"auto_sync_subtitles"`
	PreferredSubLangs []string      `json:"preferred_sub_langs"`
	ScrobbleTrakt     bool          `json:"scrobble_trakt"`
	ScrobbleSimkl     bool          `json:"scrobble_simkl"`
	TMDBAPIKey        string        `json:"tmdb_api_key,omitempty"`
	SubStyle          SubStylePrefs `json:"sub_style"`
}

// DefaultPrefs returns balanced defaults for streaming and player behavior.
func DefaultPrefs() StreamPrefs {
	return StreamPrefs{
		ExcludeTerms:      []string{"CAM", "HDCAM", "TS", "TELESYNC"},
		Exclude3D:         true,
		PreferredQuality:  "1080p",
		MaxSizeGB:         0,
		AutoPlayFastest:   false,
		AutoFallback:      true,
		AutoResolveDebrid: true,
		AutoSkipIntro:     false,
		AutoSkipRecap:     false,
		AutoSkipOutro:     false,
		AutoFindSubtitles: false,
		AutoSyncSubtitles: true,
		PreferredSubLangs: []string{"en", "tr"},
		ScrobbleTrakt:     true,
		ScrobbleSimkl:     true,
		SubStyle: SubStylePrefs{
			Color:         "#ffffff",
			BgColor:       "#000000",
			BgOpacity:     0.65,
			Size:          1.0,
			OffsetPct:     6,
			HDRDimmed:     true,
			HDRBrightness: 40,
			StripSDH:      false,
			FontFamily:    "sans",
		},
	}
}

// PrefsStore persists StreamPrefs atomically in configDir/stream-prefs.json.
type PrefsStore struct {
	mu   sync.RWMutex
	path string
	data StreamPrefs
}

// NewPrefsStore loads stream-prefs.json if present or initializes defaults.
func NewPrefsStore(configDir string) *PrefsStore {
	p := &PrefsStore{
		data: DefaultPrefs(),
	}
	if configDir != "" {
		p.path = filepath.Join(configDir, "stream-prefs.json")
		p.load()
	}
	return p
}

func (p *PrefsStore) load() {
	raw, err := os.ReadFile(p.path)
	if err != nil {
		return
	}
	cur := DefaultPrefs()
	if json.Unmarshal(raw, &cur) == nil {
		p.data = normalizePrefs(cur)
	}
}

// Get returns a defensive copy of the current streaming preferences.
func (p *PrefsStore) Get() StreamPrefs {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := p.data
	if len(p.data.ExcludeTerms) > 0 {
		out.ExcludeTerms = append([]string(nil), p.data.ExcludeTerms...)
	} else {
		out.ExcludeTerms = []string{}
	}
	if len(p.data.PreferredSubLangs) > 0 {
		out.PreferredSubLangs = append([]string(nil), p.data.PreferredSubLangs...)
	} else {
		out.PreferredSubLangs = []string{"en", "tr"}
	}
	return out
}

// Save normalizes, validates, and atomically writes new streaming preferences.
func (p *PrefsStore) Save(in StreamPrefs) (StreamPrefs, error) {
	norm := normalizePrefs(in)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.path != "" {
		if err := writeJSONAtomic(p.path, norm, 0o600); err != nil {
			return StreamPrefs{}, err
		}
	}
	p.data = norm
	out := norm
	out.ExcludeTerms = append([]string(nil), norm.ExcludeTerms...)
	out.PreferredSubLangs = append([]string(nil), norm.PreferredSubLangs...)
	return out, nil
}

func normalizePrefs(in StreamPrefs) StreamPrefs {
	def := DefaultPrefs()
	var terms []string
	seen := map[string]bool{}
	for _, t := range in.ExcludeTerms {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		k := strings.ToLower(t)
		if !seen[k] {
			seen[k] = true
			terms = append(terms, t)
		}
	}
	if terms == nil {
		terms = []string{}
	}
	in.ExcludeTerms = terms

	q := strings.ToLower(strings.TrimSpace(in.PreferredQuality))
	switch q {
	case "2160p", "1080p", "720p", "480p", "":
		in.PreferredQuality = q
	default:
		in.PreferredQuality = def.PreferredQuality
	}
	if in.MaxSizeGB < 0 {
		in.MaxSizeGB = 0
	}
	in.AnimeSkipClientID = strings.TrimSpace(in.AnimeSkipClientID)
	in.TMDBAPIKey = strings.TrimSpace(in.TMDBAPIKey)
	var langs []string
	seenLang := map[string]bool{}
	for _, l := range in.PreferredSubLangs {
		l = strings.ToLower(strings.TrimSpace(l))
		if l != "" && !seenLang[l] {
			seenLang[l] = true
			langs = append(langs, l)
		}
	}
	if len(langs) == 0 {
		langs = append([]string(nil), def.PreferredSubLangs...)
	}
	in.PreferredSubLangs = langs

	if strings.TrimSpace(in.SubStyle.Color) == "" {
		in.SubStyle.Color = def.SubStyle.Color
	}
	if strings.TrimSpace(in.SubStyle.BgColor) == "" {
		in.SubStyle.BgColor = def.SubStyle.BgColor
	}
	if in.SubStyle.BgOpacity < 0 {
		in.SubStyle.BgOpacity = 0
	} else if in.SubStyle.BgOpacity > 1 {
		in.SubStyle.BgOpacity = 1
	}
	if in.SubStyle.Size <= 0 {
		in.SubStyle.Size = def.SubStyle.Size
	} else if in.SubStyle.Size < 0.5 {
		in.SubStyle.Size = 0.5
	} else if in.SubStyle.Size > 2.5 {
		in.SubStyle.Size = 2.5
	}
	if in.SubStyle.OffsetPct < 0 {
		in.SubStyle.OffsetPct = 0
	} else if in.SubStyle.OffsetPct > 35 {
		in.SubStyle.OffsetPct = 35
	}
	if in.SubStyle.HDRBrightness <= 0 {
		in.SubStyle.HDRBrightness = def.SubStyle.HDRBrightness
	} else if in.SubStyle.HDRBrightness > 100 {
		in.SubStyle.HDRBrightness = 100
	} else if in.SubStyle.HDRBrightness < 10 {
		in.SubStyle.HDRBrightness = 10
	}
	switch strings.ToLower(strings.TrimSpace(in.SubStyle.FontFamily)) {
	case "sans", "serif", "mono":
		in.SubStyle.FontFamily = strings.ToLower(strings.TrimSpace(in.SubStyle.FontFamily))
	default:
		in.SubStyle.FontFamily = def.SubStyle.FontFamily
	}
	return in
}

func writeJSONAtomic(path string, v any, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}
