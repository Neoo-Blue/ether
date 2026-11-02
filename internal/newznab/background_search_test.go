package newznab

import (
	"context"
	"testing"

	"github.com/braedonsaunders/ether/internal/core"
)

// A background check (new-release evidence) must spend one targeted query
// per indexer, not the whole fallback fan-out, and its miss must not stop a
// later foreground search from running the fallbacks.
func TestBackgroundSearchAsksOnlyThePrimaryQuery(t *testing.T) {
	f := newFakeIndexer(t, "emptyimdb")
	c := New(testConfig(f.server.URL), nil, Options{})
	req := core.SearchRequest{Type: core.TypeMovie, IDs: core.IDs{IMDB: "tt0133093", TMDB: 603}, Title: "The Signal", Year: 1999}
	bg := core.WithBackgroundBudget(context.Background(), func(context.Context, string, string, int64) error { return nil })
	res, err := c.Search(bg, req)
	if err != nil || len(res) != 0 {
		t.Fatalf("background res=%d err=%v", len(res), err)
	}
	if got := f.hitsFor("movie") + f.hitsFor("search"); got != 1 {
		t.Fatalf("background check sent %d search queries, want 1", got)
	}
	res, err = c.Search(context.Background(), req)
	if err != nil || len(res) != 2 {
		t.Fatalf("foreground res=%d err=%v; a background miss was served from cache", len(res), err)
	}
	if got := f.hitsFor("movie"); got != 3 {
		t.Fatalf("movie hits=%d, want 1 background + imdb and tmdb foreground", got)
	}
	// The foreground answer is cached and serves later background checks.
	if res, err = c.Search(bg, req); err != nil || len(res) != 2 || f.hitsFor("movie") != 3 {
		t.Fatalf("background reuse res=%d err=%v hits=%d", len(res), err, f.hitsFor("movie"))
	}
}
