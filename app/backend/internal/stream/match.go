package stream

import (
	"math"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/HolyArrs/holyarrs/app/backend/internal/metadata"
)

// SearchMatchContext describes the target title and classification metadata used
// to gate specialty scrapers and reject mismatched instant/addon candidates.
type SearchMatchContext struct {
	Kind             string
	TMDB             string
	Title            string
	OriginalTitle    string
	Year             int
	Season           int
	Episode          int
	Genres           []string
	OriginalLanguage string
	OriginCountry    []string
	Anime            bool
	Animation        bool
	AsianDrama       bool
	HasMeta          bool
}

// NewSearchMatchContext builds a normalized matching context from search inputs
// and TMDB classification metadata.
func NewSearchMatchContext(kind, title, origTitle string, year, season, episode int, genres []string, origLang string, originCountry []string, explicitAnime bool) SearchMatchContext {
	lang := strings.ToLower(strings.TrimSpace(origLang))
	hasAnim := false
	for _, g := range genres {
		gl := strings.ToLower(strings.TrimSpace(g))
		if gl == "animation" || gl == "anime" {
			hasAnim = true
			break
		}
	}
	isJP := lang == "ja"
	isAsian := isJP || lang == "ko" || lang == "zh" || lang == "th"
	for _, c := range originCountry {
		cc := strings.ToUpper(strings.TrimSpace(c))
		if cc == "JP" {
			isJP = true
			isAsian = true
		} else if cc == "KR" || cc == "CN" || cc == "TW" || cc == "TH" || cc == "HK" {
			isAsian = true
		}
	}
	isAnime := explicitAnime || metadata.LooksAnime(genres, originCountry, lang) || (hasAnim && isAsian)
	return SearchMatchContext{
		Kind:             kind,
		Title:            strings.TrimSpace(title),
		OriginalTitle:    strings.TrimSpace(origTitle),
		Year:             year,
		Season:           season,
		Episode:          episode,
		Genres:           genres,
		OriginalLanguage: lang,
		OriginCountry:    originCountry,
		Anime:            isAnime,
		Animation:        hasAnim || isAnime,
		AsianDrama:       !isAnime && isAsian,
		HasMeta:          explicitAnime || len(genres) > 0 || lang != "" || len(originCountry) > 0,
	}
}

func scraperSupportsKind(types []string, kind string, isAnime bool) bool {
	if len(types) == 0 {
		return true
	}
	want := "movie"
	if kind == "show" {
		want = "tv"
	}
	hasWant := false
	hasAnimeType := false
	for _, t := range types {
		low := strings.ToLower(strings.TrimSpace(t))
		if low == "anime" {
			hasAnimeType = true
			if isAnime {
				hasWant = true
			}
			continue
		}
		if low == want || low == kind || (want == "tv" && low == "show") {
			hasWant = true
		}
	}
	if len(types) == 1 && hasAnimeType && !isAnime {
		return false
	}
	return hasWant
}

var animeScraperIDs = map[string]bool{
	"animetsu":     true,
	"kurage":       true,
	"anizone":      true,
	"anikoto":      true,
	"anikototv":    true,
	"anidb":        true,
	"anizen":       true,
	"anikage":      true,
	"vidnestanime": true,
	"reanime":      true,
	"hianime":      true,
}

func isAnimeOnlyScraper(sc NuvioScraper) bool {
	id := strings.ToLower(strings.TrimSpace(sc.ID))
	name := strings.ToLower(strings.TrimSpace(sc.Name))
	desc := strings.ToLower(strings.TrimSpace(sc.Description))
	if strings.Contains(id, "anime") || strings.Contains(name, "anime") || animeScraperIDs[id] {
		return true
	}
	for _, t := range sc.SupportedTypes {
		if strings.EqualFold(strings.TrimSpace(t), "anime") {
			return true
		}
	}
	if !strings.Contains(desc, "movies, series") && (strings.Contains(desc, "anime streaming") || strings.Contains(desc, "anime provider") || strings.Contains(desc, "anime from") || strings.Contains(desc, "subbed and dubbed anime") || strings.Contains(desc, "watch anime")) {
		return true
	}
	return false
}

func isCartoonOnlyScraper(sc NuvioScraper) bool {
	id := strings.ToLower(strings.TrimSpace(sc.ID))
	name := strings.ToLower(strings.TrimSpace(sc.Name))
	return strings.Contains(id, "cartoon") || strings.Contains(id, "toon") || strings.Contains(name, "cartoon") || strings.Contains(name, "toon")
}

