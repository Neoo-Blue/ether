package store

import (
	"context"
	"sync"
	"testing"
)

var viewerProbeOnce sync.Once

func registerViewerProbes() {
	viewerProbeOnce.Do(func() {
		RegisterViewerTable("viewerprobe", "viewerprobe_watch", "user_id", ViewerMergeMove, "rating_key")
		RegisterViewerTable("viewerprobe", "viewerprobe_history", "user_id", ViewerMergeUnion)
		RegisterViewerTable("viewerprobe", "viewerprobe_solo", "user_id", ViewerMergeMove)
		RegisterViewerTable("viewerprobe", "viewerprobe_ephemeral", "viewer_id", ViewerMergeDrop)
	})
}

func TestViewerPurgeAndMergeProbes(t *testing.T) {
	registerViewerProbes()
	ctx := context.Background()
	db := OpenTest(t)
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS viewerprobe_watch (user_id INTEGER NOT NULL, rating_key INTEGER NOT NULL, pos INTEGER NOT NULL, PRIMARY KEY(user_id,rating_key))`,
		`CREATE TABLE IF NOT EXISTS viewerprobe_history (id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL, rating_key INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS viewerprobe_solo (user_id INTEGER PRIMARY KEY, data TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS viewerprobe_ephemeral (viewer_id INTEGER NOT NULL, data TEXT NOT NULL)`,
	} {
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	seed := []struct {
		q string
		a []any
	}{
		{`INSERT INTO viewerprobe_watch VALUES (1,10,100),(1,11,110),(2,10,200)`, nil},
		{`INSERT INTO viewerprobe_history VALUES (1,1,10),(2,1,11),(3,2,10)`, nil},
		{`INSERT INTO viewerprobe_solo VALUES (1,'source'),(2,'target')`, nil},
		{`INSERT INTO viewerprobe_ephemeral VALUES (1,'x'),(2,'y')`, nil},
	}
	for _, s := range seed {
		if _, err := db.ExecContext(ctx, s.q, s.a...); err != nil {
			t.Fatal(err)
		}
	}
	// Merge viewer 1 into viewer 2: the target's watch row for key 10 wins,
	// key 11 moves, history unions, the solo target row wins, ephemeral drops.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := MergeViewer(ctx, tx, 1, 2); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	count := func(q string, args ...any) int {
		t.Helper()
		var n int
		if err := db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(`SELECT COUNT(*) FROM viewerprobe_watch WHERE user_id=1`); n != 0 {
		t.Fatalf("source watch rows remain: %d", n)
	}
	var pos int
	if err := db.QueryRowContext(ctx, `SELECT pos FROM viewerprobe_watch WHERE user_id=2 AND rating_key=10`).Scan(&pos); err != nil || pos != 200 {
		t.Fatalf("target watch pos=%d err=%v, want 200 (target wins)", pos, err)
	}
	if n := count(`SELECT COUNT(*) FROM viewerprobe_watch WHERE user_id=2`); n != 2 {
		t.Fatalf("target watch rows=%d, want 2", n)
	}
	if n := count(`SELECT COUNT(*) FROM viewerprobe_history WHERE user_id=2`); n != 3 {
		t.Fatalf("target history rows=%d, want 3 (union)", n)
	}
	var data string
	if err := db.QueryRowContext(ctx, `SELECT data FROM viewerprobe_solo WHERE user_id=2`).Scan(&data); err != nil || data != "target" {
		t.Fatalf("solo data=%q err=%v, want target", data, err)
	}
	if n := count(`SELECT COUNT(*) FROM viewerprobe_ephemeral WHERE viewer_id=1`); n != 0 {
		t.Fatalf("source ephemeral rows remain: %d", n)
	}
	// Purge viewer 2 removes everything of theirs.
	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := PurgeViewer(ctx, tx, 2); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for table, col := range map[string]string{
		"viewerprobe_watch": "user_id", "viewerprobe_history": "user_id",
		"viewerprobe_solo": "user_id", "viewerprobe_ephemeral": "viewer_id",
	} {
		if n := count(`SELECT COUNT(*) FROM ` + table + ` WHERE ` + col + `=2`); n != 0 {
			t.Fatalf("%s still holds %d rows for purged viewer", table, n)
		}
	}
	covered, _ := ViewerCoverage()
	for _, table := range []string{"viewerprobe_watch", "viewerprobe_history", "viewerprobe_solo", "viewerprobe_ephemeral"} {
		if covered[table] != "viewerprobe" {
			t.Fatalf("coverage[%s]=%q, want viewerprobe", table, covered[table])
		}
	}
}
