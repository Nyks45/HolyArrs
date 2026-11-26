package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// GroupSeasonLister lists episodes of one season within a TMDB episode group.
type GroupSeasonLister interface {
	GroupSeasonEpisodes(ctx context.Context, groupID string, season int) ([]DetailEpisode, error)
}

// GroupSeasonListerFor returns the group season surface selected for ctx.
func GroupSeasonListerFor(ctx context.Context, provider Provider) (GroupSeasonLister, bool) {
	if sw, ok := provider.(*Switch); ok {
		provider = sw.pick(ctx)
	}
	l, ok := provider.(GroupSeasonLister)
	return l, ok
}

type tmdbEpisodeGroupDetail struct {
	ID     string                    `json:"id"`
	Name   string                    `json:"name"`
	Type   int                       `json:"type"`
	Groups []tmdbEpisodeGroupSection `json:"groups"`
}

type tmdbEpisodeGroupSection struct {
	Name     string `json:"name"`
	Order    int    `json:"order"`
	Episodes []struct {
		SeasonNumber  int     `json:"season_number"`
		EpisodeNumber int     `json:"episode_number"`
		Order         int     `json:"order"`
		Name          string  `json:"name"`
		Overview      string  `json:"overview"`
		AirDate       *string `json:"air_date"`
		Runtime       int     `json:"runtime"`
		Still         string  `json:"still_path"`
	} `json:"episodes"`
}

// NeedsAnimeSeasonSplit reports whether cached show details were stored before
// anime episode-group splitting existed and only list one regular TMDB season.
func NeedsAnimeSeasonSplit(d *Details) bool {
	if d == nil || d.SeasonGroupChecked || d.Kind != "show" {
		return false
	}
	if !LooksAnime(d.Genres, d.OriginCountry, d.OriginalLanguage) {
		return false
	}
	regCount, regEps := countRegularSeasons(d.Seasons)
	return regCount == 1 && regEps >= 2
}

func countRegularSeasons(seasons []DetailSeason) (count int, eps int) {
	for _, s := range seasons {
		if s.Number > 0 && s.EpisodeCount > 0 {
			count++
			eps += s.EpisodeCount
		}
	}
	return count, eps
}

func pickSeasonsEpisodeGroup(groups []EpisodeGroupInfo) string {
	var exact, fallback []EpisodeGroupInfo
	for _, g := range groups {
		if g.Type != 6 || !ValidGroupID(g.ID) || g.Seasons < 2 || g.Episodes <= 0 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(g.Name))
		switch name {
		case "seasons":
			exact = append(exact, g)
		case "tv", "tv seasons", "season":
			fallback = append(fallback, g)
		}
	}
	pool := exact
	if len(pool) == 0 {
		pool = fallback
	}
	if len(pool) == 0 {
		return ""
	}
	best := pool[0]
	for _, g := range pool[1:] {
		if g.Episodes > best.Episodes || (g.Episodes == best.Episodes && g.Seasons > best.Seasons) {
			best = g
		}
	}
	return best.ID
}

func isSpecialsGroup(g tmdbEpisodeGroupSection) bool {
	name := strings.TrimSpace(g.Name)
	if strings.EqualFold(name, "specials") {
		return true
	}
	if len(g.Episodes) > 0 {
		allSpecial := true
		for _, e := range g.Episodes {
			if e.SeasonNumber != 0 {
				allSpecial = false
				break
			}
		}
		if allSpecial {
			return true
		}
	}
	return g.Order == 0 && name == ""
}

