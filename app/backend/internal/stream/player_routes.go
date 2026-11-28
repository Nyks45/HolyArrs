package stream

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/webserver"
	"github.com/HolyArrs/holyarrs/app/backend/internal/subtitles"
)

func (h *Handlers) startHLS(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Quality     string  `json:"quality"`
		AudioIndex  int     `json:"audio_index"`
		StartSec    float64 `json:"start_sec"`
		Reencode    bool    `json:"reencode"`
		H264Quality string  `json:"h264_quality"`
	}
	if r.ContentLength != 0 && !webserver.Decode(w, r, &in, "send {quality, audio_index, start_sec}") {
		return
	}
	url, startSec, err := h.Mgr.EnsureHLS(r.Context(), r.PathValue("id"), in.Quality, in.AudioIndex, in.StartSec, in.Reencode, in.H264Quality)
	if err != nil {
		webserver.Fail(w, http.StatusBadGateway, err.Error())
		return
	}
	webserver.WriteJSON(w, http.StatusOK, map[string]any{
		"playlist_url": url,
		"start_sec":    startSec,
	})
}

func (h *Handlers) fetchSubtitle(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID        string `json:"id"`
		SessionID string `json:"session_id"`
	}
	if !webserver.Decode(w, r, &in, "send {id, session_id}") {
		return
	}
	var (
		vtt  []byte
		ass  string
		lang string
		err  error
	)
	if strings.HasPrefix(in.ID, "stremio:") {
		if h.Mgr.Addons == nil {
			webserver.Fail(w, http.StatusServiceUnavailable, "addon subtitle store unavailable")
			return
		}
		raw, hint, alang, ferr := h.Mgr.Addons.FetchSubtitle(r.Context(), in.ID)
		if ferr != nil {
			webserver.Fail(w, http.StatusBadGateway, ferr.Error())
			return
		}
		lang = subtitles.NormLanguage(alang)
		vtt, err = subtitles.ConvertToWebVTT(raw, fallbackString(hint, "subtitle.srt"), lang)
		ass = styledSubtitleText(raw, hint, lang)
	} else {
		if h.Mgr.Subtitles == nil {
			webserver.Fail(w, http.StatusServiceUnavailable, "subtitle service is unavailable")
			return
		}
		vtt, lang, err = h.Mgr.Subtitles.FetchStreamSubtitleVTT(r.Context(), in.ID)
	}
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
	writeSubtitlePayload(w, vtt, ass, lang, subFile)
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
	writeSubtitlePayload(w, vtt, styledSubtitleText([]byte(in.Content), hint, lang), lang, subFile)
}
