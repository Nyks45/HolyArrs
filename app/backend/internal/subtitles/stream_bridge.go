package subtitles

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/subtitles/sites"
	"github.com/HolyArrs/holyarrs/app/backend/internal/subtitles/subtext"
)

// StreamSubSearchReq asks configured subtitle sites for matches usable in the live player.
type StreamSubSearchReq struct {
	Kind        string   `json:"kind"` // movie or show
	Title       string   `json:"title"`
	Year        int      `json:"year"`
	Season      int      `json:"season"`
	Episode     int      `json:"episode"`
	IMDb        string   `json:"imdb"`
	TMDB        string   `json:"tmdb"`
	ReleaseName string   `json:"release_name"`
	Languages   []string `json:"languages"`
}

// StreamSubCandidate describes one downloadable subtitle match for the stream player.
type StreamSubCandidate struct {
	ID              string   `json:"id"`
	SiteID          int64    `json:"site_id"`
	SiteName        string   `json:"site_name"`
	SiteKind        string   `json:"site_kind"`
	Language        string   `json:"language"`
	ReleaseNames    []string `json:"release_names,omitempty"`
	HearingImpaired bool     `json:"hearing_impaired,omitempty"`
	Forced          bool     `json:"forced,omitempty"`
	Downloads       int      `json:"downloads,omitempty"`
	Format          string   `json:"format,omitempty"`
	MachineMade     string   `json:"machine_made,omitempty"`
}

type cachedStreamSub struct {
	siteID    int64
	cand      sites.Candidate
	createdAt time.Time
}

var streamSubCache = struct {
	mu sync.Mutex
	m  map[string]cachedStreamSub
}{
	m: map[string]cachedStreamSub{},
}

func putStreamSubCache(siteID int64, cand sites.Candidate) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	now := time.Now()

	streamSubCache.mu.Lock()
	defer streamSubCache.mu.Unlock()
	if len(streamSubCache.m) >= 500 {
		for k, v := range streamSubCache.m {
			if now.Sub(v.createdAt) > 2*time.Hour {
				delete(streamSubCache.m, k)
			}
		}
		if len(streamSubCache.m) >= 500 {
			streamSubCache.m = map[string]cachedStreamSub{}
		}
	}
	streamSubCache.m[id] = cachedStreamSub{siteID: siteID, cand: cand, createdAt: now}
	return id
}

func getStreamSubCache(id string) (cachedStreamSub, bool) {
	streamSubCache.mu.Lock()
	defer streamSubCache.mu.Unlock()
	c, ok := streamSubCache.m[id]
	if !ok || time.Since(c.createdAt) > 2*time.Hour {
		if ok {
			delete(streamSubCache.m, id)
		}
		return cachedStreamSub{}, false
	}
	return c, true
}

func isHashOnlySubSite(kind string) bool {
	switch kind {
	case "bsplayer", "napiprojekt", "napisy24":
		return true
	}
	return false
}

