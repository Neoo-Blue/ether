package playback

import (
	"context"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
)

type heldReserveEngine struct {
	core.Engine
	started chan struct{}
	stopped chan struct{}
	length  chan int64
}

func (e *heldReserveEngine) Prefetch(ctx context.Context, _ string, _ []byte, _, length int64) error {
	if e.length != nil {
		e.length <- length
	}
	close(e.started)
	<-ctx.Done()
	close(e.stopped)
	return ctx.Err()
}

func TestDebridReserveHonorsConfiguredLimit(t *testing.T) {
	for _, chunks := range []int{-1, 1} {
		t.Run(map[int]string{-1: "off", 1: "fixed"}[chunks], func(t *testing.T) {
			var e *heldReserveEngine
			h := newHarness(t, []*fakeRelease{mkv("A", 100_000_000, 100*time.Second)}, []string{"A"}, func(h *harness, _ *Options) {
				e = &heldReserveEngine{Engine: h.engine, started: make(chan struct{}), stopped: make(chan struct{}), length: make(chan int64, 1)}
				h.testEngine = e
				h.resolver.candidates[0].Protocol = core.ProtocolTorrent
				h.cfg.Debrid.ReadAheadChunks = chunks
				h.cfg.Debrid.ChunkBytes = 1 << 20
			})
			ps := h.prepare(owner, movieKey, "debrid-reserve")
			h.m.PlaybackProgress(owner.ID, "debrid-reserve", movieKey, 10*time.Second, "playing")
			if chunks < 0 {
				s := h.m.lookup(ps.ID)
				s.reserve.mu.Lock()
				running := s.reserve.running
				s.reserve.mu.Unlock()
				if running {
					t.Fatal("debrid off mode started reserve work")
				}
				return
			}
			<-e.started
			if n := <-e.length; n > 1<<20 || n <= 0 {
				t.Fatalf("fixed debrid reserve = %d bytes", n)
			}
			s := h.m.lookup(ps.ID)
			s.reserve.mu.Lock()
			until := s.reserve.until
			s.reserve.mu.Unlock()
			if until <= 10*time.Second || until > 12*time.Second {
				t.Fatalf("byte-limited reserve advertised an impossible horizon: %v", until)
			}
			h.m.Stop(ps.ID)
			<-e.stopped
		})
	}
}

// Configuration and playback progress can race. The off transition must
// cancel the already admitted background read and not restart it afterward.
func TestReserveOffCancelsInFlightPrefetch(t *testing.T) {
	var e *heldReserveEngine
	h := newHarness(t, []*fakeRelease{mkv("A", 100_000_000, 100*time.Second)}, []string{"A"}, func(h *harness, _ *Options) {
		e = &heldReserveEngine{Engine: h.engine, started: make(chan struct{}), stopped: make(chan struct{})}
		h.testEngine = e
	})
	h.prepare(owner, movieKey, "reserve-off")
	h.m.PlaybackProgress(owner.ID, "reserve-off", movieKey, 10*time.Second, "playing")
	<-e.started
	zero := 0
	h.cfg.Cache.PlaybackReadAheadMinutes = &zero
	h.m.SetConfig(h.cfg)
	<-e.stopped
	h.m.PlaybackProgress(owner.ID, "reserve-off", movieKey, 20*time.Second, "playing")
	s := h.m.lookup(h.m.Sessions()[0].ID)
	s.reserve.mu.Lock()
	defer s.reserve.mu.Unlock()
	if s.reserve.running || s.reserve.generation != nil {
		t.Fatal("reserve restarted after read-ahead was disabled")
	}
}
