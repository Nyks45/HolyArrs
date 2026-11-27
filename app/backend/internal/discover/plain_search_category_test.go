package discover

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/HolyArrs/holyarrs/app/backend/internal/metadata"
	engine "github.com/HolyArrs/holyarrs/app/backend/internal/suggest"
)

func TestPlainSearchCategoryMatchAndLeadItems(t *testing.T) {
	fp := &fakeDiscoverProvider{
		Static: &metadata.Static{},
		listFn: func(kind, path string, q url.Values) ([]metadata.DiscoverItem, error) {
			return []metadata.DiscoverItem{
				{TMDBID: "101", Kind: kind, Name: "Legal Classic", Year: 1995, Genres: []string{"Drama", "Crime"}, Poster: "/p1.jpg"},
				{TMDBID: "102", Kind: kind, Name: "Modern Firm", Year: 2025, Genres: []string{"Drama", "Crime"}, Poster: "/p2.jpg"},
			}, nil
		},
	}
	srv, _, _ := subcategoryRig(t, fp)

	res, err := http.Get(srv.URL + "/api/v1/search?q=legal")
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("search?q=legal err=%v status=%v", err, res.StatusCode)
	}
	var payload struct {
		Categories    []engine.CategorySummary `json:"categories"`
		CategoryID    string                   `json:"category_id"`
		CategoryName  string                   `json:"category_name"`
		CategoryItems []ItemView               `json:"category_items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&payload)
	_ = res.Body.Close()
	if payload.CategoryID != "legal" || payload.CategoryName != "Legal" {
		t.Fatalf("expected category legal/Legal, got id=%q name=%q cats=%+v", payload.CategoryID, payload.CategoryName, payload.Categories)
	}
	if len(payload.CategoryItems) < 2 {
		t.Fatalf("expected category items for legal query, got %+v", payload.CategoryItems)
	}

	// Year filter inside category search narrows items.
	resYr, err := http.Get(srv.URL + "/api/v1/search?q=" + url.QueryEscape("legal shows from 2025"))
	if err != nil || resYr.StatusCode != 200 {
		t.Fatalf("search year filter err=%v status=%v", err, resYr.StatusCode)
	}
	var payloadYr struct {
		CategoryID    string     `json:"category_id"`
		CategoryItems []ItemView `json:"category_items"`
	}
	_ = json.NewDecoder(resYr.Body).Decode(&payloadYr)
	_ = resYr.Body.Close()
	if payloadYr.CategoryID != "legal" {
		t.Fatalf("expected legal category id, got %q", payloadYr.CategoryID)
	}
	for _, it := range payloadYr.CategoryItems {
		if it.Year < 2025 {
			t.Fatalf("expected 2025+ item after year filter, got %+v", it)
		}
	}

	// Non-category query does not claim category_id.
	resPlain, err := http.Get(srv.URL + "/api/v1/search?q=inception")
	if err != nil || resPlain.StatusCode != 200 {
		t.Fatalf("search inception err=%v status=%v", err, resPlain.StatusCode)
	}
	var payloadPlain struct {
		CategoryID string `json:"category_id"`
	}
	_ = json.NewDecoder(resPlain.Body).Decode(&payloadPlain)
	_ = resPlain.Body.Close()
	if payloadPlain.CategoryID != "" {
		t.Fatalf("expected empty category_id for title search, got %q", payloadPlain.CategoryID)
	}
}