func parseGroupSeasons(imagesBase string, raw tmdbEpisodeGroupDetail) ([]DetailSeason, map[int][]DetailEpisode, map[[2]int][2]int) {
	groups := append([]tmdbEpisodeGroupSection(nil), raw.Groups...)
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].Order < groups[j].Order })

	var seasons []DetailSeason
	bySeason := map[int][]DetailEpisode{}
	epMap := map[[2]int][2]int{}

	for _, g := range groups {
		if len(g.Episodes) == 0 || isSpecialsGroup(g) {
			continue
		}
		sNum := len(seasons) + 1
		rawEps := append([]struct {
			SeasonNumber  int     `json:"season_number"`
			EpisodeNumber int     `json:"episode_number"`
			Order         int     `json:"order"`
			Name          string  `json:"name"`
			Overview      string  `json:"overview"`
			AirDate       *string `json:"air_date"`
			Runtime       int     `json:"runtime"`
			Still         string  `json:"still_path"`
		}(nil), g.Episodes...)
		sort.SliceStable(rawEps, func(i, j int) bool { return rawEps[i].Order < rawEps[j].Order })

		eps := make([]DetailEpisode, 0, len(rawEps))
		year := 0
		for idx, e := range rawEps {
			ep := DetailEpisode{
				Number:   idx + 1,
				Name:     e.Name,
				Overview: e.Overview,
				Runtime:  e.Runtime,
			}
			if e.Still != "" && imagesBase != "" {
				ep.Still = imagesBase + "/w780" + e.Still
			}
			if e.AirDate != nil {
				_, ep.AirDay = dayEnd(*e.AirDate)
				if year == 0 && ep.AirDay != "" {
					year = yearOf(ep.AirDay)
				}
			}
			eps = append(eps, ep)
			if e.SeasonNumber > 0 && e.EpisodeNumber > 0 {
				epMap[[2]int{e.SeasonNumber, e.EpisodeNumber}] = [2]int{sNum, idx + 1}
			}
		}
		seasons = append(seasons, DetailSeason{Number: sNum, EpisodeCount: len(eps), Year: year})
		bySeason[sNum] = eps
	}
	return seasons, bySeason, epMap
}

func (t *TMDB) resolveAnimeSeasons(ctx context.Context, tmdbID string, d *Details) {
	d.SeasonGroupChecked = true
	if !LooksAnime(d.Genres, d.OriginCountry, d.OriginalLanguage) {
		return
	}
	regCount, defaultRegEps := countRegularSeasons(d.Seasons)
	if regCount != 1 || defaultRegEps < 2 {
		return
	}
	groups, err := t.EpisodeGroups(ctx, tmdbID)
	if err != nil || len(groups) == 0 {
		return
	}
	gid := pickSeasonsEpisodeGroup(groups)
	if gid == "" {
		return
	}
	b, err := t.get(ctx, "/tv/episode_group/"+gid, nil)
	if err != nil {
		return
	}
	var raw tmdbEpisodeGroupDetail
	if json.Unmarshal(b, &raw) != nil {
		return
	}
	regSeasons, _, epMap := parseGroupSeasons(t.images(), raw)
	_, groupRegEps := countRegularSeasons(regSeasons)
	if len(regSeasons) < 2 || groupRegEps < (defaultRegEps+1)/2 {
		return
	}
	var next []DetailSeason
	for _, s := range d.Seasons {
		if s.Number == 0 && s.EpisodeCount > 0 {
			next = append(next, s)
			break
		}
	}
	next = append(next, regSeasons...)
	d.Seasons = next
	d.SeasonGroupID = gid
	if d.NextEpisode != nil {
		if mapped, ok := epMap[[2]int{d.NextEpisode.Season, d.NextEpisode.Episode}]; ok {
			d.NextEpisode.Season, d.NextEpisode.Episode = mapped[0], mapped[1]
		}
	}
}

// GroupSeasonEpisodes lists one season of a TMDB episode group by group id.
func (t *TMDB) GroupSeasonEpisodes(ctx context.Context, groupID string, season int) ([]DetailEpisode, error) {
	if !ValidGroupID(groupID) || season < 1 || season > 999 {
		return nil, ErrNotFound
	}
	b, err := t.get(ctx, "/tv/episode_group/"+groupID, nil)
	if err != nil {
		return nil, err
	}
	var raw tmdbEpisodeGroupDetail
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("TMDB: odd answer: %w", err)
	}
	_, bySeason, _ := parseGroupSeasons(t.images(), raw)
	eps, ok := bySeason[season]
	if !ok {
		return nil, ErrNotFound
	}
	return eps, nil
}
