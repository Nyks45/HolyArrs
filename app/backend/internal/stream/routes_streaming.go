package stream

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/webserver"
)

func (h *Handlers) registerStreamingRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/streaming/prefs", h.getPrefs)
	m.HandleFunc("PUT /api/v1/streaming/prefs", h.putPrefs)
	m.HandleFunc("GET /api/v1/streaming/presets", h.getPresets)
	m.HandleFunc("POST /api/v1/streaming/presets/setup", h.applyPresets)

	m.HandleFunc("GET /api/v1/streaming/addons", h.listAddons)
	m.HandleFunc("POST /api/v1/streaming/addons", h.addAddon)
	m.HandleFunc("PUT /api/v1/streaming/addons/{id}", h.patchAddon)
	m.HandleFunc("DELETE /api/v1/streaming/addons/{id}", h.deleteAddon)
	m.HandleFunc("POST /api/v1/streaming/addons/{id}/refresh", h.refreshAddon)
	m.HandleFunc("GET /api/v1/streaming/addons/catalogs", h.listAddonCatalogs)
	m.HandleFunc("GET /api/v1/streaming/addons/{id}/catalog/{type}/{catalog}", h.getAddonCatalog)

	m.HandleFunc("GET /api/v1/streaming/plugins", h.listPlugins)
	m.HandleFunc("POST /api/v1/streaming/plugins", h.addPluginRepo)
	m.HandleFunc("PUT /api/v1/streaming/plugins/{id}", h.patchPluginRepo)
	m.HandleFunc("DELETE /api/v1/streaming/plugins/{id}", h.deletePluginRepo)
	m.HandleFunc("POST /api/v1/streaming/plugins/{id}/refresh", h.refreshPluginRepo)

	m.HandleFunc("GET /api/v1/streaming/debrid", h.getDebrid)
	m.HandleFunc("PUT /api/v1/streaming/debrid/{provider}", h.putDebrid)
	m.HandleFunc("DELETE /api/v1/streaming/debrid/{provider}", h.deleteDebrid)

	m.HandleFunc("GET /api/v1/streaming/skip", h.getSkip)

	m.HandleFunc("GET /api/v1/streaming/sync", h.getSync)
	m.HandleFunc("POST /api/v1/streaming/sync/scrobble", h.postScrobble)
	m.HandleFunc("POST /api/v1/streaming/sync/pull", h.pullSync)
	m.HandleFunc("POST /api/v1/streaming/sync/simkl/pin", h.startSimklPIN)
	m.HandleFunc("POST /api/v1/streaming/sync/simkl/poll", h.pollSimklPIN)
	m.HandleFunc("DELETE /api/v1/streaming/sync/simkl", h.disconnectSimkl)

	m.HandleFunc("GET /api/v1/streaming/proxy/poster", h.Mgr.Proxy.ServePosterProxy)
	m.HandleFunc("GET /api/v1/streaming/proxy/{token}", h.Mgr.Proxy.ServeProxy)
	m.HandleFunc("GET /api/v1/streaming/proxy/{token}/hls", h.Mgr.Proxy.ServeProxyHLS)
}

func actorFromRequest(r *http.Request) database.Actor {
	uid := callerID(r)
	if uid > 0 {
		return database.Actor{Kind: "user", ID: uid}
	}
	return database.System
}

func (h *Handlers) getPrefs(w http.ResponseWriter, r *http.Request) {
	webserver.WriteJSON(w, http.StatusOK, h.Mgr.Prefs.Get())
}

func (h *Handlers) putPrefs(w http.ResponseWriter, r *http.Request) {
	var in StreamPrefs
	if !webserver.Decode(w, r, &in, "send streaming preferences JSON") {
		return
	}
	saved, err := h.Mgr.Prefs.Save(in)
	if err != nil {
		webserver.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	webserver.WriteJSON(w, http.StatusOK, saved)
}

func (h *Handlers) listAddons(w http.ResponseWriter, r *http.Request) {
	webserver.WriteJSON(w, http.StatusOK, map[string]any{
		"addons": h.Mgr.Addons.List(),
	})
}

func (h *Handlers) addAddon(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ManifestURL string `json:"manifest_url"`
	}
	if !webserver.Decode(w, r, &in, "send {manifest_url}") {
		return
	}
	addon, err := h.Mgr.Addons.Add(r.Context(), in.ManifestURL)
	if err != nil {
		webserver.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	webserver.WriteJSON(w, http.StatusCreated, addon)
}

func (h *Handlers) patchAddon(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled *bool `json:"enabled"`
		Order   *int  `json:"order"`
	}
	if !webserver.Decode(w, r, &in, "send {enabled, order}") {
		return
	}
	addon, err := h.Mgr.Addons.Toggle(r.PathValue("id"), in.Enabled, in.Order)
	if err != nil {
		webserver.Fail(w, http.StatusNotFound, err.Error())
		return
	}
	webserver.WriteJSON(w, http.StatusOK, addon)
}

