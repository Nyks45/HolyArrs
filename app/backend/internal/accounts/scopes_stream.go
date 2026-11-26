package accounts

import "github.com/HolyArrs/holyarrs/app/backend/internal/core/webserver"

func init() {
	streamSignedIn := []string{
		"GET /api/v1/explore/hubs",
		"GET /api/v1/explore/hub",
		"GET /api/v1/stream/sources",
		"POST /api/v1/stream/search",
		"POST /api/v1/stream/start",
		"GET /api/v1/stream/sessions",
		"GET /api/v1/stream/history",
		"GET /api/v1/stream/history/title/{kind}/{tmdb}",
		"PUT /api/v1/stream/history",
		"DELETE /api/v1/stream/history/{key}",
		"POST /api/v1/stream/subtitles/search",
		"POST /api/v1/stream/subtitles/fetch",
		"POST /api/v1/stream/subtitles/upload",
		"GET /api/v1/stream/{id}",
		"POST /api/v1/stream/{id}/file",
		"POST /api/v1/stream/{id}/hls",
		"POST /api/v1/stream/{id}/mpv",
		"PUT /api/v1/stream/{id}/keep",
		"DELETE /api/v1/stream/{id}",
		"GET /api/v1/streaming/prefs",
		"GET /api/v1/streaming/addons",
		"GET /api/v1/streaming/addons/catalogs",
		"GET /api/v1/streaming/addons/{id}/catalog/{type}/{catalog}",
		"GET /api/v1/streaming/plugins",
		"GET /api/v1/streaming/debrid",
		"GET /api/v1/streaming/skip",
		"GET /api/v1/streaming/sync",
		"POST /api/v1/streaming/sync/scrobble",
		"POST /api/v1/streaming/sync/pull",
		"GET /api/v1/streaming/proxy/poster",
	}
	for _, r := range streamSignedIn {
		RoutePerms[r] = webserver.NeedSignedIn
	}

	streamSettings := []string{
		"PUT /api/v1/streaming/prefs",
		"POST /api/v1/streaming/addons",
		"PUT /api/v1/streaming/addons/{id}",
		"DELETE /api/v1/streaming/addons/{id}",
		"POST /api/v1/streaming/addons/{id}/refresh",
		"POST /api/v1/streaming/plugins",
		"PUT /api/v1/streaming/plugins/{id}",
		"DELETE /api/v1/streaming/plugins/{id}",
		"POST /api/v1/streaming/plugins/{id}/refresh",
		"PUT /api/v1/streaming/debrid/{provider}",
		"DELETE /api/v1/streaming/debrid/{provider}",
		"POST /api/v1/streaming/sync/simkl/pin",
		"POST /api/v1/streaming/sync/simkl/poll",
		"DELETE /api/v1/streaming/sync/simkl",
	}
	for _, r := range streamSettings {
		RoutePerms[r] = webserver.PermSettings
	}

	streamPublic := []string{
		"GET /api/v1/stream/{id}/media/{token}",
		"GET /api/v1/stream/{id}/hls/{token}/{gen}/{file}",
		"GET /api/v1/stream/{id}/sub/{token}/{idx}",
		"GET /api/v1/streaming/proxy/{token}",
		"GET /api/v1/streaming/proxy/{token}/hls",
	}
	for _, r := range streamPublic {
		RoutePerms[r] = webserver.NeedPublic
	}

	for _, r := range []string{
		"GET /api/v1/streaming/prefs",
		"GET /api/v1/streaming/addons",
		"GET /api/v1/streaming/addons/catalogs",
		"GET /api/v1/streaming/addons/{id}/catalog/{type}/{catalog}",
		"GET /api/v1/streaming/plugins",
		"GET /api/v1/streaming/debrid",
		"GET /api/v1/streaming/skip",
		"GET /api/v1/streaming/sync",
		"GET /api/v1/streaming/proxy/poster",
		"GET /api/v1/streaming/proxy/{token}",
		"GET /api/v1/streaming/proxy/{token}/hls",
	} {
		RouteScopes[r] = webserver.ScopeAdmin
	}

	ReadRoutes["GET /api/v1/explore/hubs"] = true
	ReadRoutes["GET /api/v1/explore/hub"] = true
}
