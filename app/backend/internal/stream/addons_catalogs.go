package stream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SearchSubtitles queries enabled Stremio subtitle addons and caches resolved URLs by opaque id.
func (s *AddonStore) SearchSubtitles(ctx context.Context, kind, imdbID, tmdbID string, season, episode int) []AddonSubCandidate {
	addons := s.enabledFor("subtitles", kind)
	if len(addons) == 0 {
		return nil
	}
	stremioType := "movie"
	if kind == "show" {
		stremioType = "series"
	}

	var mu sync.Mutex
	var out []AddonSubCandidate
	var wg sync.WaitGroup
	for _, addon := range addons {
		mediaID := pickAddonMediaID(addon, kind, imdbID, tmdbID, season, episode)
		if mediaID == "" {
			continue
		}
		wg.Add(1)
		go func(a StremioAddon, id string) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 6*time.Second)
			defer cancel()
			endpoint := a.BaseURL + "/subtitles/" + url.PathEscape(stremioType) + "/" + url.PathEscape(id) + ".json"
			req, err := http.NewRequestWithContext(cctx, http.MethodGet, endpoint, nil)
			if err != nil {
				return
			}
			req.Header.Set("User-Agent", defaultStreamUserAgent)
			resp, err := s.httpClient().Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return
			}
			var parsed struct {
				Subtitles []struct {
					ID   string `json:"id"`
					URL  string `json:"url"`
					Lang string `json:"lang"`
				} `json:"subtitles"`
			}
			if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&parsed) != nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			now := time.Now()
			for _, sub := range parsed.Subtitles {
				u, perr := url.Parse(strings.TrimSpace(sub.URL))
				if perr != nil || (u.Scheme != "http" && u.Scheme != "https") {
					continue
				}
				cid := "stremio:" + newOpaqueID()
				lang := fallbackString(sub.Lang, "und")
				s.subCache[cid] = cachedAddonSub{
					url:       u.String(),
					lang:      lang,
					addonName: a.Name,
					expiresAt: now.Add(30 * time.Minute),
				}
				out = append(out, AddonSubCandidate{
					ID:        cid,
					SiteName:  a.Name,
					SiteKind:  "stremio",
					Language:  lang,
					Format:    strings.TrimPrefix(strings.ToLower(filepath.Ext(u.Path)), "."),
					AddonName: a.Name,
				})
			}
		}(addon, mediaID)
	}
	wg.Wait()
	return out
}

// FetchSubtitle downloads a cached Stremio subtitle URL for conversion to WebVTT.
func (s *AddonStore) FetchSubtitle(ctx context.Context, id string) ([]byte, string, string, error) {
	s.mu.RLock()
	item, ok := s.subCache[id]
	s.mu.RUnlock()
	if !ok || time.Now().After(item.expiresAt) {
		return nil, "", "", errors.New("subtitle candidate expired")
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, item.url, nil)
	if err != nil {
		return nil, "", "", err
	}
	req.Header.Set("User-Agent", defaultStreamUserAgent)
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", "", fmt.Errorf("subtitle host returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, "", "", err
	}
	hint := filepath.Base(resp.Request.URL.Path)
	return data, hint, item.lang, nil
}

// isAnimeCatalog reports whether a catalog exposes anime content (including Stremio "series" catalogs from anime addons).
func isAnimeCatalog(addon StremioAddon, cat AddonCatalogDef) bool {
	if strings.EqualFold(strings.TrimSpace(cat.Type), "anime") {
		return true
	}
	haystack := strings.ToLower(addon.ID + " " + addon.Name + " " + cat.ID + " " + cat.Name)
	for _, kw := range []string{"anime", "kitsu", "anilist", "myanimelist"} {
		if strings.Contains(haystack, kw) {
			return true
		}
	}
	return false
}