// SearchForStream queries configured subtitle sites concurrently without requiring a Library file on disk.
func (s *Service) SearchForStream(ctx context.Context, req StreamSubSearchReq) ([]StreamSubCandidate, []string, error) {
	if s == nil || s.Store == nil {
		return nil, nil, errors.New("subtitle service is unavailable")
	}
	title := strings.TrimSpace(req.Title)
	if title == "" && req.IMDb == "" && req.TMDB == "" {
		return nil, nil, errors.New("title or id is required")
	}

	var langs []string
	seenLang := map[string]bool{}
	addLang := func(code string) {
		n := NormLanguage(code)
		if n != "" && !seenLang[n] {
			seenLang[n] = true
			langs = append(langs, n)
		}
	}
	for _, l := range req.Languages {
		addLang(l)
	}
	if len(langs) == 0 {
		set := ReadSettings(ctx, s.Store.Read())
		for _, w := range set.Languages {
			addLang(w.Language)
		}
	}
	if len(langs) == 0 {
		langs = []string{"en", "tr"}
	}

	rows, err := usable(ctx, s.Store.Read(), now())
	if err != nil {
		return nil, langs, err
	}

	isShow := req.Kind == "show"
	qKind := "movie"
	if isShow {
		qKind = "episode"
	}

	q := sites.Query{
		Kind:        qKind,
		Title:       title,
		Year:        req.Year,
		Season:      req.Season,
		Episode:     req.Episode,
		IMDb:        strings.TrimSpace(req.IMDb),
		TMDB:        strings.TrimSpace(req.TMDB),
		ReleaseName: strings.TrimSpace(req.ReleaseName),
		Languages:   langs,
	}

	sCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out []StreamSubCandidate
	)
	for _, row := range rows {
		if isHashOnlySubSite(row.Kind) {
			continue
		}
		k, ok := sites.KindOf(row.Kind)
		if !ok {
			continue
		}
		if (isShow && !k.Shows) || (!isShow && !k.Movies) {
			continue
		}
		wg.Add(1)
		go func(r SiteRow) {
			defer wg.Done()
			cl, err := s.client(sCtx, r)
			if err != nil {
				return
			}
			cands, err := cl.Search(sCtx, q)
			if err != nil || len(cands) == 0 {
				return
			}
			items := make([]StreamSubCandidate, 0, len(cands))
			for _, c := range cands {
				normLang := NormLanguage(c.Language)
				if normLang == "" {
					normLang = strings.ToLower(strings.TrimSpace(c.Language))
				}
				cid := putStreamSubCache(r.ID, c)
				items = append(items, StreamSubCandidate{
					ID:              cid,
					SiteID:          r.ID,
					SiteName:        r.Name,
					SiteKind:        r.Kind,
					Language:        normLang,
					ReleaseNames:    slices.Clone(c.ReleaseNames),
					HearingImpaired: c.HearingImpaired,
					Forced:          c.Forced,
					Downloads:       c.Downloads,
					Format:          c.Format,
					MachineMade:     c.MachineMade,
				})
			}
			mu.Lock()
			out = append(out, items...)
			mu.Unlock()
		}(row)
	}
	wg.Wait()

	if out == nil {
		out = []StreamSubCandidate{}
	}

	langRank := map[string]int{}
	for i, l := range langs {
		langRank[l] = i
	}
	relLower := strings.ToLower(q.ReleaseName)

	sort.SliceStable(out, func(i, j int) bool {
		ri, ok1 := langRank[out[i].Language]
		rj, ok2 := langRank[out[j].Language]
		if !ok1 {
			ri = 999
		}
		if !ok2 {
			rj = 999
		}
		if ri != rj {
			return ri < rj
		}
		mi := out[i].MachineMade == ""
		mj := out[j].MachineMade == ""
		if mi != mj {
			return mi
		}
		oi := releaseTokenOverlap(relLower, out[i].ReleaseNames)
		oj := releaseTokenOverlap(relLower, out[j].ReleaseNames)
		if oi != oj {
			return oi > oj
		}
		return out[i].Downloads > out[j].Downloads
	})

	return out, langs, nil
}

func releaseTokenOverlap(relLower string, names []string) int {
	if relLower == "" || len(names) == 0 {
		return 0
	}
	best := 0
	for _, n := range names {
		score := 0
		for _, tok := range strings.FieldsFunc(strings.ToLower(n), func(r rune) bool {
			return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
		}) {
			if len(tok) >= 3 && strings.Contains(relLower, tok) {
				score++
			}
		}
		if score > best {
			best = score
		}
	}
	return best
}

