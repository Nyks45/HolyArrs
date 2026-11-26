package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/outbound"
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/secrets"
	"github.com/HolyArrs/holyarrs/app/backend/internal/metadata"
)

const (
	syncSecretGroup = "links"
	// Default public Simkl Client ID used unless the user sets their own.
	defaultSimklClientID = "a2ef25e32b5e67d39b8144a064962b6a9a23f8e7045d54fd07314ef57b4a269a"
)

func syncSecretKey(key string) string {
	return "stream_sync_" + key
}

// SyncStatus summarizes Trakt and Simkl connection states for the settings and player UIs.
type SyncStatus struct {
	TraktConnected  bool   `json:"trakt_connected"`
	SimklConnected  bool   `json:"simkl_connected"`
	SimklUsername   string `json:"simkl_username,omitempty"`
	SimklClientID   string `json:"simkl_client_id,omitempty"`
	ScrobbleTrakt   bool   `json:"scrobble_trakt"`
	ScrobbleSimkl   bool   `json:"scrobble_simkl"`
}

// SimklPINChallenge holds the short-lived device code returned by Simkl's PIN flow.
type SimklPINChallenge struct {
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// ScrobbleService handles Trakt and Simkl scrobbling, Simkl PIN linking, and
// pulling remote playback progress into HistoryStore.
type ScrobbleService struct {
	mu        sync.RWMutex
	vault     *secrets.Vault
	trakt     *metadata.TraktAuth
	prefs     *PrefsStore
	client    *http.Client
	simklBase string
}

// NewScrobbleService constructs a ScrobbleService with SSRF-safe HTTP transport.
func NewScrobbleService(vault *secrets.Vault, trakt *metadata.TraktAuth, prefs *PrefsStore) *ScrobbleService {
	tr := &http.Transport{
		DialContext:         outbound.PolicyDialer(outbound.NotMetadata).DialContext,
		MaxIdleConns:        16,
		IdleConnTimeout:     60 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	return &ScrobbleService{
		vault:     vault,
		trakt:     trakt,
		prefs:     prefs,
		client:    &http.Client{Transport: tr, Timeout: 10 * time.Second},
		simklBase: "https://api.simkl.com",
	}
}

// SetExtras attaches secrets Vault and TraktAuth after startup construction.
func (s *ScrobbleService) SetExtras(v *secrets.Vault, t *metadata.TraktAuth) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if v != nil {
		s.vault = v
	}
	if t != nil {
		s.trakt = t
	}
	s.mu.Unlock()
}

// SetTestEndpoints overrides Simkl base URL and HTTP client for unit tests.
func (s *ScrobbleService) SetTestEndpoints(hc *http.Client, simklBase string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if hc != nil {
		s.client = hc
	}
	if simklBase != "" {
		s.simklBase = strings.TrimRight(simklBase, "/")
	}
}

func (s *ScrobbleService) getSetting(ctx context.Context, key string) string {
	s.mu.RLock()
	v := s.vault
	s.mu.RUnlock()
	if v == nil {
		return ""
	}
	val, err := v.Setting(ctx, syncSecretGroup, syncSecretKey(key))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(val)
}

func (s *ScrobbleService) putSetting(ctx context.Context, key, val string, by database.Actor) error {
	s.mu.RLock()
	v := s.vault
	s.mu.RUnlock()
	if v == nil {
		return errors.New("secret vault unavailable")
	}
	if by.Kind == "" {
		by = database.System
	}
	return v.SetSetting(ctx, syncSecretGroup, syncSecretKey(key), val, by)
}

func (s *ScrobbleService) simklClientID(ctx context.Context) string {
	if cid := s.getSetting(ctx, "simkl_client_id"); cid != "" {
		return cid
	}
	return defaultSimklClientID
}

// Status inspects whether Trakt and Simkl tokens are connected.
func (s *ScrobbleService) Status(ctx context.Context) SyncStatus {
	s.mu.RLock()
	t := s.trakt
	prefs := s.prefs
	s.mu.RUnlock()

	st := SyncStatus{
		SimklUsername: s.getSetting(ctx, "simkl_user"),
	}
	if st.SimklUsername != "" || s.getSetting(ctx, "simkl_token") != "" {
		st.SimklConnected = true
	}
	if cid := s.getSetting(ctx, "simkl_client_id"); cid != "" {
		st.SimklClientID = maskSecretKey(cid)
	}
	if prefs != nil {
		p := prefs.Get()
		st.ScrobbleTrakt = p.ScrobbleTrakt
		st.ScrobbleSimkl = p.ScrobbleSimkl
	}
	if t != nil {
		if _, tok, err := t.Access(ctx); err == nil && tok != "" {
			st.TraktConnected = true
		}
	}
	return st
}

// StartSimklPIN starts Simkl device/PIN OAuth linking.
func (s *ScrobbleService) StartSimklPIN(ctx context.Context, customClientID string, by database.Actor) (SimklPINChallenge, error) {
	customClientID = strings.TrimSpace(customClientID)
	if customClientID != "" {
		if err := s.putSetting(ctx, "simkl_client_id", customClientID, by); err != nil {
			return SimklPINChallenge{}, err
		}
	}
	cid := s.simklClientID(ctx)
	s.mu.RLock()
	base, hc := s.simklBase, s.client
	s.mu.RUnlock()

	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, base+"/oauth/pin?client_id="+url.QueryEscape(cid), nil)
	if err != nil {
		return SimklPINChallenge{}, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return SimklPINChallenge{}, errors.New("could not reach Simkl")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return SimklPINChallenge{}, fmt.Errorf("Simkl PIN request failed (HTTP %d)", resp.StatusCode)
	}
	var out SimklPINChallenge
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return SimklPINChallenge{}, err
	}
	if out.UserCode == "" {
		return SimklPINChallenge{}, errors.New("Simkl did not return a user code")
	}
	return out, nil
}