func (h *Handlers) deleteAddon(w http.ResponseWriter, r *http.Request) {
	if err := h.Mgr.Addons.Delete(r.PathValue("id")); err != nil {
		webserver.Fail(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) refreshAddon(w http.ResponseWriter, r *http.Request) {
	addon, err := h.Mgr.Addons.Refresh(r.Context(), r.PathValue("id"))
	if err != nil {
		webserver.Fail(w, http.StatusBadGateway, err.Error())
		return
	}
	webserver.WriteJSON(w, http.StatusOK, addon)
}

func (h *Handlers) listAddonCatalogs(w http.ResponseWriter, r *http.Request) {
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	webserver.WriteJSON(w, http.StatusOK, map[string]any{
		"shelves": h.Mgr.Addons.BrowseCatalogs(r.Context(), kind),
	})
}

func (h *Handlers) getAddonCatalog(w http.ResponseWriter, r *http.Request) {
	genre := strings.TrimSpace(r.URL.Query().Get("genre"))
	skip, _ := strconv.Atoi(r.URL.Query().Get("skip"))
	items, err := h.Mgr.Addons.FetchCatalog(r.Context(), r.PathValue("id"), r.PathValue("type"), r.PathValue("catalog"), genre, skip)
	if err != nil {
		webserver.Fail(w, http.StatusBadGateway, err.Error())
		return
	}
	webserver.WriteJSON(w, http.StatusOK, map[string]any{
		"items": items,
	})
}

func (h *Handlers) listPlugins(w http.ResponseWriter, r *http.Request) {
	webserver.WriteJSON(w, http.StatusOK, map[string]any{
		"repos":          h.Mgr.Plugins.List(),
		"node_available": h.Mgr.Plugins.NodeAvailable(),
	})
}

func (h *Handlers) addPluginRepo(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ManifestURL string `json:"manifest_url"`
	}
	if !webserver.Decode(w, r, &in, "send {manifest_url}") {
		return
	}
	repo, err := h.Mgr.Plugins.AddRepo(r.Context(), in.ManifestURL)
	if err != nil {
		webserver.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	webserver.WriteJSON(w, http.StatusCreated, repo)
}

func (h *Handlers) patchPluginRepo(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled        *bool          `json:"enabled"`
		ScraperID      string         `json:"scraper_id"`
		ScraperEnabled *bool          `json:"scraper_enabled"`
		Settings       map[string]any `json:"settings"`
	}
	if !webserver.Decode(w, r, &in, "send {enabled, scraper_id, scraper_enabled, settings}") {
		return
	}
	repo, err := h.Mgr.Plugins.UpdateRepo(r.PathValue("id"), in.Enabled, in.ScraperID, in.ScraperEnabled, in.Settings)
	if err != nil {
		webserver.Fail(w, http.StatusNotFound, err.Error())
		return
	}
	webserver.WriteJSON(w, http.StatusOK, repo)
}

func (h *Handlers) deletePluginRepo(w http.ResponseWriter, r *http.Request) {
	if err := h.Mgr.Plugins.DeleteRepo(r.PathValue("id")); err != nil {
		webserver.Fail(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) refreshPluginRepo(w http.ResponseWriter, r *http.Request) {
	repo, err := h.Mgr.Plugins.RefreshRepo(r.Context(), r.PathValue("id"))
	if err != nil {
		webserver.Fail(w, http.StatusBadGateway, err.Error())
		return
	}
	webserver.WriteJSON(w, http.StatusOK, repo)
}

func (h *Handlers) getDebrid(w http.ResponseWriter, r *http.Request) {
	webserver.WriteJSON(w, http.StatusOK, map[string]any{
		"providers": h.Mgr.Debrid.Status(r.Context()),
	})
}

func (h *Handlers) putDebrid(w http.ResponseWriter, r *http.Request) {
	var in struct {
		APIKey string `json:"api_key"`
	}
	if !webserver.Decode(w, r, &in, "send {api_key}") {
		return
	}
	st, err := h.Mgr.Debrid.SaveKey(r.Context(), r.PathValue("provider"), in.APIKey, actorFromRequest(r))
	if err != nil {
		webserver.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	webserver.WriteJSON(w, http.StatusOK, st)
}

func (h *Handlers) deleteDebrid(w http.ResponseWriter, r *http.Request) {
	if err := h.Mgr.Debrid.DeleteKey(r.Context(), r.PathValue("provider"), actorFromRequest(r)); err != nil {
		webserver.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) getSkip(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kind := strings.TrimSpace(q.Get("kind"))
	if kind != "show" {
		kind = "movie"
	}
	imdb := strings.TrimSpace(q.Get("imdb"))
	tmdb := strings.TrimSpace(q.Get("tmdb"))
	title := strings.TrimSpace(q.Get("title"))
	season, _ := strconv.Atoi(q.Get("season"))
	episode, _ := strconv.Atoi(q.Get("episode"))
	animeHint := q.Get("anime") == "1" || strings.EqualFold(q.Get("anime"), "true")
	sessID := strings.TrimSpace(q.Get("session_id"))

	var chapters []ChapterMark
	if sessID != "" {
		if st, ok := h.Mgr.Status(sessID); ok {
			chapters = st.Chapters
		}
	}
	prefs := h.Mgr.Prefs.Get()
	segs := h.Mgr.Skip.GetSegments(r.Context(), kind, imdb, tmdb, season, episode, animeHint, title, prefs.AnimeSkipClientID, chapters)
	webserver.WriteJSON(w, http.StatusOK, map[string]any{
		"segments": segs,
	})
}

func (h *Handlers) getSync(w http.ResponseWriter, r *http.Request) {
	webserver.WriteJSON(w, http.StatusOK, h.Mgr.Scrobble.Status(r.Context()))
}

func (h *Handlers) postScrobble(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Action   string  `json:"action"`
		Kind     string  `json:"kind"`
		TMDB     int     `json:"tmdb"`
		IMDb     string  `json:"imdb"`
		Season   int     `json:"season"`
		Episode  int     `json:"episode"`
		Progress float64 `json:"progress"`
	}
	if !webserver.Decode(w, r, &in, "send {action, kind, tmdb, imdb, season, episode, progress}") {
		return
	}
	sent := h.Mgr.Scrobble.Scrobble(r.Context(), in.Action, in.Kind, in.TMDB, in.IMDb, in.Season, in.Episode, in.Progress)
	webserver.WriteJSON(w, http.StatusOK, map[string]any{
		"sent": sent,
	})
}

func (h *Handlers) pullSync(w http.ResponseWriter, r *http.Request) {
	n, err := h.Mgr.Scrobble.PullProgress(r.Context(), h.Mgr.History, callerID(r))
	if err != nil {
		webserver.Fail(w, http.StatusBadGateway, err.Error())
		return
	}
	webserver.WriteJSON(w, http.StatusOK, map[string]any{
		"merged": n,
	})
}

func (h *Handlers) startSimklPIN(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ClientID string `json:"client_id"`
	}
	_ = webserver.Decode(w, r, &in, "")
	ch, err := h.Mgr.Scrobble.StartSimklPIN(r.Context(), in.ClientID, actorFromRequest(r))
	if err != nil {
		webserver.Fail(w, http.StatusBadGateway, err.Error())
		return
	}
	webserver.WriteJSON(w, http.StatusOK, ch)
}

func (h *Handlers) pollSimklPIN(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserCode string `json:"user_code"`
	}
	if !webserver.Decode(w, r, &in, "send {user_code}") {
		return
	}
	ok, user, err := h.Mgr.Scrobble.PollSimklPIN(r.Context(), in.UserCode, actorFromRequest(r))
	if err != nil {
		webserver.Fail(w, http.StatusBadGateway, err.Error())
		return
	}
	webserver.WriteJSON(w, http.StatusOK, map[string]any{
		"connected": ok,
		"username":  user,
	})
}

func (h *Handlers) disconnectSimkl(w http.ResponseWriter, r *http.Request) {
	if err := h.Mgr.Scrobble.DisconnectSimkl(r.Context(), actorFromRequest(r)); err != nil {
		webserver.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) getPresets(w http.ResponseWriter, r *http.Request) {
	webserver.WriteJSON(w, http.StatusOK, h.Mgr.Presets())
}

func (h *Handlers) applyPresets(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind        string `json:"kind"`
		ID          string `json:"id"`
		Recommended *bool  `json:"recommended"`
	}
	if r.ContentLength != 0 {
		if !webserver.Decode(w, r, &in, "send optional {kind, id, recommended}") {
			return
		}
	}
	rec := in.ID == ""
	if in.Recommended != nil {
		rec = *in.Recommended
	}
	res := h.Mgr.ApplyPresets(r.Context(), in.Kind, in.ID, rec)
	webserver.WriteJSON(w, http.StatusOK, res)
}
