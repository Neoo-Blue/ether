package catalogue

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/braedonsaunders/ether/internal/store"
)

func init() {
	store.Register("plugins_catalogue", 1, `
CREATE TABLE IF NOT EXISTS plugins_catalogue_index (
  id         INTEGER PRIMARY KEY CHECK (id = 1),
  raw_json   TEXT NOT NULL DEFAULT '',
  signature  TEXT NOT NULL DEFAULT '',
  fetched_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS plugins_catalogue_entries (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  summary    TEXT NOT NULL DEFAULT '',
  kind       TEXT NOT NULL DEFAULT '',
  category   TEXT NOT NULL DEFAULT '',
  revoked    INTEGER NOT NULL DEFAULT 0,
  reason     TEXT NOT NULL DEFAULT '',
  entry_json TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);
`)
}

type cacheStore struct {
	db *sql.DB
}

func (s *cacheStore) save(ctx context.Context, idx Index, rawJSON []byte, sig string, fetchedAt time.Time) error {
	if s.db == nil {
		return nil
	}
	unix := fetchedAt.Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
INSERT INTO plugins_catalogue_index (id, raw_json, signature, fetched_at)
VALUES (1, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
  raw_json = excluded.raw_json,
  signature = excluded.signature,
  fetched_at = excluded.fetched_at`, string(rawJSON), sig, unix); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM plugins_catalogue_entries`); err != nil {
		return err
	}
	for _, e := range idx.Entries {
		entryBytes, err := json.Marshal(e)
		if err != nil {
			return err
		}
		rev := 0
		if e.Revoked {
			rev = 1
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO plugins_catalogue_entries (id, name, summary, kind, category, revoked, reason, entry_json, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			e.ID, e.Name, e.Summary, e.Kind, e.Category, rev, e.Reason, string(entryBytes), unix); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *cacheStore) load(ctx context.Context, key ed25519.PublicKey) (Index, time.Time, bool) {
	if s.db == nil {
		return Index{}, time.Time{}, false
	}
	var raw, signature string
	var fetched int64
	err := s.db.QueryRowContext(ctx, `SELECT raw_json, signature, fetched_at FROM plugins_catalogue_index WHERE id = 1`).Scan(&raw, &signature, &fetched)
	if err != nil || raw == "" {
		return Index{}, time.Time{}, false
	}
	idx, err := ParseAndVerify([]byte(raw), []byte(signature), key)
	if err != nil {
		return Index{}, time.Time{}, false
	}
	return idx, time.Unix(fetched, 0).UTC(), true
}
