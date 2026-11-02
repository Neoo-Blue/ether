package livetv

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"

	"github.com/braedonsaunders/ether/internal/store"
)

// livetv/3 folds the retired per-channel viewer list (channel.allowedUsers)
// into the viewer policies, keeping exactly the access each viewer had: a
// channel was watchable when the viewer's channel list allowed it AND the
// channel's viewer list (empty meaning everyone) named them. Every policy
// that listed channels becomes Restricted to the channels it still reaches;
// a viewer on "all channels" who was left off some channel's list is
// restricted to every other channel. Owners are skipped (they see
// everything). The channel field is then removed.
func init() {
	store.Register("livetv", 3, "SELECT 1;")
	store.RegisterSQLiteApply("livetv", 3, carryChannelViewersSQLite)
	store.RegisterDialect("livetv", 3, store.Postgres, carryChannelViewersPostgres)
}

func carryChannelViewersSQLite(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, data FROM livetv_channels ORDER BY id`)
	if err != nil {
		return err
	}
	var ids []string
	limits := map[string][]int64{} // channel -> its non-empty viewer list
	for rows.Next() {
		var id string
		var b []byte
		var legacy struct {
			AllowedUsers []int64 `json:"allowedUsers"`
		}
		if err := rows.Scan(&id, &b); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal(b, &legacy); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
		if len(legacy.AllowedUsers) > 0 {
			limits[id] = legacy.AllowedUsers
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	policies := map[int64]Policy{}
	var viewers []int64
	rows, err = tx.QueryContext(ctx, `SELECT user_id, data FROM livetv_policies ORDER BY user_id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var b []byte
		var p Policy
		if err := rows.Scan(&id, &b); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal(b, &p); err != nil {
			rows.Close()
			return err
		}
		p.UserID = id
		policies[id] = p
		viewers = append(viewers, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	owners := map[int64]bool{}
	var hasUsers int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='auth_users'`).Scan(&hasUsers); err != nil {
		return err
	}
	if hasUsers > 0 {
		rows, err = tx.QueryContext(ctx, `SELECT id, is_owner FROM auth_users ORDER BY id`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			var owner bool
			if err := rows.Scan(&id, &owner); err != nil {
				rows.Close()
				return err
			}
			if owner {
				owners[id] = true
			} else if _, ok := policies[id]; !ok {
				viewers = append(viewers, id)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}

	for _, id := range viewers {
		if owners[id] {
			continue
		}
		p, stored := policies[id]
		if !stored {
			p = Policy{UserID: id}
		}
		denied := func(channel string) bool {
			allowed, limited := limits[channel]
			return limited && !slices.Contains(allowed, id)
		}
		switch {
		case len(p.Channels) > 0:
			p.Channels = slices.DeleteFunc(p.Channels, denied)
		case slices.ContainsFunc(ids, denied):
			p.Channels = slices.DeleteFunc(slices.Clone(ids), denied)
		default:
			continue
		}
		p.Restricted = true
		if err := savePolicy(ctx, tx, p); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE livetv_channels SET data=json_remove(data,'$.allowedUsers') WHERE json_extract(data,'$.allowedUsers') IS NOT NULL`)
	return err
}

// carryChannelViewersPostgres is carryChannelViewersSQLite in SQL.
const carryChannelViewersPostgres = `DO $$ BEGIN
IF to_regclass('livetv_channels') IS NULL OR to_regclass('livetv_policies') IS NULL THEN RETURN; END IF;
CREATE TEMP TABLE livetv_v3_limits ON COMMIT DROP AS
 SELECT id AS channel_id, data::jsonb->'allowedUsers' AS allowed FROM livetv_channels
 WHERE jsonb_typeof(data::jsonb->'allowedUsers') = 'array' AND jsonb_array_length(data::jsonb->'allowedUsers') > 0;
CREATE TEMP TABLE livetv_v3_viewers(user_id BIGINT PRIMARY KEY) ON COMMIT DROP;
INSERT INTO livetv_v3_viewers SELECT user_id FROM livetv_policies ON CONFLICT DO NOTHING;
IF to_regclass('auth_users') IS NOT NULL THEN
 INSERT INTO livetv_v3_viewers SELECT id FROM auth_users ON CONFLICT DO NOTHING;
 DELETE FROM livetv_v3_viewers v USING auth_users a WHERE a.id = v.user_id AND a.is_owner <> 0;
END IF;
INSERT INTO livetv_policies(user_id, data)
SELECT x.user_id, ((x.data - 'channels') || jsonb_build_object('restricted', true, 'channels',
 CASE WHEN x.listed THEN
  (SELECT COALESCE(jsonb_agg(e.ch ORDER BY e.o), '[]'::jsonb) FROM (
   SELECT ch, MIN(o) AS o FROM jsonb_array_elements_text(x.data->'channels') WITH ORDINALITY AS a(ch, o)
   WHERE ch <> '' AND NOT EXISTS (SELECT 1 FROM livetv_v3_limits l WHERE l.channel_id = a.ch AND NOT l.allowed @> to_jsonb(x.user_id))
   GROUP BY ch) e)
 ELSE
  (SELECT COALESCE(jsonb_agg(c.id ORDER BY c.id), '[]'::jsonb) FROM livetv_channels c
   WHERE NOT EXISTS (SELECT 1 FROM livetv_v3_limits l WHERE l.channel_id = c.id AND NOT l.allowed @> to_jsonb(x.user_id)))
 END))::text
FROM (
 SELECT v.user_id,
  COALESCE(p.data::jsonb, jsonb_build_object('userId', v.user_id, 'disabled', false)) AS data,
  COALESCE(jsonb_typeof(p.data::jsonb->'channels') = 'array' AND jsonb_array_length(p.data::jsonb->'channels') > 0, false) AS listed
 FROM livetv_v3_viewers v LEFT JOIN livetv_policies p ON p.user_id = v.user_id
) x
WHERE x.listed OR EXISTS (SELECT 1 FROM livetv_v3_limits l WHERE NOT l.allowed @> to_jsonb(x.user_id))
ON CONFLICT (user_id) DO UPDATE SET data = excluded.data;
UPDATE livetv_channels SET data = (data::jsonb - 'allowedUsers')::text WHERE data::jsonb->'allowedUsers' IS NOT NULL;
END $$;`
