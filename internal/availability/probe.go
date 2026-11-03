package availability

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
)

// A title probe searches by id for movies still inside the evidence window.
// Feed history is not required. A release counts only when it would pass the
// owner's quality profile.
func (s *Service) probeDue(ctx context.Context) error {
	if s.d.Search == nil || !s.cfg().Availability.Monitor {
		return nil
	}
	cat, ok := s.d.Catalog.(interface {
		RecentMovies(context.Context, time.Time) ([]core.Title, error)
	})
	if !ok {
		return nil
	}
	cfg := s.cfg().Availability
	since := s.now().Add(-time.Duration(cfg.EffectiveMovieEvidenceDays()) * 24 * time.Hour)
	movies, err := cat.RecentMovies(ctx, since)
	if err != nil || len(movies) == 0 {
		return err
	}
	homes := map[core.RatingKey]time.Time{}
	checked := map[core.RatingKey]bool{}
	if h, ok := s.d.Catalog.(interface {
		HomeReleases(context.Context, []core.RatingKey) (map[core.RatingKey]time.Time, error)
	}); ok {
		keys := make([]core.RatingKey, len(movies))
		for i := range movies {
			keys[i] = movies[i].Key
		}
		if got, e := h.HomeReleases(ctx, keys); e == nil {
			homes = got
			for k := range got {
				checked[k] = true
			}
		}
	}
	// Titles whose release has since been found unplayable are looked for
	// again, whatever their cadence.
	unavailable := map[core.RatingKey]time.Time{}
	if v, ok := s.d.Validator.(interface {
		KnownUnavailableSince(context.Context, []core.RatingKey) map[core.RatingKey]time.Time
	}); ok {
		keys := make([]core.RatingKey, len(movies))
		for i := range movies {
			keys[i] = movies[i].Key
		}
		unavailable = v.KnownUnavailableSince(ctx, keys)
	}
	watch := map[core.RatingKey]bool{}
	if s.d.Watchlist != nil {
		if keys, e := s.d.Watchlist(ctx); e == nil {
			watch = keys
		}
	}
	type item struct {
		t     core.Title
		score float64
		check titleCheck
	}
	var due []item
	for _, t := range movies {
		if !t.IsReleased(s.now()) {
			continue
		}
		check, err := s.titleCheck(ctx, t.Key)
		if err != nil {
			return err
		}
		state, next := check.state, check.next
		digital := time.Time{}
		if checked[t.Key] {
			digital = homes[t.Key]
		}
		dueNow := state == "" || !s.now().Before(next)
		// A digital or physical date that has arrived is due immediately,
		// even when an earlier check is still inside its cadence.
		if !digital.IsZero() && !digital.After(s.now()) && (state == "" || next.Before(digital)) {
			dueNow = true
		}
		if state == "found" {
			// A found title is available like any other; playback searches
			// for itself. It is looked for again only if its release turned
			// out unplayable, which hides it until another is found.
			since, ok := unavailable[t.Key]
			dueNow = ok && since.After(check.checked)
		}
		if !dueNow {
			continue
		}
		score := t.Popularity
		if watch[t.Key] {
			score += 1000
		}
		if !digital.IsZero() && !digital.After(s.now()) {
			score += 2000
		} else if !digital.IsZero() && digital.Sub(s.now()) < 72*time.Hour {
			score += 800
		}
		due = append(due, item{t, score, check})
	}
	sort.SliceStable(due, func(i, j int) bool { return due[i].score > due[j].score })
	covered, err := s.coveredFeeds(ctx)
	if err != nil {
		return err
	}
	// At most probeBatch searches per one-minute cycle, most important
	// first: a backlog (a first start, a long stop) drains over minutes
	// instead of bursting at every indexer at once. Titles the feeds cover
	// cost no search and do not count.
	searches := 0
	for _, it := range due {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if searches >= probeBatch {
			break
		}
		searched, err := s.probeOne(ctx, it.t, homes[it.t.Key], checked[it.t.Key], it.check, covered)
		if searched {
			searches++
		}
		if err != nil {
			if benign(err) {
				return err
			}
			s.log.Warn("title probe failed", "title", it.t.Title, "error", err)
		}
	}
	return nil
}

// Title probe cadence. The release feeds already watch every new upload
// every few minutes; a probe only catches what a feed missed, so it runs on
// hours, never on the one-minute monitor cycle.
const (
	// probeFullEvery is the longest a waiting title goes without a search
	// of every indexer, feed coverage aside: a release a feed listed under
	// a name the catalogue could not match is still found.
	probeFullEvery = 7 * 24 * time.Hour
	// probeBatch bounds the probe searches per monitor cycle (a minute).
	probeBatch         = 10
	probeDigitalFresh  = 3 * time.Hour // digital date passed within probeFreshWindow
	probeFreshWindow   = 72 * time.Hour
	probeNew           = 6 * time.Hour  // no digital date, released in the last two weeks
	probeWaiting       = 12 * time.Hour // no digital date, or it passed long ago
	probeBeforeDigital = 72 * time.Hour // longest wait while a digital date is ahead
)

