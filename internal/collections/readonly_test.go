package collections

import (
	"context"
	"errors"
	"testing"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/store"
)

func TestSnapshotsNeverBuild(t *testing.T) {
	ctx := context.Background()
	db := store.OpenTest(t)
	// A deliberately bare service has no index, source registry or builder.
	// Either read would panic if it tried to evaluate a source.
	s := &Service{db: db}
	_, err := db.ExecContext(ctx, `INSERT INTO collections_specs(id,spec_json,enabled) VALUES(1,?,1)`, `{"name":"Saved","mediaType":"movie","sources":[{"kind":"catalog"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.SnapshotList(ctx, 0, 1)
	if err != nil || len(list) != 1 || list[0].Title != "Saved" {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	_, err = s.SnapshotItems(ctx, core.SmartKey(1), 0, 5)
	if !errors.Is(err, ErrSnapshotNotBuilt) {
		t.Fatalf("unbuilt: %v", err)
	}
	_, err = db.ExecContext(ctx, `UPDATE collections_specs SET last_built_at=1,build_hash='snapshot' WHERE id=1`)
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.SnapshotItems(ctx, core.SmartKey(1), 0, 5)
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("snapshot=%+v err=%v", page, err)
	}
}
