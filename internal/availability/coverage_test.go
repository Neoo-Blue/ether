package availability

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
)

// growingFeed lists `grow` new releases before every poll, newest first,
// like a live indexer feed; a poll overlaps the previous one while fewer
// than a page arrive in between.
type growingFeed struct {
	sources []core.ReleaseFeed
	next    int
	grow    int
	items   []core.Release
	polls   map[string]int
}

func (f *growingFeed) FeedSources() []core.ReleaseFeed { return f.sources }
func (f *growingFeed) Recent(ctx context.Context, id string, offset, limit int) ([]core.Release, error) {
	if err := core.SpendBackground(ctx, "hit", id, 1); err != nil {
		return nil, err
	}
	f.polls[id]++
	fresh := make([]core.Release, 0, f.grow)
	for range f.grow {
		fresh = append([]core.Release{{ID: fmt.Sprintf("n%d", f.next), Name: "Unrelated", Indexer: id}}, fresh...)
		f.next++
	}
	f.items = append(fresh, f.items[:min(len(f.items), 1000)]...)
	if offset >= len(f.items) {
		return nil, nil
	}
	return f.items[offset:min(len(f.items), offset+limit)], nil
}

// skipCounter records which indexers each probe search reached.
type skipCounter struct {
	ids  []string
	hits map[string]int
}

func (c *skipCounter) search(ctx context.Context, _ core.SearchRequest) ([]core.Release, error) {
	for _, id := range c.ids {
		if !core.IndexerSkipped(ctx, id) {
			c.hits[id]++
		}
	}
	return nil, nil
}

func coverageFixture(t *testing.T, grow int) (*fixture, *growingFeed, *skipCounter) {
	t.Helper()
	x := setup(t)
	x.c.Availability.Verify = false
	x.c.Availability.CatchUp = false
	feed := &growingFeed{grow: grow, polls: map[string]int{}, sources: []core.ReleaseFeed{
		{ID: "geek", Name: "Geek", Generation: "g", Supported: true},
		{ID: "slug", Name: "Slug", Generation: "g", Supported: true},
		// A site with no release feed (a scraper or addon) is only ever
		// found by searching it.
		{ID: "addon", Name: "Addon", Generation: "g", Supported: false},
	}}
	x.s.d.Feeds = feed
	titles := map[core.RatingKey]core.Title{}
	for i := 1; i <= 40; i++ {
		k := core.MovieKey(int64(i))
		titles[k] = core.Title{Key: k, Type: core.TypeMovie, Title: fmt.Sprintf("Film %d", i), Year: 2026, Released: x.now.Add(-time.Duration(i) * 24 * time.Hour), Popularity: float64(i)}
	}
	x.cat.titles = titles
	x.s.d.Catalog = probeCatalog{fakeCatalog: x.cat}
	counter := &skipCounter{ids: []string{"geek", "slug", "addon"}, hits: map[string]int{}}
	x.s.d.Search = counter.search
	return x, feed, counter
}

