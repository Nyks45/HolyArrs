package accounts

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/webserver"
)

func TestStreamingRespectsKidAccounts(t *testing.T) {
	mux := http.NewServeMux()
	for _, pattern := range []string{
		"POST /api/v1/stream/search",
		"POST /api/v1/stream/start",
		"GET /api/v1/streaming/addons/{id}/catalog/{type}/{catalog}",
		"GET /api/v1/explore/hub",
	} {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
	}
	for _, tc := range []struct {
		method, path string
		kid          int
	}{
		{http.MethodPost, "/api/v1/stream/search", http.StatusForbidden},
		{http.MethodPost, "/api/v1/stream/start", http.StatusForbidden},
		{http.MethodGet, "/api/v1/streaming/addons/test/catalog/movie/recent", http.StatusForbidden},
		{http.MethodGet, "/api/v1/explore/hub", http.StatusNoContent},
	} {
		t.Run(tc.path, func(t *testing.T) {
			for _, max := range []string{"", "PG"} {
				r := httptest.NewRequest(tc.method, tc.path, nil)
				r = r.WithContext(webserver.ContextWith(r.Context(), webserver.Caller{
					UserID: 1, Role: webserver.RoleMember, MaxRating: max, Via: "session",
				}))
				w := httptest.NewRecorder()
				Gate(mux).ServeHTTP(w, r)
				want := http.StatusNoContent
				if max != "" {
					want = tc.kid
				}
				if w.Code != want {
					t.Errorf("rating limit %q: status %d, want %d", max, w.Code, want)
				}
			}
		})
	}
}
