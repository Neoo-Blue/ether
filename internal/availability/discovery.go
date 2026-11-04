package availability

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/releaseparse"
)

// EvidenceWindow is a proof window, not a delay: an eligible observed release
// unlocks a title immediately. Older catalogue entries do not need a scan.
func EvidenceWindow(t core.Title, now time.Time) bool {
	return evidenceWindow(t, now, config.Availability{})
}

func evidenceWindow(t core.Title, now time.Time, cfg config.Availability) bool {
	if t.Released.IsZero() || t.Key.Kind() == core.KindLocal {
		return false
	}
	age := now.Sub(t.Released)
	switch t.Type {
	case core.TypeMovie:
		if cfg.EffectiveMode() == config.ModeDigital {
			return true
		}
		return !cfg.DisableMovieEvidenceGate && age < time.Duration(cfg.EffectiveMovieEvidenceDays())*24*time.Hour
	case core.TypeEpisode, core.TypeShow:
		return age < 24*time.Hour
	}
	return false
}

// FilterDiscovery reads local observations only. A feed listing establishes
// release existence, not guaranteed playback; playback still checks the source.
func (s *Service) filterReleaseDiscovery(ctx context.Context, items []core.Title, input discoverySnapshot) []core.Title {
	titles := map[core.RatingKey]*core.Title{}
	keys := []core.RatingKey{}
	for i := range items {
		t := &items[i]
		if !input.upcomingMovie(*t) && evidenceWindow(*t, input.now, input.availability) && titles[t.Key] == nil {
			keys = append(keys, t.Key)
			titles[t.Key] = t
		}
	}
	if len(keys) == 0 {
		return items
	}
	feeds := map[string]core.ReleaseFeed{}
	for _, f := range input.feeds {
		feeds[f.ID] = f
	}
	s.attachRetained(ctx, titles, feeds, input)
	unavailable := map[core.RatingKey]time.Time{}
	if source, ok := s.d.Validator.(interface {
		KnownUnavailableSince(context.Context, []core.RatingKey) map[core.RatingKey]time.Time
	}); ok {
		if current, _ := ctx.Value(currentPolicyKey{}).(bool); current {
			if durable, ok := s.d.Validator.(interface {
				KnownUnavailableSinceCurrent(context.Context, []core.RatingKey) map[core.RatingKey]time.Time
			}); ok {
				unavailable = durable.KnownUnavailableSinceCurrent(ctx, keys)
			} else {
				unavailable = source.KnownUnavailableSince(ctx, keys)
			}
		} else {
			unavailable = source.KnownUnavailableSince(ctx, keys)
		}
	}
	acceptable := map[core.RatingKey]bool{}
	type observation struct {
		key                                core.RatingKey
		source, generation, payload, state string
		firstSeen                          int64
		revision                           int64
	}
	var observations []observation
	for start := 0; start < len(keys); start += 500 {
		batch := keys[start:min(start+500, len(keys))]
		marks := make([]string, len(batch))
		args := make([]any, 0, len(batch)+1)
		for i, k := range batch {
			marks[i] = "?"
			args = append(args, k)
		}
		args = append(args, input.retentionCutoff().Unix())
		rows, err := s.db.QueryContext(ctx, `SELECT rating_key,source_id,generation,payload,state,first_seen,revision FROM availability_releases WHERE rating_key IN (`+strings.Join(marks, ",")+`) AND last_seen>=?`, args...)
		if err != nil {
			continue
		}
		for rows.Next() {
			var o observation
			if rows.Scan(&o.key, &o.source, &o.generation, &o.payload, &o.state, &o.firstSeen, &o.revision) == nil {
				observations = append(observations, o)
			}
		}
		if err := rows.Err(); err != nil {
			s.log.Warn("availability observations not fully loaded", "err", err)
		}
		rows.Close()
	}
	if input.availability.DigitalDateEvidence {
		s.acceptDigitalReleases(ctx, keys, input.now, acceptable)
	}
	// Close the cursor before policy checks, which may read the same database.
	var pairs []feedPair
	for _, o := range observations {
		if input.availability.EffectiveMode() == config.ModeDigital && titles[o.key].Type == core.TypeMovie {
			continue
		}
		if acceptable[o.key] || (o.state == "unknown" && o.revision > 0) || o.state == "unavailable" {
			continue
		}
		f, ok := feeds[o.source]
		if !ok || f.Generation != o.generation || !input.usable(f.Protocol) {
			continue
		}
		if since, ok := unavailable[o.key]; ok && o.firstSeen <= since.Unix() {
			continue
		}
		r, err := s.decode(o.payload)
		if err != nil {
			continue
		}
		r.Info = releaseparse.Parse(r.Name)
		pairs = append(pairs, feedPair{t: titles[o.key], r: r})
	}
	// One evaluation pass for every observation; durable verdicts answer
	// the ones judged before, and a title stops at its first acceptance.
	for i, ok := range s.judge(ctx, pairs, true) {
		if ok {
			acceptable[pairs[i].t.Key] = true
		}
	}
	out := make([]core.Title, 0, len(items))
	for _, t := range items {
		if titles[t.Key] == nil || (!t.Released.After(input.now) && acceptable[t.Key]) {
			out = append(out, t)
		}
	}
	return out
}

