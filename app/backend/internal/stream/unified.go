package stream

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// UnifiedCandidate normalizes local files, instant Stremio/Nuvio HTTP/HLS streams,
// debrid-cached releases, and BitTorrent sources into one ranked model.
type UnifiedCandidate struct {
	ID             string            `json:"id"`
	Kind           string            `json:"kind"` // local, http, hls, debrid_cached, torrent
	Instant        bool              `json:"instant"`
	Title          string            `json:"title"`
	ReleaseName    string            `json:"release_name"`
	Provider       string            `json:"provider"`
	ProviderType   string            `json:"provider_type"` // local, stremio, nuvio, debrid, torrent
	SourceID       int64             `json:"source_id,omitempty"`
	SourceName     string            `json:"source_name,omitempty"`
	Link           string            `json:"link,omitempty"`
	Quality        string            `json:"quality,omitempty"`
	Resolution     string            `json:"resolution,omitempty"`
	Codec          string            `json:"codec,omitempty"`
	Audio          string            `json:"audio,omitempty"`
	HDR            string            `json:"hdr,omitempty"`
	Size           int64             `json:"size,omitempty"`
	Seeders        int               `json:"seeders,omitempty"`
	Peers          int               `json:"peers,omitempty"`
	StreamURL      string            `json:"-"`
	InfoHash       string            `json:"info_hash,omitempty"`
	Magnet         string            `json:"magnet,omitempty"`
	TorrentURL     string            `json:"torrent_url,omitempty"`
	IndexerID      int64             `json:"indexer_id,omitempty"`
	FileIdx        int               `json:"file_idx,omitempty"`
	LocalFileID    int64             `json:"local_file_id,omitempty"`
	DebridProvider string            `json:"debrid_provider,omitempty"`
	Headers        map[string]string `json:"-"`
	Filename       string            `json:"filename,omitempty"`
	Score          int               `json:"score"`
}

type cachedCandidate struct {
	cand      UnifiedCandidate
	expiresAt time.Time
}

// CandidateCache stores full stream candidate details (including private headers
// and resolved upstream URLs) keyed by opaque candidate ids.
type CandidateCache struct {
	mu    sync.Mutex
	items map[string]cachedCandidate
	ttl   time.Duration
	max   int
}

// NewCandidateCache constructs a bounded TTL candidate cache.
func NewCandidateCache() *CandidateCache {
	return &CandidateCache{
		items: map[string]cachedCandidate{},
		ttl:   30 * time.Minute,
		max:   2000,
	}
}

// Put stores candidates with generated opaque ids if absent and returns the
// updated slice with assigned IDs.
func (c *CandidateCache) Put(cands []UnifiedCandidate) []UnifiedCandidate {
	if c == nil || len(cands) == 0 {
		return cands
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.items) >= c.max {
		for k, v := range c.items {
			if now.After(v.expiresAt) {
				delete(c.items, k)
			}
		}
		if len(c.items) >= c.max {
			n := 0
			for k := range c.items {
				delete(c.items, k)
				n++
				if len(c.items) < c.max/2 || n > 500 {
					break
				}
			}
		}
	}

	out := make([]UnifiedCandidate, len(cands))
	for i, item := range cands {
		if item.ID == "" {
			item.ID = newOpaqueID()
		}
		out[i] = item
		c.items[item.ID] = cachedCandidate{
			cand:      item,
			expiresAt: now.Add(c.ttl),
		}
	}
	return out
}

