package suggest

import (
	"slices"
	"testing"
	"time"
)

func TestMatchCategories_AliasesAndQueries(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		query, sw string
		kid       bool
		wantFirst string
		wantLead  bool
	}{
		{"legal", "all", false, "legal", true},
		{"courtroom", "all", false, "courtroom", true},
		{"lawyer", "movies", false, "legal", true},
		{"medical", "all", false, "medical", true},
		{"heist", "movies", false, "heists", true},
		{"isekai", "all", false, "isekai", true},
		{"korean thriller", "shows", false, "kdrama-thriller", true},
		{"Se7en", "all", false, "", false},
	}
	for _, tc := range cases {
		got := MatchCategories(tc.query, tc.sw, tc.kid, now)
		lead := IsCategoryLeadQuery(tc.query, tc.sw, tc.kid, now)
		if lead != tc.wantLead {
			t.Errorf("%q lead=%v, want %v", tc.query, lead, tc.wantLead)
		}
		if tc.wantFirst == "" {
			if len(got) != 0 {
				t.Errorf("%q got %v, want empty", tc.query, got)
			}
			continue
		}
		if len(got) == 0 || got[0].ID != tc.wantFirst {
			t.Errorf("%q got %+v, want first=%q", tc.query, got, tc.wantFirst)
		}
	}

	kidRes := MatchCategories("legal", "all", true, now)
	if len(kidRes) != 0 {
		t.Fatalf("kid saw adult category legal: %+v", kidRes)
	}
}

func TestSubcategoriesFor_AnimeAndParentGenre(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	animeSubs := SubcategoriesFor("anime", "", "", false, now)
	if len(animeSubs) < 5 || animeSubs[0].ID != "anime" {
		t.Fatalf("anime subcategories %v", animeSubs)
	}
	for _, sub := range animeSubs {
		if animeSubSet[sub.ID] == 0 {
			t.Errorf("anime tab includes %s", sub.ID)
		}
	}
	for _, sw := range []string{"movies", "shows"} {
		for _, sub := range SubcategoriesFor(sw, "", "", false, now) {
			if sub.ID == "anime" || sub.ID == "shonen" || sub.ID == "isekai" {
				t.Errorf("%s tab includes %s", sw, sub.ID)
			}
		}
	}
	sliceOfLife, _ := CategoryByID("slice-of-life")
	got := sliceOfLife.Requests("anime", false)
	if len(got) != 4 {
		t.Fatalf("expected both animation genre combinations for movies and shows, got %+v", got)
	}
	for _, req := range got {
		if req.Lang != "ja" || (req.Genres != "16,35" && req.Genres != "16,18") {
			t.Errorf("unscoped anime request: %+v", req)
		}
	}
	crimeSubs := SubcategoriesFor("all", "Crime", "", false, now)
	ids := make([]string, 0, len(crimeSubs))
	for _, s := range crimeSubs[:min(12, len(crimeSubs))] {
		ids = append(ids, s.ID)
	}
	if !slices.Contains(ids, "legal") || !slices.Contains(ids, "heists") {
		t.Fatalf("crime top subcategories %v missing legal/heists", ids)
	}
}

func TestMatchTasteCategories_KeywordsAndWatchedSeeds(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	act := []ActivitySeed{
		{
			TMDBID:   "256711",
			Kind:     "show",
			Name:     "The Rainmaker",
			Year:     2025,
			Genres:   []string{"Drama", "Crime"},
			Keywords: []string{"legal drama", "lawyer", "courtroom"},
			KindTag:  "watched",
			Episodes: 4,
			At:       now.Add(-2 * time.Hour),
		},
		{
			TMDBID:  "999001",
			Kind:    "movie",
			Name:    "Peeked",
			Year:    2024,
			Genres:  []string{"Mystery"},
			KindTag: "opened",
			Opens:   2,
			At:      now.Add(-4 * time.Hour),
		},
	}
	seeds := SeedsWithActivity(1, false, nil, nil, act, now)
	if len(seeds) != 2 || seeds[0].Source != "watched" || seeds[1].Source != "opened" {
		t.Fatalf("unexpected activity seeds: %+v", seeds)
	}
	prof := NewProfile(seeds)
	cats := MatchTasteCategories(prof.KeywordWeights(), prof.GenreWeights(), "all", false, now, 3)
	if len(cats) == 0 || (cats[0].ID != "legal" && cats[0].ID != "courtroom") {
		t.Fatalf("expected legal/courtroom top taste category, got %+v", cats)
	}
}
