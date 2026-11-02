package collections

import (
	"context"
	"errors"

	"github.com/braedonsaunders/ether/internal/core"
)

// ErrSnapshotNotBuilt means the collection has no persisted build to inspect.
var ErrSnapshotNotBuilt = errors.New("collection snapshot has not been built")

// SnapshotList returns a bounded page of persisted collection specifications.
// It does not evaluate sources, expand franchises, or rebuild collection members.
func (s *Service) SnapshotList(ctx context.Context, offset, limit int) ([]core.Collection, error) {
	if offset < 0 {
		offset = 0
	}
	if limit < 1 || limit > 50 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+specCols+` FROM collections_specs ORDER BY builtin DESC,id ASC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]core.Collection, 0, limit)
	for rows.Next() {
		row, err := scanSpecRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, core.Collection{Key: row.key(), Title: row.spec.Name, Kind: "smart", Type: core.MediaType(row.spec.MediaType), ChildCount: row.itemCount})
	}
	return out, rows.Err()
}

// SnapshotItems returns a page of the last persisted shared members. It does
// not evaluate personal rules, refresh sources, or build a missing snapshot.
func (s *Service) SnapshotItems(ctx context.Context, key core.RatingKey, offset, limit int) (core.Page, error) {
	row, err := s.rowByKey(ctx, key)
	if err != nil {
		return core.Page{}, err
	}
	if row.lastBuilt.IsZero() || row.buildHash == "" {
		return core.Page{}, ErrSnapshotNotBuilt
	}
	if offset < 0 {
		offset = 0
	}
	if limit < 1 || limit > 50 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT key FROM collections_members WHERE spec_id=? AND shared=1 ORDER BY pos LIMIT ? OFFSET ?`, row.id, limit, offset)
	if err != nil {
		return core.Page{}, err
	}
	var keys []core.RatingKey
	for rows.Next() {
		var key core.RatingKey
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return core.Page{}, err
		}
		keys = append(keys, key)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return core.Page{}, err
	}
	var items []core.Title
	if row.spec.ReleaseMode == "upcoming" || row.spec.ReleaseMode == "mixed" {
		items, err = loadCollectionTitles(ctx, s.db, keys)
	} else {
		items, err = loadTitles(ctx, s.db, keys)
	}
	if err != nil {
		return core.Page{}, err
	}
	return core.Page{Items: items, Total: row.itemCount}, nil
}
