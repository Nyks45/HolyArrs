package suggest

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// KidWithout are the TMDB genres a Kid's generated lists leave out: horror,
// crime, thriller, war and war and politics.
const KidWithout = "27|80|53|10752|10768"

// EndlessIn is what the endless tail's rows are made from. It holds no
// member data beyond the region, the picked services and the because rows
// the pool already made.
type EndlessIn struct {
	Switch   string
	Kid      bool
	Region   string
	Services string
	Now      time.Time
	// Later are the pool's because rows still to show (Pool.Later).
	Later []Row
	// Skip are row ids and genre names the page already shows.
	Skip map[string]bool
	// AllFilters says the lists take network, company and origin country
	// filters (TMDB itself does; the HolyArrs metadata server does not).
	AllFilters bool
}

type named struct {
	id   string
	name string
}

// The networks, studios, languages and watch services the tail has rows
// for, as TMDB numbers them.
var (
	endlessNetworks = []named{{"213", "Netflix"}, {"49", "HBO"}, {"2552", "Apple TV+"}, {"2739", "Disney+"}, {"1024", "Prime Video"}, {"453", "Hulu"},
		{"4330", "Paramount+"}, {"3353", "Peacock"}, {"174", "AMC"}, {"88", "FX"}, {"4", "BBC One"}, {"67", "Showtime"}, {"318", "Starz"},
		{"80", "Adult Swim"}, {"866", "tvN"}, {"26", "Channel 4"}, {"9", "ITV"}, {"47", "Comedy Central"}, {"77", "Syfy"}, {"6", "NBC"},
		{"2", "ABC"}, {"16", "CBS"}, {"19", "FOX"}, {"71", "The CW"}}
	endlessStudios = []named{{"420", "Marvel Studios"}, {"3", "Pixar"}, {"2", "Walt Disney Pictures"}, {"174", "Warner Bros. Pictures"},
		{"33", "Universal Pictures"}, {"4", "Paramount Pictures"}, {"5", "Columbia Pictures"}, {"25", "20th Century Fox"}, {"1632", "Lionsgate"},
		{"41077", "A24"}, {"3172", "Blumhouse"}, {"521", "DreamWorks Animation"}, {"10342", "Studio Ghibli"}, {"923", "Legendary"},
		{"1", "Lucasfilm"}, {"12", "New Line Cinema"}, {"6704", "Illumination"}, {"90733", "NEON"}, {"43", "Searchlight Pictures"}}
	kidStudios   = []named{{"3", "Pixar"}, {"2", "Walt Disney Pictures"}, {"521", "DreamWorks Animation"}, {"6704", "Illumination"}, {"10342", "Studio Ghibli"}}
	kidNetworks  = []named{{"2739", "Disney+"}, {"56", "Cartoon Network"}, {"13", "Nickelodeon"}, {"54", "Disney Channel"}}
	endlessLangs = []named{{"ko", "Korean"}, {"ja", "Japanese"}, {"es", "Spanish"}, {"hi", "Hindi"}, {"fr", "French"}, {"de", "German"},
		{"tr", "Turkish"}, {"zh", "Chinese"}, {"it", "Italian"}, {"pt", "Portuguese"}, {"sv", "Swedish"}, {"da", "Danish"}, {"th", "Thai"},
		{"ta", "Tamil"}, {"te", "Telugu"}, {"no", "Norwegian"}, {"pl", "Polish"}}
	endlessCountries = []named{{"GB", "British"}, {"AU", "Australian"}, {"CA", "Canadian"}, {"IE", "Irish"}}
	endlessProviders = []named{{"8", "Netflix"}, {"9", "Prime Video"}, {"337", "Disney+"}, {"350", "Apple TV+"}, {"1899", "Max"}, {"15", "Hulu"},
		{"531", "Paramount+"}, {"386", "Peacock"}, {"283", "Crunchyroll"}, {"11", "MUBI"}}
	endlessDecades   = []int{2010, 2000, 1990, 1980, 1970, 1960}
	endlessKidGenres = map[string][]int{"movie": {10751, 16, 35, 12, 14, 10402}, "show": {10762, 10751, 16, 35}}
	// genreOrder puts the genres people browse most first.
	genreOrder = []int{28, 35, 18, 53, 878, 27, 10749, 80, 12, 16, 9648, 14, 99, 10751, 36, 10752, 37, 10402,
		10759, 10765, 10764, 10768, 10762}
)

