package main

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/jobs"
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/parts"
	"github.com/HolyArrs/holyarrs/app/backend/internal/discover"
	"github.com/HolyArrs/holyarrs/app/backend/internal/health"
	"github.com/HolyArrs/holyarrs/app/backend/internal/metadata"
	"github.com/HolyArrs/holyarrs/app/backend/internal/music"
	engine "github.com/HolyArrs/holyarrs/app/backend/internal/suggest"
)

// Wiring for Explore (internal/discover) and the trailers
// (internal/metadata): the logic lives in those modules, this file only
// hands them this API's store, provider and the other modules' funcs.

// wireExplore points the pool job and the card art filler at this API's
// store, provider and rating lookups (New).
func (a *handlers) wireExplore() {
	e := &a.explore
	a.viewSignals = discover.NewViewSignalStore(a.s.ConfigDir)
	e.Cards.Store = func() *database.Store { return a.s.Store }
	e.Cards.Metadata = func() metadata.Provider { return a.s.Metadata }
	e.Store = e.Cards.Store
	e.Metadata = e.Cards.Metadata
	e.KnownRating = a.knownRating
	e.LookupRating = a.lookupRating
	e.QueueCardArt = e.Cards.QueuePool
	e.LookupsOff = func() bool { return a.s.MetadataModes != nil && a.s.MetadataModes.Current() == metadata.ModeOff }
	e.ViewSignals = a.viewSignals
	if a.s.Streamer != nil && a.s.Streamer.History != nil {
		hist := a.s.Streamer.History
		e.WatchSignals = func(userID int64) []discover.WatchSignal {
			items, _ := hist.List(userID, 200)
			if len(items) == 0 {
				return nil
			}
			type agg struct {
				sig discover.WatchSignal
				eps map[[2]int]bool
			}
			by := map[string]*agg{}
			for _, it := range items {
				kind := "movie"
				if it.Kind == "show" {
					kind = "show"
				}
				tmdb := strings.TrimSpace(it.TMDB)
				if tmdb == "" {
					continue
				}
				key := kind + ":" + tmdb
				cur, ok := by[key]
				if !ok {
					cur = &agg{
						sig: discover.WatchSignal{
							TMDBID:    tmdb,
							Kind:      kind,
							Title:     it.Title,
							Year:      it.Year,
							Completed: it.Completed,
							UpdatedAt: it.UpdatedAt,
						},
						eps: map[[2]int]bool{},
					}
					by[key] = cur
				} else {
					if it.UpdatedAt.After(cur.sig.UpdatedAt) {
						cur.sig.UpdatedAt = it.UpdatedAt
					}
					if cur.sig.Title == "" && it.Title != "" {
						cur.sig.Title = it.Title
					}
					if cur.sig.Year == 0 && it.Year > 0 {
						cur.sig.Year = it.Year
					}
					if it.Completed {
						cur.sig.Completed = true
					}
				}
				if kind == "show" && it.Episode > 0 {
					cur.eps[[2]int{it.Season, it.Episode}] = true
				}
			}
			out := make([]discover.WatchSignal, 0, len(by))
			for _, cur := range by {
				cur.sig.Episodes = len(cur.eps)
				out = append(out, cur.sig)
			}
			sort.Slice(out, func(i, j int) bool {
				if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
					return out[i].UpdatedAt.After(out[j].UpdatedAt)
				}
				return out[i].Kind+":"+out[i].TMDBID < out[j].Kind+":"+out[j].TMDBID
			})
			return out
		}
		hist.OnWatchTitle = func(userID int64, _, _ string) {
			if userID > 0 && a.s.Store != nil {
				e.MarkMemberStale(context.Background(), userID, e.Now())
			}
		}
	}
}

// StartExplore starts the Explore pool job (discover.Explore) and the
// warmers that go with it.
func (a *handlers) StartExplore(ctx context.Context) {
	if !a.explore.Start(ctx) {
		return
	}
	a.uiHandlers().ApplyArtworkCap(ctx) // the picture cache size from Settings
	a.s.Store.OnEvent(a.mh().OnEvent)
	// The warmers fetch ahead of anyone: they carry X-Fetch: ahead, keep to
	// the slower pace, wait out the server's hold and have their own
	// breaker, so a restart's warm-up never makes a person's page fail.
	warm := metadata.Ahead(ctx)
	go jobs.Guard("explore-service-top-warm", func() { a.disc.WarmServiceTop(warm) })
	go jobs.Guard("explore-genre-warm", func() { a.disc.WarmGenres(warm) })
	discover.StartBackdropDark(ctx)
	a.mh().Start(ctx) // the music sync and catalog bundle warm
	go jobs.Guard("explore-browse-warm", func() { a.disc.WarmBrowse(warm) })
}

