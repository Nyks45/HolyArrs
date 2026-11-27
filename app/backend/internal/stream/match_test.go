package stream

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScraperSpecialtyGating(t *testing.T) {
	scrapers := []NuvioScraper{
		{ID: "reanime", Name: "Reanime", SupportedTypes: []string{"anime", "movie", "tv"}},
		{ID: "hianime", Name: "HiAnime", SupportedTypes: []string{"movie", "tv"}, Description: "HiAnime multi-server anime streaming"},
		{ID: "animepahe", Name: "AnimePahe", SupportedTypes: []string{"movie", "tv"}, Description: "AnimePahe (Sub/Dub)"},
		{ID: "kurage", Name: "Kurage", SupportedTypes: []string{"anime", "movie", "tv"}, Description: "Kurage.live anime streaming"},
		{ID: "topcartoons", Name: "TopCartoons", SupportedTypes: []string{"anime", "tv"}, Description: "Popular cartoons streaming"},
		{ID: "kisskh", Name: "Kisskh", SupportedTypes: []string{"movie", "tv"}, Description: "Streams Korean dramas"},
		{ID: "onlykdrama", Name: "OnlyKDrama", SupportedTypes: []string{"movie", "tv"}, Description: "Asian Drama"},
		{ID: "vidlink", Name: "Vidlink", SupportedTypes: []string{"movie", "tv"}},
		{ID: "moviesdrive", Name: "MoviesDrive", SupportedTypes: []string{"movie", "tv"}},
		{ID: "4khdhub", Name: "4KHDHub", SupportedTypes: []string{"movie", "tv"}},
		{ID: "uhdmovies", Name: "UHDMovies", SupportedTypes: []string{"movie", "tv"}},
	}
	byID := map[string]NuvioScraper{}
	for _, sc := range scrapers {
		byID[sc.ID] = sc
	}

	// K-Drama: When Life Gives You Tangerines.
	kdrama := NewSearchMatchContext("show", "When Life Gives You Tangerines", "폭싹 속았수다", 2025, 1, 2, []string{"Drama"}, "ko", []string{"KR"}, false)
	for _, id := range []string{"reanime", "hianime", "animepahe", "kurage", "topcartoons"} {
		if scraperMatchesContext(byID[id], "show", kdrama) {
			t.Fatalf("expected %s to be skipped for K-Drama", id)
		}
	}
	for _, id := range []string{"kisskh", "onlykdrama", "vidlink", "moviesdrive"} {
		if !scraperMatchesContext(byID[id], "show", kdrama) {
			t.Fatalf("expected %s to be allowed for K-Drama", id)
		}
	}

	// Hollywood Movie: Dune: Part Two.
	hollywood := NewSearchMatchContext("movie", "Dune: Part Two", "", 2024, 0, 0, []string{"Science Fiction", "Adventure"}, "en", []string{"US"}, false)
	for _, id := range []string{"reanime", "animepahe", "kisskh", "onlykdrama", "topcartoons"} {
		if scraperMatchesContext(byID[id], "movie", hollywood) {
			t.Fatalf("expected %s to be skipped for Hollywood movie", id)
		}
	}
	for _, id := range []string{"vidlink", "4khdhub", "uhdmovies"} {
		if !scraperMatchesContext(byID[id], "movie", hollywood) {
			t.Fatalf("expected %s to be allowed for Hollywood movie", id)
		}
	}

	// Anime: Attack on Titan.
	anime := NewSearchMatchContext("show", "Attack on Titan", "進撃の巨人", 2013, 1, 1, []string{"Animation", "Action"}, "ja", []string{"JP"}, false)
	for _, id := range []string{"reanime", "hianime", "animepahe", "kurage"} {
		if !scraperMatchesContext(byID[id], "show", anime) {
			t.Fatalf("expected %s to be allowed for anime", id)
		}
	}
	if scraperMatchesContext(byID["kisskh"], "show", anime) {
		t.Fatal("expected kisskh to be skipped for Japanese anime")
	}
}

