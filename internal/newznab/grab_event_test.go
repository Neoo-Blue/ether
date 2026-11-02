package newznab

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
)

type blockingFetch struct {
	fakeBackend
	fail error
}

func (b *blockingFetch) FetchNZB(ctx context.Context, r core.Release, reserve func(context.Context, string) error) ([]byte, error) {
	if b.fail != nil {
		return nil, b.fail
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

// A fetch Ether cancels itself must not count as an indexer failure: it
// raised an outage alert every time a viewer moved on from a detail page
// (#271). A real failure still does.
func TestCancelledGrabIsNotAnIndexerFailure(t *testing.T) {
	fb := &blockingFetch{}
	cfg := config.Indexer{ID: "site", Name: "Site", URL: "https://site.invalid", Kind: config.IndexerKindCardigann, Definition: "fake", Enabled: true}
	m := NewManager(nil, []config.Indexer{cfg}, Options{NZBDir: t.TempDir(), Backend: func(config.Indexer) (Backend, error) { return fb, nil }})
	var mu sync.Mutex
	var events []Event
	m.Subscribe(func(e Event) { mu.Lock(); events = append(events, e); mu.Unlock() })
	rels, err := m.Search(context.Background(), core.SearchRequest{Type: core.TypeMovie, Title: "Film", Year: 2020})
	if err != nil || len(rels) != 1 {
		t.Fatalf("search: %v %v", rels, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.FetchNZB(ctx, rels[0]); !errors.Is(err, context.Canceled) {
		t.Fatalf("fetch: %v", err)
	}
	fb.fail = errors.New("indexer answered 503")
	if _, err := m.FetchNZB(context.Background(), rels[0]); err == nil {
		t.Fatal("failing fetch succeeded")
	}
	mu.Lock()
	defer mu.Unlock()
	var grabs []Event
	for _, e := range events {
		if e.Kind == "grab" {
			grabs = append(grabs, e)
		}
	}
	if len(grabs) != 1 || grabs[0].Success {
		t.Fatalf("grab events %+v, want one failure (the cancelled fetch reports nothing)", grabs)
	}
}
