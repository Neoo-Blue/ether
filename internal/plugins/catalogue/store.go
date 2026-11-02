package catalogue

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	store.Register("plugins_catalogue", 2, `ALTER TABLE plugins_catalogue_index ADD COLUMN source_hash TEXT NOT NULL DEFAULT '';`)
}

type cacheStore struct {
	db *sql.DB
}

// rejectRollback rejects signed older indexes and equivocation at the same
// generation time. Equal indexes may be fetched repeatedly.
func rejectRollback(old, next Index) error {
	if old.GeneratedAt == "" {
		return nil
	}
	prev, err := time.Parse(time.RFC3339Nano, old.GeneratedAt)
	if err != nil {
		return fmt.Errorf("catalogue: cached generation time invalid")
	}
	current, err := time.Parse(time.RFC3339Nano, next.GeneratedAt)
	if err != nil {
		return fmt.Errorf("catalogue: generation time required")
	}
	if current.Before(prev) {
		return fmt.Errorf("catalogue: older signed index rejected")
	}
	if current.Equal(prev) {
		a, _ := json.Marshal(old)
		b, _ := json.Marshal(next)
		if !bytes.Equal(a, b) {
			return fmt.Errorf("catalogue: conflicting signed index generation")
		}
	}
	return nil
}

func (s *cacheStore) save(ctx context.Context, idx Index, rawJSON []byte, sig string, fetchedAt time.Time, source string, key ed25519.PublicKey) error {
	if s.db == nil {
		return nil
	}
	unix := fetchedAt.Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Serialize comparisons and updates across nodes sharing this database.
	if _, err := tx.ExecContext(ctx, `INSERT INTO plugins_catalogue_index (id) VALUES (1) ON CONFLICT (id) DO UPDATE SET id=excluded.id`); err != nil {
		return err
	}
	var oldRaw, oldSig, oldSource string
	if err := tx.QueryRowContext(ctx, `SELECT raw_json,signature,source_hash FROM plugins_catalogue_index WHERE id=1`).Scan(&oldRaw, &oldSig, &oldSource); err != nil {
		return err
	}
	if oldRaw != "" && oldSource == sourceHash(source) {
		old, err := ParseAndVerify([]byte(oldRaw), []byte(oldSig), key)
		if err == nil {
			if err := rejectRollback(old, idx); err != nil {
				return err
			}
		}
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO plugins_catalogue_index (id, raw_json, signature, fetched_at, source_hash)
VALUES (1, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
  raw_json = excluded.raw_json,
  signature = excluded.signature,
  fetched_at = excluded.fetched_at,
  source_hash = excluded.source_hash`, string(rawJSON), sig, unix, sourceHash(source)); err != nil {
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

func (s *cacheStore) load(ctx context.Context, key ed25519.PublicKey, source string) (Index, time.Time, bool) {
	if s.db == nil {
		return Index{}, time.Time{}, false
	}
	var raw, signature, cachedSource string
	var fetched int64
	err := s.db.QueryRowContext(ctx, `SELECT raw_json, signature, fetched_at, source_hash FROM plugins_catalogue_index WHERE id = 1`).Scan(&raw, &signature, &fetched, &cachedSource)
	if err != nil || raw == "" || cachedSource != sourceHash(source) {
		return Index{}, time.Time{}, false
	}
	idx, err := ParseAndVerify([]byte(raw), []byte(signature), key)
	if err != nil {
		return Index{}, time.Time{}, false
	}
	return idx, time.Unix(fetched, 0).UTC(), true
}

func sourceHash(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}
