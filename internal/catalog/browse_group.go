package catalog

import (
	"fmt"
	"github.com/braedonsaunders/ether/internal/core"
	"strings"
)

func (c *Catalog) compileBrowseGroup(g *core.FilterGroup, alias string, kind core.KeyKind) (string, []any, error) {
	if g.Unwatched != nil || g.InProgress != nil {
		var parts []string
		var args []any
		state := func(column string, want bool) {
			if g.UserID == 0 {
				if want {
					parts = append(parts, "1=0")
				} else {
					parts = append(parts, "1=1")
				}
				return
			}
			predicate := "EXISTS (SELECT 1 FROM state_watch w WHERE w.user_id=? AND w.rating_key=" + alias + ".key AND w." + column + ">0)"
			if !want {
				predicate = "NOT " + predicate
			}
			parts = append(parts, predicate)
			args = append(args, g.UserID)
		}
		if g.Unwatched != nil {
			state("view_count", !*g.Unwatched)
		}
		if g.InProgress != nil {
			state("view_offset_ns", *g.InProgress)
		}
		return strings.Join(parts, " AND "), args, nil
	}
	if g.Filter != nil {
		f := *g.Filter
		if kind == 0 {
			return c.browseWhere(f)
		}
		show, excluded := splitStoredFilter(f, kind)
		sql, args, err := c.browseWhere(show)
		if err != nil {
			return "", nil, err
		}
		child, vals := c.childFilterConds(alias, f, excluded)
		if len(child) > 0 {
			sql += " AND " + strings.Join(child, " AND ")
			args = append(args, vals...)
		}
		return sql, args, nil
	}
	if g.Operator != "and" && g.Operator != "or" {
		return "", nil, fmt.Errorf("catalog: invalid filter group operator %q", g.Operator)
	}
	parts := make([]string, 0, len(g.Children))
	var args []any
	for i := range g.Children {
		sql, vals, err := c.compileBrowseGroup(&g.Children[i], alias, kind)
		if err != nil {
			return "", nil, err
		}
		parts = append(parts, "("+sql+")")
		args = append(args, vals...)
	}
	if len(parts) == 0 {
		return "1=1", nil, nil
	}
	op := " AND "
	if g.Operator == "or" {
		op = " OR "
	}
	return strings.Join(parts, op), args, nil
}