func isAsianDramaOnlyScraper(sc NuvioScraper) bool {
	id := strings.ToLower(strings.TrimSpace(sc.ID))
	name := strings.ToLower(strings.TrimSpace(sc.Name))
	return strings.Contains(id, "kdrama") || strings.Contains(id, "kisskh") || strings.Contains(name, "kdrama") || strings.Contains(name, "kisskh")
}

func scraperMatchesContext(sc NuvioScraper, kind string, mc SearchMatchContext) bool {
	if isCartoonOnlyScraper(sc) {
		if !scraperSupportsKind(sc.SupportedTypes, kind, mc.Anime || mc.Animation) {
			return false
		}
		return mc.Animation
	}
	if isAnimeOnlyScraper(sc) {
		if !mc.Anime {
			return false
		}
		return scraperSupportsKind(sc.SupportedTypes, kind, true)
	}
	if !scraperSupportsKind(sc.SupportedTypes, kind, mc.Anime) {
		return false
	}
	if isAsianDramaOnlyScraper(sc) && mc.HasMeta && !mc.AsianDrama {
		return false
	}
	return true
}

var (
	reCandSeasonEp      = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])s(\d{1,2})\s*e(\d{1,3})(?:\s*[-]\s*e?(\d{1,3})|\s*e(\d{1,3}))?(?:[^a-z0-9]|$)`)
	reCandCrossEp       = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(\d{1,2})x(\d{1,3})(?:[^a-z0-9]|$)`)
	reCandSeasonOnly    = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:s(\d{1,2})|season\s+(\d{1,2}))(?:[^a-z0-9]|$)`)
	reTechBoundary      = regexp.MustCompile(`(?i)(?:^|[\s._(\[-])(s\d{1,2}\s*e\d{1,3}|\d{1,2}x\d{1,3}|season\s+\d{1,2}|2160p|1080p|720p|480p|4k|uhd|fhd|web-dl|webdl|webrip|bluray|bdrip|hdrip|hdcam|cam|hdtv|remux|x264|x265|h264|h265|hevc|av1|10bit|dual\s+audio|multi\s+audio|esub)(?:$|[\s._)\]-])`)
	reServerNumLabel    = regexp.MustCompile(`(?i)\b(?:server|srv|mirror|source|link|node|cdn|ep|episode|part)\s*#?\d+\b`)
	reSceneIndicators   = regexp.MustCompile(`(?i)(moviesdrive|hdhub4u|4khdhub|uhdmovies|web-dl|webrip|bluray|dual audio)`)
	reStatsLineCheck    = regexp.MustCompile(`(?i)^(server|source|quality|auto|hls|mp4|hd|4k|1080p|720p|480p|\d+(\.\d+)?\s*(gb|mb))`)
	reURLTMDBID         = regexp.MustCompile(`(?i)/(?:tv|movie)/(\d+)(?:[-/]|$)`)
	videoExtSuffixes    = []string{".mkv", ".mp4", ".avi", ".webm", ".mov", ".m4v", ".ts"}
	docEmojiMarker      = "\U0001F4C4" // document page emoji used by Nuvio scrapers before filename
	emojiStopMarkers    = []string{"\U0001F310", "\U0001F4E6", "\U0001F3F7", "\U0001F517", "\U0001F4BE", "\U0001F464", "\U0001F3AC"}
	genericCDNBasenames = map[string]bool{"index": true, "master": true, "playlist": true, "video": true, "stream": true, "play": true, "media": true, "chunk": true, "segment": true, "output": true}
)

var stopwordTokens = map[string]bool{
	"the": true, "a": true, "an": true, "of": true, "in": true, "on": true, "at": true, "to": true, "for": true,
	"and": true, "or": true, "is": true, "it": true, "my": true, "you": true, "your": true, "we": true, "he": true,
	"she": true, "they": true, "this": true, "that": true, "when": true, "what": true, "where": true, "who": true,
	"how": true, "with": true, "from": true, "by": true, "as": true, "be": true, "do": true, "if": true, "so": true,
	"no": true, "not": true, "all": true, "season": true, "episode": true, "film": true, "vol": true, "volume": true,
}

