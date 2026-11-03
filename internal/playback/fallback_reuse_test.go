package playback

import (
	"context"
	"testing"
	"time"
)

func TestUnavailablePreferredReusesFallbackUntilStopped(t *testing.T) {
	h := newHarness(t, []*fakeRelease{mkv("A", 10000, time.Minute), mkv("C", 20000, time.Minute)}, []string{"A", "C"})
	ctx := context.Background()
	caps := defaultCaps("fallback-reuse")
	first, err := h.m.PrepareVariant(ctx, owner, movieKey, caps, "missing")
	if err != nil || first.Resolution.Release.ID != "A" {
		t.Fatalf("initial fallback: %+v, %v", first, err)
	}
	h.m.PlaybackProgress(owner.ID, caps.ClientID, movieKey, 23*time.Second, "playing")
	before := h.engine.openCount()
	for i := 0; i < 8; i++ {
		got, err := h.m.PrepareVariant(ctx, owner, movieKey, caps, "missing")
		if err != nil || got.ID != first.ID {
			t.Fatalf("reuse %d: %+v, %v", i, got, err)
		}
	}
	if n := h.engine.openCount(); n != before {
		t.Fatalf("reuse opened %d additional sources", n-before)
	}
	if !h.m.ActiveFor(owner.ID, movieKey) {
		t.Fatal("reuse lost the active session")
	}
	s := h.m.lookup(first.ID)
	s.mu.Lock()
	position, state := s.viewerPosition, s.viewerState
	s.mu.Unlock()
	if position != 23*time.Second || state != "playing" {
		t.Fatalf("reuse changed watch telemetry: position=%v state=%q", position, state)
	}
	h.m.Stop(first.ID)
	if h.m.ActiveFor(owner.ID, movieKey) {
		t.Fatal("stopped session remains active")
	}
	next, err := h.m.PrepareVariant(ctx, owner, movieKey, caps, "missing")
	if err != nil || next.ID == first.ID {
		t.Fatalf("prepare after stop: %+v, %v", next, err)
	}
	other, err := h.m.PrepareVariant(ctx, owner, movieKey, caps, "C")
	if err != nil || other.ID == next.ID || other.Resolution.Release.ID != "C" {
		t.Fatalf("different preference: %+v, %v", other, err)
	}
	h.m.Stop(other.ID)
	if err := h.m.Close(); err != nil {
		t.Fatal(err)
	}
	h.engine.mu.Lock()
	defer h.engine.mu.Unlock()
	for _, source := range h.engine.sources {
		if !source.closed.Load() {
			t.Fatalf("source %s survived stop and drain", source.ReleaseID())
		}
	}
}
