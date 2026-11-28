package discover

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/webserver"
	"github.com/HolyArrs/holyarrs/app/backend/internal/metadata"
	engine "github.com/HolyArrs/holyarrs/app/backend/internal/suggest"
)

func TestBrowseLoadsMovieAndShowListsConcurrently(t *testing.T) {
	ready := make(chan struct{})
	var started atomic.Int32
	p := &fakeDiscoverProvider{
		Static: &metadata.Static{},
		listFn: func(kind, _ string, _ url.Values) ([]metadata.DiscoverItem, error) {
			if started.Add(1) == 2 {
				close(ready)
			}
			select {
			case <-ready:
			case <-time.After(3 * time.Second):
				return nil, errors.New("the other catalog lookup did not start")
			}
			return []metadata.DiscoverItem{{TMDBID: "123", Kind: kind, Name: kind}}, nil
		},
	}
	_, h, _ := subcategoryRig(t, p)
	cat, _ := engine.CategoryByID("drama")
	ctx := context.Background()
	items, err := h.browseItems(ctx, h.LiveExplore(ctx), cat, "all", false, "")
	if err != nil || len(items) != 2 {
		t.Fatalf("browse got %v, %v", items, err)
	}
}

type fakeDiscoverProvider struct {
	*metadata.Static
	details      map[string]metadata.Details
	detailsCalls atomic.Int32
	listFn       func(kind, path string, q url.Values) ([]metadata.DiscoverItem, error)
}

func (f *fakeDiscoverProvider) Discover(_ context.Context, _, _, _, _ string) ([]metadata.DiscoverItem, error) {
	return nil, nil
}

func (f *fakeDiscoverProvider) Details(_ context.Context, kind, id string) (metadata.Details, error) {
	f.detailsCalls.Add(1)
	if d, ok := f.details[kind+":"+id]; ok {
		return d, nil
	}
	return metadata.Details{}, metadata.ErrNotFound
}

func (f *fakeDiscoverProvider) ExploreList(_ context.Context, kind, path string, q url.Values) ([]metadata.DiscoverItem, error) {
	if f.listFn != nil {
		return f.listFn(kind, path, q)
	}
	return nil, nil
}

func (f *fakeDiscoverProvider) PersonCredits(_ context.Context, _ string) ([]metadata.DiscoverItem, error) {
	return nil, nil
}

func (f *fakeDiscoverProvider) GenreID(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}

func (f *fakeDiscoverProvider) KeywordID(_ context.Context, _ string) (int64, error) {
	return 42, nil
}

