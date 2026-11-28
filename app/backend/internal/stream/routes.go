package stream

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/webserver"
	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"
	"github.com/HolyArrs/holyarrs/app/backend/internal/sources"
	"github.com/HolyArrs/holyarrs/app/backend/internal/subtitles"
)

// Handlers exposes direct torrent streaming APIs and media endpoints.
type Handlers struct {
	Mgr *Manager
}

// Routes registers streaming API routes on m.
func (h *Handlers) Routes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/stream/sources", h.listSources)
	m.HandleFunc("POST /api/v1/stream/search", h.search)
	m.HandleFunc("POST /api/v1/stream/start", h.start)
	m.HandleFunc("GET /api/v1/stream/sessions", h.listSessions)
	m.HandleFunc("GET /api/v1/stream/history", h.listHistory)
	m.HandleFunc("GET /api/v1/stream/history/title/{kind}/{tmdb}", h.titleHistory)
	m.HandleFunc("PUT /api/v1/stream/history", h.saveHistory)
	m.HandleFunc("POST /api/v1/stream/history/batch", h.batchHistory)
	m.HandleFunc("DELETE /api/v1/stream/history/{key}", h.deleteHistory)
	m.HandleFunc("POST /api/v1/stream/subtitles/search", h.searchSubtitles)
	m.HandleFunc("POST /api/v1/stream/subtitles/fetch", h.fetchSubtitle)
	m.HandleFunc("POST /api/v1/stream/subtitles/upload", h.uploadSubtitle)
	m.HandleFunc("POST /api/v1/stream/subtitles/autosync", h.autoSyncSubtitle)
	m.HandleFunc("GET /api/v1/stream/{id}", h.status)
	m.HandleFunc("POST /api/v1/stream/{id}/file", h.selectFile)
	m.HandleFunc("POST /api/v1/stream/{id}/hls", h.startHLS)
	m.HandleFunc("POST /api/v1/stream/{id}/mpv", h.launchMPV)
	m.HandleFunc("PUT /api/v1/stream/{id}/keep", h.setKeep)
	m.HandleFunc("DELETE /api/v1/stream/{id}", h.stop)

	// Token-authenticated playback endpoints (called by mpv, ffmpeg, ffprobe, hls.js, and <track>).
	m.HandleFunc("GET /api/v1/stream/{id}/media/{token}", h.Mgr.ServeMedia)
	m.HandleFunc("GET /api/v1/stream/{id}/hls/{token}/{gen}/{file}", h.Mgr.ServeHLSFile)
	m.HandleFunc("GET /api/v1/stream/{id}/sub/{token}/{idx}", h.Mgr.ServeSubtitleVTT)

	h.registerStreamingRoutes(m)
}

func callerID(r *http.Request) int64 {
	if who, ok := webserver.From(r.Context()); ok {
		return who.UserID
	}
	return 0
}

func (h *Handlers) listSources(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	kind := r.URL.Query().Get("kind")
	if kind != "show" {
		kind = "movie"
	}
	srcs := []sources.StreamSourceInfo{}
	if h.Mgr.Store != nil {
		list, err := sources.ListStreamSources(r.Context(), h.Mgr.Store.Read(), kind)
		if err != nil {
			webserver.Internal(w, "stream", err)
			return
		}
		srcs = list
	}
	_, mpvErr := exec.LookPath("mpv")
	_, ffErr := exec.LookPath("ffmpeg")
	webserver.WriteJSON(w, http.StatusOK, map[string]any{
		"sources":          srcs,
		"mpv_available":    mpvErr == nil,
		"ffmpeg_available": ffErr == nil,
		"node_available":   h.Mgr.Plugins.NodeAvailable(),
		"debrid":           h.Mgr.Debrid.Status(r.Context()),
		"prefs":            h.Mgr.Prefs.Get(),
	})
}

