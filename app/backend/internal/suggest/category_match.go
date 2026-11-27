package suggest

import (
	"sort"
	"strings"
	"time"
)

// CategorySummary describes a Browse category / subcategory for menus and search.
type CategorySummary struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Section      string   `json:"section"`
	SectionTitle string   `json:"section_title"`
	Movie        bool     `json:"movie"`
	Show         bool     `json:"show"`
	Featured     bool     `json:"featured,omitempty"`
	ParentGenres []string `json:"parent_genres,omitempty"`
	Aliases      []string `json:"aliases,omitempty"`
}

var sectionTitles = func() map[string]string {
	m := make(map[string]string, len(BrowseSections))
	for _, s := range BrowseSections {
		m[s.ID] = s.Title
	}
	return m
}()

var featuredSubcategories = []string{
	"legal", "medical", "psychological-thriller", "true-crime", "heists",
	"spy", "cyberpunk", "time-travel", "revenge", "conspiracy",
	"whodunits", "cops", "political-thriller", "romcom", "sci-fi-action",
	"gangsters", "survival", "dark-comedy", "mind-bending", "dystopia",
	"kdrama-thriller", "korean", "turkish", "anime", "shonen", "isekai",
	"slice-of-life", "superheroes", "martial-arts", "family-adventure",
}

var animeSubcategories = []string{
	"anime", "shonen", "isekai", "slice-of-life", "comics",
	"fantasy-worlds", "time-travel", "cyberpunk", "martial-arts", "superheroes",
	"magic", "japanese", "alt-worlds", "sci-fi-action", "dark",
}

var parentGenreCategories = map[string][]string{
	"drama":              {"legal", "courtroom", "medical", "politics", "true-story", "coming-of-age", "family-stories", "tearjerkers", "slow-burn", "korean", "turkish", "slice-of-life"},
	"crime":              {"legal", "courtroom", "cops", "heists", "gangsters", "con-artists", "true-crime", "serial-killers", "whodunits", "prison", "revenge", "kdrama-thriller", "rise-fall"},
	"thriller":           {"psychological-thriller", "political-thriller", "conspiracy", "spy", "spies", "edge", "mind-bending", "survival", "serial-killers", "assassins", "revenge", "nordic"},
	"mystery":            {"whodunits", "conspiracy", "psychological-thriller", "cops", "true-crime", "mind-bending", "serial-killers", "nordic"},
	"science fiction":    {"sci-fi-action", "space", "cyberpunk", "time-travel", "dystopia", "ai", "aliens", "post-apocalyptic", "alt-worlds"},
	"sci-fi & fantasy":   {"sci-fi-action", "space", "cyberpunk", "time-travel", "dystopia", "fantasy-worlds", "magic", "dragons", "alt-worlds"},
	"action":             {"heists", "spy", "spies", "martial-arts", "superheroes", "assassins", "adrenaline", "sci-fi-action", "revenge"},
	"action & adventure": {"sci-fi-action", "heists", "spy", "spies", "martial-arts", "superheroes", "adventure", "survival", "pirates", "treasure"},
	"adventure":          {"family-adventure", "treasure", "pirates", "islands", "wilderness", "space", "survival", "road-trips"},
	"comedy":             {"romcom", "dark-comedy", "satire", "sitcoms", "feel-good", "cozy", "quirky", "funny", "slice-of-life"},
	"romance":            {"romcom", "romantic", "korean", "turkish", "weddings", "love-triangles", "tearjerkers"},
	"horror":             {"scary", "haunted", "zombies", "vampires", "monsters", "serial-killers", "psychological-thriller", "dark"},
	"fantasy":            {"fantasy-worlds", "magic", "dragons", "fairy-tales", "superheroes", "alt-worlds", "isekai"},
	"animation":          {"anime", "shonen", "isekai", "slice-of-life", "classic-animation", "comics", "family-adventure", "fantasy-worlds"},
	"documentary":        {"true-crime", "true-story", "history", "sports"},
	"family":             {"family-adventure", "cozy", "feel-good", "animals", "magic", "90s-family"},
	"war":                {"ww2", "cold-war", "spy", "spies", "true-story"},
	"war & politics":     {"politics", "political-thriller", "ww2", "cold-war", "conspiracy", "spy", "spies"},
	"history":            {"ww2", "victorian", "ancient", "medieval", "true-story", "cold-war"},
	"music":              {"musicals", "music-stories"},
	"western":            {"wild-west"},
}

