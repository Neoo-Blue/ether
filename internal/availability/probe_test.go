package availability

import (
	"context"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
)

func probeFixture(t *testing.T, digital time.Duration) (*fixture, *int, *error) {
	t.Helper()
	x := setup(t)
	x.c.Availability.Verify = false
	x.c.Availability.CatchUp = false
	title := x.cat.titles[core.MovieKey(1)]
	title.Released = x.now.Add(-20 * 24 * time.Hour)
	title.IDs.IMDB = "tt0000001"
	x.cat.titles[title.Key] = title
	x.s.d.Catalog = probeCatalog{fakeCatalog: x.cat, homes: map[core.RatingKey]time.Time{title.Key: x.now.Add(digital)}}
	searches := new(int)
	fail := new(error)
	x.s.d.Search = func(context.Context, core.SearchRequest) ([]core.Release, error) {
		*searches++
		return nil, *fail
	}
	return x, searches, fail
}

func (x *fixture) cycleFor(t *testing.T, d time.Duration) {
	t.Helper()
	for end := x.now.Add(d); x.now.Before(end); x.now = x.now.Add(time.Minute) {
		if err := x.s.Cycle(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}

// A movie whose digital date has passed without a release used to be due
// again on every one-minute cycle: a full search of every indexer per minute
// for each such title (discussion #244).
func TestProbeBacksOffAfterDigitalDateWithoutRelease(t *testing.T) {
	x, searches, _ := probeFixture(t, -time.Hour)
	x.cycleFor(t, 2*time.Hour)
	if *searches != 1 {
		t.Fatalf("searched %d times in two hours, want 1", *searches)
	}
	x.cycleFor(t, 2*time.Hour)
	if *searches != 2 {
		t.Fatalf("searched %d times in four hours, want a recheck after 3h", *searches)
	}
	// Past the first days after the digital date, rechecks settle at 12h.
	x.now = x.now.Add(4 * 24 * time.Hour)
	x.cycleFor(t, 13*time.Hour)
	if *searches != 4 {
		t.Fatalf("searched %d times, want one overdue check then one 12h later", *searches)
	}
	if got := x.s.FilterDiscovery(t.Context(), []core.Title{x.cat.titles[core.MovieKey(1)]}); len(got) != 0 {
		t.Fatal("a movie without a playable release was ungated")
	}
}

// A future digital date waits for that date instead of searching daily.
func TestProbeWaitsForAFutureDigitalDate(t *testing.T) {
	x, searches, _ := probeFixture(t, 10*time.Hour)
	x.cycleFor(t, 9*time.Hour)
	if *searches != 1 {
		t.Fatalf("searched %d times before the digital date, want 1", *searches)
	}
	x.cycleFor(t, 2*time.Hour)
	if *searches != 2 {
		t.Fatalf("searched %d times, want a check once the digital date arrived", *searches)
	}
}

// A search that timed out is an answer for this pass; a spent budget is not.
func TestProbeTimeoutIsNotRetriedEveryCycle(t *testing.T) {
	x, searches, fail := probeFixture(t, -time.Hour)
	*fail = context.DeadlineExceeded
	x.cycleFor(t, 30*time.Minute)
	if *searches != 1 {
		t.Fatalf("timed-out search retried %d times in 30 minutes", *searches)
	}

	y, budgetSearches, budgetFail := probeFixture(t, -time.Hour)
	*budgetFail = core.ErrBudgetExhausted
	y.cycleFor(t, 3*time.Minute)
	if *budgetSearches != 3 {
		t.Fatalf("budget-denied title asked %d times, want it to stay due", *budgetSearches)
	}
}
