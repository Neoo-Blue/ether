package catalog

import "github.com/braedonsaunders/ether/internal/store"

// The backfill is one set-based UPDATE: a grouped pass over aired episodes,
// left-joined to shows and applied by primary key, so each show is written
// once and the plan does not depend on statistics. A per-show correlated
// subquery took ten minutes on a 42k-show upgrade: SQLite planned it through
// catalog 26's new, not-yet-analysed index and scanned every episode per
// show. Shows without an aired episode keep their own release date.
func init() {
	store.Register("catalog", 28, `
ALTER TABLE catalog_titles ADD COLUMN show_added_ts INTEGER NOT NULL DEFAULT 0;
UPDATE catalog_titles AS t SET show_added_ts=x.ts FROM (SELECT s.key, COALESCE(a.ts, s.released_ts) AS ts FROM catalog_titles s LEFT JOIN (SELECT show_key, MAX(released_ts) AS ts FROM catalog_titles WHERE type='episode' AND release_known=1 AND released_ts<=CAST(strftime('%s','now') AS INTEGER) GROUP BY show_key) AS a ON a.show_key=s.key WHERE s.type='show') AS x WHERE t.key=x.key AND x.ts<>0;
CREATE TABLE catalog_show_added_cursor (id INTEGER PRIMARY KEY, ts INTEGER NOT NULL, episode_key INTEGER NOT NULL);
INSERT INTO catalog_show_added_cursor VALUES (1, CAST(strftime('%s','now') AS INTEGER)-1, 9223372036854775807);
CREATE INDEX catalog_titles_type_browse_show_added ON catalog_titles(type,browsable,adult,show_added_ts DESC,key ASC);
`)
	store.RegisterDialect("catalog", 28, store.Postgres, `
ALTER TABLE catalog_titles ADD COLUMN show_added_ts BIGINT NOT NULL DEFAULT 0;
UPDATE catalog_titles AS t SET show_added_ts=x.ts FROM (SELECT s.key, COALESCE(a.ts, s.released_ts) AS ts FROM catalog_titles s LEFT JOIN (SELECT show_key, MAX(released_ts) AS ts FROM catalog_titles WHERE type='episode' AND release_known=1 AND released_ts<=EXTRACT(EPOCH FROM CURRENT_TIMESTAMP) GROUP BY show_key) AS a ON a.show_key=s.key WHERE s.type='show') AS x WHERE t.key=x.key AND x.ts<>0;
CREATE TABLE catalog_show_added_cursor (id BIGINT PRIMARY KEY, ts BIGINT NOT NULL, episode_key BIGINT NOT NULL);
INSERT INTO catalog_show_added_cursor VALUES (1, CAST(EXTRACT(EPOCH FROM CURRENT_TIMESTAMP) AS BIGINT)-1, 9223372036854775807);
CREATE INDEX catalog_titles_type_browse_show_added ON catalog_titles(type,browsable,adult,show_added_ts DESC,key ASC);
`)
}
