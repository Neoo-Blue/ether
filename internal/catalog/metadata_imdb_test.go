package catalog_test

import (
	"context"
	"errors"
	"testing"

	"github.com/braedonsaunders/ether/internal/core"
)

type imdbOnlyProvider struct{ imdb string }

func (p imdbOnlyProvider) ResolveMetadata(_ context.Context, r core.MetadataRequest) (*core.Title, error) {
	return &core.Title{Type: core.TypeMovie, Title: "Add-on title", IDs: core.IDs{IMDB: p.imdb}}, nil
}

// Add-ons such as Cinemeta name titles only by IMDb ID; the catalogue
// translates that ID to the canonical TMDB identity instead of dropping it.
func TestMetadataAdoptsCanonicalTMDBFromIMDb(t *testing.T) {
	c := newTestCatalog(t, fakeTMDB(t))
	ctx := context.Background()
	c.RegisterMetadataProvider("addon", imdbOnlyProvider{imdb: "tt0137523"})
	got, err := c.MaterializeMetadata(ctx, "addon", core.MetadataRequest{ID: "tt0137523", Type: core.TypeMovie})
	if err != nil {
		t.Fatal(err)
	}
	if got.Key != core.MovieKey(550) || got.IDs.IMDB != "tt0137523" {
		t.Fatalf("materialized %v %+v", got.Key, got.IDs)
	}
	c.RegisterMetadataProvider("unknown", imdbOnlyProvider{imdb: "tt9999999"})
	if _, err := c.MaterializeMetadata(ctx, "unknown", core.MetadataRequest{ID: "tt9999999", Type: core.TypeMovie}); !errors.Is(err, core.ErrMetadataIdentity) {
		t.Fatalf("unmatched IMDb ID: %v", err)
	}
}
