package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestAnimeSingleSeasonAutoSplitAndGroupEpisodes(t *testing.T) {
	var groupCalls atomic.Int32
	smallGroupID := "64f9a35ddc1cb400e53ef175"
	fullGroupID := "6782169d78cfcd77ed4e89be"

	makeGroupEps := func(seasonNum, startEp, count int, firstAir, prefix string) []map[string]any {
		out := make([]map[string]any, 0, count)
		for i := 0; i < count; i++ {
			out = append(out, map[string]any{
				"season_number":  seasonNum,
				"episode_number": startEp + i,
				"order":          i,
				"name":           fmt.Sprintf("%s %d", prefix, i+1),
				"overview":       "Story details.",
				"air_date":       firstAir,
				"runtime":        24,
				"still_path":     fmt.Sprintf("/s%d_%d.jpg", seasonNum, i+1),
			})
		}
		return out
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/3/genre/tv/list":
			_, _ = w.Write([]byte(`{"genres":[{"id":16,"name":"Animation"},{"id":9648,"name":"Mystery"}]}`))
		case "/3/tv/220542":
			_, _ = w.Write([]byte(`{
				"id":220542,"name":"The Apothecary Diaries","first_air_date":"2023-10-22",
				"genres":[{"id":16,"name":"Animation"},{"id":9648,"name":"Mystery"}],
				"origin_country":["JP"],"original_language":"ja",
				"external_ids":{"imdb_id":"tt26743760","tvdb_id":431162},
				"next_episode_to_air":{"season_number":1,"episode_number":49,"name":"Locusts","air_date":"2026-10-02"},
				"seasons":[
					{"season_number":0,"episode_count":58,"air_date":"2023-10-23"},
					{"season_number":1,"episode_count":60,"air_date":"2023-10-22"}
				],
				"images":{"logos":[]},"videos":{"results":[]},"recommendations":{"results":[]}
			}`))
		case "/3/tv/220542/episode_groups":
			groupCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{"id": "64a000000000000000000001", "name": "Story Arcs", "type": 5, "group_count": 6, "episode_count": 60},
					{"id": smallGroupID, "name": "Seasons", "type": 6, "group_count": 3, "episode_count": 48},
					{"id": "64a000000000000000000002", "name": "Cours", "type": 7, "group_count": 4, "episode_count": 60},
					{"id": fullGroupID, "name": "Seasons", "type": 6, "group_count": 4, "episode_count": 110},
				},
			})
		case "/3/tv/episode_group/" + fullGroupID:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": fullGroupID, "name": "Seasons", "type": 6,
				"groups": []map[string]any{
					{"name": "Season 2", "order": 2, "episodes": makeGroupEps(1, 25, 24, "2025-01-10", "S2Ep")},
					{"name": "Specials", "order": 0, "episodes": makeGroupEps(0, 1, 50, "2023-10-23", "Sp")},
					{"name": "Season 3", "order": 3, "episodes": makeGroupEps(1, 49, 12, "2026-10-02", "S3Ep")},
					{"name": "Season 1", "order": 1, "episodes": makeGroupEps(1, 1, 24, "2023-10-22", "S1Ep")},
				},
			})
		case "/3/tv/100":
			// Non-anime single-season show must not query episode_groups.
			_, _ = w.Write([]byte(`{
				"id":100,"name":"NonAnime","first_air_date":"2022-01-01",
				"genres":[{"id":18,"name":"Drama"}],"origin_country":["US"],"original_language":"en",
				"seasons":[{"season_number":1,"episode_count":20,"air_date":"2022-01-01"}]
			}`))
		case "/3/tv/85937":
			// Already multi-season anime must stay untouched without querying episode_groups.
			_, _ = w.Write([]byte(`{
				"id":85937,"name":"Demon Slayer","first_air_date":"2019-04-06",
				"genres":[{"id":16,"name":"Animation"}],"origin_country":["JP"],"original_language":"ja",
				"seasons":[
					{"season_number":1,"episode_count":26,"air_date":"2019-04-06"},
					{"season_number":2,"episode_count":7,"air_date":"2021-10-10"}
				]
			}`))
		case "/3/tv/100/episode_groups", "/3/tv/85937/episode_groups":
			groupCalls.Add(1)
			t.Errorf("unexpected episode_groups call for %s", r.URL.Path)
		case "/3/tv/220542/watch/providers", "/3/tv/100/watch/providers", "/3/tv/85937/watch/providers":
			_, _ = w.Write([]byte(`{"results":{}}`))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)

	p := newTMDB(srv, fakeCred)
	ctx := context.Background()

	d, err := p.Details(ctx, "show", "220542")
	if err != nil {
		t.Fatal(err)
	}
	if d.IMDbID != "tt26743760" || d.SeasonGroupID != fullGroupID || !d.SeasonGroupChecked {
		t.Fatalf("unexpected details header: imdb=%q gid=%q checked=%v", d.IMDbID, d.SeasonGroupID, d.SeasonGroupChecked)
	}
	wantSeasons := []DetailSeason{
		{Number: 0, EpisodeCount: 58, Year: 2023},
		{Number: 1, EpisodeCount: 24, Year: 2023},
		{Number: 2, EpisodeCount: 24, Year: 2025},
		{Number: 3, EpisodeCount: 12, Year: 2026},
	}
	if len(d.Seasons) != len(wantSeasons) {
		t.Fatalf("seasons = %+v, want %+v", d.Seasons, wantSeasons)
	}
	for i := range wantSeasons {
		if d.Seasons[i] != wantSeasons[i] {
			t.Fatalf("season[%d] = %+v, want %+v", i, d.Seasons[i], wantSeasons[i])
		}
	}
	if d.NextEpisode == nil || d.NextEpisode.Season != 3 || d.NextEpisode.Episode != 1 {
		t.Fatalf("next episode = %+v, want S3E1", d.NextEpisode)
	}
	if groupCalls.Load() != 1 {
		t.Fatalf("expected 1 episode_groups call, got %d", groupCalls.Load())
	}

	eps2, err := p.GroupSeasonEpisodes(ctx, d.SeasonGroupID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps2) != 24 || eps2[0].Number != 1 || eps2[0].Name != "S2Ep 1" || eps2[0].AirDay != "2025-01-10" || eps2[0].Still != ImageBase+"/w780/s1_1.jpg" {
		t.Fatalf("group season 2 mismatch: len=%d first=%+v", len(eps2), eps2[0])
	}
	if _, err := p.GroupSeasonEpisodes(ctx, d.SeasonGroupID, 4); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing group season, got %v", err)
	}

	nonAnime, err := p.Details(ctx, "show", "100")
	if err != nil || len(nonAnime.Seasons) != 1 || nonAnime.SeasonGroupID != "" {
		t.Fatalf("non-anime show changed unexpectedly: %+v err=%v", nonAnime.Seasons, err)
	}
	multiAnime, err := p.Details(ctx, "show", "85937")
	if err != nil || len(multiAnime.Seasons) != 2 || multiAnime.SeasonGroupID != "" {
		t.Fatalf("multi-season anime changed unexpectedly: %+v err=%v", multiAnime.Seasons, err)
	}

	var sw Provider = &Switch{Use: func(context.Context) bool { return true }, Then: p, Else: Lab()}
	if gl, ok := GroupSeasonListerFor(ctx, sw); !ok || gl == nil {
		t.Fatalf("GroupSeasonListerFor did not unwrap Switch")
	}
}

func TestNeedsAnimeSeasonSplit(t *testing.T) {
	legacyAnime := &Details{
		DiscoverItem:     DiscoverItem{Kind: "show", Genres: []string{"Animation"}},
		OriginCountry:    []string{"JP"},
		OriginalLanguage: "ja",
		Seasons:          []DetailSeason{{Number: 0, EpisodeCount: 58}, {Number: 1, EpisodeCount: 60}},
	}
	if !NeedsAnimeSeasonSplit(legacyAnime) {
		t.Fatalf("expected legacy single-season anime to need split")
	}
	legacyAnime.SeasonGroupChecked = true
	if NeedsAnimeSeasonSplit(legacyAnime) {
		t.Fatalf("expected checked anime not to need split again")
	}
	nonAnime := &Details{
		DiscoverItem:     DiscoverItem{Kind: "show", Genres: []string{"Drama"}},
		OriginalLanguage: "en",
		Seasons:          []DetailSeason{{Number: 1, EpisodeCount: 20}},
	}
	if NeedsAnimeSeasonSplit(nonAnime) {
		t.Fatalf("expected non-anime not to need split")
	}
}