// BrowseCatalogs returns metadata shelves exposed by enabled Stremio addons, fetching up to 6 shelves.
func (s *AddonStore) BrowseCatalogs(ctx context.Context, kind string) []AddonCatalogShelf {
	k := strings.ToLower(strings.TrimSpace(kind))
	switch k {
	case "movies":
		k = "movie"
	case "shows":
		k = "show"
	}
	addons := s.List()
	type catJob struct {
		addon StremioAddon
		cat   AddonCatalogDef
	}
	var jobs []catJob
	for _, a := range addons {
		if !a.Enabled {
			continue
		}
		for _, c := range a.Catalogs {
			animeCat := isAnimeCatalog(a, c)
			cKind := normalizeCatalogKind(c.Type)
			switch k {
			case "anime":
				if !animeCat {
					continue
				}
			case "movie", "show":
				if animeCat || cKind != k {
					continue
				}
			default:
				if animeCat {
					continue
				}
			}
			jobs = append(jobs, catJob{addon: a, cat: c})
			if len(jobs) >= 6 {
				break
			}
		}
		if len(jobs) >= 6 {
			break
		}
	}
	if len(jobs) == 0 {
		return nil
	}
	var mu sync.Mutex
	var shelves []AddonCatalogShelf
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func(job catJob) {
			defer wg.Done()
			items, err := s.FetchCatalog(ctx, job.addon.ID, job.cat.Type, job.cat.ID, "", 0)
			if err != nil || len(items) == 0 {
				return
			}
			mu.Lock()
			shelves = append(shelves, AddonCatalogShelf{
				AddonID:   job.addon.ID,
				AddonName: job.addon.Name,
				CatalogID: job.cat.ID,
				Type:      job.cat.Type,
				Name:      job.cat.Name,
				Items:     items,
			})
			mu.Unlock()
		}(j)
	}
	wg.Wait()
	return shelves
}

