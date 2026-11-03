package catalog

import (
	"context"
	"time"

	"github.com/braedonsaunders/ether/internal/store"
)

// refreshShowAdded advances a durable episode cursor. Each transaction reads
// at most 256 index entries, including ties; restarting a leader resumes it.
func (c *Catalog) refreshShowAdded(ctx context.Context) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var ts, key int64
	if err = tx.QueryRowContext(ctx, `SELECT ts,episode_key FROM catalog_show_added_cursor WHERE id=1`).Scan(&ts, &key); err != nil {
		return err
	}
	now := c.now().Unix()
	indexHint := ""
	if store.DialectOf(c.db) == store.SQLite {
		indexHint = " INDEXED BY catalog_titles_episode_recent"
	}
	rows, err := tx.QueryContext(ctx, `SELECT released_ts,key,show_key FROM catalog_titles`+indexHint+` WHERE type='episode' AND release_known=1 AND released_ts<=? AND (released_ts,key)>(?,?) ORDER BY released_ts ASC,key ASC LIMIT 256`, now, ts, key)
	if err != nil {
		return err
	}
	type episode struct{ ts, key, show int64 }
	var batch []episode
	for rows.Next() {
		var e episode
		if err = rows.Scan(&e.ts, &e.key, &e.show); err != nil {
			rows.Close()
			return err
		}
		batch = append(batch, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, e := range batch {
		// A recomputation handles a premiere later than an earlier aired episode.
		_, err = tx.ExecContext(ctx, `UPDATE catalog_titles SET show_added_ts=COALESCE((SELECT e.released_ts FROM catalog_titles e WHERE e.type='episode' AND e.show_key=catalog_titles.key AND e.release_known=1 AND e.released_ts<=? ORDER BY e.released_ts DESC LIMIT 1),released_ts) WHERE type='show' AND key=?`, now, e.show)
		if err != nil {
			return err
		}
		ts, key = e.ts, e.key
	}
	if len(batch) < 256 {
		ts = now
		key = 0
	}
	if _, err = tx.ExecContext(ctx, `UPDATE catalog_show_added_cursor SET ts=?,episode_key=? WHERE id=1`, ts, key); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if len(batch) > 0 {
		c.writeGen.Add(1)
	}
	return nil
}

func (c *Catalog) refreshShowAddedLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := c.refreshShowAdded(ctx); err != nil && ctx.Err() == nil {
			c.log.Warn("catalog: refresh show date added", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