var genericStreamTokens = map[string]bool{
	"server": true, "stream": true, "direct": true, "auto": true, "hls": true, "mp4": true, "mkv": true, "avi": true,
	"proxy": true, "cdn": true, "main": true, "backup": true, "fast": true, "vip": true, "pro": true, "hd": true,
	"sd": true, "hq": true, "org": true, "original": true, "native": true, "sub": true, "dub": true, "raw": true,
	"audio": true, "video": true, "quality": true, "source": true, "link": true, "mirror": true, "play": true,
	"player": true, "cloud": true, "drive": true, "hub": true, "r2": true, "file": true, "download": true,
	"premium": true, "default": true, "primary": true, "secondary": true, "english": true, "hindi": true,
	"korean": true, "japanese": true, "spanish": true, "french": true, "german": true, "arabic": true,
	"tamil": true, "telugu": true, "bengali": true, "dual": true, "multi": true, "lang": true, "language": true,
	"unknown": true, "full": true, "movie": true, "series": true, "show": true, "complete": true, "esub": true,
	"msub": true, "v1": true, "v2": true, "s1": true, "s2": true, "s3": true, "s4": true,
	"hubcloud": true, "gdflix": true, "pixeldrain": true, "gofile": true, "megacloud": true, "filemoon": true,
	"streamwish": true, "vidplay": true, "ok": true, "fhd": true, "uhd": true, "4k": true,
	"2160p": true, "1080p": true, "720p": true, "480p": true,
	"eng": true, "hin": true, "kor": true, "jpn": true, "spa": true, "fre": true, "fra": true,
	"ger": true, "deu": true, "ita": true, "por": true, "rus": true, "ara": true, "tur": true,
	"tam": true, "tel": true, "mal": true, "kan": true, "vf": true, "vostfr": true,
	"subbed": true, "dubbed": true, "softsub": true, "hardsub": true, "multisub": true,
	"aac": true, "eac3": true, "ac3": true, "ddp": true, "hdr": true, "hdr10": true, "dv": true,
}

var romanSequelFold = map[string]string{
	"ii": "2", "two": "2",
	"iii": "3", "three": "3",
	"iv": "4", "four": "4",
	"v": "5", "five": "5",
	"vi": "6", "six": "6",
}

// FilterVerifiedCandidates removes mismatched candidates (wrong episode, year, or release title).
func FilterVerifiedCandidates(cands []UnifiedCandidate, mc SearchMatchContext) []UnifiedCandidate {
	if len(cands) == 0 {
		return cands
	}
	out := make([]UnifiedCandidate, 0, len(cands))
	for _, c := range cands {
		if VerifyCandidate(c, mc) {
			out = append(out, c)
		}
	}
	return out
}

// VerifyCandidate checks whether a candidate matches the requested title, episode, and year.
func VerifyCandidate(c UnifiedCandidate, mc SearchMatchContext) bool {
	if c.Kind == "local" {
		return true
	}
	if mc.TMDB != "" && c.StreamURL != "" && (c.ProviderType == "nuvio" || c.ProviderType == "stremio") {
		if m := reURLTMDBID.FindStringSubmatch(c.StreamURL); len(m) == 2 && m[1] != mc.TMDB {
			return false
		}
	}
	if strings.TrimSpace(mc.Title) == "" && strings.TrimSpace(mc.OriginalTitle) == "" {
		return true
	}
	rels, hasExplicitFilename := extractActualReleaseStrings(c)
	if len(rels) == 0 {
		return true
	}

	if mc.Kind == "show" && mc.Season > 0 && mc.Episode > 0 {
		for _, rs := range rels {
			if !matchesShowEpisode(rs, mc.Season, mc.Episode) {
				return false
			}
		}
	}

	isAddonHTTP := (c.ProviderType == "nuvio" || c.ProviderType == "stremio") && (c.Kind == "http" || c.Kind == "hls")
	if !isAddonHTTP {
		return true
	}

	if mc.Kind == "movie" && mc.Year > 1900 {
		for _, rs := range rels {
			if !matchesMovieYear(rs, mc.Year, mc.Title, mc.OriginalTitle) {
				return false
			}
		}
	}

	targets := buildTargetTokenSets(mc.Title, mc.OriginalTitle)
	if len(targets) == 0 {
		return true
	}
	provTokens := tokenSet(normalizeBaseTokens(c.Provider))
	origNonASCII := containsNonASCII(mc.OriginalTitle)

	for _, rs := range rels {
		if mc.Anime && origNonASCII && !hasExplicitFilename && !reSceneIndicators.MatchString(rs) {
			continue
		}
		candToks := candidateTitleTokens(rs, provTokens)
		if len(candToks) == 0 {
			continue
		}
		if !matchesAnyTargetTitle(candToks, targets) {
			return false
		}
	}
	return true
}