// FetchCatalog reads one Stremio catalog (10m cache) and rewrites external artwork through poster proxy.
func (s *AddonStore) FetchCatalog(ctx context.Context, addonID, mediaType, catalogID, genre string, skip int) ([]AddonCatalogItem, error) {
	cacheKey := fmt.Sprintf("%s|%s|%s|%s|%d", addonID, mediaType, catalogID, genre, skip)
	s.mu.RLock()
	cached, ok := s.catCache[cacheKey]
	var baseURL string
	for _, a := range s.addons {
		if a.ID == addonID {
			baseURL = a.BaseURL
			break
		}
	}
	s.mu.RUnlock()
	if ok && time.Now().Before(cached.expiresAt) {
		return cached.items, nil
	}
	if baseURL == "" {
		return nil, errors.New("addon not found")
	}

	var extra []string
	if genre != "" {
		extra = append(extra, "genre="+url.PathEscape(genre))
	}
	if skip > 0 {
		extra = append(extra, "skip="+strconv.Itoa(skip))
	}
	endpoint := baseURL + "/catalog/" + url.PathEscape(mediaType) + "/" + url.PathEscape(catalogID)
	if len(extra) > 0 {
		endpoint += "/" + strings.Join(extra, "&")
	}
	endpoint += ".json"

	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
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
		return nil, fmt.Errorf("catalog HTTP %d", resp.StatusCode)
	}
	var raw struct {
		Metas []struct {
			ID          string   `json:"id"`
			Type        string   `json:"type"`
			Name        string   `json:"name"`
			Poster      string   `json:"poster"`
			Background  string   `json:"background"`
			ReleaseInfo string   `json:"releaseInfo"`
			Description string   `json:"description"`
			Genres      []string `json:"genres"`
		} `json:"metas"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&raw); err != nil {
		return nil, err
	}
	items := make([]AddonCatalogItem, 0, len(raw.Metas))
	for _, m := range raw.Metas {
		if m.ID == "" || m.Name == "" {
			continue
		}
		item := AddonCatalogItem{
			ID:          m.ID,
			Kind:        normalizeCatalogKind(fallbackString(m.Type, mediaType)),
			Name:        m.Name,
			Year:        m.ReleaseInfo,
			Description: m.Description,
			Genres:      m.Genres,
		}
		if strings.HasPrefix(m.ID, "tt") {
			item.IMDbID = m.ID
		} else if strings.HasPrefix(m.ID, "tmdb:") {
			item.TMDBID, _ = strconv.Atoi(strings.TrimPrefix(m.ID, "tmdb:"))
		}
		if strings.HasPrefix(m.Poster, "http://") || strings.HasPrefix(m.Poster, "https://") {
			item.Poster = "/api/v1/streaming/proxy/poster?url=" + url.QueryEscape(m.Poster)
		}
		if strings.HasPrefix(m.Background, "http://") || strings.HasPrefix(m.Background, "https://") {
			item.Background = "/api/v1/streaming/proxy/poster?url=" + url.QueryEscape(m.Background)
		}
		items = append(items, item)
		if len(items) >= 30 {
			break
		}
	}
	s.mu.Lock()
	s.catCache[cacheKey] = cachedAddonCatalog{items: items, expiresAt: time.Now().Add(10 * time.Minute)}
	s.mu.Unlock()
	return items, nil
}

func (s *AddonStore) enabledFor(resource, kind string) []StremioAddon {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []StremioAddon
	for _, a := range s.addons {
		if !a.Enabled {
			continue
		}
		hasRes := false
		for _, r := range a.Resources {
			if r == resource {
				hasRes = true
				break
			}
		}
		if !hasRes {
			continue
		}
		if len(a.Types) > 0 && kind != "" {
			matchType := false
			for _, t := range a.Types {
				if normalizeCatalogKind(t) == kind || t == "anime" {
					matchType = true
					break
				}
			}
			if !matchType {
				continue
			}
		}
		out = append(out, a)
	}
	return out
}

func pickAddonMediaID(addon StremioAddon, kind, imdbID, tmdbID string, season, episode int) string {
	suffix := ""
	if kind == "show" && season > 0 && episode > 0 {
		suffix = fmt.Sprintf(":%d:%d", season, episode)
	}
	supportsPrefix := func(pre string) bool {
		if len(addon.IDPrefixes) == 0 {
			return pre == "tt"
		}
		for _, p := range addon.IDPrefixes {
			if p == pre {
				return true
			}
		}
		return false
	}
	if imdbID != "" && supportsPrefix("tt") {
		return imdbID + suffix
	}
	if tmdbID != "" && supportsPrefix("tmdb:") {
		return "tmdb:" + tmdbID + suffix
	}
	if tmdbID != "" && supportsPrefix("tmdb") {
		return "tmdb:" + tmdbID + suffix
	}
	if imdbID != "" {
		return imdbID + suffix
	}
	return ""
}

func normalizeCatalogKind(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "movie":
		return "movie"
	default:
		return "show"
	}
}

func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func fallbackString(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return strings.TrimSpace(a)
	}
	return strings.TrimSpace(b)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

var (
	reHumanSize = regexp.MustCompile(`(?i)([\d.]+)\s*(gb|gib|mb|mib|tb|tib)\b`)
	reSeedHint  = regexp.MustCompile(`(?i)(?:👤|seed(?:er)?s?[:\s]*)\s*(\d+)`)
)

func parseHumanSizeBytes(s string) int64 {
	m := reHumanSize.FindStringSubmatch(s)
	if len(m) != 3 {
		return 0
	}
	val, err := strconv.ParseFloat(m[1], 64)
	if err != nil || val <= 0 {
		return 0
	}
	switch strings.ToLower(m[2]) {
	case "tb", "tib":
		return int64(val * 1024 * 1024 * 1024 * 1024)
	case "gb", "gib":
		return int64(val * 1024 * 1024 * 1024)
	case "mb", "mib":
		return int64(val * 1024 * 1024)
	}
	return 0
}

func parseSeedersHint(s string) int {
	m := reSeedHint.FindStringSubmatch(s)
	if len(m) != 2 {
		return 0
	}
	v, _ := strconv.Atoi(m[1])
	return v
}

func buildMagnetFromHash(hash, name string, trackerSources []string) string {
	var b strings.Builder
	b.WriteString("magnet:?xt=urn:btih:")
	b.WriteString(hash)
	if name != "" {
		b.WriteString("&dn=")
		b.WriteString(url.QueryEscape(name))
	}
	seen := map[string]bool{}
	for _, src := range trackerSources {
		if strings.HasPrefix(src, "tracker:") {
			tr := strings.TrimPrefix(src, "tracker:")
			if tr != "" && !seen[tr] {
				seen[tr] = true
				b.WriteString("&tr=")
				b.WriteString(url.QueryEscape(tr))
			}
		}
	}
	for _, tr := range defaultTrackers {
		if !seen[tr] {
			seen[tr] = true
			b.WriteString("&tr=")
			b.WriteString(url.QueryEscape(tr))
		}
	}
	return b.String()
}
