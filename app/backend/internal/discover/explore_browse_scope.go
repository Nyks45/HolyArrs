package discover

import (
	"strings"
	"time"

	engine "github.com/HolyArrs/holyarrs/app/backend/internal/suggest"
)

// browseScope narrows a Browse category's lists to an Explore hub, so Legal
// under K-Drama lists Korean legal titles. Page asks for a later page of
// every list. A zero scope leaves the lists as they are.
type browseScope struct {
	Lang, Genres, Without string
	Page                  int
}

// hubBrowseScope reads a hub's language and genres from its first row, the
// hub's broadest list. An unknown hub gives a zero scope.
func hubBrowseScope(id string) browseScope {
	for _, h := range exploreHubs {
		if h.Info.ID != id {
			continue
		}
		rows := h.Rows(time.Now())
		if len(rows) == 0 {
			return browseScope{}
		}
		r := rows[0]
		return browseScope{Lang: r.Lang, Genres: r.Genres, Without: r.Without}
	}
	return browseScope{}
}

// apply narrows q to the scope. Hub genres join the category's only when the
// category asks for all of its genres, since TMDB cannot mix "any" and "all".
func (s browseScope) apply(q engine.Request) engine.Request {
	if s.Lang != "" {
		q.Lang = s.Lang
	}
	if s.Genres != "" {
		switch {
		case q.Genres == "":
			q.Genres = s.Genres
		case !strings.Contains(q.Genres, "|") && !strings.Contains(s.Genres, "|"):
			q.Genres = q.Genres + "," + s.Genres
		}
	}
	if s.Without != "" && q.Without == "" {
		q.Without = s.Without
	}
	if s.Page > 1 {
		q.Page = s.Page
	}
	return q
}

func (s browseScope) zero() bool { return s == browseScope{} }
