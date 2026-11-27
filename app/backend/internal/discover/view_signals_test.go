package discover

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestViewSignalStorePersistenceCadenceAndCap(t *testing.T) {
	cfgDir := t.TempDir()
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

	s := NewViewSignalStore(cfgDir)
	s.now = func() time.Time { return now }

	// First open triggers refresh.
	refresh, err := s.Record(1, "show", "256711", "The Rainmaker", 2025)
	if err != nil || !refresh {
		t.Fatalf("first Record got refresh=%v err=%v", refresh, err)
	}
	// Second open triggers refresh; third rapid open does not.
	now = now.Add(5 * time.Minute)
	refresh, err = s.Record(1, "tv", "256711", "", 0)
	if err != nil || !refresh {
		t.Fatalf("second Record got refresh=%v err=%v", refresh, err)
	}
	now = now.Add(5 * time.Minute)
	refresh, err = s.Record(1, "show", "256711", "", 0)
	if err != nil || refresh {
		t.Fatalf("third rapid Record got refresh=%v err=%v", refresh, err)
	}
	// Fifth open triggers refresh.
	now = now.Add(5 * time.Minute)
	_, _ = s.Record(1, "show", "256711", "", 0)
	now = now.Add(5 * time.Minute)
	refresh, err = s.Record(1, "show", "256711", "", 0)
	if err != nil || !refresh {
		t.Fatalf("fifth Record got refresh=%v err=%v", refresh, err)
	}
	// Over 6h later triggers refresh again.
	now = now.Add(7 * time.Hour)
	refresh, err = s.Record(1, "show", "256711", "", 0)
	if err != nil || !refresh {
		t.Fatalf("stale-gap Record got refresh=%v err=%v", refresh, err)
	}

	reloaded := NewViewSignalStore(cfgDir)
	list := reloaded.List(1)
	if len(list) != 1 || list[0].Opens != 6 || list[0].Name != "The Rainmaker" || list[0].Kind != "show" {
		t.Fatalf("reloaded list = %+v", list)
	}

	// Cap at 120 newest entries per user.
	for i := 1; i <= 130; i++ {
		now = now.Add(time.Minute)
		if _, err := reloaded.Record(2, "movie", intStr(i), "Movie", 2020); err != nil {
			t.Fatal(err)
		}
	}
	capped := reloaded.List(2)
	if len(capped) != 120 || capped[0].TMDBID != "130" || capped[len(capped)-1].TMDBID != "11" {
		t.Fatalf("unexpected cap behavior len=%d first=%v last=%v", len(capped), capped[0].TMDBID, capped[len(capped)-1].TMDBID)
	}
}

func TestViewSignalStoreInMemoryEnvSkipsDisk(t *testing.T) {
	t.Setenv("HOLYARRS_CONFIG_IN_MEMORY", "1")
	cfgDir := t.TempDir()
	s := NewViewSignalStore(cfgDir)
	if _, err := s.Record(1, "movie", "693134", "Dune: Part Two", 2024); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfgDir, "title-views.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no disk file when in-memory mode active, got err=%v", err)
	}
	if len(s.List(1)) != 1 {
		t.Fatalf("expected in-memory entry to remain queryable")
	}
}

func intStr(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
