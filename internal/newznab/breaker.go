package newznab

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
)

// An indexer that stops answering (hung, down, behind a challenge it cannot
// pass) used to cost every search its full request timeout, once per query.
// The breaker skips it after a few consecutive transport failures, then lets
// one trial request through when the pause ends: success closes it, failure
// doubles the pause. Requests to Newznab/Torznab indexers also time out
// adaptively, at a margin over the indexer's own recent p95.
const (
	breakerThreshold = 3
	breakerBasePause = 2 * time.Minute
	breakerMaxPause  = 30 * time.Minute

	latencySamples    = 64
	minLatencySamples = 10
	adaptiveFloor     = 10 * time.Second
)

// ErrIndexerPaused reports a search skipped because the indexer failed
// repeatedly. It is transient: the indexer is tried again after the pause.
var ErrIndexerPaused = fmt.Errorf("indexer paused after repeated failures: %w", core.ErrSourceUnavailable)

type breaker struct {
	mu        sync.Mutex
	failures  int // consecutive transient failures
	trips     int // consecutive trips, for the pause back-off
	openUntil time.Time
	probing   bool // the one half-open trial request is in flight

	samples [latencySamples]time.Duration
	n, next int
}

// blocked reports whether a search must skip the indexer without a request.
func (b *breaker) blocked(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return now.Before(b.openUntil) || (!b.openUntil.IsZero() && b.probing)
}

// admit claims a request slot. trial reports the half-open probe.
func (b *breaker) admit(now time.Time) (trial bool, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.openUntil.IsZero() {
		return false, nil
	}
	if now.Before(b.openUntil) || b.probing {
		return false, ErrIndexerPaused
	}
	b.probing = true
	return true, nil
}

// timeout returns the adaptive request timeout, or 0 to keep the ceiling.
// After any failure the next request gets the full ceiling, so a slow
// sign-in or cold cache is not cut off again and again.
func (b *breaker) timeout(ceiling time.Duration) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failures > 0 || !b.openUntil.IsZero() || b.n < minLatencySamples {
		return 0
	}
	s := slices.Clone(b.samples[:b.n])
	slices.Sort(s)
	p95 := s[(len(s)*95+99)/100-1]
	t := max(adaptiveFloor, p95+p95/4)
	if t >= ceiling {
		return 0
	}
	return t
}

// done records a request's outcome. failed reports a transport failure;
// anything else (results, an API error) means the indexer answered and is
// up. sample is the duration of a successful request that counts toward the
// latency profile (0 to skip). It returns the pause when the breaker opens.
func (b *breaker) done(now time.Time, trial bool, failed bool, sample time.Duration) (opened time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if trial {
		b.probing = false
	}
	if !failed {
		b.failures, b.trips, b.openUntil = 0, 0, time.Time{}
		if sample > 0 {
			b.samples[b.next] = sample
			b.next = (b.next + 1) % latencySamples
			b.n = min(b.n+1, latencySamples)
		}
		return 0
	}
	b.failures++
	if !trial && (b.failures < breakerThreshold || now.Before(b.openUntil)) {
		return 0
	}
	pause := min(breakerMaxPause, breakerBasePause<<min(b.trips, 8))
	b.trips++
	b.openUntil = now.Add(pause)
	return pause
}

// neutral releases a trial whose request ended without a verdict (the
// caller went away).
func (b *breaker) neutral(trial bool) {
	if !trial {
		return
	}
	b.mu.Lock()
	b.probing = false
	b.mu.Unlock()
}

// transientFailure reports an error that says the indexer did not answer:
// a timeout, a connection failure, an HTTP 5xx or an unpassable challenge.
// API errors, budget refusals and auth failures are answers.
func transientFailure(err error) bool {
	switch {
	case err == nil, errors.Is(err, core.ErrBudgetExhausted), errors.Is(err, core.ErrUnauthorized), errors.Is(err, context.Canceled):
		return false
	case errors.Is(err, core.ErrSourceUnavailable), errors.Is(err, context.DeadlineExceeded), errors.Is(err, io.ErrUnexpectedEOF):
		return true
	}
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Code >= 500
	}
	var ne net.Error
	return errors.As(err, &ne)
}

// guardSearch runs one search request through the breaker. adaptive applies
// the adaptive timeout under ceiling (Newznab/Torznab HTTP requests; a
// backend's own sessions and challenge solving keep their timeouts).
func (c *Client) guardSearch(ctx context.Context, adaptive bool, ceiling time.Duration, do func(context.Context) ([]core.Release, error)) ([]core.Release, error) {
	trial, err := c.br.admit(c.now())
	if err != nil {
		return nil, fmt.Errorf("indexer %q: %w", c.Name(), err)
	}
	rctx := ctx
	var limit time.Duration
	if adaptive {
		if limit = c.br.timeout(ceiling); limit > 0 {
			var cancel context.CancelFunc
			rctx, cancel = context.WithTimeout(ctx, limit)
			defer cancel()
		}
	}
	started := time.Now()
	res, err := do(rctx)
	elapsed := time.Since(started)
	if err != nil && (ctx.Err() != nil || errors.Is(err, core.ErrBudgetExhausted)) {
		// The caller went away, or a budget or rate-limit pause (which
		// has its own back-off) refused the request: no verdict.
		c.br.neutral(trial)
		return res, err
	}
	if err != nil && limit > 0 && rctx.Err() != nil {
		err = fmt.Errorf("indexer %q: no answer within %s (its usual time is far shorter): %w", c.Name(), limit.Round(time.Second), context.DeadlineExceeded)
	}
	failed := transientFailure(err)
	var sample time.Duration
	if err == nil && adaptive {
		sample = elapsed
	}
	if pause := c.br.done(c.now(), trial, failed, sample); pause > 0 {
		c.log.Warn("indexer paused after repeated failures; will try again automatically", "indexer", c.Name(), "pause", pause, "err", err)
	} else if trial && !failed {
		c.log.Info("indexer is answering again", "indexer", c.Name())
	}
	return res, err
}

// searchPaused reports whether the indexer is paused by the breaker, so a
// search can skip it before any caps or budget work.
func (c *Client) searchPaused() error {
	if c.br.blocked(c.now()) {
		return fmt.Errorf("indexer %q: %w", c.Name(), ErrIndexerPaused)
	}
	return nil
}
