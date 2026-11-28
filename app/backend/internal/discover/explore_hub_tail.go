package discover

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/webserver"
	"github.com/HolyArrs/holyarrs/app/backend/internal/metadata"
	engine "github.com/HolyArrs/holyarrs/app/backend/internal/suggest"
)

// hubFresh is how many titles a hub row keeps at least: under it, titles an
// earlier row showed come back.
const hubFresh = 10

// hubTailMin is the fewest cards a hub tail row shows.
const hubTailMin = 6

// hubRequest is the list a hub row is read from, with a Kid's exclusions.
func hubRequest(spec hubRowSpec, now time.Time, kid bool) engine.Request {
	q := engine.Request{
		List:      "discover",
		Kind:      spec.Kind,
		Genres:    spec.Genres,
		Without:   spec.Without,
		Lang:      spec.Lang,
		Sort:      spec.Sort,
		MinVote:   spec.MinVote,
		MinVotes:  spec.MinVotes,
		Companies: spec.Companies,
		From:      spec.From,
		To:        spec.To,
		Scope:     engine.ScopeShared,
	}
	if spec.RecentMo > 0 {
		q.From = now.AddDate(0, -spec.RecentMo, 0).Format("2006-01-02")
	}
	if kid {
		if q.Without == "" {
			q.Without = engine.KidWithout
		} else {
			q.Without = q.Without + "|" + engine.KidWithout
		}
		if q.Kind == "movie" {
			q.Cert = "PG"
		}
	}
	return q
}

// hubTails are the hubs whose rows go on past their own: the rows, mixed,
// for a moment.
// allFilters says company filters work (see metadata.ExploreFiltersAll).
var hubTails = map[string]func(now time.Time, allFilters bool) []hubRowSpec{"anime": animeTail}

// animeStudios are anime studios as TMDB numbers them.
var animeStudios = []hubStudio{{"21444", "MAPPA"}, {"5542", "Toei Animation"}, {"5822", "Kyoto Animation"}, {"3464", "Madhouse"},
	{"529", "Production I.G"}, {"2849", "Bones"}, {"5887", "ufotable"}, {"31058", "WIT STUDIO"}, {"10342", "Studio Ghibli"}}

type hubStudio struct{ id, name string }

