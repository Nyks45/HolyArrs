package discover

import (
	"testing"

	"github.com/HolyArrs/holyarrs/app/backend/internal/metadata"
)

func TestDetailsTextShowsLogoInReadersLanguage(t *testing.T) {
	d := metadata.Details{Logo: "en.png", Logos: map[string]string{"fr": "fr.png"}}
	detailsText(&d, metadata.TitleText{}, "fr")
	if d.Logo != "fr.png" || d.Logos != nil {
		t.Fatalf("fr: logo %q logos %v", d.Logo, d.Logos)
	}
	d = metadata.Details{Logo: "en.png", Logos: map[string]string{"fr": "fr.png"}}
	detailsText(&d, metadata.TitleText{}, "de")
	if d.Logo != "en.png" {
		t.Fatalf("de falls back to English, got %q", d.Logo)
	}
}

func TestDetailsTextFallsBackToOriginalLogo(t *testing.T) {
	d := metadata.Details{Logo: "en.png", Logos: map[string]string{"pt-br": "br.png", metadata.OrigLogo: "ja.png"}}
	detailsText(&d, metadata.TitleText{}, "pt-pt")
	if d.Logo != "ja.png" {
		t.Fatalf("pt-pt: got %q", d.Logo)
	}
	d = metadata.Details{Logo: "en.png", Logos: map[string]string{metadata.OrigLogo: "ja.png"}}
	detailsText(&d, metadata.TitleText{}, "en")
	if d.Logo != "en.png" {
		t.Fatalf("en reader keeps English, got %q", d.Logo)
	}
}

func TestDetailsTTLReadsPreLogoPagesOnce(t *testing.T) {
	vid := []metadata.DetailVideo{{Key: "k"}}
	if got := DetailsTTL(Entry{Details: &metadata.Details{Videos: vid}}); got != 0 {
		t.Fatalf("kept before logos: ttl %v", got)
	}
	if got := DetailsTTL(Entry{Details: &metadata.Details{Videos: vid, LogosRead: true}}); got != TTL["details"] {
		t.Fatalf("read with logos: ttl %v", got)
	}
}

func TestDetailsTTLRefreshesUncheckedSingleSeasonAnimeAndClearsInternalFlags(t *testing.T) {
	vid := []metadata.DetailVideo{{Key: "k"}}
	d := metadata.Details{
		DiscoverItem:       metadata.DiscoverItem{Kind: "show", Genres: []string{"Animation"}},
		OriginCountry:      []string{"JP"},
		OriginalLanguage:   "ja",
		LogosRead:          true,
		Videos:             vid,
		Seasons:            []metadata.DetailSeason{{Number: 1, EpisodeCount: 60}},
		SeasonGroupID:      "6782169d78cfcd77ed4e89be",
		SeasonGroupChecked: false,
	}
	if got := DetailsTTL(Entry{Details: &d}); got != 0 {
		t.Fatalf("unchecked single-season anime ttl = %v, want 0", got)
	}
	d.SeasonGroupChecked = true
	if got := DetailsTTL(Entry{Details: &d}); got != TTL["details"] {
		t.Fatalf("checked anime ttl = %v, want %v", got, TTL["details"])
	}
	detailsText(&d, metadata.TitleText{}, "en")
	if d.SeasonGroupID != "" || d.SeasonGroupChecked {
		t.Fatalf("detailsText leaked internal season group fields: %+v", d)
	}
}