func kindWord(kind string) string {
	if kind == "show" {
		return "shows"
	}
	return "movies"
}

// kindGenres are kind's TMDB genres in genreOrder.
func kindGenres(kind string, kid bool) []int {
	if kid {
		return endlessKidGenres[kind]
	}
	var out []int
	for _, id := range genreOrder {
		if _, ok := Genres[kind][id]; ok {
			out = append(out, id)
		}
	}
	return out
}

// EndlessRows is the endless tail past the pool: charts, every genre,
// networks, studios, languages, watch services in the region, decades and
// the pool's later because rows, mixed so two rows of one sort seldom meet.
// Rows have their lists (one kind each) but no items; ids are stable.
func EndlessRows(in EndlessIn) []Row {
	kinds := Kinds(in.Switch)
	var buckets [][]Row
	perKind := func(build func(kind string) []Row) []Row {
		var lists [][]Row
		for _, k := range kinds {
			lists = append(lists, build(k))
		}
		return interleaveRows(lists)
	}
	kidify := func(q Request) Request {
		q.Scope = ScopeShared
		if !in.Kid {
			return q
		}
		if q.Without == "" {
			q.Without = KidWithout
		} else {
			q.Without += "|" + KidWithout
		}
		if q.Kind == "movie" {
			q.Cert = "PG"
		}
		return q
	}
	row := func(kind, bucket, key, t, title, genre string, q Request) Row {
		q.Kind = kind
		return Row{ID: "more:" + kind + ":" + bucket + ":" + key, Type: t, Title: title, Genre: genre, Layer: LayerFixed, Specs: []Request{kidify(q)}}
	}
	day := Day(in.Now)
	buckets = append(buckets, perKind(func(k string) []Row {
		out := []Row{row(k, "popular", "all", TypeChart, "Popular "+kindWord(k), "", Request{List: "discover", MinVotes: 50})}
		if k == "movie" {
			out = append(out, row(k, "now", "playing", TypeChart, "In cinemas now", "", Request{List: "now_playing", Region: in.Region}))
		} else {
			out = append(out, row(k, "now", "airing", TypeChart, "Airing today", "", Request{List: "discover", AirFrom: day, AirTo: day, MinVotes: 5}))
			out = append(out, row(k, "now", "week", TypeChart, "On the air this week", "", Request{List: "upcoming"}))
		}
		votes := 1000
		if k == "show" {
			votes = 300
		}
		return append(out, row(k, "top", "all", TypeChart, "Top-rated "+kindWord(k), "", Request{List: "discover", Sort: "rating", MinVotes: votes}))
	}))
	buckets = append(buckets, perKind(func(k string) []Row {
		var out []Row
		for _, id := range kindGenres(k, in.Kid) {
			g := Genres[k][id]
			if in.Skip[g] {
				continue
			}
			out = append(out, row(k, "genre", strconv.Itoa(id), TypePopularGenre, g+" "+kindWord(k), g, Request{List: "discover", Genres: strconv.Itoa(id), MinVotes: 50}))
		}
		return out
	}))
	studios, networks := endlessStudios, endlessNetworks
	if in.Kid {
		studios, networks = kidStudios, kidNetworks
	}
	if in.AllFilters {
		buckets = append(buckets, perKind(func(k string) []Row {
			var out []Row
			if k == "show" {
				for _, n := range networks {
					out = append(out, row(k, "network", n.id, TypeNetwork, n.name+" shows", "", Request{List: "discover", Networks: n.id, MinVotes: 10}))
				}
				return out
			}
			for _, s := range studios {
				out = append(out, row(k, "studio", s.id, TypeStudio, s.name+" movies", "", Request{List: "discover", Companies: s.id, MinVotes: 20}))
			}
			return out
		}))
	}
	if !in.Kid {
		buckets = append(buckets, perKind(func(k string) []Row {
			var out []Row
			for _, l := range endlessLangs {
				q := Request{List: "discover", Lang: l.id, MinVotes: 10}
				if l.id == "ja" {
					// Anime has its own tab.
					q.Without = "16"
				}
				out = append(out, row(k, "lang", l.id, TypeLanguage, l.name+" "+kindWord(k), "", q))
			}
			for _, c := range endlessCountries {
				if in.AllFilters {
					out = append(out, row(k, "country", c.id, TypeLanguage, c.name+" "+kindWord(k), "", Request{List: "discover", Country: c.id, MinVotes: 20}))
				}
			}
			return out
		}))
	}
	if in.Region != "" {
		buckets = append(buckets, perKind(func(k string) []Row {
			var out []Row
			for _, p := range providerOrder(in.Services) {
				out = append(out, row(k, "on", p.id, TypeProvider, "Popular "+kindWord(k)+" on "+p.name, "", Request{List: "discover", Services: p.id, Region: in.Region, MinVotes: 5}))
			}
			return out
		}))
	}
	buckets = append(buckets, perKind(func(k string) []Row {
		var out []Row
		for _, d := range endlessDecades {
			out = append(out, row(k, "decade", strconv.Itoa(d), TypePopularDecade, fmt.Sprintf("%ds %s", d, kindWord(k)), "",
				Request{List: "discover", From: fmt.Sprintf("%d-01-01", d), To: fmt.Sprintf("%d-12-31", d+9), MinVotes: 100}))
		}
		return out
	}))
	buckets = append(buckets, perKind(func(k string) []Row {
		var out []Row
		votes := 500
		if k == "show" {
			votes = 150
		}
		for _, id := range kindGenres(k, in.Kid) {
			g := Genres[k][id]
			out = append(out, row(k, "topgenre", strconv.Itoa(id), TypePopularGenre, "Top-rated "+strings.ToLower(g)+" "+kindWord(k), g,
				Request{List: "discover", Genres: strconv.Itoa(id), Sort: "rating", MinVotes: votes}))
		}
		return out
	}))
	buckets = append(buckets, perKind(func(k string) []Row {
		var out []Row
		for _, d := range endlessDecades[:4] {
			for _, id := range kindGenres(k, in.Kid) {
				g := Genres[k][id]
				out = append(out, row(k, "era", fmt.Sprintf("%d-%d", d, id), TypePopularDecade, fmt.Sprintf("%ds %s %s", d, strings.ToLower(g), kindWord(k)), g,
					Request{List: "discover", Genres: strconv.Itoa(id), From: fmt.Sprintf("%d-01-01", d), To: fmt.Sprintf("%d-12-31", d+9), MinVotes: 50}))
			}
		}
		return out
	}))
	mixed := roundRobin(buckets)
	// A later because row every third row while there are any.
	var later []Row
	for _, r := range in.Later {
		if len(r.Specs) == 0 {
			continue
		}
		var specs []Request
		for _, q := range r.Specs {
			if kindIn(kinds, q.Kind) {
				specs = append(specs, kidify(q))
			}
		}
		if len(specs) > 0 {
			r.Specs, r.Layer = specs, LayerFixed
			later = append(later, r)
		}
	}
	out := make([]Row, 0, len(mixed)+len(later))
	for _, r := range mixed {
		if len(later) > 0 && len(out)%3 == 0 {
			out = append(out, later[0])
			later = later[1:]
		}
		out = append(out, r)
	}
	out = append(out, later...)
	kept := out[:0]
	for _, r := range out {
		if !in.Skip[r.ID] {
			kept = append(kept, r)
		}
	}
	return kept
}

// providerOrder puts the picked services first, then the rest.
func providerOrder(services string) []named {
	picked := map[string]bool{}
	for _, s := range strings.Split(services, "|") {
		picked[s] = true
	}
	out := append([]named{}, endlessProviders...)
	sort.SliceStable(out, func(i, j int) bool { return picked[out[i].id] && !picked[out[j].id] })
	return out
}

func interleaveRows(lists [][]Row) []Row {
	var out []Row
	for i := 0; ; i++ {
		added := false
		for _, l := range lists {
			if i < len(l) {
				out = append(out, l[i])
				added = true
			}
		}
		if !added {
			return out
		}
	}
}

// roundRobin takes one row from each bucket in turn.
func roundRobin(buckets [][]Row) []Row {
	return interleaveRows(buckets)
}

// EndlessPage is endless tail page n (from 0) of rows: TailPage rows, and
// whether more follow.
func EndlessPage(rows []Row, n int) ([]Row, bool) {
	from := n * TailPage
	if n < 0 || from >= len(rows) {
		return []Row{}, false
	}
	to := min(from+TailPage, len(rows))
	return rows[from:to], to < len(rows)
}
