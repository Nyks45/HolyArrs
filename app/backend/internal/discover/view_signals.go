package discover

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// WatchSignal summarizes a user's watch history activity for one movie or show.
type WatchSignal struct {
	TMDBID    string
	Kind      string // movie, show
	Title     string
	Year      int
	Completed bool
	Episodes  int
	UpdatedAt time.Time
}

// ViewSignal records that a signed-in user opened a title's details page.
type ViewSignal struct {
	UserID   int64     `json:"user_id"`
	Kind     string    `json:"kind"` // movie, show
	TMDBID   string    `json:"tmdb_id"`
	Name     string    `json:"name,omitempty"`
	Year     int       `json:"year,omitempty"`
	Opens    int       `json:"opens"`
	OpenedAt time.Time `json:"opened_at"`
}

// ViewSignalStore persists recent title-page opens per user atomically as JSON.
type ViewSignalStore struct {
	mu      sync.Mutex
	path    string
	now     func() time.Time
	entries map[string]ViewSignal
}

// NewViewSignalStore loads persisted title-view signals from configDir when writable.
func NewViewSignalStore(configDir string) *ViewSignalStore {
	s := &ViewSignalStore{
		now:     time.Now,
		entries: map[string]ViewSignal{},
	}
	mem := strings.TrimSpace(os.Getenv("HOLYARRS_CONFIG_IN_MEMORY"))
	if strings.TrimSpace(configDir) != "" && mem != "1" && !strings.EqualFold(mem, "true") {
		s.path = filepath.Join(configDir, "title-views.json")
		s.loadLocked()
	}
	return s
}

func normalizeSignalKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "movie":
		return "movie"
	case "show", "tv", "series":
		return "show"
	default:
		return ""
	}
}

func userViewKey(userID int64, kind, tmdbID string) string {
	return strconv.FormatInt(userID, 10) + ":" + kind + ":" + tmdbID
}

func (s *ViewSignalStore) loadLocked() {
	if s.path == "" {
		return
	}
	b, err := os.ReadFile(s.path)
	if err != nil || len(b) == 0 {
		return
	}
	var list []ViewSignal
	if err := json.Unmarshal(b, &list); err != nil {
		return
	}
	for _, e := range list {
		k := normalizeSignalKind(e.Kind)
		id := strings.TrimSpace(e.TMDBID)
		if e.UserID <= 0 || k == "" || id == "" {
			continue
		}
		e.Kind = k
		e.TMDBID = id
		if e.Opens < 1 {
			e.Opens = 1
		}
		s.entries[userViewKey(e.UserID, k, id)] = e
	}
}

func (s *ViewSignalStore) saveLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return err
	}
	list := make([]ViewSignal, 0, len(s.entries))
	for _, e := range s.entries {
		list = append(list, e)
	}
	sort.Slice(list, func(i, j int) bool {
		if !list[i].OpenedAt.Equal(list[j].OpenedAt) {
			return list[i].OpenedAt.After(list[j].OpenedAt)
		}
		if list[i].UserID != list[j].UserID {
			return list[i].UserID < list[j].UserID
		}
		if list[i].Kind != list[j].Kind {
			return list[i].Kind < list[j].Kind
		}
		return list[i].TMDBID < list[j].TMDBID
	})
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Record notes that userID opened kind/tmdbID and reports whether Explore pools should refresh.
func (s *ViewSignalStore) Record(userID int64, kind, tmdbID, name string, year int) (bool, error) {
	k := normalizeSignalKind(kind)
	id := strings.TrimSpace(tmdbID)
	if userID <= 0 || k == "" || id == "" {
		return false, nil
	}
	now := time.Now().UTC()
	if s.now != nil {
		now = s.now().UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	mk := userViewKey(userID, k, id)
	prev, ok := s.entries[mk]
	cur := ViewSignal{
		UserID:   userID,
		Kind:     k,
		TMDBID:   id,
		Name:     strings.TrimSpace(name),
		Year:     year,
		Opens:    1,
		OpenedAt: now,
	}
	shouldRefresh := !ok
	if ok {
		cur.Opens = prev.Opens + 1
		if cur.Name == "" {
			cur.Name = prev.Name
		}
		if cur.Year == 0 {
			cur.Year = prev.Year
		}
		if cur.Opens == 2 || cur.Opens == 5 || now.Sub(prev.OpenedAt) >= 6*time.Hour {
			shouldRefresh = true
		}
	}
	s.entries[mk] = cur
	s.capUserLocked(userID, 120)
	if err := s.saveLocked(); err != nil {
		return shouldRefresh, err
	}
	return shouldRefresh, nil
}

func (s *ViewSignalStore) capUserLocked(userID int64, maxCount int) {
	if maxCount <= 0 {
		return
	}
	var mine []struct {
		key string
		at  time.Time
	}
	for k, e := range s.entries {
		if e.UserID == userID {
			mine = append(mine, struct {
				key string
				at  time.Time
			}{key: k, at: e.OpenedAt})
		}
	}
	if len(mine) <= maxCount {
		return
	}
	sort.Slice(mine, func(i, j int) bool {
		return mine[i].at.After(mine[j].at)
	})
	for _, drop := range mine[maxCount:] {
		delete(s.entries, drop.key)
	}
}

// List returns the recent opened-title signals for userID, newest first.
func (s *ViewSignalStore) List(userID int64) []ViewSignal {
	if s == nil || userID <= 0 {
		return nil
	}
	s.mu.Lock()
	var out []ViewSignal
	for _, e := range s.entries {
		if e.UserID == userID {
			out = append(out, e)
		}
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if !out[i].OpenedAt.Equal(out[j].OpenedAt) {
			return out[i].OpenedAt.After(out[j].OpenedAt)
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].TMDBID < out[j].TMDBID
	})
	return out
}
