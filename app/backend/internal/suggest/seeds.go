package suggest

import (
	"math"
	"sort"
	"strings"
	"time"
)

// LibTitle is one movie or show in the Library, used as a seed source.
type LibTitle struct {
	TMDBID, Kind, Name string
	Year               int
	Genres, People     []string
	Rating             string
	Added              time.Time
}

// AskSeed is one ask by one person. Kid marks an ask made by a Kid.
type AskSeed struct {
	TMDBID, Kind, Name string
	Year               int
	Genres             []string
	Member             int64
	Kid                bool
	At                 time.Time
}

// ActivitySeed is a watched or recently opened title signal for one member.
type ActivitySeed struct {
	TMDBID, Kind, Name string
	Year               int
	Genres, People     []string
	Keywords           []string
	Rating             string
	// KindTag is "watched" or "opened".
	KindTag   string
	Completed bool
	Episodes  int
	Opens     int
	At        time.Time
}

// Seed is a title with a weight for one member.
type Seed struct {
	TMDBID, Kind, Name     string
	Year                   int
	Genres, People         []string
	Keywords               []string
	// Source identifies the primary signal: "watched", "asked", "have", or "opened".
	Source string
	Weight float64
	// Recent means added, asked or interacted with recently, so it may name a Because row.
	Recent bool
}

// Key returns the kind and TMDB id joined, which is unique per title.
func (s Seed) Key() string { return s.Kind + ":" + s.TMDBID }

// Seeds weighs the member's signals, strongest first. An own ask counts 2.0 and
// another member's 0.5. A Library title counts 1.0 if added within a year, else
// 0.5. Each weight fades by exp(-days/120). A Kid is seeded by their own asks
// only, and a Kid's asks never seed an adult. lib arrives already filtered to
// the Kid's limit.
func Seeds(member int64, kid bool, lib []LibTitle, asks []AskSeed, now time.Time) []Seed {
	return SeedsWithActivity(member, kid, lib, asks, nil, now)
}

func sourcePriority(src string) int {
	switch src {
	case "watched":
		return 4
	case "asked":
		return 3
	case "have":
		return 2
	case "opened":
		return 1
	}
	return 0
}

// SeedsWithActivity weighs Library, asks, watch history, and recent title opens.
func SeedsWithActivity(member int64, kid bool, lib []LibTitle, asks []AskSeed, activity []ActivitySeed, now time.Time) []Seed {
	by := map[string]*Seed{}
	add := func(s Seed, w float64) {
		if s.TMDBID == "" || (s.Kind != "movie" && s.Kind != "show") || w == 0 {
			return
		}
		cur, ok := by[s.Key()]
		if !ok {
			s.Weight = 0
			cur = &s
			by[s.Key()] = cur
		}
		cur.Weight += w
		cur.Recent = cur.Recent || s.Recent
		if sourcePriority(s.Source) > sourcePriority(cur.Source) {
			cur.Source = s.Source
		}
		if cur.Name == "" {
			cur.Name = s.Name
		}
		if len(cur.Genres) == 0 {
			cur.Genres = s.Genres
		}
		if len(cur.People) == 0 {
			cur.People = s.People
		}
		if len(s.Keywords) > 0 {
			haveKW := map[string]bool{}
			for _, k := range cur.Keywords {
				haveKW[k] = true
			}
			for _, k := range s.Keywords {
				if !haveKW[k] {
					haveKW[k] = true
					cur.Keywords = append(cur.Keywords, k)
				}
			}
		}
		if cur.Year == 0 {
			cur.Year = s.Year
		}
	}
	for _, t := range lib {
		w := 0.5
		if now.Sub(t.Added) <= 365*24*time.Hour {
			w = 1.0
		}
		add(Seed{TMDBID: t.TMDBID, Kind: t.Kind, Name: t.Name, Year: t.Year, Genres: t.Genres, People: t.People, Source: "have", Recent: w == 1.0}, w*decay(now, t.Added))
	}
	for _, a := range asks {
		w := 0.5
		src := ""
		switch {
		case a.Member == member:
			w = 2.0
			src = "asked"
		case kid || a.Kid:
			continue
		}
		add(Seed{TMDBID: a.TMDBID, Kind: a.Kind, Name: a.Name, Year: a.Year, Genres: a.Genres, Source: src, Recent: now.Sub(a.At) <= 365*24*time.Hour}, w*decay(now, a.At))
	}
	for _, act := range activity {
		switch act.KindTag {
		case "watched":
			w := 2.6
			if act.Completed {
				w += 0.4
			}
			if act.Episodes > 1 {
				w += min(0.4, float64(act.Episodes-1)*0.15)
			}
			add(Seed{
				TMDBID: act.TMDBID, Kind: act.Kind, Name: act.Name, Year: act.Year,
				Genres: act.Genres, People: act.People, Keywords: act.Keywords,
				Source: "watched", Recent: now.Sub(act.At) <= 365*24*time.Hour,
			}, w*decay(now, act.At))
		case "opened":
			w := 0.85
			if act.Opens > 1 {
				w += min(0.35, float64(act.Opens-1)*0.12)
			}
			add(Seed{
				TMDBID: act.TMDBID, Kind: act.Kind, Name: act.Name, Year: act.Year,
				Genres: act.Genres, People: act.People, Keywords: act.Keywords,
				Source: "opened", Recent: now.Sub(act.At) <= 60*24*time.Hour,
			}, w*decayDays(now, act.At, 30))
		}
	}
	out := make([]Seed, 0, len(by))
	for _, s := range by {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Weight != out[j].Weight {
			return out[i].Weight > out[j].Weight
		}
		return out[i].Key() < out[j].Key()
	})
	return out
}