// probeWhen is when to look for t again, spread by up to a tenth of the
// wait per title so titles first checked together do not stay in step.
func (s *Service) probeWhen(t core.Title, digital time.Time, known, found bool) time.Time {
	next := s.probeCadence(t, digital, known, found)
	wait := next.Sub(s.now())
	if next.IsZero() || wait <= 0 {
		return next
	}
	return next.Add(time.Duration(uint64(t.Key)*2654435761%1000) * wait / 10000)
}

func (s *Service) probeCadence(t core.Title, digital time.Time, known, found bool) time.Time {
	now := s.now()
	if found {
		return time.Time{} // never: see probeDue
	}
	if known && !digital.IsZero() && !digital.After(now) {
		// Releases usually land around the digital date; look a few times
		// in the first days, then settle. Rechecking every cycle spent a
		// full indexer search per minute on movies nobody had posted.
		if now.Sub(digital) < probeFreshWindow {
			return now.Add(probeDigitalFresh)
		}
		return now.Add(probeWaiting)
	}
	if known && !digital.IsZero() {
		// Wait for the digital date (probeDue makes it due the moment the
		// date arrives) but recheck at least every few days: early leaks
		// and wrong dates exist, and the feeds see most of those first.
		return now.Add(min(max(digital.Sub(now), probeDigitalFresh), probeBeforeDigital))
	}
	if now.Sub(t.Released) < 14*24*time.Hour {
		return now.Add(probeNew)
	}
	return now.Add(probeWaiting)
}

func (s *Service) probeOne(ctx context.Context, t core.Title, digital time.Time, known bool, last titleCheck, covered map[string]time.Time) (bool, error) {
	req := core.SearchRequest{Type: t.Type, IDs: t.IDs, Title: t.Title, OriginalTitle: t.OriginalTitle, Year: t.Year, AlternativeTitles: t.AlternativeTitles, Released: t.Released}
	// A title already looked for skips every indexer whose feed has shown
	// all releases since that look: the feed would have matched anything
	// new. A first look, an unplayable found release, the look when the
	// digital date arrives, and the weekly full look search everywhere.
	// The digital date arriving since the last look is when releases land,
	// so that look searches everywhere too.
	digitalArrived := known && !digital.IsZero() && !digital.After(s.now()) && digital.After(last.checked)
	full := last.state == "" || last.state == "found" || digitalArrived || last.full.IsZero() || s.now().Sub(last.full) >= probeFullEvery
	skip := map[string]bool{}
	// Background work stays on the indexers the owner gave it
	// (Availability.IndexerIDs); playback still searches every indexer.
	sources := s.d.Feeds.FeedSources()
	if ids := s.cfg().Availability.IndexerIDs; len(ids) > 0 {
		for _, f := range sources {
			if !selected(ids, f.ID) {
				skip[f.ID] = true
			}
		}
	}
	scrapers := feedless(sources, skip)
	if !full {
		for id, since := range covered {
			if !since.After(last.checked) {
				skip[id] = true
			}
		}
		// Routine checks ask one feedless source, the one that has returned
		// releases most often in full searches; Play still asks them all.
		if best := s.bestScraper(ctx, scrapers); best != "" {
			for _, f := range scrapers {
				if f.ID != best {
					skip[f.ID] = true
				}
			}
		}
		if len(skip) > 0 && everyIndexer(sources, skip) {
			return false, s.saveTitleCheckFull(ctx, t.Key, last.state, last.reason, s.probeWhen(t, digital, known, false), last.full)
		}
	}
	fullAt := last.full
	if full {
		fullAt = s.now()
	}
	bctx, cancel := context.WithTimeout(core.WithIndexerSkip(s.budgetContext(ctx, true), skip), 25*time.Second)
	releases, err := s.d.Search(bctx, req)
	cancel()
	if full && err == nil {
		s.noteScraperYield(ctx, scrapers, releases)
	}
	if err != nil && len(releases) == 0 {
		// A spent budget or a stopping service decides nothing: the title
		// stays due and the pass ends. A slow or failing indexer is a
		// result, though: retrying it on the next minute's cycle would
		// search the same title over and over.
		if errors.Is(err, core.ErrBudgetExhausted) || errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return true, err
		}
		return true, s.saveTitleCheckFull(ctx, t.Key, "none", "Search could not be completed", s.probeWhen(t, digital, known, false), last.full)
	}
	releases = s.cachedTorrents(ctx, releases)
	policy := s.policy()
	var feeds []core.ReleaseFeed
	if s.d.Feeds != nil {
		feeds = s.d.Feeds.FeedSources()
	}
	quality := ""
	for _, r := range releases {
		accepted, reason, e := s.d.Validator.FeedCandidate(ctx, &t, r)
		if e != nil {
			return true, e
		}
		if reason != "" {
			if quality == "" {
				quality = reason
			}
			continue
		}
		f := feedFor(feeds, accepted)
		if f.ID == "" {
			f = core.ReleaseFeed{ID: "probe", Name: accepted.Indexer, Generation: "probe", Supported: true}
		}
		if e = s.remember(ctx, f, t, accepted, policy); e != nil {
			return true, e
		}
		return true, s.saveTitleCheckFull(ctx, t.Key, "found", "", s.probeWhen(t, digital, known, true), fullAt)
	}
	if quality != "" {
		return true, s.saveTitleCheckFull(ctx, t.Key, "quality", quality, s.probeWhen(t, digital, known, false), fullAt)
	}
	return true, s.saveTitleCheckFull(ctx, t.Key, "none", "", s.probeWhen(t, digital, known, false), fullAt)
}