func TestVerifyCandidateMatchesAndRejects(t *testing.T) {
	kdrama := NewSearchMatchContext("show", "When Life Gives You Tangerines", "", 2025, 1, 2, []string{"Drama"}, "ko", []string{"KR"}, false)

	// Screenshot regression: synthetic header has queried title, but real filename after doc emoji is another show.
	wrongMoviesDrive := UnifiedCandidate{
		Kind:         "http",
		Instant:      true,
		Provider:     "MoviesDrive",
		ProviderType: "nuvio",
		Title:        "🎬 When Life Gives You Tangerines (2025) • S01E02 [1080p FHD • WEB-DL • HEVC] 📄 He.Is.Psychometric.S01E02.1080p.WEB-DL.HIN-KOR.x265.ESub-MoviesDrives.bid.mkv 🌐 Hindi • Korean 📦 587.88 MB",
		ReleaseName:  "🎬 When Life Gives You Tangerines (2025) • S01E02 [1080p FHD • WEB-DL • HEVC] 📄 He.Is.Psychometric.S01E02.1080p.WEB-DL.HIN-KOR.x265.ESub-MoviesDrives.bid.mkv 🌐 Hindi • Korean 📦 587.88 MB",
		StreamURL:    "https://cdn.example/He.Is.Psychometric.S01E02.1080p.WEB-DL.mkv",
	}
	if VerifyCandidate(wrongMoviesDrive, kdrama) {
		t.Fatal("expected wrong MoviesDrive filename He.Is.Psychometric to be rejected")
	}

	repressCtx := NewSearchMatchContext("show", "The Remarried Empress", "", 2026, 1, 1, []string{"Drama"}, "ko", []string{"KR"}, false)
	repressCtx.TMDB = "279246"
	wrongTMDBUrl := UnifiedCandidate{
		Kind:         "hls",
		Instant:      true,
		Provider:     "Purstream",
		ProviderType: "nuvio",
		Title:        "The Remarried Empress",
		ReleaseName:  "S1 E1 - Episode 1 | The Remarried Empress",
		StreamURL:    "https://free.finepulfe.xyz/tv/37680-l7487/S01/E01/master.m3u8",
	}
	if VerifyCandidate(wrongTMDBUrl, repressCtx) {
		t.Fatal("expected Purstream URL pointing to wrong TMDB /tv/37680- to be rejected")
	}
	goodTMDBUrl := wrongTMDBUrl
	goodTMDBUrl.StreamURL = "https://free.finepulfe.xyz/tv/279246-slug/S01/E01/master.m3u8"
	if !VerifyCandidate(goodTMDBUrl, repressCtx) {
		t.Fatal("expected matching TMDB path URL to be accepted")
	}

	goodMoviesDrive := UnifiedCandidate{
		Kind:         "http",
		Instant:      true,
		Provider:     "MoviesDrive",
		ProviderType: "nuvio",
		Title:        "🎬 When Life Gives You Tangerines (2025) • S01E02 [1080p FHD] 📄 When.Life.Gives.You.Tangerines.S01E02.1080p.WEB-DL.mkv 🌐 Korean",
		ReleaseName:  "🎬 When Life Gives You Tangerines (2025) • S01E02 [1080p FHD] 📄 When.Life.Gives.You.Tangerines.S01E02.1080p.WEB-DL.mkv 🌐 Korean",
		StreamURL:    "https://cdn.example/When.Life.Gives.You.Tangerines.S01E02.1080p.WEB-DL.mkv",
	}
	if !VerifyCandidate(goodMoviesDrive, kdrama) {
		t.Fatal("expected matching MoviesDrive filename to be accepted")
	}

	stopwordTrap := UnifiedCandidate{
		Kind:         "http",
		Instant:      true,
		Provider:     "HDHub4u",
		ProviderType: "nuvio",
		ReleaseName:  "When.The.Phone.Rings.S01E02.1080p.mkv",
		StreamURL:    "https://cdn.example/stream.mp4",
	}
	if VerifyCandidate(stopwordTrap, kdrama) {
		t.Fatal("expected show sharing only stopwords to be rejected")
	}

	wrongEpisode := UnifiedCandidate{
		Kind:         "http",
		Instant:      true,
		Provider:     "MoviesDrive",
		ProviderType: "nuvio",
		ReleaseName:  "When.Life.Gives.You.Tangerines.S01E05.1080p.mkv",
		StreamURL:    "https://cdn.example/stream.mp4",
	}
	if VerifyCandidate(wrongEpisode, kdrama) {
		t.Fatal("expected S01E05 candidate to be rejected when S01E02 requested")
	}

	genericDirect := UnifiedCandidate{
		Kind:         "hls",
		Instant:      true,
		Provider:     "Vidlink",
		ProviderType: "nuvio",
		Title:        "Vidlink · Server 1 · 1080p",
		ReleaseName:  "Vidlink · Server 1 · 1080p",
		StreamURL:    "https://cdn.example/hls/a1b2c3/master.m3u8",
	}
	if !VerifyCandidate(genericDirect, kdrama) {
		t.Fatal("expected generic TMDB-ID direct stream label to be accepted")
	}

	duneMC := NewSearchMatchContext("movie", "Dune: Part Two", "", 2024, 0, 0, []string{"Science Fiction"}, "en", []string{"US"}, false)
	wrongYear := UnifiedCandidate{
		Kind:         "http",
		Instant:      true,
		Provider:     "UHDMovies",
		ProviderType: "nuvio",
		ReleaseName:  "Dune.2021.1080p.mkv",
		StreamURL:    "https://cdn.example/Dune.2021.1080p.mkv",
	}
	if VerifyCandidate(wrongYear, duneMC) {
		t.Fatal("expected wrong movie year Dune.2021 to be rejected for Dune: Part Two (2024)")
	}

	sequelNum := UnifiedCandidate{
		Kind:         "http",
		Instant:      true,
		Provider:     "UHDMovies",
		ProviderType: "nuvio",
		ReleaseName:  "Dune.2.2024.1080p.mkv",
		StreamURL:    "https://cdn.example/Dune.2.2024.1080p.mkv",
	}
	if !VerifyCandidate(sequelNum, duneMC) {
		t.Fatal("expected Dune.2.2024.1080p.mkv to match Dune: Part Two")
	}

	duneMultilineVidRock := UnifiedCandidate{
		Kind:         "hls",
		Instant:      true,
		Provider:     "VidRock",
		ProviderType: "nuvio",
		Title:        "🎬 Dune: Part Two - (2024)\n🚀 1080p | 🌍 Original Audio | 🎧 AAC",
		ReleaseName:  "🎬 Dune: Part Two - (2024)\n🚀 1080p | 🌍 Original Audio | 🎧 AAC",
		StreamURL:    "https://vercetti.org/movie/abc/bigtits.m3u8",
	}
	if !VerifyCandidate(duneMultilineVidRock, duneMC) {
		t.Fatal("expected VidRock multi-line Dune: Part Two title to be accepted")
	}

	duneCastle := UnifiedCandidate{
		Kind:         "hls",
		Instant:      true,
		Provider:     "Castle",
		ProviderType: "nuvio",
		Title:        "Dune: Part Two (2024)",
		ReleaseName:  "Dune: Part Two (2024)",
		StreamURL:    "https://img1.fcxmb.com/myhls_mps/2024/8yue/Dune2/Hindi_720/index_266.m3u8",
	}
	if !VerifyCandidate(duneCastle, duneMC) {
		t.Fatal("expected Castle Dune: Part Two (2024) title to be accepted")
	}

	dunePurstream := UnifiedCandidate{
		Kind:         "hls",
		Instant:      true,
		Provider:     "Purstream",
		ProviderType: "nuvio",
		Title:        "🎬 Dune: Part Two - 2024\n🔥 1080p | 🔊 Dual-Audio",
		ReleaseName:  "🎬 Dune: Part Two - 2024\n🔥 1080p | 🔊 Dual-Audio",
		StreamURL:    "https://free.finepulfe.xyz/movies/693134-mrp8j/master.m3u8",
	}
	if !VerifyCandidate(dunePurstream, duneMC) {
		t.Fatal("expected Purstream/Movix Dune: Part Two - 2024 title to be accepted")
	}

	movie1917 := NewSearchMatchContext("movie", "1917", "", 2019, 0, 0, []string{"War"}, "en", []string{"GB"}, false)
	cand1917 := UnifiedCandidate{
		Kind:         "http",
		Instant:      true,
		Provider:     "4KHDHub",
		ProviderType: "nuvio",
		ReleaseName:  "1917.2019.1080p.mkv",
		StreamURL:    "https://cdn.example/1917.2019.1080p.mkv",
	}
	if !VerifyCandidate(cand1917, movie1917) {
		t.Fatal("expected numeric movie title 1917.2019 to be accepted")
	}

	localQuirk := UnifiedCandidate{
		Kind:        "local",
		Instant:     true,
		Title:       "Backup Copy.mkv",
		ReleaseName: "Backup Copy.mkv",
	}
	if !VerifyCandidate(localQuirk, duneMC) {
		t.Fatal("expected local file candidate to always be accepted")
	}

	dupA := goodMoviesDrive
	dupA.Quality = "1080p"
	dupB := goodMoviesDrive
	dupB.Quality = "720p"
	ranked := FilterAndRankCandidates([]UnifiedCandidate{dupA, dupB, genericDirect}, StreamPrefs{})
	if len(ranked) != 2 {
		t.Fatalf("expected duplicate StreamURL to be deduplicated to 2 candidates, got %d", len(ranked))
	}
}

