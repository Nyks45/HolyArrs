package suggest

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
)

// MaxSpecs is the most lists one row is read from.
const MaxSpecs = 8

// MaxRowPage is the last page a row pages to; TMDB lists stop at 500 and
// nobody scrolls one row that far.
const MaxRowPage = 60

// specLists are the lists a row's spec may name: the ones with pages.
var specLists = map[string]bool{"trending": true, "upcoming": true, "now_playing": true, "best": true, "discover": true, "similar": true}

var (
	specRef  = regexp.MustCompile(`^[1-9][0-9]{0,11}$`)
	specDay  = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	specText = regexp.MustCompile(`^[\p{L}\p{N} |,.&'_:+-]{0,300}$`)
)

// EncodeSpecs packs a row's lists into the opaque word a page sends back to
// read the row's next page. Empty when there is nothing to page.
func EncodeSpecs(specs []Request) string {
	if len(specs) == 0 || len(specs) > MaxSpecs {
		return ""
	}
	out := make([]Request, 0, len(specs))
	for _, q := range specs {
		q.Scope, q.Page = "", 0
		out = append(out, q)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeSpecs reads the word EncodeSpecs made. It refuses anything a row
// could not have been read from, so a page can only ask for list pages.
func DecodeSpecs(s string) ([]Request, error) {
	bad := errors.New("not a row's lists")
	if s == "" || len(s) > 6000 {
		return nil, bad
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, bad
	}
	var specs []Request
	if json.Unmarshal(b, &specs) != nil || len(specs) == 0 || len(specs) > MaxSpecs {
		return nil, bad
	}
	for i, q := range specs {
		if !specOK(q) {
			return nil, bad
		}
		specs[i].Scope, specs[i].Page = ScopeShared, 0
	}
	return specs, nil
}

func specOK(q Request) bool {
	if !specLists[q.List] || (q.Kind != "movie" && q.Kind != "show") {
		return false
	}
	if (q.List == "similar") != (q.Ref != "") || (q.Ref != "" && !specRef.MatchString(q.Ref)) {
		return false
	}
	for _, d := range []string{q.From, q.To, q.AirFrom, q.AirTo} {
		if d != "" && !specDay.MatchString(d) {
			return false
		}
	}
	for _, t := range []string{q.Genre, q.Services, q.Region, q.Types, q.Keywords, q.Genres, q.Without, q.Lang, q.Cert, q.Networks, q.Companies, q.Country} {
		if !specText.MatchString(t) {
			return false
		}
	}
	switch q.Sort {
	case "", "rating", "recent":
	default:
		return false
	}
	return q.MinVote >= 0 && q.MinVote <= 10 && q.MinVotes >= 0 && q.MinVotes <= 10_000_000 && q.MaxVotes >= 0 && q.MaxVotes <= 10_000_000
}
