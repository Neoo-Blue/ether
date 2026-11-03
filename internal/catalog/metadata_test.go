package catalog

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/store"
)

type metadataFunc func(context.Context, core.MetadataRequest) (*core.Title, error)

func (f metadataFunc) ResolveMetadata(ctx context.Context, r core.MetadataRequest) (*core.Title, error) {
	return f(ctx, r)
}
func metadataTestCatalog(t *testing.T) *Catalog {
	t.Helper()
	c, err := New(store.OpenTest(t), nil, config.Catalog{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func metadataTestTitle() *core.Title {
	return &core.Title{Type: core.TypeMovie, Title: "A record", Released: time.Date(1927, 1, 10, 0, 0, 0, 0, time.UTC), IDs: core.IDs{TMDB: 19, IMDB: "tt0017136", TVDB: 123}}
}

func TestMetadataPerCallerIdentityOnSharedFlightAndCache(t *testing.T) {
	c := metadataTestCatalog(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	c.RegisterMetadataProvider("test", metadataFunc(func(ctx context.Context, r core.MetadataRequest) (*core.Title, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		if r.IDs != (core.IDs{}) {
			t.Errorf("expected IDs escaped into shared provider resolution: %+v", r.IDs)
		}
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return metadataTestTitle(), nil
	}))
	ctx := context.Background()
	good := core.MetadataRequest{ID: "resource:19", Type: core.TypeMovie, IDs: core.IDs{TMDB: 19, IMDB: "tt0017136", TVDB: 123}}
	first := make(chan error, 1)
	go func() { _, err := c.MaterializeMetadata(ctx, "test", good); first <- err }()
	<-entered
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		req := good
		switch i % 3 {
		case 0:
			req.IDs.TMDB = 42
		case 1:
			req.IDs.IMDB = "tt9999999"
		case 2:
			req.IDs.TVDB = 999
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if title, err := c.MaterializeMetadata(ctx, "test", req); err == nil {
				t.Errorf("coalesced/cache identity mismatch accepted: %+v", title)
			}
		}()
	}
	close(release)
	wg.Wait()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("provider called %d times", calls.Load())
	}
	for _, ids := range []core.IDs{{TMDB: 42}, {IMDB: "tt9999999"}, {TVDB: 999}} {
		req := good
		req.IDs = ids
		if _, err := c.MaterializeMetadata(ctx, "test", req); err == nil {
			t.Fatalf("cached conflict accepted: %+v", ids)
		}
	}
	if _, err := c.MaterializeMetadata(ctx, "test", good); err != nil {
		t.Fatal(err)
	}
}

func TestMetadataDisableDuringResolutionPreventsMaterialization(t *testing.T) {
	c := metadataTestCatalog(t)
	entered, release := make(chan struct{}), make(chan struct{})
	c.RegisterMetadataProvider("test", metadataFunc(func(context.Context, core.MetadataRequest) (*core.Title, error) {
		close(entered)
		<-release
		return metadataTestTitle(), nil
	}))
	done := make(chan error, 1)
	go func() {
		_, err := c.MaterializeMetadata(context.Background(), "test", core.MetadataRequest{ID: "x", Type: core.TypeMovie})
		done <- err
	}()
	<-entered
	c.RegisterMetadataProvider("test", nil)
	close(release)
	if err := <-done; !errors.Is(err, core.ErrNotAvailable) {
		t.Fatalf("revoked resolution: %v", err)
	}
	if _, err := c.StoredTitle(context.Background(), core.MovieKey(19)); err == nil {
		t.Fatal("revoked record was persisted")
	}
}

func TestMetadataRejectsUnsupportedAndConflictingIdentity(t *testing.T) {
	for _, tc := range []struct {
		name  string
		title *core.Title
	}{
		{"no canonical ID", &core.Title{Title: "Opaque", Type: core.TypeMovie}},
		{"wrong type", &core.Title{Title: "Show", Type: core.TypeShow, IDs: core.IDs{TMDB: 19}}},
		{"arbitrary key", &core.Title{Title: "Movie", Type: core.TypeMovie, IDs: core.IDs{TMDB: 19}, Key: core.MovieKey(20)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := metadataTestCatalog(t)
			c.RegisterMetadataProvider("test", metadataFunc(func(context.Context, core.MetadataRequest) (*core.Title, error) { return tc.title, nil }))
			if _, err := c.MaterializeMetadata(context.Background(), "test", core.MetadataRequest{ID: "x", Type: core.TypeMovie}); err == nil {
				t.Fatal("invalid identity accepted")
			}
		})
	}
}