// newDiscover builds the Explore handlers on this API's cache and glue.
func (a *handlers) newDiscover() *discover.Handlers {
	cards := &a.explore.Cards
	return &discover.Handlers{Deps: discover.Deps{Store: a.s.Store, Metadata: a.s.Metadata, Secrets: a.s.Secrets, TraktBase: a.s.TraktBase, Modes: a.s.MetadataModes,
		Cache: &a.discover, Explore: &a.explore, ViewSignals: a.viewSignals, KidMax: a.kidMax, UserID: func(r *http.Request) int64 { return a.person(r).UserID }, Actor: actor,
		PartOn: a.partIsOn, Parts: a.s.Parts, TitleAllowed: a.titleAllowed, LibraryRating: a.libraryRating, KidItems: a.kidItems,
		CardFresh: func(ctx context.Context, it metadata.DiscoverItem, titleID int64) (*engine.Fresh, error) {
			return discover.CardFresh(ctx, a.s.Store, a.explore.Now(), it, titleID)
		},
		WithFreshRow: discover.WithFreshRow, OpenAsk: a.openAsk, OpenShowAsk: a.openShowAsk,
		FillCardArt: cards.Fill, QueueCardItems: cards.QueueItems,
		FillCardArtNow: func(r *http.Request, items []discover.ItemView) {
			cards.FillNow(r, items, discover.HeroArtMax, discover.HeroArtWait)
		},
		TrailersNow: a.heroTrailersNow,
		ServiceTop:  func() discover.ServiceTopFetch { return a.s.ServiceTop }, ServiceTopCache: &a.serviceTop, WatchOn: &a.watchOn, FillTrailerKeys: a.fillTrailerKeys,
		KidKnownFor: a.kidKnownFor, LibraryMovieAllowed: a.libraryMovieAllowed,
		LiveSource: func(ctx context.Context) discover.Lister { return a.disc.LiveExplore(ctx) },
		TitleView:  a.titleView, KidMovies: a.kidMovies,
		AllowedTitleIDs: a.allowedTitleIDs, KnownGenres: func(mbid string) []string { return a.mh().KnownGenres(mbid) }, ReadCardArt: cards.Read,
		GenreLists: func(ctx context.Context, kind string, reqs []engine.Request, wait time.Duration) ([][]engine.Item, bool) {
			return a.disc.GenreLists(ctx, kind, reqs, wait)
		},
		NoteTrailers: func(lang string, titles [][2]string) { a.trailers.Note(lang, titles) },
		WarmMusicNew: func() { a.musicProvider().WarmNew() },
		Strip:        a.stripBrowse(),
		AwardWinners: func(ctx context.Context, kind string) (map[string]bool, bool) {
			return a.titles.AwardWinners(ctx, kind)
		}}}
}

// stripBrowse is the Browse glue the Explore strip needs.
func (a *handlers) stripBrowse() discover.StripBrowse {
	return discover.StripBrowse{
		Page: func(ctx context.Context, r *http.Request, sw string) (engine.Page, []engine.BrowseTile, bool, error) {
			return a.disc.BrowsePage(ctx, r, sw)
		},
		Section: func(sw string, kid bool, max, id string) ([]engine.CategoryResult, bool) {
			res, built, _ := a.disc.BrowseShare(sw, kid, max).Get(id)
			return res, built
		},
		Build: func(ctx context.Context, sw string, kid bool, max, id string) {
			a.disc.BuildShared(ctx, a.disc.BrowseShare(sw, kid, max), sw, kid, max, id, false)
		},
		Tiles: func(ctx context.Context, r *http.Request, sw string, kid bool, pg engine.Page, page []engine.BrowseTile, id string) ([]engine.CategoryTile, error) {
			return a.disc.BrowseSectionTiles(ctx, r, sw, kid, pg, page, id)
		},
		Building: discover.ErrBrowseBuilding,
		Fetch:    discover.BrowseFetch,
	}
}