var categoryAliases = map[string][]string{
	"legal":                  {"legal", "legal drama", "law", "lawyer", "lawyers", "attorney", "attorneys", "law firm", "legal shows", "legal movies"},
	"courtroom":              {"courtroom", "court", "trial", "trials", "legal", "lawyer"},
	"medical":                {"medical", "medical drama", "doctor", "doctors", "hospital", "hospitals", "surgeon", "nurse", "er"},
	"cops":                   {"cop", "cops", "police", "police procedural", "detective", "detectives", "investigation"},
	"cops-stories":           {"cop", "cops", "police", "detective"},
	"heists":                 {"heist", "heists", "robbery", "bank robbery", "caper"},
	"spy":                    {"spy", "spies", "espionage", "secret agent", "cia", "mi6"},
	"spies":                  {"spy", "spies", "espionage", "secret agent"},
	"cyberpunk":              {"cyberpunk", "cyber", "virtual reality", "neon"},
	"time-travel":            {"time travel", "time loop", "temporal"},
	"true-crime":             {"true crime", "cold case", "forensic", "forensics"},
	"psychological-thriller": {"psychological", "psychological thriller", "psychological thrillers", "mind game"},
	"political-thriller":     {"political thriller", "political thrillers", "corruption", "assassination"},
	"politics":               {"politics", "political", "president", "election"},
	"korean":                 {"korean", "kdrama", "k-drama", "k drama", "korea"},
	"kdrama-thriller":        {"kdrama thriller", "korean thriller", "korean crime", "k-drama thriller"},
	"turkish":                {"turkish", "dizi", "turkey", "turk"},
	"anime":                  {"anime", "japanese animation"},
	"shonen":                 {"shonen", "shounen", "shonen anime"},
	"isekai":                 {"isekai", "another world", "reincarnation", "isekai anime"},
	"slice-of-life":          {"slice of life", "everyday life"},
	"romcom":                 {"romcom", "rom com", "rom-com", "romantic comedy", "romantic comedies"},
	"sci-fi-action":          {"sci-fi action", "scifi action"},
	"scifi":                  {"scifi", "sci-fi", "sci fi", "science fiction"},
	"whodunits":              {"whodunit", "whodunits", "murder mystery"},
	"gangsters":              {"gangster", "gangsters", "mafia", "mob", "cartel", "organized crime"},
	"rise-fall":              {"cartel", "drug cartel", "rise and fall"},
	"revenge":                {"revenge", "vigilante"},
	"conspiracy":             {"conspiracy", "conspiracies", "cover-up"},
	"con-artists":            {"con artist", "con artists", "scam", "swindle"},
	"serial-killers":         {"serial killer", "serial killers"},
	"superheroes":            {"superhero", "superheroes", "comic book"},
	"martial-arts":           {"martial arts", "kung fu", "karate"},
	"post-apocalyptic":       {"post apocalyptic", "post-apocalyptic", "apocalypse"},
	"dystopia":               {"dystopia", "dystopian"},
	"dark-comedy":            {"dark comedy", "black comedy"},
	"mind-bending":           {"mind bending", "mind-bending", "twist ending"},
	"hackers":                {"hacker", "hackers", "cybersecurity", "cyber"},
	"zombies":                {"zombie", "zombies"},
	"vampires":               {"vampire", "vampires"},
	"sports":                 {"sports", "sport", "boxing", "football", "basketball"},
	"family-adventure":       {"family adventure", "family adventures"},
	"nordic":                 {"nordic", "nordic noir", "scandi", "scandinavian"},
	"hindi":                  {"bollywood", "hindi", "indian"},
	"british":                {"british", "uk", "england"},
}

var categoryParentsByID = func() map[string][]string {
	m := map[string][]string{}
	seen := map[string]map[string]bool{}
	for g, ids := range parentGenreCategories {
		label := titleGenreLabel(g)
		for _, id := range ids {
			if seen[id] == nil {
				seen[id] = map[string]bool{}
			}
			if !seen[id][label] {
				seen[id][label] = true
				m[id] = append(m[id], label)
			}
		}
	}
	return m
}()

var featuredSet = func() map[string]int {
	m := make(map[string]int, len(featuredSubcategories))
	for i, id := range featuredSubcategories {
		m[id] = i + 1
	}
	return m
}()

var animeSubSet = func() map[string]int {
	m := make(map[string]int, len(animeSubcategories))
	for i, id := range animeSubcategories {
		m[id] = i + 1
	}
	return m
}()

func titleGenreLabel(low string) string {
	switch low {
	case "sci-fi & fantasy":
		return "Sci-Fi & Fantasy"
	case "action & adventure":
		return "Action & Adventure"
	case "war & politics":
		return "War & Politics"
	case "science fiction":
		return "Science Fiction"
	}
	return strings.ToUpper(low[:1]) + low[1:]
}