func runFor(t *testing.T, x *fixture, d time.Duration) {
	t.Helper()
	for end := x.now.Add(d); x.now.Before(end); x.now = x.now.Add(time.Minute) {
		if err := x.s.Cycle(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}

// Over a day, waiting titles are searched once on every indexer, and after
// that only on indexers whose feed cannot vouch for them; feeds keep polling
// on their own schedule.
func TestProbeSkipsIndexersTheirFeedsCover(t *testing.T) {
	x, feed, counter := coverageFixture(t, 5)
	runFor(t, x, 24*time.Hour)
	for _, id := range []string{"geek", "slug"} {
		if counter.hits[id] != 40 {
			t.Errorf("%s searched %d times in a day, want once per title (40): its feed covers every later check", id, counter.hits[id])
		}
	}
	if counter.hits["addon"] <= 40 {
		t.Errorf("addon searched %d times, want the full cadence (no feed covers it)", counter.hits["addon"])
	}
	if feed.polls["geek"] < 90 || feed.polls["addon"] != 0 {
		t.Errorf("feed polls %v, want every 15 minutes on feed indexers only", feed.polls)
	}
	// The weekly full search still reaches every indexer.
	before := counter.hits["geek"]
	runFor(t, x, 7*24*time.Hour)
	if counter.hits["geek"] <= before {
		t.Error("no full search of a covered indexer within a week")
	}
}

// A feed that drops releases between polls (more uploads than a page)
// vouches for nothing, so its indexer is searched on the normal cadence.
func TestProbeSearchesIndexersWhoseFeedHasGaps(t *testing.T) {
	x, _, counter := coverageFixture(t, 500)
	runFor(t, x, 24*time.Hour)
	if counter.hits["geek"] <= 40 {
		t.Fatalf("geek searched %d times, want the normal cadence while its feed has gaps", counter.hits["geek"])
	}
}

// A found title is available like any other and is never searched again in
// the background, unless its release proves unplayable.
func TestProbeLeavesFoundTitlesUntilUnplayable(t *testing.T) {
	x, _, counter := coverageFixture(t, 5)
	var searches int
	found := func(ctx context.Context, req core.SearchRequest) ([]core.Release, error) {
		searches++
		return []core.Release{{ID: "web-" + req.Title, Name: req.Title + ".2026.1080p.WEB-DL", Indexer: "Geek"}}, nil
	}
	x.s.d.Search = found
	_ = counter
	runFor(t, x, 3*24*time.Hour)
	x.now = x.now.Add(45 * 24 * time.Hour) // and well past any cadence
	runFor(t, x, 10*time.Minute)
	if searches != 40 {
		t.Fatalf("searched %d times, want once per found title (40)", searches)
	}
	unplayable := &unplayableValidator{fakeValidator: x.v, since: map[core.RatingKey]time.Time{core.MovieKey(3): x.now}}
	x.s.d.Validator = unplayable
	x.now = x.now.Add(time.Minute)
	runFor(t, x, 10*time.Minute)
	if searches != 41 {
		t.Fatalf("searched %d times after a release proved unplayable, want one more", searches)
	}
}

type unplayableValidator struct {
	*fakeValidator
	since map[core.RatingKey]time.Time
}

func (v *unplayableValidator) KnownUnavailableSince(_ context.Context, keys []core.RatingKey) map[core.RatingKey]time.Time {
	out := map[core.RatingKey]time.Time{}
	for _, k := range keys {
		if at, ok := v.since[k]; ok {
			out[k] = at
		}
	}
	return out
}

// Background probes stay on the indexers the owner chose for background
// work (Availability.IndexerIDs).
func TestProbeHonoursBackgroundIndexers(t *testing.T) {
	x, _, counter := coverageFixture(t, 5)
	x.c.Availability.IndexerIDs = []string{"geek"}
	runFor(t, x, 2*time.Hour)
	if counter.hits["slug"] != 0 || counter.hits["addon"] != 0 || counter.hits["geek"] != 40 {
		t.Fatalf("probe hits %v, want only geek (once per title)", counter.hits)
	}
}

// The look when a title's digital date arrives searches every indexer,
// feed coverage or not: that is when releases land.
func TestProbeSearchesEverywhereWhenDigitalDateArrives(t *testing.T) {
	x, _, counter := coverageFixture(t, 5)
	digital := x.now.Add(5 * time.Hour)
	cat := probeCatalog{fakeCatalog: x.cat, homes: map[core.RatingKey]time.Time{core.MovieKey(1): digital}}
	x.s.d.Catalog = cat
	runFor(t, x, 4*time.Hour)
	before := counter.hits["geek"]
	runFor(t, x, 2*time.Hour)
	if counter.hits["geek"] != before+1 {
		t.Fatalf("geek searched %d times around the digital date, want one more (%d)", counter.hits["geek"], before+1)
	}
}

// Routine checks ask only the feedless source that has returned releases
// most often in full searches; full searches still ask every source.
func TestProbeAsksTheBestScraperOnly(t *testing.T) {
	x, feed, _ := coverageFixture(t, 5)
	feed.sources = append(feed.sources, core.ReleaseFeed{ID: "aggregator", Name: "Aggregator", Generation: "g", Supported: false})
	hits := map[string]int{}
	x.s.d.Search = func(ctx context.Context, req core.SearchRequest) ([]core.Release, error) {
		for _, f := range feed.sources {
			if !core.IndexerSkipped(ctx, f.ID) {
				hits[f.ID]++
			}
		}
		// The aggregator lists something (rejected as a cam, so titles keep
		// waiting); the other scraper never does.
		if core.IndexerSkipped(ctx, "aggregator") {
			return nil, nil
		}
		return []core.Release{{ID: "x" + req.Title, Name: req.Title + ".CAM", Indexer: "Aggregator"}}, nil
	}
	x.v.reject = true
	x.s.d.Validator = x.v
	runFor(t, x, 24*time.Hour)
	if hits["aggregator"] <= 40 {
		t.Fatalf("aggregator asked %d times, want routine checks too (more than the 40 first looks)", hits["aggregator"])
	}
	if hits["addon"] != 40 {
		t.Fatalf("addon asked %d times, want only the 40 full first looks", hits["addon"])
	}
}