func (s *Service) cachedTorrents(ctx context.Context, rs []core.Release) []core.Release {
	if s.d.CacheCheck == nil {
		return rs
	}
	var hashes []string
	for _, r := range rs {
		if r.Protocol == core.ProtocolTorrent && r.InfoHash != "" {
			hashes = append(hashes, r.InfoHash)
		}
	}
	if len(hashes) == 0 {
		return rs
	}
	bctx, cancel := context.WithTimeout(s.budgetContext(ctx, false), 15*time.Second)
	cached, definitive := s.d.CacheCheck(bctx, hashes)
	cancel()
	if !definitive {
		return rs
	}
	out := rs[:0]
	for _, r := range rs {
		if r.Protocol == core.ProtocolTorrent && r.InfoHash != "" && !cached[r.InfoHash] {
			continue
		}
		out = append(out, r)
	}
	return out
}

func feedFor(feeds []core.ReleaseFeed, r core.Release) core.ReleaseFeed {
	for _, f := range feeds {
		if f.Name == r.Indexer || f.ID == r.Indexer {
			return f
		}
	}
	return core.ReleaseFeed{}
}

// titleCheck is a title's last probe: when, what it found, and when every
// indexer was last searched for it.
type titleCheck struct {
	checked, next, full time.Time
	state, reason       string
}

func (s *Service) titleCheck(ctx context.Context, key core.RatingKey) (titleCheck, error) {
	var checked, next, full int64
	var c titleCheck
	err := s.db.QueryRowContext(ctx, `SELECT checked_at,next_check,full_at,state,reason FROM availability_title_checks WHERE rating_key=?`, int64(key)).Scan(&checked, &next, &full, &c.state, &c.reason)
	if errors.Is(err, sql.ErrNoRows) {
		return titleCheck{}, nil
	}
	if err != nil {
		return titleCheck{}, err
	}
	c.checked = time.Unix(checked, 0)
	if next > 0 {
		c.next = time.Unix(next, 0)
	}
	if full > 0 {
		c.full = time.Unix(full, 0)
	}
	return c, nil
}

func (s *Service) saveTitleCheck(ctx context.Context, key core.RatingKey, state, reason string, next time.Time) error {
	return s.saveTitleCheckFull(ctx, key, state, reason, next, time.Time{})
}

// saveTitleCheckFull records a probe; full is when every indexer was last
// searched for the title (zero keeps none recorded).
func (s *Service) saveTitleCheckFull(ctx context.Context, key core.RatingKey, state, reason string, next, full time.Time) error {
	fullAt := int64(0)
	if !full.IsZero() {
		fullAt = full.Unix()
	}
	nextAt := int64(0) // zero: no scheduled look (a found title)
	if !next.IsZero() {
		nextAt = next.Unix()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO availability_title_checks(rating_key,checked_at,next_check,state,reason,full_at) VALUES(?,?,?,?,?,?)
 ON CONFLICT(rating_key) DO UPDATE SET checked_at=excluded.checked_at,next_check=excluded.next_check,state=excluded.state,reason=excluded.reason,full_at=excluded.full_at`,
		int64(key), s.now().Unix(), nextAt, state, reason, fullAt)
	return err
}

// everyIndexer reports whether skip leaves out every searchable source.
func everyIndexer(sources []core.ReleaseFeed, skip map[string]bool) bool {
	for _, f := range sources {
		if !skip[f.ID] {
			return false
		}
	}
	return true
}

func (s *Service) acceptDigitalReleases(ctx context.Context, keys []core.RatingKey, now time.Time, acceptable map[core.RatingKey]bool) {
	h, ok := s.d.Catalog.(interface {
		HomeReleases(context.Context, []core.RatingKey) (map[core.RatingKey]time.Time, error)
	})
	if !ok {
		return
	}
	got, err := h.HomeReleases(ctx, keys)
	if err != nil {
		return
	}
	for k, at := range got {
		if !at.IsZero() && !at.After(now) {
			acceptable[k] = true
		}
	}
}
