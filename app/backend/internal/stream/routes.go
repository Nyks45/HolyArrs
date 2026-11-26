package stream

import (
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/webserver"
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
	m.HandleFunc("DELETE /api/v1/stream/history/{key}", h.deleteHistory)
	m.HandleFunc("POST /api/v1/stream/subtitles/search", h.searchSubtitles)
	m.HandleFunc("POST /api/v1/stream/subtitles/fetch", h.fetchSubtitle)
	m.HandleFunc("POST /api/v1/stream/subtitles/upload", h.uploadSubtitle)
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
	srcs, err := sources.ListStreamSources(r.Context(), h.Mgr.Store.Read(), kind)
	if err != nil {
		webserver.Internal(w, "stream", err)
		return
	}
	_, mpvErr := exec.LookPath("mpv")
	_, ffErr := exec.LookPath("ffmpeg")
	webserver.WriteJSON(w, http.StatusOK, map[string]any{
		"sources":          srcs,
		"mpv_available":    mpvErr == nil,
		"ffmpeg_available": ffErr == nil,
	})
}

func (h *Handlers) search(w http.ResponseWriter, r *http.Request) {
	var in sources.StreamSearchReq
	if !webserver.Decode(w, r, &in, "send {query, kind, year, season, episode, imdb, tmdb, source_ids}") {
		return
	}
	cands, checks, err := sources.SearchForStream(r.Context(), h.Mgr.Store.Read(), h.Mgr.Secrets, in)
	if err != nil {
		webserver.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	webserver.WriteJSON(w, http.StatusOK, map[string]any{
		"candidates": cands,
		"sources":    checks,
	})
}

func (h *Handlers) start(w http.ResponseWriter, r *http.Request) {
	var in StartReq
	if !webserver.Decode(w, r, &in, "send {source_id, source_name, link, title, release_title, kind, season, episode, keep_file}") {
		return
	}
	st, err := h.Mgr.Start(r.Context(), callerID(r), in)
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

func (h *Handlers) startHLS(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Quality    string  `json:"quality"`
		AudioIndex int     `json:"audio_index"`
		StartSec   float64 `json:"start_sec"`
	}
	if r.ContentLength != 0 && !webserver.Decode(w, r, &in, "send {quality, audio_index, start_sec}") {
		return
	}
	url, startSec, err := h.Mgr.EnsureHLS(r.Context(), r.PathValue("id"), in.Quality, in.AudioIndex, in.StartSec)
	if err != nil {
		webserver.Fail(w, http.StatusBadGateway, err.Error())
		return
	}
	webserver.WriteJSON(w, http.StatusOK, map[string]any{
		"playlist_url": url,
		"start_sec":    startSec,
	})
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
	if h.Mgr.History != nil {
		_ = h.Mgr.History.Delete(callerID(r), r.PathValue("key"))
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) searchSubtitles(w http.ResponseWriter, r *http.Request) {
	if h.Mgr.Subtitles == nil {
		webserver.Fail(w, http.StatusServiceUnavailable, "subtitle service is unavailable")
		return
	}
	var in subtitles.StreamSubSearchReq
	if !webserver.Decode(w, r, &in, "send {kind, title, year, season, episode, imdb, tmdb, release_name, languages}") {
		return
	}
	cands, langs, err := h.Mgr.Subtitles.SearchForStream(r.Context(), in)
	if err != nil {
		webserver.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	webserver.WriteJSON(w, http.StatusOK, map[string]any{
		"candidates": cands,
		"languages":  langs,
	})
}

func (h *Handlers) fetchSubtitle(w http.ResponseWriter, r *http.Request) {
	if h.Mgr.Subtitles == nil {
		webserver.Fail(w, http.StatusServiceUnavailable, "subtitle service is unavailable")
		return
	}
	var in struct {
		ID        string `json:"id"`
		SessionID string `json:"session_id"`
	}
	if !webserver.Decode(w, r, &in, "send {id, session_id}") {
		return
	}
	vtt, lang, err := h.Mgr.Subtitles.FetchStreamSubtitleVTT(r.Context(), in.ID)
	if err != nil {
		webserver.Fail(w, http.StatusBadGateway, err.Error())
		return
	}
	subFile := ""
	if strings.TrimSpace(in.SessionID) != "" {
		if savedPath, serr := h.Mgr.SaveSessionSubtitle(in.SessionID, vtt); serr == nil {
			subFile = savedPath
		}
	}
	webserver.WriteJSON(w, http.StatusOK, map[string]any{
		"vtt":      string(vtt),
		"language": lang,
		"sub_file": subFile,
	})
}

func (h *Handlers) uploadSubtitle(w http.ResponseWriter, r *http.Request) {
	const maxUploadBytes = (5 << 20) + 8192
	rawBody, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxUploadBytes))
	if err != nil {
		webserver.Fail(w, http.StatusRequestEntityTooLarge, "subtitle file exceeds 5 MB limit")
		return
	}
	var in struct {
		Filename  string `json:"filename"`
		Language  string `json:"language"`
		Content   string `json:"content"`
		SessionID string `json:"session_id"`
	}
	if json.Unmarshal(rawBody, &in) != nil {
		webserver.Fail(w, http.StatusBadRequest, "send {filename, language, content, session_id}")
		return
	}
	if len(in.Content) > 5<<20 {
		webserver.Fail(w, http.StatusRequestEntityTooLarge, "subtitle file exceeds 5 MB limit")
		return
	}
	if strings.TrimSpace(in.Content) == "" {
		webserver.Fail(w, http.StatusBadRequest, "subtitle content is empty")
		return
	}
	hint := strings.TrimSpace(in.Filename)
	if hint == "" {
		hint = "subtitle.srt"
	}
	lang := subtitles.NormLanguage(in.Language)
	vtt, err := subtitles.ConvertToWebVTT([]byte(in.Content), hint, lang)
	if err != nil {
		webserver.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	subFile := ""
	if strings.TrimSpace(in.SessionID) != "" {
		if savedPath, serr := h.Mgr.SaveSessionSubtitle(in.SessionID, vtt); serr == nil {
			subFile = savedPath
		}
	}
	webserver.WriteJSON(w, http.StatusOK, map[string]any{
		"vtt":      string(vtt),
		"language": lang,
		"sub_file": subFile,
	})
}