func (h *Handlers) search(w http.ResponseWriter, r *http.Request) {
	var in struct {
		sources.StreamSearchReq
		LocalOnly        bool     `json:"local_only"`
		OriginalTitle    string   `json:"original_title"`
		Genres           []string `json:"genres"`
		OriginalLanguage string   `json:"original_language"`
		OriginCountry    []string `json:"origin_country"`
		Anime            bool     `json:"anime"`
		Mode             string   `json:"mode"`
		NoCache          bool     `json:"no_cache"`
		PreferredQuality string   `json:"preferred_quality"`
		RuntimeMin       int      `json:"runtime_min"`
	}
	if !webserver.Decode(w, r, &in, "send {query, kind, year, season, episode, imdb, tmdb, source_ids}") {
		return
	}
	kind := "movie"
	if in.Kind == "show" {
		kind = "show"
	}
	expectedSec := float64(in.RuntimeMin) * 60
	tmdbStr := ""
	if in.TMDB > 0 {
		tmdbStr = strconv.Itoa(in.TMDB)
	}
	imdbID := strings.TrimSpace(in.IMDb)
	prefs := h.Mgr.Prefs.Get()
	if strings.TrimSpace(in.PreferredQuality) != "" {
		prefs.PreferredQuality = strings.TrimSpace(in.PreferredQuality)
	}
	mode := "full"
	if strings.EqualFold(strings.TrimSpace(in.Mode), "fast") {
		mode = "fast"
	}
	canCache := !in.LocalOnly && len(in.SourceIDs) == 0 && h.Mgr.SearchCache != nil
	if canCache && !in.NoCache {
		fullKey := SearchCacheKey(kind, tmdbStr, imdbID, in.Season, in.Episode, in.Query, "full")
		if cached, ok := h.Mgr.SearchCache.Get(fullKey); ok {
			ranked := FilterAndRankCandidatesWithRuntime(cached.Candidates, prefs, kind, expectedSec)
			writeSearchCandidates(w, h.Mgr.CandCache, ranked, cached.Sources, cached.Warnings)
			return
		}
		if mode == "fast" {
			fastKey := SearchCacheKey(kind, tmdbStr, imdbID, in.Season, in.Episode, in.Query, "fast")
			if cached, ok := h.Mgr.SearchCache.Get(fastKey); ok {
				ranked := FilterAndRankCandidatesWithRuntime(cached.Candidates, prefs, kind, expectedSec)
				writeSearchCandidates(w, h.Mgr.CandCache, ranked, cached.Sources, cached.Warnings)
				return
			}
		}
	}
	if !in.LocalOnly && imdbID == "" && tmdbStr != "" && h.Mgr.ResolveIMDb != nil {
		imdbID = strings.TrimSpace(h.Mgr.ResolveIMDb(r.Context(), kind, tmdbStr))
		in.StreamSearchReq.IMDb = imdbID
	}
	mc := NewSearchMatchContext(kind, in.Query, in.OriginalTitle, in.Year, in.Season, in.Episode, in.Genres, in.OriginalLanguage, in.OriginCountry, in.Anime)
	mc.TMDB = tmdbStr

	var mu sync.Mutex
	var rawCands []UnifiedCandidate
	var checks any = []any{}
	var warnings []string
	var wg sync.WaitGroup

	// 1. Local Library files matching TMDB/episode.
	if h.Mgr.Store != nil && tmdbStr != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			locals, err := mediafiles.FindLocalVideoFiles(r.Context(), h.Mgr.Store.Read(), kind, tmdbStr, in.Season, in.Episode)
			if err != nil || len(locals) == 0 {
				return
			}
			mu.Lock()
			for _, lf := range locals {
				title := filepath.Base(lf.RelPath)
				if lf.VersionLabel != "" {
					title = lf.VersionLabel + " · " + title
				}
				rawCands = append(rawCands, UnifiedCandidate{
					Kind:         "local",
					Instant:      true,
					Title:        title,
					ReleaseName:  filepath.Base(lf.RelPath),
					Provider:     "Local Library",
					ProviderType: "local",
					SourceName:   "Local Library",
					Quality:      lf.Quality,
					Codec:        lf.Codec,
					Size:         lf.Size,
					LocalFileID:  lf.FileID,
					Filename:     filepath.Base(lf.RelPath),
				})
			}
			mu.Unlock()
		}()
	}

	if !in.LocalOnly {
		// 2. Stremio Addons.
		if h.Mgr.Addons != nil && (imdbID != "" || tmdbStr != "") {
			wg.Add(1)
			go func() {
				defer wg.Done()
				aCtx, aCancel := context.WithTimeout(r.Context(), 4*time.Second)
				defer aCancel()
				cands, warns := h.Mgr.Addons.SearchStreams(aCtx, kind, imdbID, tmdbStr, in.Season, in.Episode)
				mu.Lock()
				rawCands = append(rawCands, cands...)
				warnings = append(warnings, warns...)
				mu.Unlock()
			}()
		}

		// 3. Nuvio JS Plugin Workers.
		if h.Mgr.Plugins != nil && h.Mgr.Plugins.NodeAvailable() && tmdbStr != "" {
			tmdbKey := fallbackString(prefs.TMDBAPIKey, os.Getenv("TMDB_API_KEY"))
			wg.Add(1)
			go func() {
				defer wg.Done()
				cands, warns := h.Mgr.Plugins.SearchStreams(r.Context(), kind, tmdbStr, in.Season, in.Episode, tmdbKey, mc, mode == "fast", h.Mgr.Health)
				mu.Lock()
				rawCands = append(rawCands, cands...)
				warnings = append(warnings, warns...)
				mu.Unlock()
			}()
		}

		// 4. Prowlarr / Torznab Torrent Sources (deferred to background full pass in fast mode).
		if mode != "fast" && h.Mgr.Store != nil && strings.TrimSpace(in.Query) != "" {
			wg.Add(1)
			go func() {
				defer wg.Done()
				tCtx, tCancel := context.WithTimeout(r.Context(), 10*time.Second)
				defer tCancel()
				tCands, srcChecks, err := sources.SearchForStream(tCtx, h.Mgr.Store.Read(), h.Mgr.Secrets, in.StreamSearchReq)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					warnings = append(warnings, err.Error())
					return
				}
				checks = srcChecks
				for _, sc := range tCands {
					ih := strings.ToLower(strings.TrimSpace(sc.InfoHash))
					if ih == "" && strings.HasPrefix(strings.ToLower(sc.Link), "magnet:") {
						ih = ExtractInfoHash(sc.Link)
					}
					magnet := ""
					torrentURL := ""
					if strings.HasPrefix(strings.ToLower(sc.Link), "magnet:") {
						magnet = sc.Link
					} else {
						torrentURL = sc.Link
					}
					rawCands = append(rawCands, UnifiedCandidate{
						Kind:         "torrent",
						Instant:      false,
						Title:        sc.Title,
						ReleaseName:  sc.Title,
						Provider:     sc.SourceName,
						ProviderType: "torrent",
						SourceID:     sc.SourceID,
						SourceName:   sc.SourceName,
						Link:         sc.Link,
						Quality:      sc.Resolution,
						Codec:        sc.Codec,
						Audio:        sc.Audio,
						HDR:          sc.HDR,
						Size:         sc.Size,
						Seeders:      sc.Seeders,
						Peers:        sc.Peers,
						InfoHash:     ih,
						Magnet:       magnet,
						TorrentURL:   torrentURL,
						FileIdx:      -1,
					})
				}
			}()
		}
	}

	wg.Wait()

	// Promote cached torrents into Debrid Instant candidates when configured.
	if h.Mgr.Debrid != nil && len(rawCands) > 0 {
		var hashes []string
		for _, c := range rawCands {
			if c.Kind == "torrent" && len(c.InfoHash) == 40 {
				hashes = append(hashes, c.InfoHash)
			}
		}
		if len(hashes) > 0 {
			cachedMap := h.Mgr.Debrid.CheckCached(r.Context(), hashes)
			if len(cachedMap) > 0 {
				var promoted []UnifiedCandidate
				for _, c := range rawCands {
					if c.Kind == "torrent" {
						if prov, ok := cachedMap[c.InfoHash]; ok {
							dc := c
							dc.Kind = "debrid_cached"
							dc.Instant = true
							dc.DebridProvider = prov
							dc.ProviderType = "debrid"
							dc.Provider = debridDisplayName(prov) + " · " + c.Provider
							promoted = append(promoted, dc)
						}
					}
				}
				rawCands = append(promoted, rawCands...)
			}
		}
	}

	rawCands = FilterVerifiedCandidates(rawCands, mc)
	rawCands = PreflightAndMeasureCandidates(r.Context(), h.Mgr.Proxy, rawCands, h.Mgr.Health)
	ranked := FilterAndRankCandidatesWithRuntime(rawCands, prefs, kind, expectedSec)
	if canCache && len(ranked) > 0 {
		h.Mgr.SearchCache.Set(SearchCacheKey(kind, tmdbStr, imdbID, in.Season, in.Episode, in.Query, mode), CachedSearchResult{
			Candidates: ranked,
			Sources:    checks,
			Warnings:   warnings,
		})
	}
	writeSearchCandidates(w, h.Mgr.CandCache, ranked, checks, warnings)
}

