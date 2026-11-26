package discover

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/webserver"
	"github.com/HolyArrs/holyarrs/app/backend/internal/metadata"
	engine "github.com/HolyArrs/holyarrs/app/backend/internal/suggest"
)

// ExploreHubInfo describes one dedicated content category hub on Explore.
type ExploreHubInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Tagline string `json:"tagline"`
	Group   string `json:"group"`
	Accent  string `json:"accent"`
}

type hubRowSpec struct {
	ID       string
	Title    string
	Subtitle string
	Kind     string // movie or show
	Genres   string
	Without  string
	Lang     string
	Sort     string // "", "rating", "recent"
	RecentMo int
	MinVote  float64
	MinVotes int
}

type exploreHubDef struct {
	Info ExploreHubInfo
	Rows func(now time.Time) []hubRowSpec
}

var exploreHubs = []exploreHubDef{
	{
		Info: ExploreHubInfo{
			ID:      "anime",
			Name:    "Anime",
			Tagline: "Japanese anime shows, movies, shonen, isekai and classics",
			Group:   "spotlight",
			Accent:  "rose",
		},
		Rows: func(now time.Time) []hubRowSpec {
			return []hubRowSpec{
				{ID: "anime_trending", Title: "Trending anime", Subtitle: "Most popular Japanese animated shows right now", Kind: "show", Genres: "16", Lang: "ja", MinVotes: 30},
				{ID: "anime_airing", Title: "New & currently airing anime", Subtitle: "Recent seasons and latest hits", Kind: "show", Genres: "16", Lang: "ja", RecentMo: 14, MinVotes: 5},
				{ID: "anime_top", Title: "Top-rated anime", Subtitle: "Highest rated anime shows of all time", Kind: "show", Genres: "16", Lang: "ja", Sort: "rating", MinVote: 7.8, MinVotes: 250},
				{ID: "anime_movies", Title: "Anime movies", Subtitle: "Animated feature films from Japan", Kind: "movie", Genres: "16", Lang: "ja", MinVotes: 40},
				{ID: "anime_action", Title: "Action & shonen anime", Subtitle: "Battles, superpowers and adventure", Kind: "show", Genres: "16,10759", Lang: "ja", MinVotes: 20},
				{ID: "anime_fantasy", Title: "Fantasy, sci-fi & isekai", Subtitle: "Other worlds, magic and futuristic anime", Kind: "show", Genres: "16,10765", Lang: "ja", MinVotes: 20},
				{ID: "anime_comedy", Title: "Comedy & slice of life anime", Subtitle: "Lighthearted, funny and cozy anime", Kind: "show", Genres: "16,35", Lang: "ja", MinVotes: 15},
			}
		},
	},
	{
		Info: ExploreHubInfo{
			ID:      "kdrama",
			Name:    "K-Drama",
			Tagline: "Korean dramas, romance, thrillers and Korean cinema",
			Group:   "regional",
			Accent:  "violet",
		},
		Rows: func(now time.Time) []hubRowSpec {
			return []hubRowSpec{
				{ID: "kdrama_trending", Title: "Trending K-Dramas", Subtitle: "Popular Korean TV shows right now", Kind: "show", Lang: "ko", Without: "16,10763,10767", MinVotes: 15},
				{ID: "kdrama_new", Title: "New K-Dramas", Subtitle: "Latest Korean shows from the past year", Kind: "show", Lang: "ko", Without: "16,10763,10767", RecentMo: 14, MinVotes: 3},
				{ID: "kdrama_top", Title: "Top-rated K-Dramas", Subtitle: "Acclaimed Korean TV shows", Kind: "show", Lang: "ko", Without: "16,10763,10767", Sort: "rating", MinVote: 7.8, MinVotes: 80},
				{ID: "kdrama_romance", Title: "Romance & comedy K-Dramas", Subtitle: "Love stories and lighthearted hits", Kind: "show", Genres: "18|35", Without: "16,10763,10767", Lang: "ko", MinVotes: 10},
				{ID: "kdrama_thriller", Title: "Crime, mystery & action K-Dramas", Subtitle: "Tense Korean thrillers and action", Kind: "show", Genres: "80|9648|10759", Without: "16,10763,10767", Lang: "ko", MinVotes: 10},
				{ID: "kdrama_movies", Title: "Korean movies", Subtitle: "Critically acclaimed cinema from South Korea", Kind: "movie", Without: "16", Lang: "ko", MinVotes: 30},
			}
		},
	},
	{
		Info: ExploreHubInfo{
			ID:      "turkish",
			Name:    "Turkish",
			Tagline: "Turkish dizi, dramas, classics and Turkish cinema",
			Group:   "regional",
			Accent:  "red",
		},
		Rows: func(now time.Time) []hubRowSpec {
			return []hubRowSpec{
				{ID: "tr_shows", Title: "Popular Turkish shows", Subtitle: "Top Turkish dizi and dramas", Kind: "show", Lang: "tr", Without: "10763,10767", MinVotes: 5},
				{ID: "tr_new", Title: "New Turkish shows", Subtitle: "Recently premiered Turkish shows", Kind: "show", Lang: "tr", Without: "10763,10767", RecentMo: 18, MinVotes: 2},
				{ID: "tr_top", Title: "Top-rated Turkish shows", Subtitle: "Highest rated Turkish dramas and comedies", Kind: "show", Lang: "tr", Without: "10763,10767", Sort: "rating", MinVote: 7.0, MinVotes: 15},
				{ID: "tr_movies", Title: "Turkish movies", Subtitle: "Popular films from Turkey", Kind: "movie", Lang: "tr", MinVotes: 15},
			}
		},
	},
	{
		Info: ExploreHubInfo{
			ID:      "asian-drama",
			Name:    "C-Drama & J-Drama",
			Tagline: "Chinese, Japanese and Thai live-action dramas and films",
			Group:   "regional",
			Accent:  "amber",
		},
		Rows: func(now time.Time) []hubRowSpec {
			return []hubRowSpec{
				{ID: "cdrama_shows", Title: "Popular C-Dramas", Subtitle: "Chinese dramas, historicals and romance", Kind: "show", Lang: "zh", Without: "16,10763,10767", MinVotes: 5},
				{ID: "jdrama_shows", Title: "Popular J-Dramas", Subtitle: "Japanese live-action dramas and thrillers", Kind: "show", Lang: "ja", Without: "16,10763,10767", MinVotes: 10},
				{ID: "thai_shows", Title: "Thai & Southeast Asian shows", Subtitle: "Popular dramas from Thailand", Kind: "show", Lang: "th", Without: "16,10763,10767", MinVotes: 3},
				{ID: "asian_movies_zh", Title: "Chinese cinema", Subtitle: "Action, wuxia and drama films", Kind: "movie", Lang: "zh", Without: "16", MinVotes: 25},
				{ID: "asian_movies_ja", Title: "Japanese live-action cinema", Subtitle: "Drama, thriller and award-winning films", Kind: "movie", Lang: "ja", Without: "16", MinVotes: 35},
			}
		},
	},
	{
		Info: ExploreHubInfo{
			ID:      "animation",
			Name:    "Cartoons & Animation",
			Tagline: "Western animated films, shows and family animation",
			Group:   "format",
			Accent:  "cyan",
		},
		Rows: func(now time.Time) []hubRowSpec {
			return []hubRowSpec{
				{ID: "anim_movies", Title: "Animated feature films", Subtitle: "Popular non-anime animated movies", Kind: "movie", Genres: "16", Lang: "en", MinVotes: 60},
				{ID: "anim_shows", Title: "Animated shows", Subtitle: "Adult animation and cartoon classics", Kind: "show", Genres: "16", Lang: "en", MinVotes: 30},
				{ID: "anim_top", Title: "Top-rated animation", Subtitle: "Critically acclaimed animated films", Kind: "movie", Genres: "16", Lang: "en", Sort: "rating", MinVote: 7.4, MinVotes: 300},
				{ID: "anim_family", Title: "Family & kids animation", Subtitle: "Animated adventures for everyone", Kind: "movie", Genres: "16,10751", MinVotes: 50},
			}
		},
	},
	{
		Info: ExploreHubInfo{
			ID:      "documentary",
			Name:    "Documentaries",
			Tagline: "Documentary films, docushows, true crime and nature",
			Group:   "format",
			Accent:  "emerald",
		},
		Rows: func(now time.Time) []hubRowSpec {
			return []hubRowSpec{
				{ID: "doc_movies", Title: "Popular documentary films", Subtitle: "Non-fiction features and investigations", Kind: "movie", Genres: "99", MinVotes: 30},
				{ID: "doc_shows", Title: "Documentary shows", Subtitle: "Limited docushows and investigative TV", Kind: "show", Genres: "99", MinVotes: 15},
				{ID: "doc_top", Title: "Top-rated documentaries", Subtitle: "Highest rated documentary features", Kind: "movie", Genres: "99", Sort: "rating", MinVote: 7.6, MinVotes: 120},
				{ID: "doc_crime", Title: "True crime & history docs", Subtitle: "Historical chronicles and crime studies", Kind: "show", Genres: "99,80|10768", MinVotes: 10},
			}
		},
	},
	{
		Info: ExploreHubInfo{
			ID:      "indian",
			Name:    "Bollywood & Indian",
			Tagline: "Hindi, Tamil and Telugu cinema and Indian web shows",
			Group:   "regional",
			Accent:  "orange",
		},
		Rows: func(now time.Time) []hubRowSpec {
			return []hubRowSpec{
				{ID: "in_hindi_movies", Title: "Popular Bollywood & Hindi movies", Subtitle: "Top Hindi language films", Kind: "movie", Lang: "hi", MinVotes: 25},
				{ID: "in_new_movies", Title: "New Indian releases", Subtitle: "Recent hits from India", Kind: "movie", Lang: "hi", RecentMo: 18, MinVotes: 10},
				{ID: "in_shows", Title: "Indian web shows", Subtitle: "Crime, drama and thriller shows", Kind: "show", Lang: "hi", Without: "10763,10767", MinVotes: 10},
				{ID: "in_tamil", Title: "Tamil cinema", Subtitle: "Popular Tamil language films", Kind: "movie", Lang: "ta", MinVotes: 15},
				{ID: "in_telugu", Title: "Telugu cinema", Subtitle: "Popular Telugu language films", Kind: "movie", Lang: "te", MinVotes: 15},
			}
		},
	},
	{
		Info: ExploreHubInfo{
			ID:      "scifi",
			Name:    "Sci-Fi & Fantasy",
			Tagline: "Space epics, cyberpunk, dystopia and fantasy worlds",
			Group:   "genre",
			Accent:  "indigo",
		},
		Rows: func(now time.Time) []hubRowSpec {
			return []hubRowSpec{
				{ID: "sf_movies", Title: "Popular sci-fi movies", Kind: "movie", Genres: "878", MinVotes: 80},
				{ID: "sf_shows", Title: "Sci-fi & fantasy shows", Kind: "show", Genres: "10765", MinVotes: 40},
				{ID: "sf_fantasy_movies", Title: "Fantasy movies", Kind: "movie", Genres: "14", MinVotes: 80},
				{ID: "sf_top", Title: "Top-rated sci-fi classics", Kind: "movie", Genres: "878", Sort: "rating", MinVote: 7.5, MinVotes: 500},
			}
		},
	},
	{
		Info: ExploreHubInfo{
			ID:      "horror",
			Name:    "Horror & Thriller",
			Tagline: "Supernatural horror, psychological thrillers and suspense",
			Group:   "genre",
			Accent:  "crimson",
		},
		Rows: func(now time.Time) []hubRowSpec {
			return []hubRowSpec{
				{ID: "hor_movies", Title: "Popular horror movies", Kind: "movie", Genres: "27", MinVotes: 60},
				{ID: "thr_movies", Title: "Edge-of-your-seat thrillers", Kind: "movie", Genres: "53", MinVotes: 80},
				{ID: "hor_new", Title: "Recent horror & suspense", Kind: "movie", Genres: "27|53", RecentMo: 18, MinVotes: 30},
				{ID: "hor_top", Title: "Top-rated horror classics", Kind: "movie", Genres: "27", Sort: "rating", MinVote: 7.3, MinVotes: 400},
			}
		},
	},
	{
		Info: ExploreHubInfo{
			ID:      "action",
			Name:    "Action & Adventure",
			Tagline: "Blockbusters, martial arts, heist and spy thrillers",
			Group:   "genre",
			Accent:  "blue",
		},
		Rows: func(now time.Time) []hubRowSpec {
			return []hubRowSpec{
				{ID: "act_movies", Title: "Popular action movies", Kind: "movie", Genres: "28", MinVotes: 100},
				{ID: "act_shows", Title: "Action & adventure shows", Kind: "show", Genres: "10759", MinVotes: 40},
				{ID: "act_adv", Title: "Epic adventures", Kind: "movie", Genres: "12", MinVotes: 100},
				{ID: "act_top", Title: "Top-rated action hits", Kind: "movie", Genres: "28", Sort: "rating", MinVote: 7.5, MinVotes: 500},
			}
		},
	},
	{
		Info: ExploreHubInfo{
			ID:      "comedy",
			Name:    "Comedy",
			Tagline: "Stand-out comedies, sitcoms and romantic comedies",
			Group:   "genre",
			Accent:  "yellow",
		},
		Rows: func(now time.Time) []hubRowSpec {
			return []hubRowSpec{
				{ID: "com_movies", Title: "Popular comedy movies", Kind: "movie", Genres: "35", Without: "16", MinVotes: 80},
				{ID: "com_shows", Title: "Comedy shows & sitcoms", Kind: "show", Genres: "35", Without: "16,10762", MinVotes: 40},
				{ID: "com_rom", Title: "Romantic comedies", Kind: "movie", Genres: "35,10749", MinVotes: 60},
				{ID: "com_top", Title: "Top-rated comedies", Kind: "movie", Genres: "35", Sort: "rating", MinVote: 7.4, MinVotes: 400},
			}
		},
	},
	{
		Info: ExploreHubInfo{
			ID:      "crime",
			Name:    "Crime & Mystery",
			Tagline: "Gritty crime dramas, whodunits and noir",
			Group:   "genre",
			Accent:  "slate",
		},
		Rows: func(now time.Time) []hubRowSpec {
			return []hubRowSpec{
				{ID: "cri_movies", Title: "Crime & noir films", Kind: "movie", Genres: "80", MinVotes: 80},
				{ID: "cri_shows", Title: "Crime & detective shows", Kind: "show", Genres: "80", Without: "16", MinVotes: 40},
				{ID: "cri_mys", Title: "Mystery thrillers", Kind: "movie", Genres: "9648", MinVotes: 60},
				{ID: "cri_top", Title: "Top-rated crime classics", Kind: "movie", Genres: "80", Sort: "rating", MinVote: 7.7, MinVotes: 400},
			}
		},
	},
}

