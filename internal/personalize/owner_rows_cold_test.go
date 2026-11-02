package personalize

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
)

// gatedCollections serves fixed owner rows, but each fetch blocks until the
// test opens the gate (a cold catalogue index or a slow collection build).
type gatedCollections struct {
	mu    sync.Mutex
	rows  []core.CollectionRow
	gate  chan struct{}
	calls int
}

func (g *gatedCollections) HomeRowsFor(ctx context.Context, _ *core.User, _ core.HubContext) ([]core.CollectionRow, error) {
	g.mu.Lock()
	g.calls++
	gate := g.gate
	g.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return g.rows, nil
}

func (g *gatedCollections) setGate(c chan struct{}) {
	g.mu.Lock()
	g.gate = c
	g.mu.Unlock()
}

func hasHub(hubs []core.Hub, id string) bool {
	for _, h := range hubs {
		if h.ID == id {
			return true
		}
	}
	return false
}

// ownerSectionHarness places owner collection 6 on the Movies page.
func ownerSectionHarness(t *testing.T) (*harness, *gatedCollections, func(time.Time) []core.Hub) {
	t.Helper()
	cols := &gatedCollections{}
	h := newHarness(t, func(o *Options) { o.Collections = cols })
	ctx := context.Background()
	var picks []core.Title
	for j := 0; j < 6; j++ {
		picks = append(picks, h.cat.titles[movieKey(8, j)])
	}
	cols.rows = []core.CollectionRow{{CollectionID: "6", SectionRow: true, HomeRow: true,
		Hub: core.Hub{ID: "collection.6", Title: "Marvel Cinematic Universe", Type: core.TypeMovie, Items: picks}}}
	if _, err := h.e.SetHomeLayout(ctx, core.HomeLayout{Global: []core.LayoutPage{{Section: "movies",
		Rows: []core.RowPolicy{{Row: "collection:6", Status: "always", Slot: 1}}}}}); err != nil {
		t.Fatal(err)
	}
	u := core.User{ID: 1, IsOwner: true}
	h.rebuild(u)
	movies := func(at time.Time) []core.Hub {
		t.Helper()
		hc := hubContext(at, core.DeviceDesktop)
		hc.SectionID = 1
		hubs, err := h.e.SectionHubsFor(ctx, &u, core.TypeMovie, hc, 12)
		if err != nil {
			t.Fatal(err)
		}
		return hubs
	}
	return h, cols, movies
}

// warmMovies loads Movies until the owner rows are stored (the viewer
// navigating away and back).
func warmMovies(t *testing.T, movies func(time.Time) []core.Hub, at time.Time) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !hasHub(movies(at), "collection.6") {
		if time.Now().After(deadline) {
			t.Fatal("owner collection never appeared")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Issue #270: an owner collection placed on the Movies page must be on the
// first Movies load after a restart, and on the first load of a new hour,
// daypart or day, not only after the viewer navigates away and back.
func TestOwnerCollectionOnFirstSectionLoad(t *testing.T) {
	t.Run("restart", func(t *testing.T) {
		_, cols, movies := ownerSectionHarness(t)
		// The first fetch takes a moment; the first load waits for it.
		gate := make(chan struct{})
		cols.setGate(gate)
		time.AfterFunc(50*time.Millisecond, func() { close(gate) })
		if hubs := movies(testNow); !hasHub(hubs, "collection.6") {
			t.Fatalf("first Movies load after a restart lacks the owner collection: %v", hubIDs(hubs))
		}
	})
	t.Run("new daypart", func(t *testing.T) {
		h, cols, movies := ownerSectionHarness(t)
		warmMovies(t, movies, testNow)
		// The stored rows still apply while a refresh is stuck behind a
		// slow source.
		blocked := make(chan struct{})
		t.Cleanup(func() { close(blocked) })
		cols.setGate(blocked)
		later := testNow.Add(3 * time.Hour)
		if core.DaypartAt(later) == core.DaypartAt(testNow) || later.Day() != testNow.Day() {
			t.Fatal("test needs a new daypart on the same day")
		}
		h.clock.Set(later)
		if hubs := movies(later); !hasHub(hubs, "collection.6") {
			t.Fatalf("first Movies load of a new daypart lacks the owner collection: %v", hubIDs(hubs))
		}
	})
	t.Run("new day", func(t *testing.T) {
		h, cols, movies := ownerSectionHarness(t)
		warmMovies(t, movies, testNow)
		blocked := make(chan struct{})
		t.Cleanup(func() { close(blocked) })
		cols.setGate(blocked)
		// The previous day's rows stand in while today's fetch runs.
		tomorrow := testNow.Add(24 * time.Hour)
		h.clock.Set(tomorrow)
		start := time.Now()
		if hubs := movies(tomorrow); !hasHub(hubs, "collection.6") {
			t.Fatalf("first Movies load of a new day lacks the owner collection: %v", hubIDs(hubs))
		}
		if waited := time.Since(start); waited > time.Second {
			t.Fatalf("a new day waited %v on a slow collection source", waited)
		}
	})
}

func hubIDs(hubs []core.Hub) []string {
	out := make([]string, 0, len(hubs))
	for _, h := range hubs {
		out = append(out, h.ID)
	}
	return out
}
