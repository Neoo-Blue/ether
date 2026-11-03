package livetv

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/braedonsaunders/ether/internal/store"
)

func init() {
	store.Register("livetv", 4, `CREATE TABLE IF NOT EXISTS livetv_numbers (source_id TEXT NOT NULL, channel_id TEXT NOT NULL, group_name TEXT NOT NULL, number TEXT NOT NULL, seen_ms BIGINT NOT NULL, PRIMARY KEY(source_id,channel_id));`)
}

// assignImportedNumbers reserves numbers for seven days after a channel disappears.
func assignImportedNumbers(ctx context.Context, tx *sql.Tx, source string, cs []Channel, old map[string]Channel, all []Channel, now time.Time) error {
	cutoff := now.Add(-7 * 24 * time.Hour).UnixMilli()
	if _, err := tx.ExecContext(ctx, `DELETE FROM livetv_numbers WHERE seen_ms < ?`, cutoff); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT source_id, channel_id, group_name, number FROM livetv_numbers`)
	if err != nil {
		return err
	}
	type reservation struct{ group, number string }
	saved := map[string]reservation{}
	used := map[string]map[string]bool{}
	reserve := func(group, number string) {
		if number == "" {
			return
		}
		if used[group] == nil {
			used[group] = map[string]bool{}
		}
		used[group][number] = true
	}
	for rows.Next() {
		var sourceID, id, group, number string
		if err := rows.Scan(&sourceID, &id, &group, &number); err != nil {
			rows.Close()
			return err
		}
		if sourceID == source {
			saved[id] = reservation{group, number}
		}
		reserve(group, number)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, c := range all {
		if c.SourceID != source {
			reserve(c.Group, c.Number)
		}
	}
	for _, c := range cs {
		if previous, ok := old[c.ID]; ok {
			reserve(c.Group, previous.Number)
		} else if !c.numberMissing {
			reserve(c.Group, c.Number)
		}
	}
	for i := range cs {
		c := &cs[i]
		if previous, ok := old[c.ID]; ok {
			c.Number = previous.Number
		} else if c.Number == "" || c.numberMissing {
			if v, ok := saved[c.ID]; ok && v.group == c.Group {
				c.Number = v.number
			} else {
				for n := 1; ; n++ {
					value := fmt.Sprint(n)
					if !used[c.Group][value] {
						c.Number = value
						break
					}
				}
			}
		}
		reserve(c.Group, c.Number)
		_, err := tx.ExecContext(ctx, `INSERT INTO livetv_numbers(source_id,channel_id,group_name,number,seen_ms) VALUES(?,?,?,?,?) ON CONFLICT(source_id,channel_id) DO UPDATE SET group_name=excluded.group_name,number=excluded.number,seen_ms=excluded.seen_ms`, source, c.ID, c.Group, c.Number, now.UnixMilli())
		if err != nil {
			return err
		}
	}
	return nil
}
