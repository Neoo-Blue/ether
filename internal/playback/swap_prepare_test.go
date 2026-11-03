package playback

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
)

type blockedSwapHLS struct {
	*fakeHLS
	entered chan struct{}
	resume  chan struct{}
}

type synchronizedHLSLookup struct {
	core.Transcoder
	mu  sync.Mutex
	hls core.HLSSession
}

func (t *synchronizedHLSLookup) Session(id string) (core.HLSSession, bool) {
	t.mu.Lock()
	hls := t.hls
	t.mu.Unlock()
	if hls != nil && hls.ID() == id {
		return hls, true
	}
	return t.Transcoder.Session(id)
}

func (t *synchronizedHLSLookup) set(hls core.HLSSession) {
	t.mu.Lock()
	t.hls = hls
	t.mu.Unlock()
}

type pendingOutputHLS struct {
	*fakeHLS
	mu       sync.Mutex
	input    string
	waiting  chan struct{}
	released chan struct{}
	fail     error
	once     sync.Once
}

func (h *pendingOutputHLS) SwapInput(_ context.Context, input string, _ core.MediaInfo, _ time.Duration) error {
	h.mu.Lock()
	h.input = input
	h.mu.Unlock()
	return nil
}
func (h *pendingOutputHLS) CurrentSwapInput() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.input
}
func (h *pendingOutputHLS) WaitForReplacementOutput(ctx context.Context, _ string) error {
	h.once.Do(func() { close(h.waiting) })
	select {
	case <-h.released:
		return h.fail
	case <-ctx.Done():
		return ctx.Err()
	}
}

type contextSource struct{ core.MediaSource }

func (s *contextSource) ReadAtContext(ctx context.Context, p []byte, off int64) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return s.MediaSource.ReadAt(p, off)
}

type contextEngine struct{ core.Engine }

func (e *contextEngine) Open(ctx context.Context, id string, nzb []byte) (core.MediaSource, error) {
	src, err := e.Engine.Open(ctx, id, nzb)
	if err != nil {
		return nil, err
	}
	return &contextSource{src}, nil
}

func TestFailedReplacementRollsBackOnlyToViableSource(t *testing.T) {
	for _, viable := range []bool{true, false} {
		t.Run(map[bool]string{true: "viable", false: "failed"}[viable], func(t *testing.T) {
			var lookup *synchronizedHLSLookup
			h := newHarness(t, []*fakeRelease{mkv("A", 100_000, time.Minute), mkv("B", 20_000, time.Minute)}, []string{"A", "B"}, func(h *harness, _ *Options) {
				h.testEngine = &contextEngine{h.engine}
				lookup = &synchronizedHLSLookup{Transcoder: h.tr}
				h.testTranscode = lookup
			})
			ps := h.prepare(owner, movieKey, "rollback")
			s := h.m.lookup(ps.ID)
			old := s.current()
			if !viable {
				old.noteReadFailure(core.ErrArticleMissing)
			}
			f := &pendingOutputHLS{fakeHLS: h.tr.addHLS(ps.ID), waiting: make(chan struct{}), released: make(chan struct{}), fail: errors.New("replacement failed")}
			lookup.set(f)
			done := make(chan error, 1)
			go func() { _, err := h.m.failoverWith(t.Context(), s, old, 0, failoverOpts{down: true}); done <- err }()
			<-f.waiting
			close(f.released)
			err := <-done
			if viable {
				if err != nil || s.current() != old || f.CurrentSwapInput() != h.m.urlFor(old) {
					t.Fatalf("viable original not restored: %v", err)
				}
			} else if err == nil || h.m.lookup(ps.ID) != nil {
				t.Fatalf("failed original was reused: %v", err)
			}
			if h.cache.pinned("B", PinReasonLive) {
				t.Fatal("failed replacement retained its pin")
			}
		})
	}
}

func TestHandoffRetainsOldSourceWhileReplacementOutputIsPending(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(map[bool]string{false: "publish", true: "stop"}[stop], func(t *testing.T) {
			var lookup *synchronizedHLSLookup
			h := newHarness(t, []*fakeRelease{mkv("A", 100_000, time.Minute), mkv("B", 100_000, time.Minute)}, []string{"A", "B"}, func(h *harness, _ *Options) {
				lookup = &synchronizedHLSLookup{Transcoder: h.tr}
				h.testTranscode = lookup
			})
			ps := h.prepare(owner, movieKey, "output-pending")
			s := h.m.lookup(ps.ID)
			old := s.current()
			f := &pendingOutputHLS{fakeHLS: h.tr.addHLS(ps.ID), waiting: make(chan struct{}), released: make(chan struct{})}
			lookup.set(f)
			done := make(chan error, 1)
			go func() { _, err := h.m.failoverWith(t.Context(), s, old, 0, failoverOpts{}); done <- err }()
			<-f.waiting
			if s.current() != old || !old.acquire() {
				t.Fatal("old source retired before replacement output")
			}
			old.release()
			if stop {
				h.m.Stop(ps.ID)
				if err := <-done; err == nil {
					t.Fatal("stop did not cancel output wait")
				}
				for _, id := range []string{"A", "B"} {
					if h.cache.pinned(id, PinReasonLive) {
						t.Fatalf("%s remains pinned after stop", id)
					}
				}
			} else {
				close(f.released)
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				if s.current() == old {
					t.Fatal("replacement not published")
				}
			}
		})
	}
}

func (h *blockedSwapHLS) SwapInput(ctx context.Context, _ string, _ core.MediaInfo, _ time.Duration) error {
	close(h.entered)
	select {
	case <-h.resume:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestHandoffKeepsOldGenerationUntilSwapReturns(t *testing.T) {
	var lookup *synchronizedHLSLookup
	h := newHarness(t, []*fakeRelease{mkv("A", 100_000, time.Minute), mkv("B", 100_000, time.Minute)}, []string{"A", "B"}, func(h *harness, _ *Options) {
		lookup = &synchronizedHLSLookup{Transcoder: h.tr}
		h.testTranscode = lookup
	})
	ps := h.prepare(owner, movieKey, "prepare-swap")
	s := h.m.lookup(ps.ID)
	old := s.current()
	f := &blockedSwapHLS{fakeHLS: h.tr.addHLS(ps.ID), entered: make(chan struct{}), resume: make(chan struct{})}
	lookup.set(f)
	done := make(chan error, 1)
	go func() { _, err := h.m.failoverWith(t.Context(), s, old, 0, failoverOpts{}); done <- err }()
	select {
	case <-f.entered:
	case <-t.Context().Done():
		t.Fatal("swap did not start")
	}
	if s.current() != old || !old.acquire() {
		t.Fatal("old source was retired before the replacement started")
	}
	old.release()
	if !h.cache.pinned("A", PinReasonLive) {
		t.Fatal("old source lost its pin during preparation")
	}
	close(f.resume)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if s.current() == old {
		t.Fatal("replacement was not published")
	}
}
