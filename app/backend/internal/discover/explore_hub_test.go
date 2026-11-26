package discover

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/metadata"
)

type stubHubMetadata struct{}

func (s *stubHubMetadata) Search(context.Context, string, int) ([]metadata.Movie, error) {
	return nil, nil
}
func (s *stubHubMetadata) Movie(context.Context, string) (metadata.Movie, error) {
	return metadata.Movie{}, nil
}
func (s *stubHubMetadata) ExploreList(_ context.Context, kind, _ string, q url.Values) ([]metadata.DiscoverItem, error) {
	lang := q.Get("with_original_language")
	genres := q.Get("with_genres")
	name := "Sample " + kind + " (" + lang + " " + genres + ")"
	return []metadata.DiscoverItem{
		{
			TMDBID:   "101",
			Kind:     kind,
			Name:     name,
			Year:     2024,
			Poster:   "/poster.jpg",
			Backdrop: "/backdrop.jpg",
			Genres:   []string{"Animation"},
		},
	}, nil
}
func (s *stubHubMetadata) PersonCredits(context.Context, string) ([]metadata.DiscoverItem, error) {
	return nil, nil
}
func (s *stubHubMetadata) GenreID(context.Context, string, string) (int64, error) {
	return 0, nil
}

func TestExploreHubEndpoints(t *testing.T) {
	ClearExploreHubCache()
	h := &Handlers{
		Deps: Deps{
			Metadata: &stubHubMetadata{},
			KidMax:   func(*http.Request) string { return "" },
			UserID:   func(*http.Request) int64 { return 1 },
		},
	}

	mux := http.NewServeMux()
	h.Routes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/explore/hubs", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("hubs status = %d", rec.Code)
	}
	var body struct {
		Hubs []ExploreHubInfo `json:"hubs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Hubs) < 7 {
		t.Fatalf("expected at least 7 hubs, got %d", len(body.Hubs))
	}
	if body.Hubs[0].ID != "anime" || body.Hubs[1].ID != "kdrama" {
		t.Fatalf("unexpected hub order: %+v", body.Hubs[:2])
	}

	// Verify row builders populate dedicated Anime, K-Drama, and Turkish hubs.
	for _, id := range []string{"anime", "kdrama", "turkish", "asian-drama", "animation", "documentary"} {
		def, ok := findExploreHub(id)
		if !ok {
			t.Fatalf("missing hub %s", id)
		}
		rows := h.loadHubRows(context.Background(), def, false, time.Now().UTC())
		if len(rows) == 0 {
			t.Fatalf("hub %s returned 0 rows", id)
		}
	}

	// Unknown hub returns 404.
	reqBad := httptest.NewRequest(http.MethodGet, "/api/v1/explore/hub?hub=not-real", nil)
	recBad := httptest.NewRecorder()
	mux.ServeHTTP(recBad, reqBad)
	if recBad.Code != http.StatusNotFound {
		t.Fatalf("unknown hub status = %d, want 404", recBad.Code)
	}
}
