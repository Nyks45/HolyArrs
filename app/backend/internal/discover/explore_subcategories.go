package discover

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/webserver"
	"github.com/HolyArrs/holyarrs/app/backend/internal/metadata"
	engine "github.com/HolyArrs/holyarrs/app/backend/internal/suggest"
)

// exploreSubcategories answers GET /api/v1/explore/subcategories?type=&genre=&q=
// with ordered Browse categories / subcategories for the tab or query.
func (h *Handlers) exploreSubcategories(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sw, ok := categoryExploreSwitch(q.Get("type"))
	if !ok {
		webserver.Fail(w, http.StatusBadRequest, "type must be all, movies, shows or anime")
		return
	}
	now := time.Now()
	if h.Explore != nil {
		now = h.Explore.Now()
	}
	kid := h.KidMax(r) != ""
	items := engine.SubcategoriesFor(sw, q.Get("genre"), q.Get("q"), kid, now)
	if items == nil {
		items = []engine.CategorySummary{}
	}
	h.Caching(w, r, "private, no-cache")
	webserver.WriteJSON(w, http.StatusOK, map[string]any{"type": sw, "items": items})
}

func categoryExploreSwitch(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.EqualFold(raw, "anime") {
		return "anime", true
	}
	return ExploreSwitch(raw)
}

// exploreOpened answers POST /api/v1/explore/opened {kind, tmdb_id, name, year}
// so opening a title details page gently feeds future personalized recommendations.
func (h *Handlers) exploreOpened(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind   string `json:"kind"`
		TMDBID string `json:"tmdb_id"`
		Name   string `json:"name"`
		Year   int    `json:"year"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&body); err != nil {
		webserver.Fail(w, http.StatusBadRequest, "kind must be movie or show")
		return
	}
	kind := normalizeSignalKind(body.Kind)
	if kind == "" {
		webserver.Fail(w, http.StatusBadRequest, "kind must be movie or show")
		return
	}
	tmdb := strings.TrimSpace(body.TMDBID)
	if tmdb == "" {
		webserver.Fail(w, http.StatusBadRequest, "tmdb_id must be a TMDB number")
		return
	}
	uid := h.UserID(r)
	if uid > 0 && h.ViewSignals != nil {
		shouldRefresh, err := h.ViewSignals.Record(uid, kind, tmdb, body.Name, body.Year)
		if err != nil {
			webserver.Internal(w, "discover", err)
			return
		}
		if shouldRefresh && h.Explore != nil && h.Store != nil {
			h.Explore.MarkMemberStale(context.Background(), uid, h.Explore.Now())
		}
	}
	webserver.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handlers) enrichSimilar(ctx context.Context, kind, id string, existing []metadata.DiscoverItem) []metadata.DiscoverItem {
	if len(existing) >= 16 || h.Explore == nil {
		return existing
	}
	seen := make(map[string]bool, len(existing)+1)
	seen[kind+":"+id] = true
	out := make([]metadata.DiscoverItem, 0, 20)
	for _, it := range existing {
		seen[it.Kind+":"+it.TMDBID] = true
		out = append(out, it)
	}
	sctx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
	defer cancel()
	extra, err := h.LiveExplore(sctx).List(sctx, engine.Request{List: "similar", Kind: kind, Ref: id})
	if err != nil || len(extra) == 0 {
		return existing
	}
	for _, it := range extra {
		k := it.Kind + ":" + it.TMDBID
		if it.TMDBID == "" || seen[k] {
			continue
		}
		seen[k] = true
		genres := it.Genres
		if genres == nil {
			genres = []string{}
		}
		out = append(out, metadata.DiscoverItem{
			TMDBID:      it.TMDBID,
			Kind:        it.Kind,
			Name:        it.Name,
			Year:        it.Year,
			Poster:      it.Poster,
			Backdrop:    it.Backdrop,
			Overview:    it.Overview,
			Genres:      genres,
			Popularity:  it.Pop,
			VoteAverage: it.Vote,
			VoteCount:   it.Votes,
			Released:    it.Released,
		})
		if len(out) >= 20 {
			break
		}
	}
	return out
}