// animeTail is the Anime tab past its own rows: seasons, genres, anime
// movies by genre, studios, years and decades, mixed.
func animeTail(now time.Time, allFilters bool) []hubRowSpec {
	var seasons, genres, movies, studios, years, decades []hubRowSpec
	names := []string{"Winter", "Spring", "Summer", "Fall"}
	y, q := now.Year(), (int(now.Month())-1)/3
	for i := 0; i < 8; i++ {
		from := time.Date(y, time.Month(q*3+1), 1, 0, 0, 0, 0, time.UTC)
		to := from.AddDate(0, 3, -1)
		seasons = append(seasons, hubRowSpec{ID: fmt.Sprintf("anime_season_%d_%d", y, q), Title: fmt.Sprintf("%s %d anime", names[q], y),
			Kind: "show", Genres: "16", Lang: "ja", From: engine.Day(from), To: engine.Day(to), MinVotes: 1})
		if q--; q < 0 {
			q, y = 3, y-1
		}
	}
	for _, g := range []struct{ id, name string }{{"10759", "Action & adventure"}, {"35", "Comedy"}, {"18", "Drama"}, {"10765", "Sci-fi & fantasy"},
		{"9648", "Mystery"}, {"80", "Crime"}, {"10751", "Family"}} {
		genres = append(genres, hubRowSpec{ID: "anime_g_" + g.id, Title: g.name + " anime", Kind: "show", Genres: "16," + g.id, Lang: "ja", MinVotes: 10},
			hubRowSpec{ID: "anime_gt_" + g.id, Title: "Top-rated " + lower(g.name) + " anime", Kind: "show", Genres: "16," + g.id, Lang: "ja", Sort: "rating", MinVotes: 100})
	}
	for _, g := range []struct{ id, name string }{{"28", "Action"}, {"12", "Adventure"}, {"14", "Fantasy"}, {"10749", "Romance"}, {"18", "Drama"},
		{"878", "Science fiction"}, {"35", "Comedy"}, {"10402", "Music"}} {
		movies = append(movies, hubRowSpec{ID: "anime_m_" + g.id, Title: g.name + " anime movies", Kind: "movie", Genres: "16," + g.id, Lang: "ja", MinVotes: 10})
	}
	movies = append(movies, hubRowSpec{ID: "anime_m_top", Title: "Top-rated anime movies", Kind: "movie", Genres: "16", Lang: "ja", Sort: "rating", MinVotes: 200})
	for _, s := range animeStudios {
		if !allFilters {
			break
		}
		studios = append(studios, hubRowSpec{ID: "anime_s_" + s.id, Title: "From " + s.name, Kind: "show", Genres: "16", Lang: "ja", Companies: s.id, MinVotes: 5},
			hubRowSpec{ID: "anime_sm_" + s.id, Title: s.name + " movies", Kind: "movie", Genres: "16", Lang: "ja", Companies: s.id, MinVotes: 5})
	}
	for yr := now.Year() - 1; yr >= now.Year()-16; yr-- {
		years = append(years, hubRowSpec{ID: "anime_y_" + strconv.Itoa(yr), Title: fmt.Sprintf("Anime from %d", yr), Kind: "show", Genres: "16", Lang: "ja",
			From: fmt.Sprintf("%d-01-01", yr), To: fmt.Sprintf("%d-12-31", yr), MinVotes: 5})
	}
	for _, d := range []int{2010, 2000, 1990, 1980} {
		decades = append(decades, hubRowSpec{ID: fmt.Sprintf("anime_d_%d", d), Title: fmt.Sprintf("%ds anime", d), Kind: "show", Genres: "16", Lang: "ja",
			From: fmt.Sprintf("%d-01-01", d), To: fmt.Sprintf("%d-12-31", d+9), MinVotes: 20},
			hubRowSpec{ID: fmt.Sprintf("anime_dm_%d", d), Title: fmt.Sprintf("%ds anime movies", d), Kind: "movie", Genres: "16", Lang: "ja",
				From: fmt.Sprintf("%d-01-01", d), To: fmt.Sprintf("%d-12-31", d+9), MinVotes: 20})
	}
	var out []hubRowSpec
	lists := [][]hubRowSpec{seasons, genres, movies, studios, years, decades}
	for i := 0; ; i++ {
		added := false
		for _, l := range lists {
			if i < len(l) {
				out = append(out, l[i])
				added = true
			}
		}
		if !added {
			return out
		}
	}
}

func lower(s string) string {
	b := []byte(s)
	if len(b) > 0 && b[0] >= 'A' && b[0] <= 'Z' {
		b[0] += 'a' - 'A'
	}
	return string(b)
}

// hubTailPage answers GET /explore/hub?hub=<id>&page=n (n from 1): the hub's
// rows past its own, TailPage at a time, read now; more says another page
// follows.
func (h *Handlers) hubTailPage(w http.ResponseWriter, r *http.Request, def exploreHubDef, n int) {
	tail, ok := hubTails[def.Info.ID]
	if !ok || h.Explore == nil {
		webserver.WriteJSON(w, http.StatusOK, map[string]any{"hub": def.Info, "page": n, "more": false, "rows": []ExploreRowView{}})
		return
	}
	now := h.Explore.Now().UTC()
	kid := h.KidMax(r) != ""
	var rows []engine.Row
	for _, sp := range tail(now, metadata.ExploreFiltersAll(r.Context(), h.Metadata)) {
		rows = append(rows, engine.Row{ID: sp.ID, Type: "browse", Title: sp.Title, Subtitle: sp.Subtitle, Wide: true, Specs: []engine.Request{hubRequest(sp, now, kid)}})
	}
	slice, more := engine.EndlessPage(rows, n-1)
	got, failed := h.readSpecs(r.Context(), slice, 1)
	read := 0
	for _, row := range got {
		read += len(row.Items)
	}
	if failed || read == 0 {
		more = false
	}
	views, err := h.rowViews(r, got)
	if err != nil {
		webserver.Internal(w, "discover", err)
		return
	}
	full := views[:0]
	for _, v := range views {
		if len(v.Items) >= hubTailMin {
			full = append(full, v)
		}
	}
	views = full
	h.Caching(w, r, "private, max-age=300")
	webserver.WriteJSON(w, http.StatusOK, map[string]any{"hub": def.Info, "page": n, "more": more, "rows": views})
}

// hubHasTail says the hub's rows go on past its own.
func hubHasTail(id string) bool { _, ok := hubTails[id]; return ok }
