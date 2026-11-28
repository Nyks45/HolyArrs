package stream

import "testing"

func TestWatchHistoryCompletionRespectsProgress(t *testing.T) {
	for _, tc := range []struct {
		name         string
		position     float64
		duration     float64
		wantComplete bool
	}{
		{"short at halfway", 60, 120, false},
		{"short before credits", 102, 120, false},
		{"short near the end", 110, 120, true},
		{"long near the end", 5_500, 6_000, false},
		{"long past the percentage threshold", 5_520, 6_000, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHistoryStore(t.TempDir())
			got, err := h.Upsert(1, WatchEntry{Kind: "movie", TMDB: "900002", PositionSec: tc.position, DurationSec: tc.duration})
			if err != nil {
				t.Fatal(err)
			}
			if got.Completed != tc.wantComplete {
				t.Fatalf("position %g/%g: completed = %t, want %t", tc.position, tc.duration, got.Completed, tc.wantComplete)
			}
		})
	}
}

func TestWatchHistoryKeepsCommittedEntriesOnError(t *testing.T) {
	h := NewHistoryStore(t.TempDir())
	const user = 1
	prior, err := h.Upsert(user, WatchEntry{Kind: "movie", TMDB: "900002", PositionSec: 60, DurationSec: 120})
	if err != nil {
		t.Fatal(err)
	}
	checkPrior := func() {
		t.Helper()
		entries, _ := h.List(user, 10)
		if len(entries) != 1 || entries[0].Key != prior.Key || entries[0].PositionSec != prior.PositionSec {
			t.Fatalf("history no longer shows saved progress at %g: %+v", prior.PositionSec, entries)
		}
	}

	if _, err := h.BatchUpdate(user, []WatchEntry{{Kind: "movie", TMDB: "900006"}, {}}, []string{prior.Key}); err == nil {
		t.Fatal("expected the invalid batch to be rejected")
	}
	checkPrior()

	// Renaming the output over this directory makes persistence fail.
	h.path = t.TempDir()
	update := WatchEntry{Kind: "movie", TMDB: prior.TMDB, PositionSec: 80, DurationSec: 120}
	if _, err := h.Upsert(user, update); err == nil {
		t.Fatal("expected the write to fail")
	}
	checkPrior()
	if _, err := h.BatchUpdate(user, []WatchEntry{update, {Kind: "movie", TMDB: "900006"}}, nil); err == nil {
		t.Fatal("expected the batch write to fail")
	}
	checkPrior()
}

func TestWatchHistoryDropsOldLinkWhenReleaseChanges(t *testing.T) {
	h := NewHistoryStore(t.TempDir())
	base := WatchEntry{Kind: "movie", TMDB: "900003", PositionSec: 600, DurationSec: 6000}
	first := base
	first.ReleaseTitle, first.ReleaseLink, first.InfoHash = "Film.2160p.WEB", "magnet:?xt=urn:btih:abc", "abc"
	if _, err := h.Upsert(1, first); err != nil {
		t.Fatal(err)
	}
	same := base
	same.ReleaseTitle = "Film.2160p.WEB"
	got, err := h.Upsert(1, same)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReleaseLink != first.ReleaseLink || got.InfoHash != "abc" {
		t.Fatalf("same release lost its link: %+v", got)
	}
	other := base
	other.ReleaseTitle = "Instant 1080p"
	got, err = h.Upsert(1, other)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReleaseLink != "" || got.InfoHash != "" {
		t.Fatalf("new release kept the old torrent: link %q hash %q", got.ReleaseLink, got.InfoHash)
	}
}
