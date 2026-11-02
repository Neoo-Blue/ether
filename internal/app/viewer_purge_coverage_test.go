package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/braedonsaunders/ether/internal/store"
)

// Every table that holds a viewer column must say how a viewer's rows are
// purged and merged (store.RegisterViewerTable and friends, in the owning
// package) or be explicitly exempt. This walks the fully migrated schema
// of the real binary so a future per-viewer table cannot slip through.
func TestViewerPurgeCoverage(t *testing.T) {
	ctx := context.Background()
	db := store.OpenTest(t)
	viewerColumns := map[string]bool{"user_id": true, "viewer_id": true, "viewer": true, "owner_user_id": true}

	holders := map[string][]string{} // table -> viewer columns
	if store.DialectOf(db) == store.Postgres {
		rows, err := db.QueryContext(ctx, `SELECT table_name, column_name FROM information_schema.columns WHERE table_schema = current_schema() ORDER BY table_name, column_name`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var table, column string
			if err := rows.Scan(&table, &column); err != nil {
				t.Fatal(err)
			}
			if viewerColumns[column] {
				holders[table] = append(holders[table], column)
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
	} else {
		rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
		if err != nil {
			t.Fatal(err)
		}
		var tables []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			tables = append(tables, name)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		for _, table := range tables {
			cols, err := db.QueryContext(ctx, fmt.Sprintf(`SELECT name FROM pragma_table_info(%q)`, table))
			if err != nil {
				t.Fatal(err)
			}
			for cols.Next() {
				var column string
				if err := cols.Scan(&column); err != nil {
					cols.Close()
					t.Fatal(err)
				}
				if viewerColumns[column] {
					holders[table] = append(holders[table], column)
				}
			}
			cols.Close()
			if err := cols.Err(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(holders) == 0 {
		t.Fatal("no per-viewer tables found; schema walk is broken")
	}
	covered, exempt := store.ViewerCoverage()
	var missing []string
	for table, cols := range holders {
		if _, ok := covered[table]; ok {
			continue
		}
		if _, ok := exempt[table]; ok {
			continue
		}
		missing = append(missing, table+" ("+strings.Join(cols, ",")+")")
	}
	if len(missing) > 0 {
		t.Fatalf("per-viewer tables with no registered purge/merge or exemption:\n%s", strings.Join(missing, "\n"))
	}
	// Registrations must name real tables: a renamed table should fail
	// here, not silently stop being purged.
	for table := range covered {
		if _, ok := holders[table]; !ok {
			if _, exempted := exempt[table]; !exempted {
				t.Fatalf("viewer registration for %s, but no such per-viewer table in the schema", table)
			}
		}
	}
}
