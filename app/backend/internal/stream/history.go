package stream

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// SubPref remembers the viewer's chosen subtitle configuration for a title or episode.
type SubPref struct {
	Mode     string `json:"mode"`               // off, embedded, site, upload
	Language string `json:"language,omitempty"` // ISO 639-1 code
	Label    string `json:"label,omitempty"`
	Index    int    `json:"index,omitempty"`    // 0-based embedded track index when mode=embedded
	SiteID   string `json:"site_id,omitempty"`  // cached subtitle candidate ID when mode=site
	SiteKey  string `json:"site_key,omitempty"` // site candidate key/name hint
	DelayMS  int    `json:"delay_ms,omitempty"`
	Size     string `json:"size,omitempty"` // sm, md, lg, xl
}

// WatchEntry stores playback resume position and chosen playback preferences for one movie or episode.
type WatchEntry struct {
	Key          string    `json:"key"`
	UserID       int64     `json:"user_id"`
	Kind         string    `json:"kind"` // movie or show
	TMDB         string    `json:"tmdb"`
	IMDb         string    `json:"imdb,omitempty"`
	Title        string    `json:"title"`
	Year         int       `json:"year,omitempty"`
	Poster       string    `json:"poster,omitempty"`
	Backdrop     string    `json:"backdrop,omitempty"`
	Season       int       `json:"season,omitempty"`
	Episode      int       `json:"episode,omitempty"`
	EpisodeName  string    `json:"episode_name,omitempty"`
	PositionSec  float64   `json:"position_sec"`
	DurationSec  float64   `json:"duration_sec"`
	ProgressPct  float64   `json:"progress_pct"`
	Completed    bool      `json:"completed"`
	SourceID     int64     `json:"source_id,omitempty"`
	SourceName   string    `json:"source_name,omitempty"`
	ReleaseTitle string    `json:"release_title,omitempty"`
	ReleaseLink  string    `json:"release_link,omitempty"`
	InfoHash     string    `json:"info_hash,omitempty"`
	Resolution   string    `json:"resolution,omitempty"`
	FileIndex    int       `json:"file_index,omitempty"`
	Quality      string    `json:"quality,omitempty"`
	AudioIndex   int       `json:"audio_index,omitempty"`
	PlaybackRate float64   `json:"playback_rate,omitempty"`
	Subtitle     *SubPref  `json:"subtitle,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// WatchKey builds the stable storage key for a movie or TV episode.
func WatchKey(kind, tmdb string, season, episode int) string {
	tmdb = strings.TrimSpace(tmdb)
	if kind == "show" {
		return fmt.Sprintf("show:%s:s%de%d", tmdb, season, episode)
	}
	return "movie:" + tmdb
}

// HistoryStore persists per-user watch history and resume positions atomically as JSON.
type HistoryStore struct {
	mu      sync.Mutex
	path    string
	entries map[string]WatchEntry // keyed by fmt.Sprintf("%d:%s", userID, entryKey)
	// OnWatchTitle fires outside mu when a key is newly watched or completes.
	OnWatchTitle func(userID int64, kind, tmdb string)
}

// NewHistoryStore initializes a HistoryStore in configDir and loads any existing entries.
func NewHistoryStore(configDir string) *HistoryStore {
	hs := &HistoryStore{
		entries: map[string]WatchEntry{},
	}
	if strings.TrimSpace(configDir) != "" {
		hs.path = filepath.Join(configDir, "watch-history.json")
		hs.loadLocked()
	}
	return hs
}

func storeMapKey(userID int64, entryKey string) string {
	return fmt.Sprintf("%d:%s", userID, entryKey)
}

func (s *HistoryStore) loadLocked() {
	if s.path == "" {
		return
	}
	b, err := os.ReadFile(s.path)
	if err != nil || len(b) == 0 {
		return
	}
	var list []WatchEntry
	if err := json.Unmarshal(b, &list); err != nil {
		return
	}
	for _, e := range list {
		if e.Key == "" && e.TMDB != "" {
			e.Key = WatchKey(e.Kind, e.TMDB, e.Season, e.Episode)
		}
		if e.Key != "" {
			s.entries[storeMapKey(e.UserID, e.Key)] = e
		}
	}
}

func (s *HistoryStore) saveLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return err
	}
	list := make([]WatchEntry, 0, len(s.entries))
	for _, e := range s.entries {
		list = append(list, e)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].UpdatedAt.After(list[j].UpdatedAt)
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

// Upsert saves or merges watch progress and preferences for userID.
func (s *HistoryStore) Upsert(userID int64, in WatchEntry) (WatchEntry, error) {
	s.mu.Lock()
	previous := maps.Clone(s.entries)
	merged, notify, err := s.upsertEntryLocked(userID, in, time.Now().UTC())
	if err != nil {
		s.mu.Unlock()
		return WatchEntry{}, err
	}
	s.capUserEntriesLocked(userID, 2000)
	if err := s.saveLocked(); err != nil {
		s.entries = previous
		s.mu.Unlock()
		return merged, err
	}
	cb := s.OnWatchTitle
	s.mu.Unlock()
	if cb != nil && notify {
		cb(userID, merged.Kind, merged.TMDB)
	}
	return merged, nil
}

// BatchUpdate atomically applies episode/movie upserts and key deletions for userID in one save.
func (s *HistoryStore) BatchUpdate(userID int64, upserts []WatchEntry, deleteKeys []string) ([]WatchEntry, error) {
	s.mu.Lock()
	previous := maps.Clone(s.entries)
	for _, dk := range deleteKeys {
		trimmed := strings.TrimSpace(dk)
		if trimmed != "" {
			delete(s.entries, storeMapKey(userID, trimmed))
		}
	}
	now := time.Now().UTC()
	updated := make([]WatchEntry, 0, len(upserts))
	var notified [][2]string
	seenNotify := map[string]bool{}
	for _, in := range upserts {
		merged, notify, err := s.upsertEntryLocked(userID, in, now)
		if err != nil {
			s.entries = previous
			s.mu.Unlock()
			return updated, err
		}
		updated = append(updated, merged)
		if notify {
			tk := merged.Kind + ":" + merged.TMDB
			if !seenNotify[tk] {
				seenNotify[tk] = true
				notified = append(notified, [2]string{merged.Kind, merged.TMDB})
			}
		}
	}
	s.capUserEntriesLocked(userID, 2000)
	if err := s.saveLocked(); err != nil {
		s.entries = previous
		s.mu.Unlock()
		return updated, err
	}
	cb := s.OnWatchTitle
	s.mu.Unlock()
	if cb != nil {
		for _, pair := range notified {
			cb(userID, pair[0], pair[1])
		}
	}
	return updated, nil
}

func (s *HistoryStore) upsertEntryLocked(userID int64, in WatchEntry, now time.Time) (WatchEntry, bool, error) {
	tmdb := strings.TrimSpace(in.TMDB)
	if tmdb == "" {
		return WatchEntry{}, false, errors.New("tmdb is required")
	}
	kind := in.Kind
	if kind != "show" {
		kind = "movie"
	}
	key := strings.TrimSpace(in.Key)
	if key == "" {
		key = WatchKey(kind, tmdb, in.Season, in.Episode)
	}

	mk := storeMapKey(userID, key)
	prev, hasPrev := s.entries[mk]
	merged := in
	merged.Key = key
	merged.UserID = userID
	merged.Kind = kind
	merged.TMDB = tmdb
	if hasPrev {
		// A different release played last, so the old link, hash and file would resume the wrong source.
		sameRelease := in.ReleaseTitle == "" || in.ReleaseTitle == prev.ReleaseTitle
		if merged.IMDb == "" {
			merged.IMDb = prev.IMDb
		}
		if merged.Title == "" {
			merged.Title = prev.Title
		}
		if merged.Year == 0 {
			merged.Year = prev.Year
		}
		if merged.Poster == "" {
			merged.Poster = prev.Poster
		}
		if merged.Backdrop == "" {
			merged.Backdrop = prev.Backdrop
		}
		if merged.EpisodeName == "" {
			merged.EpisodeName = prev.EpisodeName
		}
		if merged.DurationSec <= 0 && prev.DurationSec > 0 {
			merged.DurationSec = prev.DurationSec
		}
		if merged.SourceID == 0 {
			merged.SourceID = prev.SourceID
		}
		if merged.SourceName == "" {
			merged.SourceName = prev.SourceName
		}
		if merged.ReleaseTitle == "" {
			merged.ReleaseTitle = prev.ReleaseTitle
		}
		if merged.ReleaseLink == "" && sameRelease {
			merged.ReleaseLink = prev.ReleaseLink
		}
		if merged.InfoHash == "" && sameRelease {
			merged.InfoHash = prev.InfoHash
		}
		if merged.Resolution == "" {
			merged.Resolution = prev.Resolution
		}
		if merged.FileIndex == 0 && prev.FileIndex > 0 && merged.ReleaseLink == prev.ReleaseLink {
			merged.FileIndex = prev.FileIndex
		}
		if merged.Quality == "" {
			merged.Quality = prev.Quality
		}
		if merged.PlaybackRate <= 0 {
			merged.PlaybackRate = prev.PlaybackRate
		}
		if merged.Subtitle == nil {
			merged.Subtitle = prev.Subtitle
		}
	}

	if merged.PositionSec < 0 {
		merged.PositionSec = 0
	}
	if merged.DurationSec < 0 {
		merged.DurationSec = 0
	}
	if merged.PlaybackRate <= 0 {
		merged.PlaybackRate = 1
	}
	if merged.DurationSec > 0 && !merged.Completed {
		pct := (merged.PositionSec / merged.DurationSec) * 100
		if pct < 0 {
			pct = 0
		} else if pct > 100 {
			pct = 100
		}
		merged.ProgressPct = pct
		if merged.DurationSec > 60 && (pct >= 92 || (pct >= 90 && merged.DurationSec-merged.PositionSec <= 90)) {
			merged.Completed = true
		}
	} else if merged.Completed {
		if merged.DurationSec > 0 && merged.PositionSec > 0 {
			pct := (merged.PositionSec / merged.DurationSec) * 100
			if pct > 100 {
				pct = 100
			}
			merged.ProgressPct = pct
		}
		if merged.ProgressPct < 92 {
			merged.ProgressPct = 100
		}
	} else {
		if merged.ProgressPct < 0 {
			merged.ProgressPct = 0
		} else if merged.ProgressPct > 100 {
			merged.ProgressPct = 100
		}
	}
	merged.UpdatedAt = now

	notify := !hasPrev || (!prev.Completed && merged.Completed)
	s.entries[mk] = merged
	return merged, notify, nil
}

func (s *HistoryStore) capUserEntriesLocked(userID int64, maxCount int) {
	if maxCount <= 0 {
		return
	}
	var userKeys []struct {
		mk string
		at time.Time
	}
	for mk, e := range s.entries {
		if e.UserID == userID {
			userKeys = append(userKeys, struct {
				mk string
				at time.Time
			}{mk: mk, at: e.UpdatedAt})
		}
	}
	if len(userKeys) <= maxCount {
		return
	}
	sort.Slice(userKeys, func(i, j int) bool {
		return userKeys[i].at.After(userKeys[j].at)
	})
	for _, drop := range userKeys[maxCount:] {
		delete(s.entries, drop.mk)
	}
}

// List returns recent watch history entries and deduplicated Continue Watching cards for userID.
func (s *HistoryStore) List(userID int64, limit int) (items []WatchEntry, continueWatching []WatchEntry) {
	if limit <= 0 {
		limit = 100
	}
	s.mu.Lock()
	var all []WatchEntry
	for _, e := range s.entries {
		if e.UserID == userID {
			all = append(all, e)
		}
	}
	s.mu.Unlock()

	sort.Slice(all, func(i, j int) bool {
		return all[i].UpdatedAt.After(all[j].UpdatedAt)
	})

	if len(all) > limit {
		items = append([]WatchEntry(nil), all[:limit]...)
	} else {
		items = append([]WatchEntry(nil), all...)
	}
	if items == nil {
		items = []WatchEntry{}
	}

	seenTitles := map[string]bool{}
	continueWatching = make([]WatchEntry, 0, 24)
	for _, e := range all {
		tk := e.Kind + ":" + e.TMDB
		if seenTitles[tk] {
			continue
		}
		seenTitles[tk] = true
		if e.Kind == "movie" {
			if e.Completed || e.PositionSec < 5 {
				continue
			}
		} else {
			if e.PositionSec < 5 && !e.Completed {
				continue
			}
		}
		continueWatching = append(continueWatching, e)
		if len(continueWatching) >= 24 {
			break
		}
	}
	return items, continueWatching
}

// ForTitle returns the latest watched entry and episode entries for a given movie or TV show.
func (s *HistoryStore) ForTitle(userID int64, kind, tmdb string) (latest *WatchEntry, episodes []WatchEntry) {
	if kind != "show" {
		kind = "movie"
	}
	tmdb = strings.TrimSpace(tmdb)
	s.mu.Lock()
	var matches []WatchEntry
	for _, e := range s.entries {
		if e.UserID == userID && e.Kind == kind && e.TMDB == tmdb {
			matches = append(matches, e)
		}
	}
	s.mu.Unlock()

	episodes = []WatchEntry{}
	if len(matches) == 0 {
		return nil, episodes
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].UpdatedAt.After(matches[j].UpdatedAt)
	})
	top := matches[0]
	latest = &top

	if kind == "show" {
		episodes = append([]WatchEntry(nil), matches...)
		sort.Slice(episodes, func(i, j int) bool {
			if episodes[i].Season != episodes[j].Season {
				return episodes[i].Season < episodes[j].Season
			}
			return episodes[i].Episode < episodes[j].Episode
		})
	}
	return latest, episodes
}

// Delete removes one watch history entry for userID.
func (s *HistoryStore) Delete(userID int64, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := storeMapKey(userID, strings.TrimSpace(key))
	entry, ok := s.entries[k]
	if !ok {
		return nil
	}
	delete(s.entries, k)
	if err := s.saveLocked(); err != nil {
		s.entries[k] = entry
		return err
	}
	return nil
}