// PollSimklPIN checks whether the user approved code on Simkl and stores the token encrypted.
func (s *ScrobbleService) PollSimklPIN(ctx context.Context, userCode string, by database.Actor) (bool, string, error) {
	userCode = strings.TrimSpace(userCode)
	if userCode == "" {
		return false, "", errors.New("user code is required")
	}
	cid := s.simklClientID(ctx)
	s.mu.RLock()
	base, hc := s.simklBase, s.client
	s.mu.RUnlock()

	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, fmt.Sprintf("%s/oauth/pin/%s?client_id=%s", base, url.PathEscape(userCode), url.QueryEscape(cid)), nil)
	if err != nil {
		return false, "", err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()
	var parsed struct {
		Result      string `json:"result"`
		AccessToken string `json:"access_token"`
		Message     string `json:"message"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&parsed); err != nil {
		return false, "", err
	}
	if parsed.Result != "OK" || parsed.AccessToken == "" {
		return false, "", nil
	}
	if err := s.putSetting(ctx, "simkl_token", parsed.AccessToken, by); err != nil {
		return false, "", err
	}
	username := s.fetchSimklUsername(ctx, base, hc, cid, parsed.AccessToken)
	_ = s.putSetting(ctx, "simkl_user", username, by)
	return true, username, nil
}

// DisconnectSimkl clears stored Simkl credentials from the Vault.
func (s *ScrobbleService) DisconnectSimkl(ctx context.Context, by database.Actor) error {
	if err := s.putSetting(ctx, "simkl_token", "", by); err != nil {
		return err
	}
	return s.putSetting(ctx, "simkl_user", "", by)
}

func (s *ScrobbleService) fetchSimklUsername(ctx context.Context, base string, hc *http.Client, cid, tok string) string {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, base+"/users/settings", nil)
	if err != nil {
		return "Simkl User"
	}
	req.Header.Set("simkl-api-key", cid)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := hc.Do(req)
	if err != nil || resp.StatusCode >= 300 {
		if resp != nil {
			resp.Body.Close()
		}
		return "Simkl User"
	}
	defer resp.Body.Close()
	var parsed struct {
		User struct {
			Name string `json:"name"`
		} `json:"user"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 256<<10)).Decode(&parsed)
	return fallbackString(parsed.User.Name, "Simkl User")
}

