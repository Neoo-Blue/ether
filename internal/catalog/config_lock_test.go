package catalog

import (
	"context"
	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/store"
	"testing"
	"time"
)

func TestUpdateConfigDoesNotNeedDatabaseConnection(t *testing.T) {
	db, err := store.OpenMemory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	c, err := New(db, nil, config.Catalog{ReleaseMode: config.ModeDigital}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	done := make(chan struct{})
	go func() { c.UpdateConfig(config.Catalog{ReleaseMode: config.ModePlayable, MinVotes: 42}); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("mode update blocked on database")
	}
	if got := c.config(); got.ReleaseMode != config.ModePlayable || got.MinVotes != 42 {
		t.Fatalf("config not applied: %+v", got)
	}
}