func subcategoryRig(t *testing.T, p metadata.Provider) (*httptest.Server, *Handlers, *database.Store) {
	t.Helper()
	s, err := database.Open(t.TempDir(), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Write(context.Background(), func(tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO users(id, created_at, created_by, updated_at, updated_by, name, role, password_hash) VALUES (1, 't', '{}', 't', '{}', 'owner', 'owner', 'x')`,
			`INSERT INTO users(id, created_at, created_by, updated_at, updated_by, name, role, max_rating, password_hash) VALUES (3, 't', '{}', 't', '{}', 'kid', 'member', 'PG', 'x')`,
		} {
			if _, err := tx.Exec(q); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := testHandlers(s, p)
	h.CardFresh = func(context.Context, metadata.DiscoverItem, int64) (*engine.Fresh, error) { return nil, nil }
	h.ViewSignals = NewViewSignalStore("")
	h.Explore.ViewSignals = h.ViewSignals
	m := http.NewServeMux()
	h.Routes(m)
	srv := httptest.NewServer(m)
	t.Cleanup(srv.Close)
	return srv, h, s
}

func TestSubcategoriesAndOpenedRoutes(t *testing.T) {
	fp := &fakeDiscoverProvider{
		Static: &metadata.Static{},
		listFn: func(kind, path string, _ url.Values) ([]metadata.DiscoverItem, error) {
			if strings.Contains(path, "/similar") {
				return []metadata.DiscoverItem{
					{TMDBID: "901", Kind: kind, Name: "Similar A", Genres: []string{"Drama"}},
					{TMDBID: "902", Kind: kind, Name: "Similar B", Genres: []string{"Crime"}},
				}, nil
			}
			return nil, nil
		},
	}
	srv, h, _ := subcategoryRig(t, fp)

	res, err := http.Get(srv.URL + "/api/v1/explore/subcategories?q=legal")
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("subcategories q=legal: err=%v status=%v", err, res.StatusCode)
	}
	var subResp struct {
		Type  string                   `json:"type"`
		Items []engine.CategorySummary `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&subResp)
	_ = res.Body.Close()
	if len(subResp.Items) == 0 || subResp.Items[0].ID != "legal" {
		t.Fatalf("expected legal subcategory first, got %+v", subResp.Items)
	}

	res2, err := http.Get(srv.URL + "/api/v1/explore/subcategories?type=anime")
	if err != nil || res2.StatusCode != 200 {
		t.Fatalf("subcategories type=anime: err=%v status=%v", err, res2.StatusCode)
	}
	var animeResp struct {
		Type  string                   `json:"type"`
		Items []engine.CategorySummary `json:"items"`
	}
	_ = json.NewDecoder(res2.Body).Decode(&animeResp)
	_ = res2.Body.Close()
	if animeResp.Type != "anime" || len(animeResp.Items) == 0 || animeResp.Items[0].ID != "anime" {
		t.Fatalf("unexpected anime subcategories response: %+v", animeResp)
	}

	badRes, err := http.Get(srv.URL + "/api/v1/explore/subcategories?type=bogus")
	if err != nil || badRes.StatusCode != 400 {
		t.Fatalf("expected 400 for invalid type, got %v %v", err, badRes.StatusCode)
	}
	_ = badRes.Body.Close()

	// Record opened title.
	postRes, err := http.Post(srv.URL+"/api/v1/explore/opened", "application/json", strings.NewReader(`{"kind":"show","tmdb_id":"256711","name":"The Rainmaker","year":2025}`))
	if err != nil || postRes.StatusCode != 200 {
		t.Fatalf("POST /explore/opened failed: err=%v status=%v", err, postRes.StatusCode)
	}
	_ = postRes.Body.Close()
	if got := h.ViewSignals.List(1); len(got) != 1 || got[0].TMDBID != "256711" {
		t.Fatalf("expected recorded view signal, got %+v", got)
	}

	badPost, err := http.Post(srv.URL+"/api/v1/explore/opened", "application/json", strings.NewReader(`{"kind":"music","tmdb_id":"1"}`))
	if err != nil || badPost.StatusCode != 400 {
		t.Fatalf("expected 400 for invalid kind, got %v %v", err, badPost.StatusCode)
	}
	_ = badPost.Body.Close()

	// enrichSimilar tops up thin TMDB similar lists up to 20 items while deduping self.
	existing := []metadata.DiscoverItem{{TMDBID: "901", Kind: "show", Name: "Similar A"}}
	if got := h.enrichSimilar(context.Background(), "show", "256711", existing, true); len(got) != 1 {
		t.Fatalf("a kept page without a cached list should use its own items, got %+v", got)
	}
	sim := h.enrichSimilar(context.Background(), "show", "256711", existing, false)
	if len(sim) != 2 || sim[1].TMDBID != "902" {
		t.Fatalf("expected deduplicated enriched similar items, got %+v", sim)
	}
	if got := h.enrichSimilar(context.Background(), "show", "256711", existing, true); len(got) != 2 {
		t.Fatalf("a kept page should use cached recommendations, got %+v", got)
	}
}

func TestExploreInputIncludesWatchAndViewSignalsWithKidGuard(t *testing.T) {
	fp := &fakeDiscoverProvider{
		Static: &metadata.Static{},
		details: map[string]metadata.Details{
			"show:256711": {
				DiscoverItem:     metadata.DiscoverItem{TMDBID: "256711", Kind: "show", Name: "The Rainmaker", Year: 2025, Genres: []string{"Drama", "Crime"}},
				Rating:           "TV-14",
				Keywords:         []string{"lawyer", "legal drama"},
				OriginalLanguage: "en",
				People:           []metadata.DetailPerson{{Name: "Milo Callaghan", Role: "actor"}},
			},
			"movie:555": {
				DiscoverItem:     metadata.DiscoverItem{TMDBID: "555", Kind: "movie", Name: "Adult Only", Year: 2024, Genres: []string{"Thriller"}},
				Rating:           "R",
				Keywords:         []string{"revenge"},
				OriginalLanguage: "en",
			},
		},
	}
	_, h, _ := subcategoryRig(t, fp)
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	h.Explore.Clock = func() time.Time { return now }
	h.Explore.WatchSignals = func(userID int64) []WatchSignal {
		return []WatchSignal{
			{TMDBID: "256711", Kind: "show", Title: "The Rainmaker", Year: 2025, Episodes: 3, UpdatedAt: now.Add(-time.Hour)},
			{TMDBID: "555", Kind: "movie", Title: "Adult Only", Year: 2024, Completed: true, UpdatedAt: now.Add(-2 * time.Hour)},
		}
	}

	adult, err := h.Explore.LoadMember(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	h.ServiceTopCache = &ServiceTopCache{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/explore/head?type=all", nil)
	cold, building, err := h.ExplorePageFor(req.Context(), req, adult.ID, "all")
	if err != nil || !building {
		t.Fatalf("a cold head should answer while its background pool builds, got building=%t, err=%v", building, err)
	}
	if fp.detailsCalls.Load() != 0 {
		t.Fatalf("building the cold head made %d provider calls", fp.detailsCalls.Load())
	}
	if cold.Seeds != 2 {
		t.Fatalf("expected two local watch signals on the cold head, got %d", cold.Seeds)
	}
	adultIn, err := h.Explore.Input(context.Background(), adult, "all", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(adultIn.Seeds) != 2 || adultIn.Seeds[0].Source != "watched" || len(adultIn.Seeds[0].Keywords) == 0 {
		t.Fatalf("expected watched enriched seeds for adult, got %+v", adultIn.Seeds)
	}
	if adultIn.Owned["show:256711"] {
		t.Fatalf("watched title outside Library must not be marked Owned")
	}
	if adultIn.OwnedPeople["Milo Callaghan"] == 0 {
		t.Fatalf("expected actor taste weight from watched title")
	}

	kid, err := h.Explore.LoadMember(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	kidIn, err := h.Explore.Input(context.Background(), kid, "all", now)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range kidIn.Seeds {
		if s.TMDBID == "555" || s.TMDBID == "256711" {
			t.Fatalf("expected TV-14/R watched seeds to be excluded under PG kid limit, got %+v", kidIn.Seeds)
		}
	}
	_ = webserver.NeedSignedIn
}