func writeSearchCandidates(w http.ResponseWriter, cc *CandidateCache, ranked []UnifiedCandidate, checks any, warnings []string) {
	stored := cc.Put(ranked)
	if stored == nil {
		stored = []UnifiedCandidate{}
	}
	if checks == nil {
		checks = []any{}
	}
	counts := map[string]int{"local": 0, "instant": 0, "debrid": 0, "torrent": 0}
	for _, c := range stored {
		switch c.Kind {
		case "local":
			counts["local"]++
			counts["instant"]++
		case "debrid_cached":
			counts["debrid"]++
			counts["instant"]++
		case "http", "hls":
			counts["instant"]++
		default:
			counts["torrent"]++
		}
	}
	webserver.WriteJSON(w, http.StatusOK, map[string]any{
		"candidates": stored,
		"sources":    checks,
		"counts":     counts,
		"warnings":   warnings,
	})
}

func (h *Handlers) start(w http.ResponseWriter, r *http.Request) {
	var in StartReq
	if !webserver.Decode(w, r, &in, "send {candidate_id, local_file_id, stream_url, or torrent link}") {
		return
	}
	uid := callerID(r)
	prefs := h.Mgr.Prefs.Get()

	if in.CandidateID != "" {
		cand, ok := h.Mgr.CandCache.Get(in.CandidateID)
		if !ok {
			webserver.Fail(w, http.StatusGone, "stream candidate expired; search again")
			return
		}
		if in.Title == "" {
			in.Title = cand.Title
		}
		if in.ReleaseTitle == "" {
			in.ReleaseTitle = fallbackString(cand.ReleaseName, cand.Title)
		}
		if in.SourceID == 0 {
			in.SourceID = cand.SourceID
		}
		if in.SourceName == "" {
			in.SourceName = cand.Provider
		}
		if in.Link == "" {
			in.Link = fallbackString(cand.Magnet, fallbackString(cand.Link, cand.TorrentURL))
		}
		switch cand.Kind {
		case "local":
			in.LocalFileID = cand.LocalFileID
		case "http", "hls":
			st, err := h.Mgr.StartVerifiedRemote(r.Context(), uid, cand, in)
			if err != nil {
				webserver.Fail(w, http.StatusBadRequest, err.Error())
				return
			}
			webserver.WriteJSON(w, http.StatusCreated, st)
			return
		case "debrid_cached", "torrent":
			prov := in.DebridProvider
			if prov == "" {
				prov = cand.DebridProvider
			}
			if prov == "" && prefs.AutoResolveDebrid && h.Mgr.Debrid != nil {
				prov = h.Mgr.Debrid.FirstConfigured(r.Context())
			}
			if prov != "" && (cand.Magnet != "" || cand.InfoHash != "") {
				dlURL, _, err := h.Mgr.Debrid.ResolveMagnet(r.Context(), prov, cand.Magnet, cand.InfoHash, cand.FileIdx)
				if err == nil && dlURL != "" {
					st, serr := h.Mgr.StartRemote(r.Context(), uid, dlURL, nil, false, debridDisplayName(prov), in)
					if serr == nil {
						webserver.WriteJSON(w, http.StatusCreated, st)
						return
					}
				}
				if !prefs.AutoFallback || in.Link == "" {
					if err != nil {
						webserver.Fail(w, http.StatusBadGateway, err.Error())
						return
					}
				}
			}
		}
	}

	if in.LocalFileID > 0 {
		if h.Mgr.Store == nil {
			webserver.Fail(w, http.StatusServiceUnavailable, "library storage unavailable")
			return
		}
		lf, err := mediafiles.FindLocalVideoByID(r.Context(), h.Mgr.Store.Read(), in.LocalFileID)
		if err != nil {
			webserver.Fail(w, http.StatusNotFound, err.Error())
			return
		}
		st, err := h.Mgr.StartLocal(r.Context(), uid, lf, in)
		if err != nil {
			webserver.Fail(w, http.StatusBadRequest, err.Error())
			return
		}
		webserver.WriteJSON(w, http.StatusCreated, st)
		return
	}

	if in.StreamURL != "" {
		st, err := h.Mgr.StartRemote(r.Context(), uid, in.StreamURL, in.Headers, looksLikeHLSURL(in.StreamURL), in.SourceName, in)
		if err != nil {
			webserver.Fail(w, http.StatusBadRequest, err.Error())
			return
		}
		webserver.WriteJSON(w, http.StatusCreated, st)
		return
	}

	if in.DebridProvider != "" && in.Link != "" && h.Mgr.Debrid != nil {
		dlURL, _, err := h.Mgr.Debrid.ResolveMagnet(r.Context(), in.DebridProvider, in.Link, ExtractInfoHash(in.Link), -1)
		if err == nil && dlURL != "" {
			st, serr := h.Mgr.StartRemote(r.Context(), uid, dlURL, nil, false, debridDisplayName(in.DebridProvider), in)
			if serr == nil {
				webserver.WriteJSON(w, http.StatusCreated, st)
				return
			}
		} else if !prefs.AutoFallback {
			webserver.Fail(w, http.StatusBadGateway, err.Error())
			return
		}
	}

	st, err := h.Mgr.Start(r.Context(), uid, in)
	if err != nil {
		webserver.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	webserver.WriteJSON(w, http.StatusCreated, st)
}

func (h *Handlers) listSessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	webserver.WriteJSON(w, http.StatusOK, map[string]any{
		"sessions": h.Mgr.ListSessions(callerID(r)),
	})
}

