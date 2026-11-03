package availability

import (
	"context"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
)

// Feed coverage lets a title probe skip indexers whose release feed has
// already shown it everything posted since the title's last check. A head
// poll that still holds the previous poll's newest release saw every release
// in between; one that does not (more uploads than a page, a restart after
// a long stop, a changed feed) may have missed some, so coverage restarts
// there. Only continuous, current coverage counts.

// markCoverage records one finished feed sync: continuous keeps the feed's
// coverage, otherwise it starts again now.
func (s *Service) markCoverage(ctx context.Context, f core.ReleaseFeed, continuous bool) error {
	now := s.now().Unix()
	_, err := s.db.ExecContext(ctx, `UPDATE availability_feeds SET covered_since=CASE WHEN ? AND covered_since>0 THEN covered_since ELSE ? END WHERE source_id=? AND generation=?`,
		boolInt(continuous), now, f.ID, f.Generation)
	return err
}

// coveredFeeds returns, for each feed that is polled and current, the time
// since which it has shown every release.
func (s *Service) coveredFeeds(ctx context.Context) (map[string]time.Time, error) {
	cfg := s.cfg().Availability
	if !cfg.Monitor {
		return nil, nil
	}
	interval := time.Duration(max(5, s.budgets().PollMinutes)) * time.Minute
	out := map[string]time.Time{}
	for _, f := range s.d.Feeds.FeedSources() {
		if !f.Supported || !selected(cfg.IndexerIDs, f.ID) {
			continue
		}
		var since, synced int64
		var gap int
		err := s.db.QueryRowContext(ctx, `SELECT covered_since,synced_at,gap FROM availability_feeds WHERE source_id=? AND generation=?`, f.ID, f.Generation).Scan(&since, &synced, &gap)
		if err != nil {
			continue
		}
		// Stale coverage (polls failing or paused) proves nothing about now.
		if since == 0 || gap != 0 || s.now().Sub(time.Unix(synced, 0)) > 3*interval {
			continue
		}
		out[f.ID] = time.Unix(since, 0)
	}
	return out, nil
}

// pageHolds reports whether releases include the release id.
func pageHolds(releases []core.Release, id string) bool {
	for i := range releases {
		if releases[i].ID == id {
			return true
		}
	}
	return false
}
