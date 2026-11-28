package stream

import "testing"

// Marking a title watched or unwatched by hand must reach the recommendations hook.
func TestWatchHistoryNotifiesOnManualMarks(t *testing.T) {
	h := NewHistoryStore(t.TempDir())
	var got []string
	h.OnWatchTitle = func(_ int64, kind, tmdb string) { got = append(got, kind+":"+tmdb) }
	want := func(step string, calls ...string) {
		t.Helper()
		if len(got) != len(calls) {
			t.Fatalf("%s: notified %v, want %v", step, got, calls)
		}
		for i := range calls {
			if got[i] != calls[i] {
				t.Fatalf("%s: notified %v, want %v", step, got, calls)
			}
		}
		got = nil
	}

	movie, err := h.Upsert(1, WatchEntry{Kind: "movie", TMDB: "11", Completed: true})
	if err != nil {
		t.Fatal(err)
	}
	want("movie marked", "movie:11")
	if _, err := h.Upsert(1, WatchEntry{Kind: "movie", TMDB: "11", Completed: true}); err != nil {
		t.Fatal(err)
	}
	want("movie marked again")
	if err := h.Delete(1, movie.Key); err != nil {
		t.Fatal(err)
	}
	want("movie unmarked", "movie:11")
	if err := h.Delete(1, movie.Key); err != nil {
		t.Fatal(err)
	}
	want("missing key")

	eps := []WatchEntry{{Kind: "show", TMDB: "22", Season: 1, Episode: 1, Completed: true}, {Kind: "show", TMDB: "22", Season: 1, Episode: 2, Completed: true}}
	if _, err := h.BatchUpdate(1, eps, nil); err != nil {
		t.Fatal(err)
	}
	want("show marked", "show:22")
	if _, err := h.BatchUpdate(1, nil, []string{WatchKey("show", "22", 1, 1), WatchKey("show", "22", 1, 2), "show:22:s9e9"}); err != nil {
		t.Fatal(err)
	}
	want("show unmarked", "show:22")
	if items, _ := h.List(1, 10); len(items) != 0 {
		t.Fatalf("history still holds %d entries", len(items))
	}
}