func extractActualReleaseStrings(c UnifiedCandidate) ([]string, bool) {
	var out []string
	seen := map[string]bool{}
	hasDocOrFile := c.Filename != ""
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}

	docFound := false
	for _, raw := range []string{c.ReleaseName, c.Title} {
		if raw == "" {
			continue
		}
		if idx := strings.Index(raw, docEmojiMarker); idx >= 0 {
			docFound = true
			hasDocOrFile = true
			sub := raw[idx+len(docEmojiMarker):]
			sub = cutAtStopMarker(sub)
			add(sub)
		}
	}
	if !docFound {
		if c.Filename != "" {
			add(c.Filename)
		}
		fallback := strings.TrimSpace(fallbackString(c.ReleaseName, c.Title))
		if fallback != "" {
			fallback = strings.TrimSpace(strings.TrimPrefix(fallback, "\U0001F3AC"))
			line := cutAtStopMarker(fallback)
			if line != "" && !reStatsLineCheck.MatchString(line) {
				add(line)
			}
		}
	}
	if base := extractURLReleaseBasename(c.StreamURL); base != "" {
		hasDocOrFile = true
		add(base)
	}
	return out, hasDocOrFile
}

func cutAtStopMarker(s string) string {
	if nl := strings.IndexAny(s, "\r\n"); nl >= 0 {
		s = s[:nl]
	}
	for _, m := range emojiStopMarkers {
		if i := strings.Index(s, m); i >= 0 {
			s = s[:i]
		}
	}
	return strings.TrimSpace(s)
}

func extractURLReleaseBasename(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Path == "" {
		return ""
	}
	base, err := url.PathUnescape(path.Base(u.Path))
	if err != nil || base == "" || base == "." || base == "/" {
		return ""
	}
	low := strings.ToLower(base)
	hasVidExt := false
	for _, ext := range videoExtSuffixes {
		if strings.HasSuffix(low, ext) {
			hasVidExt = true
			base = base[:len(base)-len(ext)]
			break
		}
	}
	clean := strings.ReplaceAll(base, "_", " ")
	hasTag := reCandSeasonEp.MatchString(clean) || strings.Contains(low, "1080p") || strings.Contains(low, "2160p") || strings.Contains(low, "720p")
	if !hasVidExt && !hasTag {
		return ""
	}
	toks := normalizeBaseTokens(cutBeforeTechBoundary(base))
	alphaWords := 0
	for _, tk := range toks {
		if len(tk) < 2 || genericCDNBasenames[tk] || genericStreamTokens[tk] {
			continue
		}
		if isAllASCIIAlpha(tk) {
			alphaWords++
		}
	}
	if alphaWords < 2 {
		return ""
	}
	return base
}

func isAllASCIIAlpha(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 'a' || s[i] > 'z' {
			return false
		}
	}
	return len(s) > 0
}

func matchesShowEpisode(rs string, wantSeason, wantEp int) bool {
	clean := strings.ReplaceAll(rs, "_", " ")
	if m := reCandSeasonEp.FindStringSubmatch(clean); len(m) >= 5 {
		sNum, _ := strconv.Atoi(m[1])
		eStart, _ := strconv.Atoi(m[2])
		eEnd := eStart
		if endStr := fallbackString(m[3], m[4]); endStr != "" {
			if n, err := strconv.Atoi(endStr); err == nil && n >= eStart {
				eEnd = n
			}
		}
		return sNum == wantSeason && wantEp >= eStart && wantEp <= eEnd
	}
	if m := reCandCrossEp.FindStringSubmatch(clean); len(m) == 3 {
		sNum, _ := strconv.Atoi(m[1])
		eNum, _ := strconv.Atoi(m[2])
		return sNum == wantSeason && eNum == wantEp
	}
	if m := reCandSeasonOnly.FindStringSubmatch(clean); len(m) == 3 {
		sNum, _ := strconv.Atoi(fallbackString(m[1], m[2]))
		return sNum == wantSeason
	}
	return true
}

