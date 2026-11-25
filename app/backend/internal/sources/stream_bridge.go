package sources

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/outbound"
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/releasename"
)

var reMagnetHash = regexp.MustCompile(`(?i)xt=urn:btih:([a-z0-9]{32,40})`)

// StreamSourceInfo describes an enabled torrent Source usable for streaming.
type StreamSourceInfo struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	Format   string   `json:"format"`
	Kinds    []string `json:"kinds"`
	Priority int      `json:"priority"`
	State    string   `json:"state"`
}

// StreamSearchReq asks selected or all enabled torrent Sources for matching releases.
type StreamSearchReq struct {
	Query     string  `json:"query"`
	Kind      string  `json:"kind"` // movie or show
	Year      int     `json:"year"`
	Season    int     `json:"season"`
	Episode   int     `json:"episode"`
	IMDb      string  `json:"imdb"`
	TMDB      int     `json:"tmdb"`
	SourceIDs []int64 `json:"source_ids"`
}

// StreamCandidate is one torrent search result enriched with parsed release traits.
type StreamCandidate struct {
	SourceID      int64     `json:"source_id"`
	SourceName    string    `json:"source_name"`
	SourcePri     int       `json:"-"`
	Title         string    `json:"title"`
	Link          string    `json:"link"`
	InfoHash      string    `json:"info_hash"`
	Protocol      string    `json:"protocol"`
	Size          int64     `json:"size"`
	Seeders       int       `json:"seeders"`
	Peers         int       `json:"peers"`
	Resolution    string    `json:"resolution"`
	ResolutionNum int       `json:"resolution_num"`
	SourceTag     string    `json:"source_tag"`
	Codec         string    `json:"codec"`
	HDR           string    `json:"hdr"`
	Audio         string    `json:"audio"`
	Languages     []string  `json:"languages"`
	Season        int       `json:"season,omitempty"`
	Episode       int       `json:"episode,omitempty"`
	IsPack        bool      `json:"is_pack,omitempty"`
	Published     time.Time `json:"published,omitempty"`
}

// ListStreamSources returns enabled non-RSS torrent Sources handling kind.
func ListStreamSources(ctx context.Context, db *sql.DB, kind string) ([]StreamSourceInfo, error) {
	if kind != "show" {
		kind = "movie"
	}
	srcs, err := database.Select[domain.Source](ctx, db, "sources", "state != 'disabled' ORDER BY priority, name")
	if err != nil {
		return nil, err
	}
	out := make([]StreamSourceInfo, 0, len(srcs))
	for _, s := range srcs {
		if s.Format == "rss" || (s.Protocol != "" && s.Protocol != "torrent") || !checkHandles(s, kind) {
			continue
		}
		kinds := s.Kinds
		if kinds == nil {
			kinds = []string{}
		}
		out = append(out, StreamSourceInfo{
			ID:       s.ID,
			Name:     s.Name,
			Format:   s.Format,
			Kinds:    kinds,
			Priority: s.Priority,
			State:    s.State,
		})
	}
	return out, nil
}