// Scrobble posts playback start/pause/stop telemetry to enabled providers (Trakt & Simkl).
func (s *ScrobbleService) Scrobble(ctx context.Context, action, kind string, tmdbID int, imdbID string, season, episode int, progressPct float64) []string {
	switch action {
	case "start", "pause", "stop":
	default:
		return nil
	}
	if progressPct < 0 {
		progressPct = 0
	} else if progressPct > 100 {
		progressPct = 100
	}

	ids := map[string]any{}
	if tmdbID > 0 {
		ids["tmdb"] = tmdbID
	}
	if imdbID != "" {
		ids["imdb"] = imdbID
	}
	if len(ids) == 0 {
		return nil
	}

	var traktBody map[string]any
	var simklBody map[string]any
	if kind == "show" && season > 0 && episode > 0 {
		traktBody = map[string]any{
			"show":     map[string]any{"ids": ids},
			"episode":  map[string]any{"season": season, "number": episode},
			"progress": progressPct,
		}
		simklBody = map[string]any{
			"shows":    []any{map[string]any{"ids": ids}},
			"season":   season,
			"episode":  episode,
			"progress": progressPct,
		}
	} else {
		traktBody = map[string]any{
			"movie":    map[string]any{"ids": ids},
			"progress": progressPct,
		}
		simklBody = map[string]any{
			"movie":    map[string]any{"ids": ids},
			"progress": progressPct,
		}
	}

	s.mu.RLock()
	trakt := s.trakt
	prefs := s.prefs
	base, hc := s.simklBase, s.client
	s.mu.RUnlock()

	wantTrakt, wantSimkl := true, true
	if prefs != nil {
		p := prefs.Get()
		wantTrakt = p.ScrobbleTrakt
		wantSimkl = p.ScrobbleSimkl
	}

	var sent []string
	if wantTrakt && trakt != nil {
		if _, tok, err := trakt.Access(ctx); err == nil && tok != "" {
			if err := trakt.PostJSON(ctx, "/scrobble/"+action, traktBody, nil); err == nil {
				sent = append(sent, "trakt")
			}
		}
	}
	if wantSimkl {
		if simklTok := s.getSetting(ctx, "simkl_token"); simklTok != "" {
			if err := s.postSimkl(ctx, base, hc, s.simklClientID(ctx), simklTok, "/scrobble/"+action, simklBody, nil); err == nil {
				sent = append(sent, "simkl")
			}
		}
	}
	return sent
}

func (s *ScrobbleService) postSimkl(ctx context.Context, base string, hc *http.Client, cid, tok, path string, body, into any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, base+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("simkl-api-key", cid)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("Simkl HTTP %d", resp.StatusCode)
	}
	if into != nil {
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(into)
	}
	return nil
}

// PullProgress imports newer playback progress entries from Trakt and Simkl into HistoryStore.
func (s *ScrobbleService) PullProgress(ctx context.Context, history *HistoryStore, userID int64) (int, error) {
	if history == nil {
		return 0, errors.New("watch history unavailable")
	}
	existingList, _ := history.List(userID, 250)
	existingByKey := map[string]WatchEntry{}
	for _, e := range existingList {
		existingByKey[e.Key] = e
	}

	var incoming []WatchEntry
	s.mu.RLock()
	trakt := s.trakt
	base, hc := s.simklBase, s.client
	s.mu.RUnlock()

	if trakt != nil {
		if _, tok, err := trakt.Access(ctx); err == nil && tok != "" {
			incoming = append(incoming, s.fetchTraktPlayback(ctx, trakt)...)
		}
	}
	if simklTok := s.getSetting(ctx, "simkl_token"); simklTok != "" {
		incoming = append(incoming, s.fetchSimklPlayback(ctx, base, hc, s.simklClientID(ctx), simklTok)...)
	}

	merged := 0
	for _, ent := range incoming {
		if ent.TMDB == "" || ent.ProgressPct <= 0 {
			continue
		}
		if cur, ok := existingByKey[ent.Key]; ok {
			if !ent.UpdatedAt.After(cur.UpdatedAt) {
				continue
			}
		}
		if _, err := history.Upsert(userID, ent); err == nil {
			existingByKey[ent.Key] = ent
			merged++
		}
	}
	return merged, nil
}