func matchesMovieYear(rs string, wantYear int, title, origTitle string) bool {
	toks := normalizeBaseTokens(stripVideoExt(rs))
	if len(toks) < 2 {
		return true
	}
	titleYears := tokenSet(append(normalizeBaseTokens(title), normalizeBaseTokens(origTitle)...))
	var candYears []int
	for _, tk := range toks[1:] {
		if len(tk) != 4 || tk == "2160" {
			continue
		}
		if y, err := strconv.Atoi(tk); err == nil && y >= 1920 && y <= 2035 {
			if titleYears[tk] && int(math.Abs(float64(y-wantYear))) > 1 {
				continue
			}
			candYears = append(candYears, y)
		}
	}
	if len(candYears) == 0 {
		return true
	}
	for _, y := range candYears {
		if int(math.Abs(float64(y-wantYear))) <= 1 {
			return true
		}
	}
	return false
}

func stripVideoExt(s string) string {
	low := strings.ToLower(strings.TrimSpace(s))
	for _, ext := range videoExtSuffixes {
		if strings.HasSuffix(low, ext) {
			return s[:len(s)-len(ext)]
		}
	}
	return s
}

func cutBeforeTechBoundary(s string) string {
	s = stripVideoExt(s)
	clean := strings.ReplaceAll(s, "_", " ")
	for _, loc := range reTechBoundary.FindAllStringSubmatchIndex(clean, -1) {
		if loc[2] > 0 {
			return strings.TrimSpace(clean[:loc[2]])
		}
	}
	toks := normalizeBaseTokens(clean)
	for i := 1; i < len(toks); i++ {
		if len(toks[i]) == 4 && toks[i] != "2160" {
			if y, err := strconv.Atoi(toks[i]); err == nil && y >= 1920 && y <= 2035 {
				return strings.Join(toks[:i], " ")
			}
		}
	}
	return clean
}

func normalizeBaseTokens(s string) []string {
	s = strings.ReplaceAll(s, "'", "")
	s = strings.ReplaceAll(s, "\u2019", "")
	parts := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if rep, ok := romanSequelFold[p]; ok {
			p = rep
		}
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func significantTokens(toks []string) []string {
	out := make([]string, 0, len(toks))
	for _, tk := range toks {
		if !stopwordTokens[tk] {
			out = append(out, tk)
		}
	}
	if len(out) == 0 && len(toks) > 0 {
		return slices.Clone(toks)
	}
	return out
}

func buildTargetTokenSets(title, origTitle string) [][]string {
	var sets [][]string
	for _, raw := range []string{title, origTitle} {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		sig := significantTokens(normalizeBaseTokens(raw))
		if len(sig) > 0 {
			sets = append(sets, sig)
		}
	}
	return sets
}

func candidateTitleTokens(rs string, provTokens map[string]bool) []string {
	head := cutBeforeTechBoundary(rs)
	head = reServerNumLabel.ReplaceAllString(head, " ")
	raw := normalizeBaseTokens(head)
	filtered := make([]string, 0, len(raw))
	for _, tk := range raw {
		if provTokens[tk] || genericStreamTokens[tk] {
			continue
		}
		filtered = append(filtered, tk)
	}
	if len(filtered) == 0 {
		return nil
	}
	return significantTokens(filtered)
}

func matchesAnyTargetTitle(candToks []string, targets [][]string) bool {
	for _, target := range targets {
		if tokensMatchTarget(candToks, target) {
			return true
		}
	}
	return false
}

func tokensMatchTarget(candToks, targetToks []string) bool {
	matched := 0
	used := make([]bool, len(candToks))
	for _, tt := range targetToks {
		for ci, ct := range candToks {
			if used[ci] {
				continue
			}
			if tokenSimilar(ct, tt) {
				used[ci] = true
				matched++
				break
			}
		}
	}
	switch len(targetToks) {
	case 0:
		return true
	case 1:
		cCov := float64(matched) / float64(len(candToks))
		return matched >= 1 && cCov >= 0.34
	case 2:
		return matched >= 2 || (matched == 1 && len(candToks) <= 2)
	default:
		tCov := float64(matched) / float64(len(targetToks))
		cCov := float64(matched) / float64(len(candToks))
		return matched >= 2 || (tCov >= 0.5 && cCov >= 0.5)
	}
}

func tokenSimilar(a, b string) bool {
	if a == b {
		return true
	}
	if len(a) >= 4 && len(b) >= 4 {
		return a == b+"s" || a == b+"es" || b == a+"s" || b == a+"es"
	}
	return false
}

func tokenSet(toks []string) map[string]bool {
	m := make(map[string]bool, len(toks))
	for _, tk := range toks {
		if tk != "" {
			m[tk] = true
		}
	}
	return m
}

func containsNonASCII(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII {
			return true
		}
	}
	return false
}