// SearchForStream searches enabled torrent Sources concurrently and returns
// parsed torrent candidates along with per-Source call summaries.
func SearchForStream(ctx context.Context, db *sql.DB, sec SecretReader, req StreamSearchReq) ([]StreamCandidate, []SourceCheck, error) {
	kind := req.Kind
	if kind != "show" {
		kind = "movie"
	}
	qTitle := strings.TrimSpace(req.Query)
	if qTitle == "" && req.IMDb == "" && req.TMDB <= 0 {
		return nil, nil, errors.New("query or title id is required")
	}

	srcs, err := database.Select[domain.Source](ctx, db, "sources", "state != 'disabled' ORDER BY priority, name")
	if err != nil {
		return nil, nil, err
	}
	filterIDs := map[int64]bool{}
	for _, id := range req.SourceIDs {
		if id > 0 {
			filterIDs[id] = true
		}
	}

	var targets []domain.Source
	for _, s := range srcs {
		if s.Format == "rss" || (s.Protocol != "" && s.Protocol != "torrent") || !checkHandles(s, kind) {
			continue
		}
		if len(filterIDs) > 0 && !filterIDs[s.ID] {
			continue
		}
		targets = append(targets, s)
	}

	checks := make([]SourceCheck, len(targets))
	perSrc := make([][]StreamCandidate, len(targets))
	sCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for i, src := range targets {
		checks[i] = SourceCheck{
			ID:     src.ID,
			Name:   src.Name,
			Health: HealthOf(src),
			Paused: src.State == "paused",
		}
		if src.State == "paused" && len(filterIDs) == 0 {
			continue
		}
		wg.Add(1)
		go func(idx int, s domain.Source) {
			defer wg.Done()
			cands, chk := searchOneStreamSource(sCtx, sec, s, qTitle, kind, req)
			checks[idx] = chk
			perSrc[idx] = cands
		}(i, src)
	}
	wg.Wait()

	seen := map[string]bool{}
	out := make([]StreamCandidate, 0, 64)
	for _, group := range perSrc {
		for _, c := range group {
			dedupeKey := fmt.Sprintf("%d:%s", c.SourceID, c.Link)
			if c.InfoHash != "" {
				dedupeKey = fmt.Sprintf("%d:hash:%s", c.SourceID, c.InfoHash)
			}
			if seen[dedupeKey] {
				continue
			}
			seen[dedupeKey] = true
			out = append(out, c)
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Seeders != out[j].Seeders {
			return out[i].Seeders > out[j].Seeders
		}
		if out[i].ResolutionNum != out[j].ResolutionNum {
			return out[i].ResolutionNum > out[j].ResolutionNum
		}
		if out[i].SourcePri != out[j].SourcePri {
			return out[i].SourcePri < out[j].SourcePri
		}
		return out[i].Size > out[j].Size
	})

	return out, checks, nil
}

func searchOneStreamSource(ctx context.Context, sec SecretReader, src domain.Source, qTitle, kind string, req StreamSearchReq) ([]StreamCandidate, SourceCheck) {
	chk := SourceCheck{ID: src.ID, Name: src.Name, Health: HealthOf(src)}
	start := time.Now()
	cfg, err := ConfigFor(ctx, src, sec)
	if err != nil {
		chk.Error = ExplainVia(src.URL, err, ProbeDial(src))
		return nil, chk
	}
	cfg.Timeout = 20 * time.Second
	key := map[string]string{"movie": "movie", "show": "tv"}[kind]
	cats := src.Categories[key]
	if len(cats) == 0 {
		cats = DefaultCategories[key]
	}
	qq := Query{
		Title:      qTitle,
		Year:       req.Year,
		IMDb:       req.IMDb,
		TMDb:       req.TMDB,
		Categories: cats,
		TV:         kind == "show",
		Season:     req.Season,
	}
	if kind == "show" {
		upperQ := strings.ToUpper(qTitle)
		var token string
		if req.Episode > 0 {
			qq.Episode = strconv.Itoa(req.Episode)
			if req.Season > 0 {
				token = fmt.Sprintf("S%02dE%02d", req.Season, req.Episode)
			} else {
				token = fmt.Sprintf("E%02d", req.Episode)
			}
		} else if req.Season > 0 {
			token = fmt.Sprintf("S%02d", req.Season)
		}
		if token != "" && !strings.Contains(upperQ, token) {
			qq.Words = token
		}
	}

	items, err := New(cfg).Search(ctx, qq)
	chk.MS = time.Since(start).Milliseconds()
	if err != nil {
		chk.Error = ExplainVia(src.URL, err, ProbeDial(src))
		return nil, chk
	}
	chk.Answered = true

	cands := make([]StreamCandidate, 0, len(items))
	for _, it := range items {
		if it.Protocol != "" && it.Protocol != "torrent" && !strings.HasPrefix(strings.ToLower(it.Link), "magnet:") {
			continue
		}
		cands = append(cands, candidateFromItem(src, it, kind == "show"))
	}
	chk.Count = len(cands)
	return cands, chk
}

