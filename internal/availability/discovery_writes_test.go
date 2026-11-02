package availability

import (
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/resolver"
	"github.com/braedonsaunders/ether/internal/store"
)

// Home rows filter new releases on every request. Once an observation is
// recorded, filtering the same titles again must not write: on slow disks
// hundreds of no-op upserts made one Home request take 41 s (#238).
func TestRepeatDiscoveryDoesNotWrite(t *testing.T) {
	x := setup(t)
	r := resolver.New(nil, nil, nil, x.c, resolver.Options{})
	defer r.Close()
	x.s.d.Validator = suitabilityValidator{x.v, r}
	rel := core.Release{ID: "upload", Name: "Series.S01E02.1080p.WEB-DL", Indexer: "Fake"}
	if err := x.s.retainFeed(t.Context(), x.f.sources[0], []core.Release{rel}); err != nil {
		t.Fatal(err)
	}
	ep := core.Title{Key: core.EpisodeKey(7, 1, 2), Type: core.TypeEpisode, Title: "Second", ShowTitle: "Series", Season: 1, Episode: 2, Released: x.now.Add(-time.Hour)}

	ctx, first := store.WithQueryStats(t.Context())
	if got := x.s.FilterDiscoveryCurrent(ctx, []core.Title{ep}); len(got) != 1 {
		t.Fatalf("first browse: %+v", got)
	}
	if first.Writes() == 0 {
		t.Fatal("first browse recorded no observation")
	}
	ctx, again := store.WithQueryStats(t.Context())
	if got := x.s.FilterDiscoveryCurrent(ctx, []core.Title{ep}); len(got) != 1 {
		t.Fatalf("repeat browse: %+v", got)
	}
	if n := again.Writes(); n != 0 {
		t.Fatalf("repeat browse wrote %d times", n)
	}
}