func (h *Handlers) status(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	st, ok := h.Mgr.Status(r.PathValue("id"))
	if !ok {
		webserver.Fail(w, http.StatusNotFound, "stream session not found")
		return
	}
	h.Mgr.KeepAlive(st.ID)
	webserver.WriteJSON(w, http.StatusOK, st)
}

func (h *Handlers) selectFile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		FileIndex int `json:"file_index"`
	}
	if !webserver.Decode(w, r, &in, "send {file_index}") {
		return
	}
	st, err := h.Mgr.SelectFile(r.PathValue("id"), in.FileIndex)
	if err != nil {
		webserver.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	webserver.WriteJSON(w, http.StatusOK, st)
}

func (h *Handlers) launchMPV(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AudioLang string      `json:"audio_lang"`
		SubLang   string      `json:"sub_lang"`
		StartSec  float64     `json:"start_sec"`
		SubFile   string      `json:"sub_file"`
		Watch     *WatchEntry `json:"watch"`
	}
	if r.ContentLength != 0 && !webserver.Decode(w, r, &in, "send {audio_lang, sub_lang, start_sec, sub_file, watch}") {
		return
	}
	opts := MPVOpts{
		AudioLang: in.AudioLang,
		SubLang:   in.SubLang,
		StartSec:  in.StartSec,
		SubFile:   in.SubFile,
		Watch:     in.Watch,
	}
	if err := h.Mgr.LaunchMPV(r.PathValue("id"), opts); err != nil {
		webserver.Fail(w, http.StatusBadGateway, err.Error())
		return
	}
	st, _ := h.Mgr.Status(r.PathValue("id"))
	webserver.WriteJSON(w, http.StatusOK, st)
}