type hubCacheEntry struct {
	at   time.Time
	rows []engine.Row
}

var (
	hubCacheMu sync.Mutex
	hubCache   = map[string]hubCacheEntry{}
)

func findExploreHub(id string) (exploreHubDef, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, h := range exploreHubs {
		if h.Info.ID == id {
			return h, true
		}
	}
	return exploreHubDef{}, false
}

// ListExploreHubs answers GET /api/v1/explore/hubs with the curated category hub metadata.
func (h *Handlers) listExploreHubs(w http.ResponseWriter, r *http.Request) {
	out := make([]ExploreHubInfo, 0, len(exploreHubs))
	for _, hub := range exploreHubs {
		out = append(out, hub.Info)
	}
	h.Caching(w, r, "private, max-age=3600")
	webserver.WriteJSON(w, http.StatusOK, map[string]any{"hubs": out})
}

// exploreHub answers GET /api/v1/explore/hub?hub=<id>: curated shelves and hero for one dedicated category hub.
func (h *Handlers) exploreHub(w http.ResponseWriter, r *http.Request) {
	hubID := strings.TrimSpace(r.URL.Query().Get("hub"))
	def, ok := findExploreHub(hubID)
	if !ok {
		webserver.Fail(w, http.StatusNotFound, "unknown explore category hub")
		return
	}
	if h.KidLimited(w, r) {
		return
	}
	if h.Cache != nil && !h.Cache.Allow(h.UserID(r), time.Now()) {
		w.Header().Set("Retry-After", "60")
		webserver.Fail(w, http.StatusTooManyRequests, "too many Explore requests; try again in a minute")
		return
	}

	now := time.Now().UTC()
	if h.Explore != nil {
		now = h.Explore.Now().UTC()
	}
	kidMax := h.KidMax(r)
	kid := kidMax != ""
	rawRows := h.loadHubRows(r.Context(), def, kid, now)

	// Deduplicate titles across shelves within the hub (first row wins), capping each row at RowMax.
	seen := map[string]bool{}
	deduped := make([]engine.Row, 0, len(rawRows))
	for _, row := range rawRows {
		keep := make([]engine.Item, 0, engine.RowMax)
		for _, it := range row.Items {
			k := it.Key()
			if seen[k] {
				continue
			}
			keep = append(keep, it)
			if len(keep) >= engine.RowMax {
				break
			}
		}
		for _, it := range keep {
			seen[it.Key()] = true
		}
		if len(keep) > 0 {
			row.Items = keep
			deduped = append(deduped, row)
		}
	}

	views, err := h.ExploreViews(r.Context(), r, deduped)
	if err != nil {
		webserver.Internal(w, "discover", err)
		return
	}

	hero := make([]ItemView, 0, 6)
	seenHero := map[string]bool{}
	for _, vrow := range views {
		for _, it := range vrow.Items {
			if it.Backdrop == "" {
				continue
			}
			k := it.Kind + ":" + it.TMDBID
			if seenHero[k] {
				continue
			}
			seenHero[k] = true
			hero = append(hero, it)
			if len(hero) >= 6 {
				break
			}
		}
		if len(hero) >= 6 {
			break
		}
	}

	if h.FillTrailerKeys != nil {
		h.FillTrailerKeys(r, hero)
		for i := range views {
			h.FillTrailerKeys(r, views[i].Items)
		}
	}
	if h.FillCardArt != nil {
		h.FillCardArt(r, hero)
		h.FillRowsCardArt(r, views)
	}
	h.fillStreamingRows(r, views)

	h.Caching(w, r, "private, max-age=300")
	webserver.WriteJSON(w, http.StatusOK, map[string]any{
		"hub":  def.Info,
		"hero": hero,
		"rows": views,
	})
}

