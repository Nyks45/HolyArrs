package suggest

import (
	"strings"
	"testing"
	"time"
)

func TestEndlessRowsAreManyStableAndSingleKind(t *testing.T) {
	now := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	in := EndlessIn{Switch: "all", Region: "GB", Services: "337", Now: now, Skip: map[string]bool{"Comedy": true}, AllFilters: true}
	rows := EndlessRows(in)
	if len(rows) < 200 {
		t.Fatalf("only %d endless rows", len(rows))
	}
	again := EndlessRows(in)
	ids := map[string]bool{}
	for i, r := range rows {
		if r.ID != again[i].ID {
			t.Fatalf("row %d moved: %s then %s", i, r.ID, again[i].ID)
		}
		if ids[r.ID] {
			t.Fatalf("row %s twice", r.ID)
		}
		ids[r.ID] = true
		if len(r.Specs) != 1 || r.Title == "" || MinItems(r.Type) == 0 || r.Layer == "" {
			t.Fatalf("row %+v", r)
		}
		if q := r.Specs[0]; !strings.HasPrefix(r.ID, "more:"+q.Kind+":") || !specOK(q) {
			t.Fatalf("row %s list %+v", r.ID, q)
		}
	}
	if ids["more:movie:genre:35"] || ids["more:show:genre:35"] {
		t.Fatal("a genre the page already shows came back")
	}
	if !ids["more:movie:genre:28"] || !ids["more:show:network:213"] || !ids["more:movie:studio:3"] || !ids["more:show:lang:ko"] {
		t.Fatal("a genre, network, studio or language row is missing")
	}
	// Without network and company filters (the HolyArrs metadata server) those rows are left out.
	in.AllFilters = false
	for _, r := range EndlessRows(in) {
		if q := r.Specs[0]; q.Networks != "" || q.Companies != "" || q.Country != "" {
			t.Fatalf("row %s needs a filter the lists refuse", r.ID)
		}
	}
	// The picked service comes first among the watch service rows.
	for _, r := range rows {
		if strings.Contains(r.ID, ":on:") {
			if r.ID != "more:movie:on:337" {
				t.Fatalf("first service row is %s", r.ID)
			}
			break
		}
	}
	// Two rows of one kind of list seldom meet: the first rows mix their sorts.
	types := map[string]bool{}
	for _, r := range rows[:12] {
		types[r.Type] = true
	}
	if len(types) < 4 {
		t.Fatalf("the first rows hold %d sorts", len(types))
	}
}

func TestEndlessRowsKidAndLater(t *testing.T) {
	later := []Row{{ID: TypeBecauseWatched + ":movie:9", Type: TypeBecauseWatched, Title: "Because you watched X", Specs: []Request{{List: "similar", Kind: "movie", Ref: "9"}}},
		{ID: TypeBecauseWatched + ":show:4", Type: TypeBecauseWatched, Specs: []Request{{List: "similar", Kind: "show", Ref: "4"}}}}
	rows := EndlessRows(EndlessIn{Switch: "movies", Kid: true, Now: time.Now(), Later: later})
	if rows[0].ID != later[0].ID {
		t.Fatalf("first row %s, want the later because row", rows[0].ID)
	}
	for _, r := range rows {
		q := r.Specs[0]
		if q.Kind != "movie" {
			t.Fatalf("a Movies tab row of kind %s: %s", q.Kind, r.ID)
		}
		if !strings.Contains(q.Without, KidWithout) || q.Cert != "PG" {
			t.Fatalf("kid row %s without the Kid limits: %+v", r.ID, q)
		}
		if r.Type == TypeLanguage || r.Type == TypeProvider {
			t.Fatalf("kid row %s", r.ID)
		}
	}
	page, more := EndlessPage(rows, 0)
	if len(page) != TailPage || !more {
		t.Fatalf("page 0: %d rows, more %v", len(page), more)
	}
	if page, more = EndlessPage(rows, 10_000); len(page) != 0 || more {
		t.Fatal("a page past the end")
	}
}

func TestSpecsRoundTripAndRefuseOthers(t *testing.T) {
	specs := []Request{{List: "discover", Kind: "show", Genres: "16,10759", Lang: "ja", Without: "16|10762", MinVotes: 10, Scope: ScopeShared, Page: 3},
		{List: "similar", Kind: "movie", Ref: "550"}}
	got, err := DecodeSpecs(EncodeSpecs(specs))
	if err != nil || len(got) != 2 || got[0].Genres != "16,10759" || got[0].Page != 0 || got[1].Ref != "550" || got[1].Scope != ScopeShared {
		t.Fatalf("%+v %v", got, err)
	}
	for _, bad := range [][]Request{{{List: "person", Kind: "movie", Ref: "x"}}, {{List: "similar", Kind: "movie", Ref: "../1"}},
		{{List: "discover", Kind: "music"}}, {{List: "discover", Kind: "movie", From: "soon"}}, {{List: "discover", Kind: "movie", Lang: "a\"b<"}}} {
		if _, err := DecodeSpecs(EncodeSpecs(bad)); err == nil {
			t.Fatalf("took %+v", bad)
		}
	}
	if _, err := DecodeSpecs("not base64!"); err == nil {
		t.Fatal("took a broken word")
	}
}

func TestRequestKeyKeepsFirstPagesAndSplitsLater(t *testing.T) {
	q := Request{List: "discover", Kind: "movie", Genre: "Drama"}
	base := q.String()
	q.Page = 1
	if q.String() != base {
		t.Fatal("page 1 changed the key")
	}
	q.Page = 2
	if q.String() == base {
		t.Fatal("page 2 shares page 1's key")
	}
	n := Request{List: "discover", Kind: "show", Networks: "213"}
	if n.String() == (Request{List: "discover", Kind: "show"}).String() {
		t.Fatal("a network list shares the plain list's key")
	}
}
