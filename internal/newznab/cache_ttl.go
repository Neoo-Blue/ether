package newznab

import (
	"context"
	"time"
)

// An indexer's results for an old title barely change, so they are kept
// longer than a new title's: a title out under a month keeps the client's
// base lifetime (6 hours by default), one out over a year keeps a week, and
// ages in between scale. A search that found nothing is kept briefly for a
// title out under a week (an episode checked before its upload is searched
// again soon after it drops) and longer for anything older.
const (
	cacheFreshAge  = 30 * 24 * time.Hour
	cacheOldAge    = 365 * 24 * time.Hour
	cacheOldTTL    = 7 * 24 * time.Hour
	emptyFreshAge  = 7 * 24 * time.Hour
	emptyStableTTL = 30 * time.Minute
)

type cacheAgeKey struct{}

// withCacheAge carries the searched title's release time to cachePut.
func withCacheAge(ctx context.Context, released time.Time) context.Context {
	if released.IsZero() {
		return ctx
	}
	return context.WithValue(ctx, cacheAgeKey{}, released)
}

func cacheAge(ctx context.Context) time.Time {
	t, _ := ctx.Value(cacheAgeKey{}).(time.Time)
	return t
}

// searchCacheTTL is how long to keep a search's results, from the client's
// base lifetime and the title's release time (zero: unknown, base rules).
func searchCacheTTL(base time.Duration, released, now time.Time, empty bool) time.Duration {
	if base <= 0 {
		return base
	}
	if released.IsZero() || released.After(now) {
		if empty {
			return min(base, emptySearchCacheTTL)
		}
		return base
	}
	age := now.Sub(released)
	if empty {
		if age < emptyFreshAge {
			return min(base, emptySearchCacheTTL)
		}
		return min(base, emptyStableTTL)
	}
	if age <= cacheFreshAge || base >= cacheOldTTL {
		return base
	}
	if age >= cacheOldAge {
		return cacheOldTTL
	}
	frac := float64(age-cacheFreshAge) / float64(cacheOldAge-cacheFreshAge)
	return base + time.Duration(frac*float64(cacheOldTTL-base))
}