func categoryAvailable(c Category, sw string, kid bool, now time.Time) bool {
	if !c.inExploreTab(sw) || !c.InSeason(now) || (kid && !c.Kid) {
		return false
	}
	for _, k := range Kinds(sw) {
		if q := c.Query(k); q != nil && !(kid && kidBlocked(q.Genres)) {
			return true
		}
	}
	return false
}

func (c Category) inExploreTab(sw string) bool {
	switch sw {
	case "anime":
		return animeSubSet[c.ID] > 0
	case "movies", "shows":
		return c.ID != "anime" && c.ID != "shonen" && c.ID != "isekai"
	default:
		return true
	}
}

func summarizeCategory(c Category) CategorySummary {
	return CategorySummary{
		ID:           c.ID,
		Name:         c.Name,
		Section:      c.Section,
		SectionTitle: sectionTitles[c.Section],
		Movie:        c.Movie != nil,
		Show:         c.Show != nil,
		Featured:     featuredSet[c.ID] > 0,
		ParentGenres: categoryParentsByID[c.ID],
		Aliases:      categoryAliases[c.ID],
	}
}

var leadNoiseWords = map[string]bool{
	"movie": true, "movies": true, "film": true, "films": true,
	"show": true, "shows": true, "tv": true, "series": true,
	"drama": true, "dramas": true, "category": true, "genre": true,
	"best": true, "top": true, "good": true, "great": true, "new": true,
}

func normalizeCatQuery(q string) string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(q)))
	return strings.Join(fields, " ")
}

func strippedCatQuery(q string) string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(q)))
	kept := make([]string, 0, len(fields))
	for _, f := range fields {
		if !leadNoiseWords[f] {
			kept = append(kept, f)
		}
	}
	return strings.Join(kept, " ")
}

func categoryMatchScore(c Category, qNorm, qCore string) (score int, strong bool) {
	if qNorm == "" {
		return 0, false
	}
	nameLow := strings.ToLower(c.Name)
	idLow := strings.ToLower(strings.ReplaceAll(c.ID, "-", " "))
	terms := []string{nameLow, strings.ToLower(c.ID), idLow}
	terms = append(terms, categoryAliases[c.ID]...)

	for _, term := range terms {
		tLow := strings.ToLower(term)
		if qNorm == tLow || (qCore != "" && qCore == tLow) {
			return 120, true
		}
	}
	for _, term := range terms {
		tLow := strings.ToLower(term)
		if len(qNorm) >= 3 && (strings.HasPrefix(tLow, qNorm) || (qCore != "" && len(qCore) >= 3 && strings.HasPrefix(tLow, qCore))) {
			return 85, true
		}
		if len(tLow) >= 4 && strings.Contains(" "+qNorm+" ", " "+tLow+" ") {
			return 75, false
		}
		if len(qNorm) >= 4 && strings.Contains(tLow, qNorm) {
			return 65, false
		}
	}
	// Keyword fallback for partial descriptors.
	checkQuery := func(cq *CatQuery) int {
		if cq == nil {
			return 0
		}
		for _, kw := range cq.Keywords {
			kl := strings.ToLower(kw)
			if qNorm == kl || (qCore != "" && qCore == kl) {
				return 70
			}
			if len(qNorm) >= 4 && strings.Contains(kl, qNorm) {
				return 45
			}
		}
		return 0
	}
	best := max(checkQuery(c.Movie), checkQuery(c.Show))
	return best, best >= 70
}

// MatchCategories returns available subcategories matching a free-text query.
func MatchCategories(query, sw string, kid bool, now time.Time) []CategorySummary {
	qNorm := normalizeCatQuery(query)
	qCore := strippedCatQuery(query)
	if len(qNorm) < 2 {
		return []CategorySummary{}
	}
	type hit struct {
		cat   Category
		score int
		idx   int
	}
	var hits []hit
	for idx, c := range Categories {
		if !categoryAvailable(c, sw, kid, now) {
			continue
		}
		sc, _ := categoryMatchScore(c, qNorm, qCore)
		if sc > 0 {
			hits = append(hits, hit{cat: c, score: sc, idx: idx})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		fi, fj := featuredSet[hits[i].cat.ID] > 0, featuredSet[hits[j].cat.ID] > 0
		if fi != fj {
			return fi
		}
		return hits[i].idx < hits[j].idx
	})
	seen := map[string]bool{}
	out := make([]CategorySummary, 0, min(12, len(hits)))
	for _, h := range hits {
		key := strings.ToLower(h.cat.Name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, summarizeCategory(h.cat))
		if len(out) >= 12 {
			break
		}
	}
	return out
}