func (h *Handlers) setKeep(w http.ResponseWriter, r *http.Request) {
	var in struct {
		KeepFile bool `json:"keep_file"`
	}
	if !webserver.Decode(w, r, &in, "send {keep_file}") {
		return
	}
	st, err := h.Mgr.SetKeep(r.PathValue("id"), in.KeepFile)
	if err != nil {
		webserver.Fail(w, http.StatusNotFound, err.Error())
		return
	}
	webserver.WriteJSON(w, http.StatusOK, st)
}

func (h *Handlers) stop(w http.ResponseWriter, r *http.Request) {
	_ = h.Mgr.Stop(r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) listHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.Mgr.History == nil {
		webserver.WriteJSON(w, http.StatusOK, map[string]any{"items": []WatchEntry{}, "continue_watching": []WatchEntry{}})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, cw := h.Mgr.History.List(callerID(r), limit)
	webserver.WriteJSON(w, http.StatusOK, map[string]any{
		"items":             items,
		"continue_watching": cw,
	})
}

func (h *Handlers) titleHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.Mgr.History == nil {
		webserver.WriteJSON(w, http.StatusOK, map[string]any{"latest": nil, "episodes": []WatchEntry{}})
		return
	}
	kind := r.PathValue("kind")
	tmdb := r.PathValue("tmdb")
	latest, eps := h.Mgr.History.ForTitle(callerID(r), kind, tmdb)
	webserver.WriteJSON(w, http.StatusOK, map[string]any{
		"latest":   latest,
		"episodes": eps,
	})
}