// SeedCount counts the seeds that add signal (positive weight).
func SeedCount(seeds []Seed) int {
	n := 0
	for _, s := range seeds {
		if s.Weight > 0 {
			n++
		}
	}
	return n
}

// TailCap returns the most tail rows a page gets: 8 under 3 seeds, 20 for 3 to
// 9, 40 for 10 or more. A Kid is capped at 8 to keep the page short.
func TailCap(seeds int, kid bool) int {
	n := 8
	switch {
	case seeds >= 10:
		n = 40
	case seeds >= 3:
		n = 20
	}
	if kid && n > 8 {
		n = 8
	}
	return n
}

// Profile is a member's taste: a genre weight vector, keyword vector, a set of people and the
// median seed year.
type Profile struct {
	genres   map[string]float64
	keywords map[string]float64
	people   map[string]bool
	year     int
}

// NewProfile builds a profile from seeds. A negative seed takes genre weight
// away at half strength, so one dislike does not outweigh a liking.
func NewProfile(seeds []Seed) Profile {
	p := Profile{genres: map[string]float64{}, keywords: map[string]float64{}, people: map[string]bool{}}
	var years []int
	for _, s := range seeds {
		w := s.Weight
		if w < 0 {
			w /= 2
		}
		for _, g := range s.Genres {
			p.genres[g] += w
		}
		if s.Weight > 0 {
			for _, kw := range s.Keywords {
				if k := strings.ToLower(strings.TrimSpace(kw)); k != "" {
					p.keywords[k] += w
				}
			}
			for _, n := range s.People {
				p.people[strings.ToLower(n)] = true
			}
			if s.Year > 0 {
				years = append(years, s.Year)
			}
		}
	}
	if len(years) > 0 {
		sort.Ints(years)
		p.year = years[len(years)/2]
	}
	return p
}

// KeywordWeights returns the member's taste keyword weight map.
func (p Profile) KeywordWeights() map[string]float64 { return p.keywords }

// GenreWeights returns the member's taste genre weight map.
func (p Profile) GenreWeights() map[string]float64 { return p.genres }

// TopGenres returns up to n of the strongest genres whose share of the total
// positive weight is at least minShare.
func (p Profile) TopGenres(n int, minShare float64) []string {
	total := 0.0
	for _, w := range p.genres {
		if w > 0 {
			total += w
		}
	}
	type gw struct {
		g string
		w float64
	}
	var list []gw
	for g, w := range p.genres {
		if w > 0 && total > 0 && w/total >= minShare {
			list = append(list, gw{g, w})
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].w != list[j].w {
			return list[i].w > list[j].w
		}
		return list[i].g < list[j].g
	})
	out := []string{}
	for i := 0; i < len(list) && i < n; i++ {
		out = append(out, list[i].g)
	}
	return out
}

// Affinity scores an item from 0 to 1 as 0.5 genre + 0.3 people + 0.2 era match.
func (p Profile) Affinity(it Item) float64 {
	// Genre match is the cosine between the vector and the item's genres (each 1).
	var dot, norm float64
	for _, w := range p.genres {
		norm += w * w
	}
	for _, g := range it.Genres {
		dot += p.genres[g]
	}
	genre := 0.0
	if norm > 0 && len(it.Genres) > 0 {
		genre = dot / (math.Sqrt(norm) * math.Sqrt(float64(len(it.Genres))))
	}
	genre = clamp(genre)
	people := 0.0
	for _, n := range it.People {
		if p.people[strings.ToLower(n)] {
			people = 1
			break
		}
	}
	era := 0.0
	if p.year > 0 && it.Year > 0 {
		// Era fades to 0 at 40 years from the median seed year.
		era = clamp(1 - math.Abs(float64(it.Year-p.year))/40)
	}
	return 0.5*genre + 0.3*people + 0.2*era
}

// decay fades a signal by age with a 120 day time constant. A future date
// counts as now.
func decay(now, at time.Time) float64 {
	return decayDays(now, at, 120)
}

func decayDays(now, at time.Time, tauDays float64) float64 {
	if at.IsZero() {
		return 0.5
	}
	days := now.Sub(at).Hours() / 24
	if days < 0 {
		days = 0
	}
	return math.Exp(-days / tauDays)
}

// clamp limits x to the range 0 to 1.
func clamp(x float64) float64 { return math.Max(0, math.Min(1, x)) }
