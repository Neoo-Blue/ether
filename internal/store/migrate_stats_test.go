package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// A migration's new index on an analysed table must leave the migration with
// statistics, or a later backfill in the same upgrade is planned on SQLite's
// default estimates (catalog/28 took ten minutes that way).
func TestMigrationAnalysesNewIndexesOnAnalysedTables(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	exec := func(q string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	analyse := func() {
		t.Helper()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := analyseNewIndexes(ctx, tx); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	has := func(idx string) bool {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_stat1 WHERE idx=?`, idx).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n > 0
	}
	exec(`CREATE TABLE a(k INTEGER PRIMARY KEY, x INT, y INT); CREATE INDEX a_x ON a(x);
		CREATE TABLE b(k INTEGER PRIMARY KEY, x INT); CREATE INDEX b_x ON b(x);
		WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<500) INSERT INTO a SELECT i, i%5, i FROM n;`)
	analyse() // no statistics yet: a fresh database is left alone
	var stat1 int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema WHERE name='sqlite_stat1'`).Scan(&stat1); err != nil || stat1 != 0 {
		t.Fatalf("fresh database gained statistics: %d %v", stat1, err)
	}
	exec(`ANALYZE`)
	exec(`CREATE INDEX a_y ON a(y); CREATE INDEX b_x2 ON b(x, k)`)
	analyse()
	if !has("a_y") {
		t.Fatal("new index on an analysed table has no statistics")
	}
	if has("b_x2") {
		t.Fatal("index on a table without statistics was analysed")
	}
}
