package discover

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/webserver"
	"github.com/HolyArrs/holyarrs/app/backend/internal/metadata"
	engine "github.com/HolyArrs/holyarrs/app/backend/internal/suggest"
)

// searchPageSize is how many results one TMDB search page holds; a shorter
// page is the last.
const searchPageSize = 20

// searchMore answers GET /api/v1/explore/search?kind=movie|show&q=&page=n:
// search results past the first page (from 2), joined with the Library and
// passed through the Kid, hidden and never-add filters like a row; more says
// another page may follow.
func (h *Handlers) searchMore(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kind, term := q.Get("kind"), strings.TrimSpace(q.Get("q"))
	n, err := strconv.Atoi(q.Get("page"))
	if err != nil || n < 2 || n > 500 {
		webserver.Fail(w, http.StatusBadRequest, "page must be 1 to 500")
		return
	}
	if (kind != "movie" && kind != "show") || term == "" || len(term) > 200 {
		webserver.Fail(w, http.StatusBadRequest, "send ?kind=movie or show, q=<words> and page=<n>")
		return
	}
	if h.KidLimited(w, r) {
		return
	}
	sp, ok := metadata.SearchPagerFor(r.Context(), h.Metadata)
	if !ok {
		webserver.WriteJSON(w, http.StatusOK, map[string]any{"page": n, "more": false, "items": []ItemView{}})
		return
	}
	got, err := sp.SearchPage(r.Context(), kind, term, n)
	if err != nil {
		Fail(w, err)
		return
	}
	items := make([]engine.Item, 0, len(got))
	for _, d := range got {
		items = append(items, engine.Item{TMDBID: d.TMDBID, Kind: d.Kind, Name: d.Name, Year: d.Year, Poster: d.Poster, Backdrop: d.Backdrop,
			Overview: d.Overview, Genres: d.Genres, Released: d.Released})
	}
	views, err := h.rowViews(r, []engine.Row{{ID: "search", Type: "browse", Items: items}})
	if err != nil {
		webserver.Internal(w, "api", err)
		return
	}
	out := []ItemView{}
	if len(views) == 1 {
		out = views[0].Items
	}
	h.Caching(w, r, "private, max-age=300")
	webserver.WriteJSON(w, http.StatusOK, map[string]any{"page": n, "more": len(got) >= searchPageSize && n < 500, "items": out})
}
