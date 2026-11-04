package catalog

import "github.com/braedonsaunders/ether/internal/store"

// The backfill is set-based: one grouped pass over aired episodes, joined to
// shows by primary key. A per-show correlated subquery is planner-dependent;
// on an upgraded library SQLite planned it through catalog 26's new,
// not-yet-analysed index and scanned every episode per show (10 minutes on
// 42k shows). Shows without an aired episode keep their own release date.
func init() {
	store.Register("catalog", 28, `
ALTER TABLE catalog_titles ADD COLUMN show_added_ts INTEGER NOT NULL DEFAULT 0;
UPDATE catalog_titles SET show_added_ts=released_ts WHERE type='show' AND released_ts<>0;
UPDATE catalog_titles AS t SET show_added_ts=a.ts FROM (SELECT show_key, MAX(released_ts) AS ts FROM catalog_titles WHERE type='episode' AND release_known=1 AND released_ts<=CAST(strftime('%s','now') AS INTEGER) GROUP BY show_key) AS a WHERE t.key=a.show_key AND t.type='show';
CREATE TABLE catalog_show_added_cursor (id INTEGER PRIMARY KEY, ts INTEGER NOT NULL, episode_key INTEGER NOT NULL);
INSERT INTO catalog_show_added_cursor VALUES (1, CAST(strftime('%s','now') AS INTEGER)-1, 9223372036854775807);
CREATE INDEX catalog_titles_type_browse_show_added ON catalog_titles(type,browsable,adult,show_added_ts DESC,key ASC);
`)
	store.RegisterDialect("catalog", 28, store.Postgres, `
ALTER TABLE catalog_titles ADD COLUMN show_added_ts BIGINT NOT NULL DEFAULT 0;
UPDATE catalog_titles SET show_added_ts=released_ts WHERE type='show' AND released_ts<>0;
UPDATE catalog_titles AS t SET show_added_ts=a.ts FROM (SELECT show_key, MAX(released_ts) AS ts FROM catalog_titles WHERE type='episode' AND release_known=1 AND released_ts<=EXTRACT(EPOCH FROM CURRENT_TIMESTAMP) GROUP BY show_key) AS a WHERE t.key=a.show_key AND t.type='show';
CREATE TABLE catalog_show_added_cursor (id BIGINT PRIMARY KEY, ts BIGINT NOT NULL, episode_key BIGINT NOT NULL);
INSERT INTO catalog_show_added_cursor VALUES (1, CAST(EXTRACT(EPOCH FROM CURRENT_TIMESTAMP) AS BIGINT)-1, 9223372036854775807);
CREATE INDEX catalog_titles_type_browse_show_added ON catalog_titles(type,browsable,adult,show_added_ts DESC,key ASC);
`)
}
