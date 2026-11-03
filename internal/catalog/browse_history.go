package catalog

import (
	"context"
	"github.com/braedonsaunders/ether/internal/core"
)

// browseHistoryPage puts dated watch history first, followed by never-viewed
// titles in catalogue order. The history index bounds the first walk to this
// viewer; state changes are read afresh for every request.
func (c *Catalog) browseHistoryPage(ctx context.Context, q core.BrowseQuery, where string, args []any, alias string, offset, limit int) ([]core.Title, error) {
	from := "catalog_titles t"
	historyJoin := " CROSS JOIN catalog_titles t"
	historyLink := "t.key=w.rating_key"
	if alias != "t" {
		from = "catalog_titles " + alias + " JOIN catalog_titles t ON t.key=" + alias + ".show_key"
		historyJoin = " CROSS JOIN catalog_titles " + alias + " CROSS JOIN catalog_titles t"
		historyLink = alias + ".key=w.rating_key AND t.key=" + alias + ".show_key"
	}
	var count int
	if q.SortViewerID > 0 {
		a := append([]any{q.SortViewerID}, args...)
		if err := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM state_watch w"+historyJoin+" WHERE w.user_id=? AND w.last_viewed_at>0 AND "+historyLink+" AND "+where, a...).Scan(&count); err != nil {
			return nil, err
		}
	}
	items := []core.Title{}
	if offset < count && q.SortViewerID > 0 {
		dir := "ASC"
		if q.Desc {
			dir = "DESC"
		}
		order := "w.last_viewed_at " + dir + browseSecondaryOrder(alias, q.SecondarySorts)
		if alias == "t" {
			order += ", t.sort_title ASC"
		}
		order += ", " + alias + ".key ASC"
		a := append([]any{q.SortViewerID}, args...)
		a = append(a, limit, offset)
		rows, err := c.queryTitles(ctx, "SELECT "+prefixedCols(alias)+" FROM state_watch w"+historyJoin+" WHERE w.user_id=? AND w.last_viewed_at>0 AND "+historyLink+" AND "+where+" ORDER BY "+order+" LIMIT ? OFFSET ?", a...)
		if err != nil {
			return nil, err
		}
		items = append(items, rows...)
	}
	if len(items) < limit && offset+len(items) >= count {
		skip := max(0, offset-count)
		q.SortLastViewed = false
		q.Desc = false
		order := browseOrder(q)
		if alias != "t" {
			order = storedChildOrder(alias, q)
		}
		a := append([]any{}, args...)
		predicate := ""
		if q.SortViewerID > 0 {
			predicate = " AND NOT EXISTS (SELECT 1 FROM state_watch w WHERE w.user_id=? AND w.rating_key=" + alias + ".key AND w.last_viewed_at>0)"
			a = append(a, q.SortViewerID)
		}
		a = append(a, limit-len(items), skip)
		rows, err := c.queryTitles(ctx, "SELECT "+prefixedCols(alias)+" FROM "+from+" WHERE "+where+predicate+" ORDER BY "+order+", "+alias+".key ASC LIMIT ? OFFSET ?", a...)
		if err != nil {
			return nil, err
		}
		items = append(items, rows...)
	}
	return items, nil
}