// HiddenMovies supplies SQL pagination with exclusions from the recent local
// mirror only. No per-title provider lookup or historical catalogue walk occurs.
func (s *Service) HiddenMovies(ctx context.Context) []core.RatingKey {
	input := s.snapshotFor(ctx)
	if input.availability.DisableMovieEvidenceGate {
		return nil
	}
	return s.hidden(ctx, hiddenMovies, input)
}

// HiddenShows excludes first-day series before SQL pagination, just as movies.
func (s *Service) HiddenShows(ctx context.Context) []core.RatingKey {
	return s.hidden(ctx, hiddenShows, s.snapshotFor(ctx))
}

const (
	hiddenMovies = "movie"
	hiddenShows  = "show"
)

// recent lists the catalogue titles of kind inside the evidence window: one
// indexed range read bounded by the window, never the whole library.
func (s *Service) recent(ctx context.Context, kind string, input discoverySnapshot) ([]core.Title, bool, error) {
	switch kind {
	case hiddenMovies:
		c, ok := s.d.Catalog.(interface {
			RecentMovies(context.Context, time.Time) ([]core.Title, error)
		})
		if !ok {
			return nil, false, nil
		}
		ts, err := c.RecentMovies(ctx, input.now.Add(-time.Duration(input.availability.EffectiveMovieEvidenceDays())*24*time.Hour))
		return ts, true, err
	default:
		c, ok := s.d.Catalog.(interface {
			RecentShows(context.Context, time.Time) ([]core.Title, error)
		})
		if !ok {
			return nil, false, nil
		}
		ts, err := c.RecentShows(ctx, input.now.Add(-24*time.Hour))
		return ts, true, err
	}
}

// hiddenSet is the last computed evidence answer for one kind: which titles
// in the window were considered, and which of them no evidence unlocks.
type hiddenSet struct {
	key      string
	revision uint64
	at       time.Time
	covered  map[core.RatingKey]bool
	hidden   map[core.RatingKey]bool
	keys     []core.RatingKey
}

// fresh reports whether the set still answers for these inputs: the same
// settings, no observation or invalidation since, and at most fifteen
// seconds old (other cluster nodes write evidence too, and titles leave
// the window as time passes).
func (h *hiddenSet) fresh(key string, revision uint64, now time.Time) bool {
	return h.key == key && h.revision == revision && now.Sub(h.at) < 15*time.Second && !now.Before(h.at)
}

func hiddenKey(kind string, input discoverySnapshot) string {
	return fmt.Sprintf("%s:%s:%+v:%+v", kind, input.policy, input.availability, input.feeds)
}