// FetchStreamSubtitleVTT downloads one cached candidate and converts it into UTF-8 WebVTT.
func (s *Service) FetchStreamSubtitleVTT(ctx context.Context, candID string) ([]byte, string, error) {
	if s == nil || s.Store == nil {
		return nil, "", errors.New("subtitle service is unavailable")
	}
	cached, ok := getStreamSubCache(strings.TrimSpace(candID))
	if !ok {
		return nil, "", errors.New("subtitle selection expired; please search subtitles again")
	}
	rows, err := usable(ctx, s.Store.Read(), now())
	if err != nil {
		return nil, "", err
	}
	var target *SiteRow
	for i := range rows {
		if rows[i].ID == cached.siteID {
			target = &rows[i]
			break
		}
	}
	if target == nil {
		return nil, "", errors.New("subtitle site is no longer enabled")
	}
	cl, err := s.client(ctx, *target)
	if err != nil {
		return nil, "", err
	}
	raw, err := cl.Download(ctx, cached.cand)
	if err != nil {
		return nil, "", err
	}
	hint := "subtitle.srt"
	if cached.cand.Format != "" {
		hint = "subtitle." + strings.TrimPrefix(cached.cand.Format, ".")
	}
	lang := NormLanguage(cached.cand.Language)
	vtt, err := ConvertToWebVTT(raw, hint, lang)
	if err != nil {
		return nil, "", err
	}
	return vtt, lang, nil
}

// ConvertToWebVTT unpacks archives if needed, detects encoding, cleans control codes, and outputs UTF-8 WebVTT.
func ConvertToWebVTT(raw []byte, nameHint, lang string) ([]byte, error) {
	entries, err := subtext.Unpack(raw, nameHint)
	if err != nil {
		return nil, err
	}
	rankFmt := func(f string) int {
		switch f {
		case subtext.VTT:
			return 0
		case subtext.SRT:
			return 1
		case subtext.ASS:
			return 2
		case subtext.SSA:
			return 3
		default:
			return 9
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return rankFmt(entries[i].Format) < rankFmt(entries[j].Format)
	})

	var lastErr error
	for _, e := range entries {
		if e.Format != subtext.VTT && e.Format != subtext.SRT && e.Format != subtext.ASS && e.Format != subtext.SSA {
			continue
		}
		cleaned, cerr := subtext.CleanFor(e.Data, e.Format, lang)
		if cerr != nil {
			lastErr = cerr
			continue
		}
		cues, perr := subtext.ParseCues(cleaned, e.Format)
		if perr != nil {
			lastErr = perr
			continue
		}
		var sb strings.Builder
		sb.WriteString("WEBVTT\n\n")
		for _, c := range cues {
			text := strings.TrimSpace(strings.ReplaceAll(c.Text, "\r\n", "\n"))
			if text == "" {
				continue
			}
			for strings.Contains(text, "\n\n") {
				text = strings.ReplaceAll(text, "\n\n", "\n")
			}
			start := max(c.StartMS, 0)
			end := max(c.EndMS, start+100)
			fmt.Fprintf(&sb, "%s --> %s\n%s\n\n", formatVTTMS(start), formatVTTMS(end), text)
		}
		return []byte(sb.String()), nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, subtext.ErrNotSubtitle
}

func formatVTTMS(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	remMS := ms % 1000
	totalSec := ms / 1000
	sec := totalSec % 60
	totalMin := totalSec / 60
	min := totalMin % 60
	hr := totalMin / 60
	return fmt.Sprintf("%02d:%02d:%02d.%03d", hr, min, sec, remMS)
}

// StyledText returns cleaned ASS or SSA text when raw holds a styled subtitle, else "".
func StyledText(raw []byte, nameHint, lang string) string {
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(nameHint)), ".")
	if format != subtext.ASS && format != subtext.SSA {
		head := bytes.TrimSpace(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")))
		if !bytes.HasPrefix(bytes.ToLower(head[:min(len(head), 13)]), []byte("[script info]")) {
			return ""
		}
		format = subtext.ASS
	}
	text, _ := subtext.CleanFor(raw, format, lang)
	return text
}