func TestCustomScraperLifecycle(t *testing.T) {
	dir := t.TempDir()
	ps := NewPluginStore(dir)
	if !ps.NodeAvailable() {
		t.Skip("node runtime unavailable")
	}

	repo, err := ps.SaveCustomScraper(CustomScraperInput{
		Name:           "My Personal API",
		Description:    "Direct personal provider",
		SupportedTypes: []string{"movie", "tv"},
		Code: `
		function getStreams(tmdbId, mediaType) {
			return [{ name: 'Personal', title: 'Direct 1080p', url: 'https://cdn.example.com/hls/' + tmdbId + '/master.m3u8', quality: '1080p' }];
		}
		`,
	})
	if err != nil {
		t.Fatalf("SaveCustomScraper failed: %v", err)
	}
	if repo.ID != customRepoID || len(repo.Scrapers) != 1 || !repo.Scrapers[0].Enabled {
		t.Fatalf("unexpected custom repo state: %+v", repo)
	}
	sid := repo.Scrapers[0].ID
	if _, statErr := os.Stat(filepath.Join(dir, "stream-plugins", customRepoID, sid+".js")); statErr != nil {
		t.Fatalf("expected persisted custom JS file on disk: %v", statErr)
	}

	detail, err := ps.GetCustomScraper(sid)
	if err != nil || !strings.Contains(detail.Code, "getStreams") {
		t.Fatalf("GetCustomScraper failed: err=%v detail=%+v", err, detail)
	}

	mc := NewSearchMatchContext("movie", "Fight Club", "", 1999, 0, 0, []string{"Drama"}, "en", []string{"US"}, false)
	cands, warns := ps.SearchStreams(context.Background(), "movie", "550", 0, 0, "", mc, false, nil)
	if len(cands) != 1 || cands[0].StreamURL != "https://cdn.example.com/hls/550/master.m3u8" {
		t.Fatalf("expected custom scraper to execute in SearchStreams, got cands=%+v warns=%v", cands, warns)
	}

	refreshed, err := ps.RefreshRepo(context.Background(), customRepoID)
	if err != nil || len(refreshed.Scrapers) != 1 {
		t.Fatalf("expected RefreshRepo on custom repo to succeed without HTTP, got err=%v repo=%+v", err, refreshed)
	}

	if err := ps.DeleteCustomScraper(sid); err != nil {
		t.Fatalf("DeleteCustomScraper failed: %v", err)
	}
	if len(ps.List()) != 0 {
		t.Fatalf("expected empty repo list after deleting last custom scraper, got %+v", ps.List())
	}
}
