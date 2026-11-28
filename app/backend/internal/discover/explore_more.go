package discover

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/webserver"
	"github.com/HolyArrs/holyarrs/app/backend/internal/metadata"
	engine "github.com/HolyArrs/holyarrs/app/backend/internal/suggest"
)

// endlessWait is how long one tail page or row page waits for its lists;
// a list still out lands in the cache for the next ask.
const endlessWait = 20 * time.Second

// maxTailPage is the last tail page a page may ask for.
const maxTailPage = 2000

// readSpecs reads every row's lists at page, each row's lists side by side
// and the rows side by side, and returns the rows with the items they got
// (interleaved by list, each title once). failed says every list failed.
func (h *Handlers) readSpecs(ctx context.Context, rows []engine.Row, page int) (out []engine.Row, failed bool) {
	if h.Explore == nil {
		return []engine.Row{}, true
	}
	src := h.LiveExplore(ctx)
	ctx, cancel := context.WithTimeout(ctx, endlessWait)
	defer cancel()
	got := make([][][]engine.Item, len(rows))
	var mu sync.Mutex
	asked, bad := 0, 0
	var wg sync.WaitGroup
	for i, row := range rows {
		got[i] = make([][]engine.Item, len(row.Specs))
		for j, q := range row.Specs {
			q.Page = page
			asked++
			wg.Add(1)
			go func(i, j int, q engine.Request) {
				defer wg.Done()
				items, err := src.List(ctx, q)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					bad++
					return
				}
				got[i][j] = items
			}(i, j, q)
		}
	}
	wg.Wait()
	out = make([]engine.Row, 0, len(rows))
	for i, row := range rows {
		seen := map[string]bool{}
		var items []engine.Item
		for k := 0; ; k++ {
			added := false
			for _, l := range got[i] {
				if k < len(l) {
					added = true
					if it := l[k]; !seen[it.Key()] {
						seen[it.Key()] = true
						items = append(items, it)
					}
				}
			}
			if !added {
				break
			}
		}
		row.Items = items
		out = append(out, row)
	}
	return out, asked > 0 && bad == asked
}

// endlessCatalog is the member's endless tail past the pool (no items yet),
// leaving out the genres and rows the pool's page already has.
func (h *Handlers) endlessCatalog(ctx context.Context, r *http.Request, member int64, sw string, pg engine.Page) []engine.Row {
	in := engine.EndlessIn{Switch: sw, Kid: pg.Kid || h.KidMax(r) != "", Now: h.Explore.Now(), Later: pg.Later, Skip: map[string]bool{},
		AllFilters: metadata.ExploreFiltersAll(ctx, h.Metadata)}
	if m, err := h.Explore.LoadMember(ctx, member); err == nil {
		in.Region, in.Services = m.Region, m.Services
		in.Kid = in.Kid || m.Kid
	}
	if reg := strings.ToUpper(r.URL.Query().Get("region")); IsCountry(reg) {
		in.Region = reg
	}
	for _, rows := range [][]engine.Row{pg.Head, pg.Tail} {
		for _, row := range rows {
			in.Skip[row.ID] = true
			if row.Genre != "" && (row.Type == engine.TypePopularGenre || row.Type == engine.TypeGenre) {
				in.Skip[row.Genre] = true
			}
		}
	}
	return engine.EndlessRows(in)
}

// rowViews joins rows read from their lists with the Library, the way a
// head or tail row is joined, and fills their card extras.
func (h *Handlers) rowViews(r *http.Request, rows []engine.Row) ([]ExploreRowView, error) {
	views, err := h.ExploreViews(r.Context(), r, rows)
	if err != nil {
		return nil, err
	}
	if h.FillTrailerKeys != nil {
		for i := range views {
			h.FillTrailerKeys(r, views[i].Items)
		}
	}
	if h.FillCardArt != nil {
		h.FillRowsCardArt(r, views)
	}
	h.fillStreamingRows(r, views)
	return views, nil
}

// exploreRow answers GET /api/v1/explore/row?more=<word>&page=n: page n
// (from 2) of one row, read from the lists its word names (a head, tail,
// endless or hub row's more). The cards pass the same Kid, hidden and
// never-add filters as the row did; more says another page may follow.
func (h *Handlers) exploreRow(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	n, err := strconv.Atoi(q.Get("page"))
	if err != nil || n < 2 || n > engine.MaxRowPage {
		webserver.Fail(w, http.StatusBadRequest, "page must be 1 to 500")
		return
	}
	specs, err := engine.DecodeSpecs(q.Get("more"))
	if err != nil {
		webserver.Fail(w, http.StatusBadRequest, "more must be the word a row was sent with")
		return
	}
	if h.KidLimited(w, r) {
		return
	}
	rows, failed := h.readSpecs(r.Context(), []engine.Row{{ID: "more", Type: "browse", Specs: specs}}, n)
	if failed {
		webserver.Fail(w, http.StatusServiceUnavailable, "The movie and show details did not answer; try again shortly")
		return
	}
	got := len(rows[0].Items)
	views, err := h.rowViews(r, rows)
	if err != nil {
		webserver.Internal(w, "api", err)
		return
	}
	items := []ItemView{}
	if len(views) == 1 {
		items = views[0].Items
	}
	h.Caching(w, r, "private, max-age=300")
	webserver.WriteJSON(w, http.StatusOK, map[string]any{"page": n, "more": got > 0 && n < engine.MaxRowPage, "items": items})
}

// endlessTail is tail page n past the pool's own pages: rows of the endless
// tail, read now. A row too thin after the filters is left out, so a page
// may hold fewer rows; more stays true until the tail ends, and turns false
// when every list failed or came back empty, so the page stops asking.
func (h *Handlers) endlessTail(r *http.Request, member int64, sw string, pg engine.Page, n int) ([]ExploreRowView, bool, error) {
	cat := h.endlessCatalog(r.Context(), r, member, sw, pg)
	slice, more := engine.EndlessPage(cat, n)
	if len(slice) == 0 {
		return []ExploreRowView{}, false, nil
	}
	rows, failed := h.readSpecs(r.Context(), slice, 1)
	got := 0
	for _, row := range rows {
		got += len(row.Items)
	}
	// Every list failed, or none had anything (lookups off with nothing
	// kept): the page stops asking instead of running through every page.
	if failed || got == 0 {
		return []ExploreRowView{}, false, nil
	}
	views, err := h.ExploreViews(r.Context(), r, rows)
	return views, more, err
}