func (h *Handlers) loadHubRows(ctx context.Context, def exploreHubDef, kid bool, now time.Time) []engine.Row {
	kidKey := "0"
	if kid {
		kidKey = "1"
	}
	cKey := def.Info.ID + "|" + kidKey
	hubCacheMu.Lock()
	if ent, ok := hubCache[cKey]; ok && time.Since(ent.at) < 30*time.Minute {
		hubCacheMu.Unlock()
		return ent.rows
	}
	hubCacheMu.Unlock()

	specs := def.Rows(now)
	var listFn func(context.Context, engine.Request) ([]engine.Item, error)
	if h.Explore != nil {
		src := h.LiveExplore(ctx)
		listFn = src.List
	} else if l, ok := metadata.ExploreListerFor(ctx, h.Metadata); ok {
		src := ListerSource{L: l}
		listFn = src.List
	} else {
		return nil
	}
	results := make([][]engine.Item, len(specs))

	var wg sync.WaitGroup
	for i, sp := range specs {
		wg.Add(1)
		go func(idx int, spec hubRowSpec) {
			defer wg.Done()
			q := engine.Request{
				List:     "discover",
				Kind:     spec.Kind,
				Genres:   spec.Genres,
				Without:  spec.Without,
				Lang:     spec.Lang,
				Sort:     spec.Sort,
				MinVote:  spec.MinVote,
				MinVotes: spec.MinVotes,
				Scope:    engine.ScopeShared,
			}
			if spec.RecentMo > 0 {
				q.From = now.AddDate(0, -spec.RecentMo, 0).Format("2006-01-02")
			}
			if kid {
				const kw = "27|80|53|10752|10768"
				if q.Without == "" {
					q.Without = kw
				} else {
					q.Without = q.Without + "|" + kw
				}
				if q.Kind == "movie" {
					q.Cert = "PG"
				}
			}
			items, err := listFn(ctx, q)
			if err != nil {
				return
			}
			if len(items) == 0 && q.MinVotes > 5 {
				// Retry with a relaxed vote floor so niche catalogs stay populated.
				q.MinVotes = 0
				q.MinVote = 0
				if retry, rerr := listFn(ctx, q); rerr == nil {
					items = retry
				}
			}
			sort.SliceStable(items, func(a, b int) bool {
				if spec.Sort == "rating" {
					if items[a].Vote != items[b].Vote {
						return items[a].Vote > items[b].Vote
					}
				}
				return items[a].Pop > items[b].Pop
			})
			results[idx] = items
		}(i, sp)
	}
	wg.Wait()

	rows := make([]engine.Row, 0, len(specs))
	for i, sp := range specs {
		if len(results[i]) == 0 {
			continue
		}
		rows = append(rows, engine.Row{
			ID:       sp.ID,
			Type:     "browse",
			Title:    sp.Title,
			Subtitle: sp.Subtitle,
			Wide:     true,
			Items:    results[i],
		})
	}
	if len(rows) > 0 {
		hubCacheMu.Lock()
		hubCache[cKey] = hubCacheEntry{at: time.Now(), rows: rows}
		hubCacheMu.Unlock()
	}
	return rows
}

// ClearExploreHubCache drops cached hub rows (used by tests).
func ClearExploreHubCache() {
	hubCacheMu.Lock()
	hubCache = map[string]hubCacheEntry{}
	hubCacheMu.Unlock()
}

// HubItemKindsAllowed checks whether an Explore item uses a valid media kind.
func HubItemKindsAllowed(it metadata.DiscoverItem) bool {
	return it.Kind == "movie" || it.Kind == "show"
}
