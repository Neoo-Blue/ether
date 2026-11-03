package core

import (
	"context"
	"sync/atomic"
)

// SearchCoverage records whether a fleet search heard from every indexer it
// meant to ask. A search that skipped a paused indexer, or lost one to a
// timeout or an outage, is incomplete: its answer must not be cached as the
// fleet's answer or turned into "unavailable", because the missing indexer
// may hold the release.
type SearchCoverage struct{ incomplete atomic.Bool }

type searchCoverageKey struct{}

// WithSearchCoverage returns ctx carrying a fresh coverage record.
func WithSearchCoverage(ctx context.Context) (context.Context, *SearchCoverage) {
	c := &SearchCoverage{}
	return context.WithValue(ctx, searchCoverageKey{}, c), c
}

// SearchCoverageFrom returns ctx's coverage record, or nil.
func SearchCoverageFrom(ctx context.Context) *SearchCoverage {
	c, _ := ctx.Value(searchCoverageKey{}).(*SearchCoverage)
	return c
}

// CarrySearchCoverage carries src's coverage record into dst, for a search
// that runs on a context detached from its caller.
func CarrySearchCoverage(dst, src context.Context) context.Context {
	if c := SearchCoverageFrom(src); c != nil {
		return context.WithValue(dst, searchCoverageKey{}, c)
	}
	return dst
}

// NoteSearchIncomplete marks ctx's search as missing an indexer's answer.
func NoteSearchIncomplete(ctx context.Context) {
	if c := SearchCoverageFrom(ctx); c != nil {
		c.incomplete.Store(true)
	}
}

// Incomplete reports whether an indexer's answer was missing. A nil record
// reports false.
func (c *SearchCoverage) Incomplete() bool { return c != nil && c.incomplete.Load() }
