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
	_, err := s.db.ExecContext(ctx, `UPDATE availability_feeds SET covered_since=CASE WHEN ? <> 0 AND covered_since>0 THEN covered_since ELSE ? END WHERE source_id=? AND generation=?`,
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

// feedless returns the searchable sources without a release feed (scrapers
// and addons) that skip leaves in.
func feedless(sources []core.ReleaseFeed, skip map[string]bool) []core.ReleaseFeed {
	var out []core.ReleaseFeed
	for _, f := range sources {
		if !f.Supported && !skip[f.ID] {
			out = append(out, f)
		}
	}
	return out
}

// bestScraper is the feedless source that has most often returned releases
// in full searches (smoothed, so an untried source is not written off), or
// "" when there is no choice to make.
func (s *Service) bestScraper(ctx context.Context, scrapers []core.ReleaseFeed) string {
	if len(scrapers) < 2 {
		return ""
	}
	best, bestScore := "", -1.0
	for _, f := range scrapers {
		var searches, found int64
		_ = s.db.QueryRowContext(ctx, `SELECT searches,found FROM availability_source_yield WHERE source_id=?`, f.ID).Scan(&searches, &found)
		score := float64(found+1) / float64(searches+2)
		if score > bestScore {
			best, bestScore = f.ID, score
		}
	}
	return best
}

// noteScraperYield records, after a full search, which feedless sources
// returned anything.
func (s *Service) noteScraperYield(ctx context.Context, scrapers []core.ReleaseFeed, releases []core.Release) {
	for _, f := range scrapers {
		found := 0
		for i := range releases {
			if releases[i].Indexer == f.Name || releases[i].Indexer == f.ID {
				found = 1
				break
			}
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO availability_source_yield(source_id,searches,found) VALUES(?,1,?)
 ON CONFLICT(source_id) DO UPDATE SET searches=availability_source_yield.searches+1,found=availability_source_yield.found+excluded.found`, f.ID, found); err != nil {
			s.log.Debug("availability: record source yield", "source", f.ID, "err", err)
		}
	}
}
