package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestHistoryStoreBatchUpdateAndCap(t *testing.T) {
	cfgDir := t.TempDir()
	hs := NewHistoryStore(cfgDir)

	upserts := make([]WatchEntry, 0, 10)
	for ep := 1; ep <= 10; ep++ {
		upserts = append(upserts, WatchEntry{
			Kind:        "show",
			TMDB:        "1396",
			Title:       "Breaking Bad",
			Season:      1,
			Episode:     ep,
			EpisodeName: "Ep " + strconv.Itoa(ep),
			Completed:   true,
		})
	}
	updated, err := hs.BatchUpdate(7, upserts, nil)
	if err != nil {
		t.Fatalf("BatchUpdate upsert error: %v", err)
	}
	if len(updated) != 10 {
		t.Fatalf("expected 10 updated entries, got %d", len(updated))
	}
	for _, u := range updated {
		if !u.Completed || u.ProgressPct != 100 {
			t.Fatalf("expected completed entry at 100%% progress, got %+v", u)
		}
	}

	reloaded := NewHistoryStore(cfgDir)
	latest, eps := reloaded.ForTitle(7, "show", "1396")
	if latest == nil || len(eps) != 10 {
		t.Fatalf("expected 10 persisted episodes on reload, got latest=%v len=%d", latest, len(eps))
	}

	// Delete season 1 episodes 1..5 in a single batch.
	delKeys := make([]string, 0, 5)
	for ep := 1; ep <= 5; ep++ {
		delKeys = append(delKeys, WatchKey("show", "1396", 1, ep))
	}
	if _, err := reloaded.BatchUpdate(7, nil, delKeys); err != nil {
		t.Fatalf("BatchUpdate delete error: %v", err)
	}
	afterReload := NewHistoryStore(cfgDir)
	_, remaining := afterReload.ForTitle(7, "show", "1396")
	if len(remaining) != 5 {
		t.Fatalf("expected 5 remaining episodes after batch delete, got %d", len(remaining))
	}

	// Verify per-user cap accepts > 500 entries (up to 2000).
	bulk := make([]WatchEntry, 0, 620)
	for ep := 1; ep <= 620; ep++ {
		bulk = append(bulk, WatchEntry{
			Kind:      "show",
			TMDB:      "99999",
			Title:     "Long Show",
			Season:    1,
			Episode:   ep,
			Completed: true,
		})
	}
	if _, err := afterReload.BatchUpdate(8, bulk, nil); err != nil {
		t.Fatalf("BatchUpdate >500 entries failed: %v", err)
	}
	_, user8Eps := afterReload.ForTitle(8, "show", "99999")
	if len(user8Eps) != 620 {
		t.Fatalf("expected all 620 entries retained under 2000 cap, got %d", len(user8Eps))
	}
}

func TestBatchHistoryHTTPHandler(t *testing.T) {
	mgr := NewManager(nil, nil, t.TempDir(), nil)
	h := &Handlers{Mgr: mgr}
	mux := http.NewServeMux()
	h.Routes(mux)

	payload := map[string]any{
		"upserts": []map[string]any{
			{"kind": "show", "tmdb": "2500", "title": "Test Show", "season": 2, "episode": 1, "completed": true},
			{"kind": "show", "tmdb": "2500", "title": "Test Show", "season": 2, "episode": 2, "completed": true},
		},
	}
	raw, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/stream/history/batch", bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from batch endpoint, got %d: %s", rec.Code, rec.Body.String())
	}

	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/api/v1/stream/history/title/show/2500", nil))
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from titleHistory, got %d", getRec.Code)
	}
	var body struct {
		Episodes []WatchEntry `json:"episodes"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Episodes) != 2 {
		t.Fatalf("expected 2 batch-saved episodes, got %d", len(body.Episodes))
	}
}

func TestAddonBrowseCatalogsIsolatesAnimeFromMoviesAndShows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/cinemeta/catalog/movie/top.json":
			_, _ = w.Write([]byte(`{"metas":[{"id":"tt0111161","type":"movie","name":"The Shawshank Redemption","releaseInfo":"1994"}]}`))
		case "/cinemeta/catalog/series/top.json":
			_, _ = w.Write([]byte(`{"metas":[{"id":"tt0903747","type":"series","name":"Breaking Bad","releaseInfo":"2008"}]}`))
		case "/kitsu/catalog/anime/kitsu-anime-trending.json":
			_, _ = w.Write([]byte(`{"metas":[{"id":"kitsu:1","type":"anime","name":"Cowboy Bebop","releaseInfo":"1998"}]}`))
		case "/kitsu/catalog/series/kitsu-anime-last-videos.json":
			_, _ = w.Write([]byte(`{"metas":[{"id":"kitsu:2","type":"series","name":"Attack on Titan","releaseInfo":"2013"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	store := NewAddonStore(t.TempDir())
	store.addons = []StremioAddon{
		{
			ID:      "com.linvo.cinemeta",
			Name:    "Cinemeta",
			BaseURL: srv.URL + "/cinemeta",
			Enabled: true,
			Types:   []string{"movie", "series"},
			Catalogs: []AddonCatalogDef{
				{Type: "movie", ID: "top", Name: "Popular Movies"},
				{Type: "series", ID: "top", Name: "Popular Shows"},
			},
		},
		{
			ID:      "community.anime.kitsu",
			Name:    "Anime Kitsu",
			BaseURL: srv.URL + "/kitsu",
			Enabled: true,
			Types:   []string{"anime", "movie", "series"},
			Catalogs: []AddonCatalogDef{
				{Type: "anime", ID: "kitsu-anime-trending", Name: "Kitsu Trending"},
				{Type: "series", ID: "kitsu-anime-last-videos", Name: "Last videos"},
			},
		},
	}

	checkIDs := func(kind string, wantAddons []string) {
		t.Helper()
		got := store.BrowseCatalogs(context.Background(), kind)
		if len(got) != len(wantAddons) {
			t.Fatalf("kind=%q: expected %d shelves (%v), got %d (%+v)", kind, len(wantAddons), wantAddons, len(got), got)
		}
		for _, sh := range got {
			found := false
			for _, w := range wantAddons {
				if sh.AddonID == w || sh.CatalogID == w {
					found = true
				}
			}
			if !found {
				t.Fatalf("kind=%q: unexpected shelf %s/%s", kind, sh.AddonID, sh.CatalogID)
			}
		}
	}

	checkIDs("movie", []string{"com.linvo.cinemeta"})
	checkIDs("movies", []string{"com.linvo.cinemeta"})
	checkIDs("show", []string{"com.linvo.cinemeta"})
	checkIDs("shows", []string{"com.linvo.cinemeta"})
	checkIDs("", []string{"com.linvo.cinemeta", "com.linvo.cinemeta"})

	animeShelves := store.BrowseCatalogs(context.Background(), "anime")
	if len(animeShelves) != 2 {
		t.Fatalf("kind=anime: expected 2 anime shelves, got %d (%+v)", len(animeShelves), animeShelves)
	}
	for _, sh := range animeShelves {
		if sh.AddonID != "community.anime.kitsu" {
			t.Fatalf("kind=anime: expected only Anime Kitsu shelf, got %s", sh.AddonID)
		}
	}
}
