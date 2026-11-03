package newznab

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
)

func TestBreakerStateMachine(t *testing.T) {
	var b breaker
	now := time.Unix(1_000_000, 0)
	fail := func(trial bool) time.Duration { return b.done(now, trial, true, 0) }

	// Closed: failures below the threshold keep it closed; an answer resets.
	fail(false)
	fail(false)
	b.done(now, false, false, 0)
	fail(false)
	fail(false)
	if b.blocked(now) {
		t.Fatal("opened below the threshold")
	}
	if pause := fail(false); pause != breakerBasePause || !b.blocked(now) {
		t.Fatalf("third consecutive failure: pause %v blocked %v", pause, b.blocked(now))
	}
	if _, err := b.admit(now); !errors.Is(err, ErrIndexerPaused) {
		t.Fatalf("admitted while open: %v", err)
	}

	// Half-open: exactly one trial; others stay paused while it runs.
	now = now.Add(breakerBasePause)
	trial, err := b.admit(now)
	if err != nil || !trial {
		t.Fatalf("no trial after the pause: %v %v", trial, err)
	}
	if _, err := b.admit(now); !errors.Is(err, ErrIndexerPaused) || !b.blocked(now) {
		t.Fatal("second request admitted during the trial")
	}
	// A trial whose caller went away gives no verdict.
	b.neutral(true)
	if trial, err = b.admit(now); err != nil || !trial {
		t.Fatalf("trial not re-offered after a neutral end: %v %v", trial, err)
	}
	// A failed trial reopens with a doubled pause, capped.
	if pause := fail(true); pause != 2*breakerBasePause {
		t.Fatalf("failed trial pause %v", pause)
	}
	for range 10 {
		now = now.Add(breakerMaxPause)
		if trial, _ := b.admit(now); !trial {
			t.Fatal("no trial after the pause")
		}
		if pause := fail(true); pause > breakerMaxPause {
			t.Fatalf("pause %v above the cap", pause)
		}
	}
	// A successful trial closes it and resets the back-off.
	now = now.Add(breakerMaxPause)
	trial, _ = b.admit(now)
	b.done(now, trial, false, 0)
	if b.blocked(now) {
		t.Fatal("still open after a successful trial")
	}
	if trial, err := b.admit(now); trial || err != nil {
		t.Fatalf("closed breaker: trial %v err %v", trial, err)
	}
	fail(false)
	fail(false)
	if pause := fail(false); pause != breakerBasePause {
		t.Fatalf("back-off not reset: %v", pause)
	}
}

func TestBreakerAdaptiveTimeout(t *testing.T) {
	var b breaker
	now := time.Unix(1_000_000, 0)
	const ceiling = 30 * time.Second
	for i := range minLatencySamples - 1 {
		b.done(now, false, false, time.Duration(i+1)*100*time.Millisecond)
	}
	if got := b.timeout(ceiling); got != 0 {
		t.Fatalf("adaptive timeout with too few samples: %v", got)
	}
	b.done(now, false, false, time.Second)
	if got := b.timeout(ceiling); got != adaptiveFloor {
		t.Fatalf("fast indexer: %v, want the floor", got)
	}
	// A slow indexer gets its p95 plus a quarter.
	for range latencySamples {
		b.done(now, false, false, 16*time.Second)
	}
	if got := b.timeout(ceiling); got != 20*time.Second {
		t.Fatalf("slow indexer: %v", got)
	}
	// Past the ceiling the ordinary timeout stands.
	for range latencySamples {
		b.done(now, false, false, 28*time.Second)
	}
	if got := b.timeout(ceiling); got != 0 {
		t.Fatalf("above the ceiling: %v", got)
	}
	// After a failure the next request gets the full ceiling.
	for range latencySamples {
		b.done(now, false, false, time.Second)
	}
	b.done(now, false, true, 0)
	if got := b.timeout(ceiling); got != 0 {
		t.Fatalf("adaptive timeout right after a failure: %v", got)
	}
}

func TestTransientFailure(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{context.DeadlineExceeded, true},
		{fmt.Errorf("x: %w", core.ErrSourceUnavailable), true},
		{ErrIndexerPaused, true},
		{&APIError{Code: 503, Description: "Service Unavailable"}, true},
		{&APIError{Code: 404, Description: "Not Found"}, false},
		{&APIError{Code: 201, Description: "Incorrect parameter"}, false},
		{apiError(429, "too many requests"), false},
		{&APIError{Code: 100, Description: "Incorrect user credentials"}, false},
		{context.Canceled, false},
		{errors.New("search request has no title or supported identifier"), false},
	} {
		if got := transientFailure(tc.err); got != tc.want {
			t.Errorf("transientFailure(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
