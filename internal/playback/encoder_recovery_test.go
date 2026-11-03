package playback

import (
	"sync"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
)

type observedEncoderHLS struct {
	*repairHLS
	statusMu sync.Mutex
	status   core.EncoderStatus
}

func (h *observedEncoderHLS) EncoderStatus() *core.EncoderStatus {
	h.statusMu.Lock()
	defer h.statusMu.Unlock()
	e := h.status
	return &e
}
func (h *observedEncoderHLS) SwapWouldCopyVideo(info core.MediaInfo) bool {
	v := info.Video()
	return v != nil && v.ColorTransfer != "smpte2084"
}
func (h *observedEncoderHLS) update(at time.Time, speed float64) {
	h.statusMu.Lock()
	h.status.UpdatedAt = at
	h.status.Speed = speed
	h.statusMu.Unlock()
}

func TestSlowEncoderRecoversToCopyWithoutBlamingProvider(t *testing.T) {
	b := mkv("B", 100_000, time.Minute)
	b.HDR = true
	h := newHarness(t, []*fakeRelease{mkv("A", 100_000, time.Minute), b, mkv("C", 100_000, time.Minute)}, []string{"A", "B", "C"})
	ps := h.prepare(owner, movieKey, "encoder-recovery")
	s := h.m.lookup(ps.ID)
	g := s.current()
	now := time.Now()
	f := &observedEncoderHLS{repairHLS: &repairHLS{fakeHLS: h.tr.addHLS(ps.ID), buffer: core.RecoveryBuffer{Position: 10 * time.Second, ReadyUntil: 12 * time.Second}}, status: core.EncoderStatus{Decision: core.Decision{Kind: core.DecisionTranscode, VideoCopy: false}, Phase: "running", Processor: "gpu", Filters: "cpu", StartedAt: now.Add(-20 * time.Second), UpdatedAt: now.Add(-time.Second), Speed: 0.85}}
	h.m.deps.HLSLookup = func(string) (core.HLSSession, bool) { return f, true }
	s.mu.Lock()
	s.viewerAt = now
	s.viewerPosition = 10 * time.Second
	s.viewerState = "playing"
	s.mu.Unlock()
	got, ok := h.m.Session(ps.ID)
	if !ok || got.Decision != core.DecisionTranscode || got.DecisionVideoCopy || got.Encoder == nil || got.Encoder.Filters != "cpu" || got.Encoder.Processor != "gpu" {
		t.Fatalf("actual encoder status: %+v", got)
	}
	if !h.m.checkEncoderSpeed(s, g, f, now) {
		t.Fatal("slow conversion not detected")
	}
	if g.backgroundFailover.Load() {
		t.Fatal("recovered on a single sample")
	}
	f.update(now, 0.85)
	if !h.m.checkEncoderSpeed(s, g, f, now) {
		t.Fatal("second slow sample lost")
	}
	select {
	case sw := <-f.swapC:
		if v := sw.Info.Video(); v == nil || v.ColorTransfer == "smpte2084" {
			t.Fatalf("selected conversion: %+v", sw.Info)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no compatible encoder recovery")
	}
	if h.resolver.isBad("A") || h.resolver.isBad("B") {
		t.Fatal("slow encoder blamed provider or release")
	}
}

func TestEncoderRunwayAndFreshSamples(t *testing.T) {
	h := newHarness(t, []*fakeRelease{mkv("A", 100_000, time.Minute)}, []string{"A"})
	ps := h.prepare(owner, movieKey, "encoder-runway")
	s := h.m.lookup(ps.ID)
	g := s.current()
	now := time.Now()
	f := &observedEncoderHLS{repairHLS: &repairHLS{fakeHLS: h.tr.addHLS(ps.ID), buffer: core.RecoveryBuffer{Position: 10 * time.Second, ReadyUntil: 50 * time.Second}}, status: core.EncoderStatus{Decision: core.Decision{Kind: core.DecisionTranscode}, Phase: "running", StartedAt: now.Add(-20 * time.Second), UpdatedAt: now, Speed: 0.85}}
	s.mu.Lock()
	s.viewerAt = now
	s.viewerPosition = 10 * time.Second
	s.viewerState = "playing"
	s.mu.Unlock()
	h.m.checkEncoderSpeed(s, g, f, now)
	h.m.checkEncoderSpeed(s, g, f, now)
	s.speedMu.Lock()
	count := s.encoderSlowChecks
	s.speedMu.Unlock()
	if count != 1 || g.backgroundFailover.Load() {
		t.Fatalf("duplicate sample triggered recovery: count %d", count)
	}
	f.update(now.Add(time.Second), 0.85)
	h.m.checkEncoderSpeed(s, g, f, now.Add(time.Second))
	if g.backgroundFailover.Load() {
		t.Fatal("ample contiguous output triggered recovery")
	}
	f.update(now.Add(2*time.Second), 1.2)
	h.m.checkEncoderSpeed(s, g, f, now.Add(2*time.Second))
	s.speedMu.Lock()
	count = s.encoderSlowChecks
	s.speedMu.Unlock()
	if count != 0 {
		t.Fatalf("speed recovery did not reset hysteresis: %d", count)
	}
	f.update(now.Add(3*time.Second), 0.85)
	h.m.checkEncoderSpeed(s, g, f, now.Add(3*time.Second))
	f.mu.Lock()
	f.buffer.SeekAt = now.Add(4 * time.Second)
	f.mu.Unlock()
	s.mu.Lock()
	s.viewerAt = now.Add(5 * time.Second)
	s.mu.Unlock()
	f.update(now.Add(5*time.Second), 0.85)
	h.m.checkEncoderSpeed(s, g, f, now.Add(5*time.Second))
	s.speedMu.Lock()
	count = s.encoderSlowChecks
	s.speedMu.Unlock()
	if count != 1 {
		t.Fatalf("seek carried old samples into a new position: %d", count)
	}
}

func TestEncoderRecoveryKeepsCurrentWhenOnlyConversionExists(t *testing.T) {
	b := mkv("B", 100_000, time.Minute)
	b.HDR = true
	h := newHarness(t, []*fakeRelease{mkv("A", 100_000, time.Minute), b}, []string{"A", "B"})
	ps := h.prepare(owner, movieKey, "encoder-no-copy")
	s := h.m.lookup(ps.ID)
	g := s.current()
	f := &observedEncoderHLS{repairHLS: &repairHLS{fakeHLS: h.tr.addHLS(ps.ID)}}
	h.m.deps.HLSLookup = func(string) (core.HLSSession, bool) { return f, true }
	_, err := h.m.failoverWith(t.Context(), s, g, 10*time.Second, failoverOpts{encoder: true, quietFailure: true})
	if err == nil {
		t.Fatal("conversion accepted as encoder recovery")
	}
	if s.current() != g || h.resolver.isBad("A") || h.resolver.isBad("B") {
		t.Fatal("encoder recovery blamed provider or replaced current source")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range s.tried {
		if id == "A" || id == "B" {
			t.Fatalf("healthy source excluded: %v", s.tried)
		}
	}
}
