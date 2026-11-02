package livetv

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/braedonsaunders/ether/internal/store"
)

// livetv/3 retires the per-channel viewer list (channel.allowedUsers) without
// widening anyone's access. A channel with a list becomes Private, which hides
// it from every viewer the owner has not given it to, including viewers added
// later. Each listed viewer is given it: an unrestricted viewer as a grant; a
// restricted one keeps it only when their own list already had it (both lists
// had to allow it). Restricted viewers left off a channel's list lose it from
// theirs. Every stored policy that listed channels becomes Restricted, which
// is what a non-empty list meant before. Owners see everything and are
// skipped. Reruns change nothing.
func init() {
	store.Register("livetv", 3, "SELECT 1;")
	store.RegisterApply("livetv", 3, retireChannelViewerLists)
}

func retireChannelViewerLists(ctx context.Context, tx *sql.Tx) error {
	type channel struct {
		id   string
		data map[string]json.RawMessage
		list []int64
	}
	var channels []channel
	rows, err := tx.QueryContext(ctx, `SELECT id, data FROM livetv_channels ORDER BY id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var c channel
		var b []byte
		if err := rows.Scan(&c.id, &b); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal(b, &c.data); err != nil {
			rows.Close()
			return fmt.Errorf("live TV channel %s: %w", c.id, err)
		}
		if raw, ok := c.data["allowedUsers"]; ok {
			if err := json.Unmarshal(raw, &c.list); err != nil {
				rows.Close()
				return fmt.Errorf("live TV channel %s viewers: %w", c.id, err)
			}
			channels = append(channels, c)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	policies := map[int64]*Policy{}
	rows, err = tx.QueryContext(ctx, `SELECT user_id, data FROM livetv_policies ORDER BY user_id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var b []byte
		p := &Policy{}
		if err := rows.Scan(&id, &b); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal(b, p); err != nil {
			rows.Close()
			return fmt.Errorf("live TV policy %d: %w", id, err)
		}
		p.UserID = id
		policies[id] = p
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	owners, err := ownerIDs(ctx, tx)
	if err != nil {
		return err
	}

	changed := map[int64]bool{}
	for id, p := range policies {
		if len(p.Channels) > 0 && !p.Restricted {
			p.Restricted = true
			changed[id] = true
		}
	}
	for _, c := range channels {
		if len(c.list) == 0 {
			continue
		}
		for id, p := range policies {
			if p.Restricted && !slices.Contains(c.list, id) && slices.Contains(p.Channels, c.id) {
				p.Channels = slices.DeleteFunc(p.Channels, func(ch string) bool { return ch == c.id })
				changed[id] = true
			}
		}
		for _, id := range c.list {
			if id <= 0 || owners[id] {
				continue
			}
			p, ok := policies[id]
			if !ok {
				p = &Policy{UserID: id}
				policies[id] = p
			}
			if !p.Restricted && !slices.Contains(p.Granted, c.id) {
				p.Granted = append(p.Granted, c.id)
				changed[id] = true
			}
		}
	}
	ids := make([]int64, 0, len(changed))
	for id := range changed {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if err := savePolicy(ctx, tx, *policies[id]); err != nil {
			return err
		}
	}

	for _, c := range channels {
		delete(c.data, "allowedUsers")
		if len(c.list) > 0 {
			c.data["private"] = json.RawMessage("true")
		}
		b, err := json.Marshal(c.data)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE livetv_channels SET data=? WHERE id=?`, b, c.id); err != nil {
			return err
		}
	}
	return nil
}

// ownerIDs lists owner accounts, or none when the auth tables are absent (a
// Live TV-only database). The probe runs in a savepoint so a missing table
// does not abort the migration transaction on PostgreSQL.
func ownerIDs(ctx context.Context, tx *sql.Tx) (map[int64]bool, error) {
	if _, err := tx.ExecContext(ctx, `SAVEPOINT livetv_v3_owners`); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM auth_users WHERE is_owner <> 0`)
	if err != nil {
		if _, rerr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT livetv_v3_owners`); rerr != nil {
			return nil, rerr
		}
		return map[int64]bool{}, nil
	}
	owners := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		owners[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT livetv_v3_owners`)
	return owners, err
}
