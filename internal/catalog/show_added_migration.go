package catalog

import "github.com/braedonsaunders/ether/internal/store"

func init() {
	store.Register("catalog", 28, `
ALTER TABLE catalog_titles ADD COLUMN show_added_ts INTEGER NOT NULL DEFAULT 0;
UPDATE catalog_titles AS t SET show_added_ts=COALESCE((SELECT e.released_ts FROM catalog_titles e WHERE e.type='episode' AND e.show_key=t.key AND e.release_known=1 AND e.released_ts<=CAST(strftime('%s','now') AS INTEGER) ORDER BY e.released_ts DESC LIMIT 1), CASE WHEN t.released_ts<>0 THEN t.released_ts ELSE 0 END) WHERE t.type='show';
CREATE TABLE catalog_show_added_cursor (id INTEGER PRIMARY KEY, ts INTEGER NOT NULL, episode_key INTEGER NOT NULL);
INSERT INTO catalog_show_added_cursor VALUES (1, CAST(strftime('%s','now') AS INTEGER)-1, 9223372036854775807);
CREATE INDEX catalog_titles_type_browse_show_added ON catalog_titles(type,browsable,adult,show_added_ts DESC,key ASC);
`)
	store.RegisterDialect("catalog", 28, store.Postgres, `
ALTER TABLE catalog_titles ADD COLUMN show_added_ts BIGINT NOT NULL DEFAULT 0;
UPDATE catalog_titles AS t SET show_added_ts=COALESCE((SELECT e.released_ts FROM catalog_titles e WHERE e.type='episode' AND e.show_key=t.key AND e.release_known=1 AND e.released_ts<=EXTRACT(EPOCH FROM CURRENT_TIMESTAMP) ORDER BY e.released_ts DESC LIMIT 1), CASE WHEN t.released_ts<>0 THEN t.released_ts ELSE 0 END) WHERE t.type='show';
CREATE TABLE catalog_show_added_cursor (id BIGINT PRIMARY KEY, ts BIGINT NOT NULL, episode_key BIGINT NOT NULL);
INSERT INTO catalog_show_added_cursor VALUES (1, CAST(EXTRACT(EPOCH FROM CURRENT_TIMESTAMP) AS BIGINT)-1, 9223372036854775807);
CREATE INDEX catalog_titles_type_browse_show_added ON catalog_titles(type,browsable,adult,show_added_ts DESC,key ASC);
`)
}