// suggest wires the live search handlers (discover.Suggest) over this API.
func (a *handlers) suggest() *discover.Suggest {
	return &discover.Suggest{
		MusicSuggest: func() func(ctx context.Context, q string) ([]metadata.MusicSuggestion, error) {
			return a.s.MusicSuggest
		},
		Metadata:   func() metadata.Provider { return a.s.Metadata },
		Store:      func() *database.Store { return a.s.Store },
		SafeMode:   func() bool { return a.s.Parts.SafeMode },
		Music:      func() discover.SuggestMusic { return a.mh() },
		ShowSource: func(name string) (metadata.ShowProvider, error) { return a.shows.ShowSource(name) },
		PartOn:     a.partIsOn, KidMax: a.kidMax, UserID: func(r *http.Request) int64 { return a.person(r).UserID },
		TitleAllowed: a.titleAllowed, TitleView: a.titleView, LibraryPeople: a.libraryPeople,
		OpenAsk: a.openAsk, OpenShowAsk: a.openShowAsk,
	}
}

// newTrailers builds the metadata trailers on this API's settings, age
// limits, Explore pools and music videos.
func (a *handlers) newTrailers() *metadata.Trailers {
	t := &metadata.Trailers{}
	t.Store, t.Version = a.s.Store, a.s.Version
	t.Provider = func() metadata.Provider { return a.s.Metadata }
	t.Pick = func() func(ctx context.Context, kind, id, lang string) (string, error) { return a.s.TrailerPick }
	t.Explore, t.Ages, t.Settings, t.People = titleExplore{a}, titleAges{a}, trailerHost{a}, trailerHost{a}
	t.KindsOn, t.Wants, t.Videos = a.disc.KindsOn, trailerHost{a}, a.mh().VideoHooks()
	return t
}

// Trailers is the trailer reader, pass and routes (for the health check
// and the job loops).
func (a *handlers) Trailers() *metadata.Trailers { return a.trailers }

// trailerHost gives the trailers the look settings, who asks, and the
// titles Explore shows.
type trailerHost struct{ a *handlers }

func (h trailerHost) AutoplayTrailers(ctx context.Context) bool {
	return health.AutoplayOn(ctx, h.a.s.Store)
}

func (h trailerHost) UserID(r *http.Request) int64 { return h.a.person(r).UserID }

func (h trailerHost) PassTitles(ctx context.Context) [][2]string {
	return h.a.disc.TrailerPassTitles(ctx)
}

func (h trailerHost) PassLanguages(ctx context.Context) []string {
	return h.a.trailerPassLanguages(ctx)
}

func (h trailerHost) Warm(lang string, titles [][2]string) { h.a.warmTitles(lang, titles) }

// fillTrailerKeys sets trailer_key on Explore cards whose key is kept.
func (a *handlers) fillTrailerKeys(r *http.Request, items []discover.ItemView) {
	discover.FillTrailerKeys(r, items, a.kidMax(r) != "", a.warmTitles, a.trailers.KnownKey)
}

// trailerPassLanguages are the languages the trailer pass fills.
func (a *handlers) trailerPassLanguages(ctx context.Context) []string {
	return discover.TrailerPassLanguages(ctx, a.s.Store, a.explore.Cards.LangsAsked())
}

// artistVideosOn says whether artist music videos can play here: music on,
// a music source and a metadata server that lists videos.
func (a *handlers) artistVideosOn() bool {
	p, _ := parts.ForTitleKind("artist")
	return a.partIsOn(p) && a.s.Library != nil && a.s.Library.Artists != nil && a.s.ArtistVideos != nil
}

// musicFresh is a music card's strip (discover.MusicFresh).
func (a *handlers) musicFresh(row string, c music.MusicCard) *engine.Fresh {
	return discover.MusicFresh(a.explore.Now(), row, c)
}

// warmTitles queues titles to be warmed ahead (discover.TitleWarmer).
func (a *handlers) warmTitles(lang string, titles [][2]string) {
	a.warmSet.Do(func() {
		w := &a.warm
		w.Store = func() *database.Store { return a.s.Store }
		w.Metadata = func() metadata.Provider { return a.s.Metadata }
		w.Cache = func() *discover.Cache { return &a.discover }
		w.WarmExtras = func(ctx context.Context, kind, id string) { a.titles.WarmExtras(ctx, kind, id) }
		w.TrailerKey = func(ctx context.Context, kind, id, lang string) { _, _ = a.trailers.Key(ctx, kind, id, lang) }
	})
	a.warm.Warm(lang, titles)
}

// heroTrailersNow looks the hero's trailers up now, one after another in the
// background, instead of waiting for the read-ahead's turn. They are noted
// too, so a failed lookup is asked again by the next pass.
func (a *handlers) heroTrailersNow(lang string, titles [][2]string) {
	a.trailers.Note(lang, titles)
	go func() {
		for _, ti := range titles {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_, _ = a.trailers.ResolveTitle(ctx, ti[0], ti[1], lang)
			cancel()
		}
	}()
}
