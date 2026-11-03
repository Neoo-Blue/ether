package core

import "context"

// Indexer purposes say why an indexer API request was made. They are
// observability only: the owner console breaks each indexer's real API
// hits and grabs down by purpose, so an owner comparing Ether with the
// indexer's own counter can see what spent the quota. A purpose never
// changes how a search or a budget behaves.
const (
	IndexerPurposeFeed         = "feed"         // release-feed polls
	IndexerPurposeAvailability = "availability" // background availability checks
	IndexerPurposePlayback     = "playback"     // a viewer pressed Play (incl. failover)
	IndexerPurposeWarmup       = "warmup"       // warm-ups, prefetch, managed cache, watchlist
	IndexerPurposeRescue       = "rescue"       // second-pass rescue searches
	IndexerPurposeCaps         = "caps"         // capability (t=caps) fetches
	IndexerPurposeTest         = "test"         // the owner's "Test" button
	IndexerPurposeConsole      = "console"      // owner release searches and checks in the console
	IndexerPurposeOther        = "other"        // untagged callers
)

type indexerPurposeKey struct{}

// WithIndexerPurpose tags ctx's indexer requests with purpose. The
// innermost tag wins, so a rescue fan-out started by playback counts as
// rescue.
func WithIndexerPurpose(ctx context.Context, purpose string) context.Context {
	if purpose == "" {
		return ctx
	}
	return context.WithValue(ctx, indexerPurposeKey{}, purpose)
}

// IndexerPurpose returns ctx's purpose tag, or IndexerPurposeOther.
func IndexerPurpose(ctx context.Context) string {
	if p, ok := ctx.Value(indexerPurposeKey{}).(string); ok && p != "" {
		return p
	}
	return IndexerPurposeOther
}

// InheritIndexerPurpose carries src's purpose tag into dst, for work
// detached from the caller's cancellation (a search that keeps filling the
// cache after Play started).
func InheritIndexerPurpose(dst, src context.Context) context.Context {
	if p, ok := src.Value(indexerPurposeKey{}).(string); ok && p != "" {
		return context.WithValue(dst, indexerPurposeKey{}, p)
	}
	return dst
}
