package core

import "context"

// FeedVerdict is one retained feed release judged against the owner's
// default quality policy: the "When they can play" evidence check.
type FeedVerdict struct {
	// Reason is empty when the policy accepts the release.
	Reason string
	// Volatile verdicts depend on short-lived state (a hosted ranking
	// session, a brief health-failure skip, a blocklist mark that can
	// expire, a regex timeout) and must not be persisted.
	Volatile bool
}

// FeedEvaluation judges many feed releases under one prepared policy: the
// profile, compiled release rules and resolver settings are read once.
type FeedEvaluation interface {
	// Key identifies everything a durable verdict depends on besides the
	// title and release, so verdicts persisted under it stay exact.
	Key() string
	// Evaluate judges each release for t, in order.
	Evaluate(ctx context.Context, t *Title, rs []Release) ([]FeedVerdict, error)
	// Transient is the rejection a durable acceptance gains from
	// short-lived state right now, or "".
	Transient(releaseID string) string
	Close()
}