// IsCategoryLeadQuery reports whether query directly asks for a category/subcategory
// (e.g. "legal", "courtroom", "legal drama", "heist movies", "isekai").
func IsCategoryLeadQuery(query, sw string, kid bool, now time.Time) bool {
	qNorm := normalizeCatQuery(query)
	qCore := strippedCatQuery(query)
	if len(qNorm) < 2 {
		return false
	}
	for _, c := range Categories {
		if !categoryAvailable(c, sw, kid, now) {
			continue
		}
		sc, strong := categoryMatchScore(c, qNorm, qCore)
		if strong && sc >= 85 {
			return true
		}
	}
	return false
}

// SubcategoriesFor returns ordered subcategories for an Explore tab, optional
// parent genre name, and optional search filter.
func SubcategoriesFor(sw, genre, query string, kid bool, now time.Time) []CategorySummary {
	if strings.TrimSpace(query) != "" {
		return MatchCategories(query, sw, kid, now)
	}
	gLow := strings.ToLower(strings.TrimSpace(genre))
	parentOrder := map[string]int{}
	for i, id := range parentGenreCategories[gLow] {
		parentOrder[id] = i + 1
	}

	type cand struct {
		cat  Category
		rank int
		idx  int
	}
	var list []cand
	for idx, c := range Categories {
		if !categoryAvailable(c, sw, kid, now) {
			continue
		}
		rank := 10000 + idx
		if sw == "anime" {
			if r := animeSubSet[c.ID]; r > 0 {
				rank = r
			}
		} else if r := parentOrder[c.ID]; r > 0 {
			rank = r
		} else if gLow == "" {
			if r := featuredSet[c.ID]; r > 0 {
				rank = 500 + r
			}
		} else if r := featuredSet[c.ID]; r > 0 {
			rank = 2000 + r
		}
		list = append(list, cand{cat: c, rank: rank, idx: idx})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].rank != list[j].rank {
			return list[i].rank < list[j].rank
		}
		return list[i].idx < list[j].idx
	})
	seen := map[string]bool{}
	out := make([]CategorySummary, 0, len(list))
	for _, it := range list {
		k := strings.ToLower(it.cat.Name)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, summarizeCategory(it.cat))
	}
	return out
}

// MatchTasteCategories picks up to max personalized subcategories matching the
// member's keyword and genre affinity profile.
func MatchTasteCategories(keywords, genres map[string]float64, sw string, kid bool, now time.Time, max int) []Category {
	if max <= 0 || (len(keywords) == 0 && len(genres) == 0) {
		return nil
	}
	if sw == "" {
		sw = "all"
	}
	type scored struct {
		cat   Category
		score float64
		idx   int
	}
	var cands []scored
	for idx, c := range Categories {
		if !categoryAvailable(c, sw, kid, now) {
			continue
		}
		// Avoid duplicating top-level single genre rows that already exist as TypeGenre.
		if c.Section == "genres" && featuredSet[c.ID] == 0 {
			continue
		}
		sc := 0.0
		check := func(cq *CatQuery) {
			if cq == nil {
				return
			}
			for _, kw := range cq.Keywords {
				kl := strings.ToLower(kw)
				sc += keywords[kl] * 3.0
				// Partial overlap (e.g. "legal drama" vs "lawyer" / "legal").
				for tk, w := range keywords {
					if tk != kl && len(tk) >= 4 && (strings.Contains(tk, kl) || strings.Contains(kl, tk)) {
						sc += w * 1.4
					}
				}
			}
			if cq.Lang != "" && keywords["lang:"+cq.Lang] > 0 {
				sc += keywords["lang:"+cq.Lang] * 2.5
			}
		}
		check(c.Movie)
		check(c.Show)
		for g, w := range genres {
			if w <= 0 {
				continue
			}
			for _, pid := range parentGenreCategories[strings.ToLower(g)] {
				if pid == c.ID {
					sc += w * 0.35
					break
				}
			}
		}
		if featuredSet[c.ID] > 0 {
			sc *= 1.15
		}
		if sc > 0 {
			cands = append(cands, scored{cat: c, score: sc, idx: idx})
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].idx < cands[j].idx
	})
	var out []Category
	seenName := map[string]bool{}
	for _, c := range cands {
		k := strings.ToLower(c.cat.Name)
		if seenName[k] {
			continue
		}
		seenName[k] = true
		out = append(out, c.cat)
		if len(out) == max {
			break
		}
	}
	return out
}