// Get returns a cached candidate by opaque id if not expired.
func (c *CandidateCache) Get(id string) (UnifiedCandidate, bool) {
	if c == nil || id == "" {
		return UnifiedCandidate{}, false
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.items[id]
	if !ok {
		return UnifiedCandidate{}, false
	}
	if now.After(entry.expiresAt) {
		delete(c.items, id)
		return UnifiedCandidate{}, false
	}
	return entry.cand, true
}

func newOpaqueID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

var (
	re3DToken    = regexp.MustCompile(`(?i)\b(3d|hsbs|sbs|hou|half[\s.-]?sbs|half[\s.-]?ou)\b`)
	reInfoHashBT = regexp.MustCompile(`(?i)btih:([a-f0-9]{40})`)
)

// ExtractInfoHash extracts a 40-char lowercase hex BTIH hash from a magnet URI or candidate string.
func ExtractInfoHash(s string) string {
	m := reInfoHashBT.FindStringSubmatch(s)
	if len(m) == 2 {
		return strings.ToLower(m[1])
	}
	return ""
}

// EnrichReleaseMeta fills missing quality, codec, audio, and HDR metadata from release/title text.
func EnrichReleaseMeta(text string, q, codec, audio, hdr string) (string, string, string, string, bool) {
	low := strings.ToLower(text)
	if q == "" {
		switch {
		case strings.Contains(low, "2160p") || strings.Contains(low, "4k") || strings.Contains(low, "uhd"):
			q = "2160p"
		case strings.Contains(low, "1080p") || strings.Contains(low, "fhd"):
			q = "1080p"
		case strings.Contains(low, "720p"):
			q = "720p"
		case strings.Contains(low, "480p") || strings.Contains(low, "sd"):
			q = "480p"
		}
	}
	if codec == "" {
		switch {
		case strings.Contains(low, "av1"):
			codec = "AV1"
		case strings.Contains(low, "hevc") || strings.Contains(low, "x265") || strings.Contains(low, "h265") || strings.Contains(low, "h.265"):
			codec = "HEVC"
		case strings.Contains(low, "x264") || strings.Contains(low, "h264") || strings.Contains(low, "h.264") || strings.Contains(low, "avc"):
			codec = "H264"
		case strings.Contains(low, "vp9"):
			codec = "VP9"
		}
	}
	if audio == "" {
		switch {
		case strings.Contains(low, "atmos"):
			audio = "Atmos"
		case strings.Contains(low, "truehd"):
			audio = "TrueHD"
		case strings.Contains(low, "dts-hd") || strings.Contains(low, "dts:x"):
			audio = "DTS-HD"
		case strings.Contains(low, "ddp") || strings.Contains(low, "dd+") || strings.Contains(low, "eac3"):
			audio = "DDP"
		case strings.Contains(low, "aac"):
			audio = "AAC"
		}
	}
	if hdr == "" {
		switch {
		case strings.Contains(low, "dolby vision") || strings.Contains(low, "dovi") || regexp.MustCompile(`(?i)\bdv\b`).MatchString(text):
			hdr = "DV"
		case strings.Contains(low, "hdr10+"):
			hdr = "HDR10+"
		case strings.Contains(low, "hdr10"):
			hdr = "HDR10"
		case strings.Contains(low, "hdr"):
			hdr = "HDR"
		}
	}
	is3D := re3DToken.MatchString(text)
	return q, codec, audio, hdr, is3D
}

// FilterAndRankCandidates applies user filters (exclude terms, 3D, max GB) and
// ranks Local > Debrid Cached / HTTP / HLS > Torrents.
func FilterAndRankCandidates(cands []UnifiedCandidate, prefs StreamPrefs) []UnifiedCandidate {
	maxBytes := int64(0)
	if prefs.MaxSizeGB > 0 {
		maxBytes = int64(prefs.MaxSizeGB * 1024 * 1024 * 1024)
	}
	prefQ := strings.ToLower(strings.TrimSpace(prefs.PreferredQuality))
	out := make([]UnifiedCandidate, 0, len(cands))
	for _, c := range cands {
		metaText := strings.TrimSpace(c.ReleaseName + " " + c.Title + " " + c.Filename)
		q, codec, audio, hdr, is3D := EnrichReleaseMeta(metaText, fallbackString(c.Quality, c.Resolution), c.Codec, c.Audio, c.HDR)
		c.Quality, c.Resolution, c.Codec, c.Audio, c.HDR = q, q, codec, audio, hdr
		if c.Kind != "local" {
			if prefs.Exclude3D && is3D {
				continue
			}
			if maxBytes > 0 && c.Size > maxBytes {
				continue
			}
			if matchesExcludeTerm(metaText, prefs.ExcludeTerms) {
				continue
			}
		}
		if c.InfoHash == "" && c.Magnet != "" {
			c.InfoHash = ExtractInfoHash(c.Magnet)
		}
		if c.Kind == "local" || c.Kind == "http" || c.Kind == "hls" || c.Kind == "debrid_cached" {
			c.Instant = true
		}
		c.Score = scoreCandidate(c, prefQ)
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if out[i].Seeders != out[j].Seeders {
			return out[i].Seeders > out[j].Seeders
		}
		return out[i].Size > out[j].Size
	})
	if len(out) > 1 {
		seenURLs := make(map[string]bool, len(out))
		deduped := make([]UnifiedCandidate, 0, len(out))
		for _, c := range out {
			if c.Kind == "http" || c.Kind == "hls" {
				u := strings.TrimSpace(c.StreamURL)
				if u != "" {
					if seenURLs[u] {
						continue
					}
					seenURLs[u] = true
				}
			}
			deduped = append(deduped, c)
		}
		out = deduped
	}
	return out
}

func matchesExcludeTerm(text string, terms []string) bool {
	if len(terms) == 0 {
		return false
	}
	low := strings.ToLower(text)
	for _, t := range terms {
		needle := strings.ToLower(strings.TrimSpace(t))
		if needle != "" && strings.Contains(low, needle) {
			return true
		}
	}
	return false
}

func scoreCandidate(c UnifiedCandidate, prefQ string) int {
	score := 0
	switch c.Kind {
	case "local":
		score += 100000
	case "debrid_cached":
		score += 65000
	case "http", "hls":
		score += 50000
	case "torrent":
		score += 1000
	}
	ql := strings.ToLower(c.Quality)
	if prefQ != "" && ql == prefQ {
		score += 3000
	}
	switch ql {
	case "2160p":
		score += 400
	case "1080p":
		score += 300
	case "720p":
		score += 200
	case "480p":
		score += 100
	}
	switch c.HDR {
	case "DV", "HDR10+":
		score += 80
	case "HDR10", "HDR":
		score += 50
	}
	switch c.Codec {
	case "HEVC", "AV1":
		score += 25
	case "H264":
		score += 15
	}
	if c.Kind == "torrent" {
		if c.Seeders == 0 {
			score -= 500
		} else {
			seedScore := c.Seeders * 2
			if seedScore > 1200 {
				seedScore = 1200
			}
			score += seedScore
		}
	}
	return score
}