// hidden is the titles in the evidence window that no observation unlocks
// yet. Every library page and section hub excludes them, so requests only
// read the last computed answer; judging retained feed releases happens in
// a refresh (in the background when Deps.Async is set). A stale answer
// is served while its refresh runs. Before the first answer exists every
// title in the window stays hidden, as it would until evidence arrives.
func (s *Service) hidden(ctx context.Context, kind string, input discoverySnapshot) []core.RatingKey {
	if h := s.currentHidden(kind, input); h != nil {
		return slices.Clone(h.keys)
	}
	items, ok, err := s.recent(ctx, kind, input)
	if !ok || err != nil {
		return nil
	}
	var hidden []core.RatingKey
	for _, t := range items {
		if !input.upcomingMovie(t) && evidenceWindow(t, input.now, input.availability) {
			hidden = append(hidden, t.Key)
		}
	}
	return hidden
}

// currentHidden returns the last computed set for kind, first scheduling a
// refresh when it no longer answers for input. It never evaluates feed
// releases itself unless refreshes run inline (Deps.Async false).
func (s *Service) currentHidden(kind string, input discoverySnapshot) *hiddenSet {
	key := hiddenKey(kind, input)
	s.visibilityMu.Lock()
	h := s.hiddenSets[kind]
	stale := h == nil || !h.fresh(key, s.visibilityRevision, input.now)
	s.visibilityMu.Unlock()
	if stale {
		s.refreshHidden(kind)
		s.visibilityMu.Lock()
		h = s.hiddenSets[kind]
		s.visibilityMu.Unlock()
	}
	return h
}

// refreshHidden recomputes kind's hidden set, at most one pass per kind at a
// time; a request arriving mid-pass queues exactly one more pass.
func (s *Service) refreshHidden(kind string) {
	s.visibilityMu.Lock()
	if s.refreshing == nil {
		s.refreshing = map[string]int{}
	}
	if s.refreshing[kind] > 0 {
		s.refreshing[kind] = 2
		s.visibilityMu.Unlock()
		return
	}
	s.refreshing[kind] = 1
	s.visibilityMu.Unlock()
	run := func() {
		for {
			ctx, cancel := context.WithTimeout(s.bgCtx, 5*time.Minute)
			s.computeHidden(ctx, kind)
			cancel()
			s.visibilityMu.Lock()
			if s.refreshing[kind] == 1 {
				delete(s.refreshing, kind)
				s.visibilityMu.Unlock()
				return
			}
			s.refreshing[kind] = 1
			s.visibilityMu.Unlock()
		}
	}
	if !s.d.Async {
		run()
		return
	}
	s.visibilityMu.Lock()
	defer s.visibilityMu.Unlock()
	if s.closed {
		delete(s.refreshing, kind)
		return
	}
	s.bg.Add(1)
	go func() {
		defer s.bg.Done()
		run()
	}()
}

// warmHidden keeps both sets current between requests.
func (s *Service) warmHidden() {
	input := s.discoverySnapshot()
	if !input.availability.DisableMovieEvidenceGate {
		s.currentHidden(hiddenMovies, input)
	}
	s.currentHidden(hiddenShows, input)
}

func (s *Service) computeHidden(ctx context.Context, kind string) {
	input := s.discoverySnapshot()
	s.visibilityMu.Lock()
	revision := s.visibilityRevision
	s.visibilityMu.Unlock()
	items, ok, err := s.recent(ctx, kind, input)
	if !ok || err != nil {
		return
	}
	visible := map[core.RatingKey]bool{}
	for _, t := range s.filterDiscovery(ctx, items, input) {
		visible[t.Key] = true
	}
	h := &hiddenSet{key: hiddenKey(kind, input), revision: revision, at: input.now, covered: map[core.RatingKey]bool{}, hidden: map[core.RatingKey]bool{}}
	for _, t := range items {
		h.covered[t.Key] = true
		// Mirror filterDiscoverySnapshot: outside the window or an
		// upcoming movie the owner shows is never hidden.
		if !visible[t.Key] && !input.upcomingMovie(t) && evidenceWindow(t, input.now, input.availability) && !h.hidden[t.Key] {
			h.hidden[t.Key] = true
			h.keys = append(h.keys, t.Key)
		}
	}
	if ctx.Err() != nil {
		return
	}
	s.visibilityMu.Lock()
	s.hiddenSets[kind] = h
	s.visibilityMu.Unlock()
}
