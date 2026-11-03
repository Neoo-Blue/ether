package newznab

import (
	"context"
	"testing"

	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/store"
)

// A search told to skip an indexer (a background probe whose feed already
// covers it) sends that indexer nothing and records no search for it.
func TestManagerSearchSkipsIndexers(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, b := newFakeIndexer(t, ""), newFakeIndexer(t, "")
	ca, cb := testConfig(a.server.URL), testConfig(b.server.URL)
	ca.ID, ca.Name, cb.ID, cb.Name = "a", "A", "b", "B"
	m := NewManager(db, []config.Indexer{ca, cb}, Options{NZBDir: t.TempDir()})
	var events []string
	m.Subscribe(func(e Event) { events = append(events, e.Indexer) })
	skipID := m.Clients()[1].indexerID()
	req := core.SearchRequest{Type: core.TypeMovie, Title: "The Signal", Year: 1999}
	if _, err := m.Search(core.WithIndexerSkip(ctx, map[string]bool{skipID: true}), req); err != nil {
		t.Fatal(err)
	}
	if len(b.hits) != 0 || len(a.hits) == 0 {
		t.Fatalf("hits a=%v b=%v, want only a", a.hits, b.hits)
	}
	if len(events) != 1 || events[0] != "A" {
		t.Fatalf("events %v, want one search for A", events)
	}
}
