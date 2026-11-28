package discover

import (
	"testing"

	engine "github.com/HolyArrs/holyarrs/app/backend/internal/suggest"
)

func TestHubBrowseScopeNarrowsCategory(t *testing.T) {
	s := hubBrowseScope("kdrama")
	if s.Lang != "ko" {
		t.Fatalf("kdrama scope language = %q, want ko", s.Lang)
	}
	q := s.apply(engine.Request{List: "discover", Kind: "show", Keywords: "lawyer|court"})
	if q.Lang != "ko" || q.Keywords != "lawyer|court" {
		t.Fatalf("legal under kdrama = %+v", q)
	}
	anime := hubBrowseScope("anime").apply(engine.Request{Genres: "80"})
	if anime.Genres != "80,16" || anime.Lang != "ja" {
		t.Fatalf("crime under anime = %+v", anime)
	}
	anyOf := hubBrowseScope("anime").apply(engine.Request{Genres: "80|18"})
	if anyOf.Genres != "80|18" {
		t.Fatalf("an any-of category keeps its genres, got %q", anyOf.Genres)
	}
	if got := (browseScope{Page: 3}).apply(engine.Request{}); got.Page != 3 {
		t.Fatalf("page = %d, want 3", got.Page)
	}
	if !hubBrowseScope("no-such-hub").zero() {
		t.Fatal("an unknown hub must not narrow anything")
	}
}