func candidateFromItem(src domain.Source, it Item, isShow bool) StreamCandidate {
	parsed := releasename.Parse(it.Title, releasename.Hints{
		Size:           it.Size,
		Show:           isShow,
		MultiLanguages: src.MultiLanguages,
		NoLanguage:     src.NoLanguageLanguages,
	})
	hash := strings.ToLower(strings.TrimSpace(it.InfoHash))
	if hash == "" {
		if m := reMagnetHash.FindStringSubmatch(it.Link); len(m) == 2 {
			hash = strings.ToLower(m[1])
		}
	}
	seeders, peers := 0, 0
	if it.Seeders != nil {
		seeders = *it.Seeders
	}
	if it.Peers != nil {
		peers = *it.Peers
	}
	resNum := parsed.Resolution.Value
	resStr := ""
	if resNum > 0 {
		resStr = fmt.Sprintf("%dp", resNum)
	}
	audioParts := make([]string, 0, 3)
	if parsed.Audio.Format != "" {
		audioParts = append(audioParts, strings.ToUpper(strings.ReplaceAll(parsed.Audio.Format, "_", "-")))
	}
	if parsed.Audio.Channels != "" {
		audioParts = append(audioParts, parsed.Audio.Channels)
	}
	if parsed.Audio.Atmos {
		audioParts = append(audioParts, "Atmos")
	}
	langs := slices.Clone(it.Languages)
	if len(langs) == 0 {
		langs = slices.Clone(parsed.Languages)
	}
	if langs == nil {
		langs = []string{}
	}
	c := StreamCandidate{
		SourceID:      src.ID,
		SourceName:    src.Name,
		SourcePri:     src.Priority,
		Title:         it.Title,
		Link:          it.Link,
		InfoHash:      hash,
		Protocol:      "torrent",
		Size:          it.Size,
		Seeders:       seeders,
		Peers:         peers,
		Resolution:    resStr,
		ResolutionNum: resNum,
		SourceTag:     strings.ReplaceAll(parsed.Source.Value, "_", "-"),
		Codec:         strings.ToUpper(parsed.Codec.Value),
		HDR:           strings.Join(parsed.HDR.Value, " "),
		Audio:         strings.Join(audioParts, " "),
		Languages:     langs,
		IsPack:        parsed.Kind.Value == releasename.KindSeasonPack || parsed.Kind.Value == releasename.KindMultiSeason || parsed.Kind.Value == releasename.KindComplete,
		Published:     it.Published,
	}
	if parsed.Show != nil {
		if len(parsed.Show.Seasons) > 0 {
			c.Season = parsed.Show.Seasons[0]
		}
		if len(parsed.Show.Episodes) > 0 {
			c.Episode = parsed.Show.Episodes[0]
		}
	}
	return c
}

// FetchTorrentPayload resolves a release link into either a magnet URI or raw .torrent bytes.
// It intercepts magnet: redirects (common from Prowlarr/Jackett) and applies Source auth/proxy rules.
func FetchTorrentPayload(ctx context.Context, db *sql.DB, sec SecretReader, sourceID int64, rawLink string) (magnet string, torrentBytes []byte, err error) {
	rawLink = strings.TrimSpace(rawLink)
	if rawLink == "" {
		return "", nil, errors.New("empty torrent link")
	}
	if strings.HasPrefix(strings.ToLower(rawLink), "magnet:") {
		return rawLink, nil, nil
	}
	u, err := url.Parse(rawLink)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", nil, errors.New("unsupported torrent link scheme")
	}

	var rt http.RoundTripper
	var hdr http.Header
	if sourceID > 0 && db != nil {
		var src domain.Source
		if getErr := database.Get(ctx, db, "sources", sourceID, &src); getErr == nil {
			if cfg, cfgErr := ConfigFor(ctx, src, sec); cfgErr == nil {
				rt = Transport(cfg)
			}
			hdr, _ = FileHeader(ctx, src, sec)
		}
	}
	if rt == nil {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.Proxy = nil
		tr.DialContext = outbound.PolicyDialer(outbound.NotMetadata).DialContext
		rt = tr
	}

	var caughtMagnet string
	client := &http.Client{
		Timeout:   25 * time.Second,
		Transport: rt,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if strings.EqualFold(req.URL.Scheme, "magnet") {
				caughtMagnet = req.URL.String()
				return http.ErrUseLastResponse
			}
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawLink, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	for k, vs := range hdr {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	resp, err := client.Do(req)
	if caughtMagnet != "" {
		return caughtMagnet, nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		loc := strings.TrimSpace(resp.Header.Get("Location"))
		if strings.HasPrefix(strings.ToLower(loc), "magnet:") {
			return loc, nil, nil
		}
		return "", nil, fmt.Errorf("unexpected redirect (%s)", resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("source returned %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return "", nil, err
	}
	if len(body) == 0 {
		return "", nil, errors.New("empty torrent response")
	}
	return "", body, nil
}
