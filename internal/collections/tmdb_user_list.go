package collections

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/tmdb"
)

var tmdbUserListPath = regexp.MustCompile(`^/list/([0-9]+)(?:-[^/]+)?(?:/[^/]+)?/?$`)

func parseTMDBUserList(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	id, err := strconv.ParseInt(raw, 10, 64)
	if err == nil && id > 0 {
		return id, nil
	}
	u, err := url.Parse(raw)
	if err == nil && u.Scheme == "https" && (strings.EqualFold(u.Host, "www.themoviedb.org") || strings.EqualFold(u.Host, "themoviedb.org")) && u.User == nil {
		if m := tmdbUserListPath.FindStringSubmatch(u.Path); m != nil {
			id, err = strconv.ParseInt(m[1], 10, 64)
			if err == nil && id > 0 {
				return id, nil
			}
		}
	}
	return 0, fmt.Errorf("enter a TMDB list ID or a link like https://www.themoviedb.org/list/123")
}

type tmdbUserListKind struct{}

func (tmdbUserListKind) ID() string { return "tmdbUserList" }
func (tmdbUserListKind) Describe() KindDescribe {
	return KindDescribe{Label: extend.T("TMDB user list"), Hint: extend.T("A public TMDB list URL or ID"), Icon: "list-video", Order: 135, Editor: "urlList", Defaults: `{"url":""}`, Params: []extend.ParamField{{Name: "url", Kind: "url", Label: extend.T("List URL"), Required: true, Placeholder: "https://www.themoviedb.org/list/123"}}}
}
func (tmdbUserListKind) Validate(ve *ValidationError, path string, src core.CollectionSource) {
	if _, err := parseTMDBUserList(src.URL); err != nil {
		ve.add(path+".url", err.Error())
	}
}
func (tmdbUserListKind) Gather(g *Gather) {
	id, err := parseTMDBUserList(g.Src.URL)
	if err != nil {
		g.Warn("%v", err)
		return
	}
	for page := 1; page <= maxMembers; page++ {
		if g.Svc.api == nil || g.Svc.api.c == nil {
			g.Warn("TMDB is not configured")
			return
		}
		p, err := tmdb.GetUserListPage(g.Ctx, g.Svc.api.c, id, page)
		if err != nil {
			var he *tmdb.HTTPError
			if errors.Is(err, core.ErrNotFound) || (errors.As(err, &he) && (he.StatusCode == http.StatusForbidden || he.StatusCode == http.StatusUnauthorized)) {
				g.Warn("TMDB list not found or private; check the link and make the list public")
			} else {
				g.Warn("TMDB list page %d could not be loaded: %v", page, err)
			}
			if page > 1 {
				g.Res.err = &partialListError{msg: fmt.Sprintf("TMDB list is incomplete: page %d failed: %v", page, err)}
			}
			return
		}
		items := p.Results
		if items == nil {
			items = p.Items
		}
		for _, it := range items {
			if it.ID <= 0 {
				continue
			}
			switch it.MediaType {
			case "movie":
				g.Res.keys = append(g.Res.keys, core.MovieKey(it.ID))
			case "tv":
				g.Res.keys = append(g.Res.keys, core.ShowKey(it.ID))
			}
			if len(g.Res.keys) == maxMembers {
				return
			}
		}
		if len(items) == 0 || p.TotalPages <= page {
			return
		}
	}
}