func (h *Handlers) saveHistory(w http.ResponseWriter, r *http.Request) {
	if h.Mgr.History == nil {
		webserver.Fail(w, http.StatusServiceUnavailable, "watch history is unavailable")
		return
	}
	var in WatchEntry
	if !webserver.Decode(w, r, &in, "send {kind, tmdb, title, position_sec, duration_sec, ...}") {
		return
	}
	saved, err := h.Mgr.History.Upsert(callerID(r), in)
	if err != nil {
		webserver.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	webserver.WriteJSON(w, http.StatusOK, saved)
}

func (h *Handlers) deleteHistory(w http.ResponseWriter, r *http.Request) {
	if h.Mgr.History == nil {
		webserver.Fail(w, http.StatusServiceUnavailable, "watch history is unavailable")
		return
	}
	if err := h.Mgr.History.Delete(callerID(r), r.PathValue("key")); err != nil {
		webserver.Internal(w, "stream history", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) searchSubtitles(w http.ResponseWriter, r *http.Request) {
	var in subtitles.StreamSubSearchReq
	if !webserver.Decode(w, r, &in, "send {kind, title, year, season, episode, imdb, tmdb, release_name, languages}") {
		return
	}
	if strings.TrimSpace(in.IMDb) == "" && strings.TrimSpace(in.TMDB) != "" && h.Mgr.ResolveIMDb != nil {
		subKind := "movie"
		if in.Kind == "show" {
			subKind = "show"
		}
		in.IMDb = strings.TrimSpace(h.Mgr.ResolveIMDb(r.Context(), subKind, strings.TrimSpace(in.TMDB)))
	}
	var siteCands []any
	var langs []string
	var addonCands []AddonSubCandidate
	var wg sync.WaitGroup

	if h.Mgr.Subtitles != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cands, l, err := h.Mgr.Subtitles.SearchForStream(r.Context(), in)
			if err == nil {
				for _, c := range cands {
					siteCands = append(siteCands, c)
				}
				langs = l
			}
		}()
	}
	if h.Mgr.Addons != nil && (in.IMDb != "" || in.TMDB != "") {
		wg.Add(1)
		go func() {
			defer wg.Done()
			kind := "movie"
			if in.Kind == "show" {
				kind = "show"
			}
			addonCands = h.Mgr.Addons.SearchSubtitles(r.Context(), kind, in.IMDb, in.TMDB, in.Season, in.Episode)
		}()
	}
	wg.Wait()

	merged := make([]any, 0, len(siteCands)+len(addonCands))
	merged = append(merged, siteCands...)
	for _, ac := range addonCands {
		merged = append(merged, ac)
	}
	if langs == nil {
		langs = []string{}
	}
	webserver.WriteJSON(w, http.StatusOK, map[string]any{
		"candidates": merged,
		"languages":  langs,
	})
}

func (h *Handlers) autoSyncSubtitle(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SessionID    string  `json:"session_id"`
		VTT          string  `json:"vtt"`
		StartSec     float64 `json:"start_sec"`
		MaxOffsetSec float64 `json:"max_offset_sec"`
	}
	if !webserver.Decode(w, r, &in, "send {session_id, vtt, start_sec, max_offset_sec}") {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	res, err := h.Mgr.AutoSyncSubtitle(ctx, in.SessionID, in.VTT, in.StartSec, in.MaxOffsetSec)
	if err != nil {
		webserver.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	webserver.WriteJSON(w, http.StatusOK, res)
}
