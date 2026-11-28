package suggest

import (
	"fmt"
	"strconv"
)

// Request is one outside list the pool build asks for. It carries only
// title ids, genres, dates, services and the region, never anything about
// a member.
type Request struct {
	// List is the kind of list: similar (recommendations and similar of Ref),
	// trending, upcoming (movies coming out, shows airing now), best, discover,
	// person (credits of the person named Ref) or collection (the movies of
	// TMDB collection Ref).
	List  string
	Kind  string
	Ref   string
	Genre string
	// From and To are release dates, YYYY-MM-DD.
	From, To         string
	Services, Region string
	MinVote          float64
	MinVotes         int
	MaxVotes         int
	// Sort is popularity (default), rating or recent (newest out first).
	Sort string
	// AirFrom and AirTo keep shows with an episode airing in that window.
	// Types reads From and To as release dates of those TMDB release types
	// ("4|3" is digital or cinema) in Region, for movies.
	AirFrom, AirTo, Types string
	// Keywords are TMDB keyword names joined by "|", any of them.
	Keywords string
	// Genres and Without are raw TMDB genre ids, "|" for any of and "," for
	// all of. Lang is an original language and Cert the highest US movie
	// certification.
	Genres, Without, Lang, Cert string
	// Networks (shows) and Companies (movies and shows) are TMDB network and
	// company ids, "|" for any of. Country is an origin country code.
	Networks, Companies, Country string
	// Page is the list's page, from 1; 0 and 1 are the first page.
	Page int
	// Scope is ScopeShared for region-wide lists. It is left out of the cache
	// key so it never splits a shared list.
	Scope string
}

// String is the cache key of the Request. It leaves out Scope, so a shared
// list is never split from an identical member one. A first page keeps the
// key it always had.
func (q Request) String() string {
	return q.baseKey() + q.moreKey()
}

func (q Request) baseKey() string {
	if q.AirFrom != "" || q.AirTo != "" || q.Types != "" {
		return fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%.1f|%d|%d|%s|%s|a=%s-%s|t=%s", q.List, q.Kind, q.Ref, q.Genre, q.From, q.To, q.Services, q.Region, q.MinVote, q.MinVotes, q.MaxVotes, q.Sort, q.Keywords, q.AirFrom, q.AirTo, q.Types)
	}
	if q.Genres != "" || q.Without != "" || q.Lang != "" || q.Cert != "" {
		return fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%.1f|%d|%d|%s|%s|g=%s|w=%s|l=%s|c=%s", q.List, q.Kind, q.Ref, q.Genre, q.From, q.To, q.Services, q.Region, q.MinVote, q.MinVotes, q.MaxVotes, q.Sort, q.Keywords, q.Genres, q.Without, q.Lang, q.Cert)
	}
	if q.Keywords != "" {
		return fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%.1f|%d|%d|%s|%s", q.List, q.Kind, q.Ref, q.Genre, q.From, q.To, q.Services, q.Region, q.MinVote, q.MinVotes, q.MaxVotes, q.Sort, q.Keywords)
	}
	return fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%.1f|%d|%d|%s", q.List, q.Kind, q.Ref, q.Genre, q.From, q.To, q.Services, q.Region, q.MinVote, q.MinVotes, q.MaxVotes, q.Sort)
}

// moreKey holds the fields added after the first keys were stored, so older
// keys stay as they were.
func (q Request) moreKey() string {
	s := ""
	if q.Genres != "" && (q.AirFrom != "" || q.AirTo != "" || q.Types != "") {
		s += "|g=" + q.Genres + "|w=" + q.Without + "|l=" + q.Lang + "|c=" + q.Cert
	}
	if q.Networks != "" || q.Companies != "" || q.Country != "" {
		s += "|n=" + q.Networks + "|co=" + q.Companies + "|oc=" + q.Country
	}
	if q.Page > 1 {
		s += "|p=" + strconv.Itoa(q.Page)
	}
	return s
}