func (s *ScrobbleService) fetchTraktPlayback(ctx context.Context, trakt *metadata.TraktAuth) []WatchEntry {
	var raw []struct {
		Type     string  `json:"type"`
		Progress float64 `json:"progress"`
		PausedAt string  `json:"paused_at"`
		Movie    *struct {
			Title string `json:"title"`
			Year  int    `json:"year"`
			IDs   struct {
				TMDB int    `json:"tmdb"`
				IMDb string `json:"imdb"`
			} `json:"ids"`
		} `json:"movie"`
		Show *struct {
			Title string `json:"title"`
			Year  int    `json:"year"`
			IDs   struct {
				TMDB int    `json:"tmdb"`
				IMDb string `json:"imdb"`
			} `json:"ids"`
		} `json:"show"`
		Episode *struct {
			Season int    `json:"season"`
			Number int    `json:"number"`
			Title  string `json:"title"`
		} `json:"episode"`
	}
	if err := trakt.Read(ctx, "/sync/playback?limit=40", &raw); err != nil {
		return nil
	}
	var out []WatchEntry
	for _, item := range raw {
		ts, _ := time.Parse(time.RFC3339, item.PausedAt)
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		if item.Type == "movie" && item.Movie != nil && item.Movie.IDs.TMDB > 0 {
			tmdb := strconv.Itoa(item.Movie.IDs.TMDB)
			out = append(out, WatchEntry{
				Key:         WatchKey("movie", tmdb, 0, 0),
				Kind:        "movie",
				TMDB:        tmdb,
				IMDb:        item.Movie.IDs.IMDb,
				Title:       item.Movie.Title,
				Year:        item.Movie.Year,
				ProgressPct: item.Progress,
				SourceName:  "Trakt Sync",
				UpdatedAt:   ts,
			})
		} else if item.Type == "episode" && item.Show != nil && item.Episode != nil && item.Show.IDs.TMDB > 0 {
			tmdb := strconv.Itoa(item.Show.IDs.TMDB)
			out = append(out, WatchEntry{
				Key:         WatchKey("show", tmdb, item.Episode.Season, item.Episode.Number),
				Kind:        "show",
				TMDB:        tmdb,
				IMDb:        item.Show.IDs.IMDb,
				Title:       item.Show.Title,
				Year:        item.Show.Year,
				Season:      item.Episode.Season,
				Episode:     item.Episode.Number,
				EpisodeName: item.Episode.Title,
				ProgressPct: item.Progress,
				SourceName:  "Trakt Sync",
				UpdatedAt:   ts,
			})
		}
	}
	return out
}

func (s *ScrobbleService) fetchSimklPlayback(ctx context.Context, base string, hc *http.Client, cid, tok string) []WatchEntry {
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, base+"/sync/playback", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("simkl-api-key", cid)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := hc.Do(req)
	if err != nil || resp.StatusCode >= 300 {
		if resp != nil {
			resp.Body.Close()
		}
		return nil
	}
	defer resp.Body.Close()
	var raw []struct {
		Type     string  `json:"type"`
		Progress float64 `json:"progress"`
		PausedAt string  `json:"paused_at"`
		Movie    *struct {
			Title string `json:"title"`
			Year  int    `json:"year"`
			IDs   struct {
				TMDB string `json:"tmdb"`
				IMDb string `json:"imdb"`
			} `json:"ids"`
		} `json:"movie"`
		Show *struct {
			Title string `json:"title"`
			Year  int    `json:"year"`
			IDs   struct {
				TMDB string `json:"tmdb"`
				IMDb string `json:"imdb"`
			} `json:"ids"`
		} `json:"show"`
		Episode *struct {
			Season int    `json:"season"`
			Number int    `json:"episode"`
			Title  string `json:"title"`
		} `json:"episode"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&raw) != nil {
		return nil
	}
	var out []WatchEntry
	for _, item := range raw {
		ts, _ := time.Parse(time.RFC3339, item.PausedAt)
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		if item.Type == "movie" && item.Movie != nil && item.Movie.IDs.TMDB != "" {
			tmdb := strings.TrimSpace(item.Movie.IDs.TMDB)
			out = append(out, WatchEntry{
				Key:         WatchKey("movie", tmdb, 0, 0),
				Kind:        "movie",
				TMDB:        tmdb,
				IMDb:        item.Movie.IDs.IMDb,
				Title:       item.Movie.Title,
				Year:        item.Movie.Year,
				ProgressPct: item.Progress,
				SourceName:  "Simkl Sync",
				UpdatedAt:   ts,
			})
		} else if item.Type == "episode" && item.Show != nil && item.Episode != nil && item.Show.IDs.TMDB != "" {
			tmdb := strings.TrimSpace(item.Show.IDs.TMDB)
			out = append(out, WatchEntry{
				Key:         WatchKey("show", tmdb, item.Episode.Season, item.Episode.Number),
				Kind:        "show",
				TMDB:        tmdb,
				IMDb:        item.Show.IDs.IMDb,
				Title:       item.Show.Title,
				Year:        item.Show.Year,
				Season:      item.Episode.Season,
				Episode:     item.Episode.Number,
				EpisodeName: item.Episode.Title,
				ProgressPct: item.Progress,
				SourceName:  "Simkl Sync",
				UpdatedAt:   ts,
			})
		}
	}
	return out
}
