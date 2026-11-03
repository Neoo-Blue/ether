package prerolls

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/store"
)

type fakeActivity struct {
	finished time.Time
	stopped  time.Time
}

func (f fakeActivity) History(_ context.Context, _ int64, since time.Time, _ int) ([]core.HistoryEntry, error) {
	if f.finished.IsZero() || f.finished.Before(since) {
		return nil, nil
	}
	return []core.HistoryEntry{{ViewedAt: f.finished, Completed: true}}, nil
}

func (f fakeActivity) InProgress(context.Context, int64, int) ([]core.WatchStatus, error) {
	if f.stopped.IsZero() {
		return nil, nil
	}
	return []core.WatchStatus{{Key: core.MovieKey(9), LastViewedAt: f.stopped, ViewOffset: time.Minute}}, nil
}

type ruleEnv struct {
	t        *testing.T
	s        *Service
	now      time.Time
	activity *fakeActivity
	shows    map[core.RatingKey]*core.Title
}

func newRuleEnv(t *testing.T) *ruleEnv {
	t.Helper()
	env := &ruleEnv{t: t, now: time.Date(2026, 10, 15, 20, 0, 0, 0, time.UTC), activity: &fakeActivity{}, shows: map[core.RatingKey]*core.Title{}}
	s, err := New(Options{
		DB: store.OpenTest(t), Dir: t.TempDir(), Activity: env.activity,
		Now:   func() time.Time { return env.now },
		Rand:  func(int) int { return 0 },
		Title: func(_ context.Context, k core.RatingKey) (*core.Title, error) { return env.shows[k], nil },
		InCollection: func(_ context.Context, id string, k core.RatingKey) bool {
			return id == "7" && k == core.ShowKey(100)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	env.s = s
	return env
}

// add creates a ready pre-roll.
func (e *ruleEnv) add(in Input) Preroll {
	e.t.Helper()
	ctx := context.Background()
	p, err := e.s.Create(ctx, in)
	if err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(e.s.path(p.seq, ".mp4"), []byte("mp4"), 0o600); err != nil {
		e.t.Fatal(err)
	}
	if err := e.s.setVideo(ctx, p.seq, videoUpdate{state: StateReady, durationMs: 9500, width: 1920, height: 1080}); err != nil {
		e.t.Fatal(err)
	}
	return p
}

var (
	viewer12 = &core.User{ID: 12, Username: "ann"}
	viewer15 = &core.User{ID: 15, Username: "bo"}
	horror   = &core.Title{Key: core.MovieKey(1), Type: core.TypeMovie, Title: "Night", Genres: []string{"Horror"}, ContentRating: "R"}
	comedy   = &core.Title{Key: core.MovieKey(2), Type: core.TypeMovie, Title: "Laughs", Genres: []string{"Comedy"}, ContentRating: "PG"}
	episode  = &core.Title{Key: core.EpisodeKey(100, 1, 2), Type: core.TypeEpisode, ShowKey: core.ShowKey(100), Title: "Two", Season: 1, Episode: 2}
)

func (e *ruleEnv) req(v *core.User, t *core.Title) core.PrerollRequest {
	return core.PrerollRequest{Viewer: v, Device: core.DeviceTV, Feature: t, At: e.now, Client: "Plex for Apple TV"}
}

func (e *ruleEnv) pick(req core.PrerollRequest) string {
	e.t.Helper()
	e.s.invalidate()
	clip, ok := e.s.Pick(context.Background(), req)
	if !ok {
		return ""
	}
	all, _ := e.s.loadAll(context.Background())
	for _, p := range all {
		if p.Key() == clip.Key {
			return p.Name
		}
	}
	e.t.Fatalf("picked unknown clip %v", clip.Key)
	return ""
}

func reasonCodes(c Candidate) []string {
	var out []string
	for _, r := range c.Reasons {
		if !r.OK {
			out = append(out, r.Code)
		}
	}
	return out
}

// The rules engine is the feature's core invariant: every condition is
// evaluated in one fixed order, playback and the simulator share it, at
// most one pre-roll wins, and nothing is recorded until a play starts.
func TestRulesEngineSelectsOneInFixedOrder(t *testing.T) {
	env := newRuleEnv(t)
	ctx := context.Background()
	bg := context.Background()
	f := false
	env.add(Input{Name: "General", Priority: 0})
	env.add(Input{Name: "Horror", Priority: 5, Rules: Rules{Kinds: []string{"movie"}, Genres: []string{"horror"}, DateFrom: "10-01", DateTo: "10-31"}})
	env.add(Input{Name: "Kids", Priority: 9, Rules: Rules{MaxContentRating: "PG", Audience: Audience{Mode: "only", Viewers: []int64{15}}}})
	env.add(Input{Name: "Off", Priority: 99, Enabled: &f})
	env.add(Input{Name: "Away TV", Priority: 50, Rules: Rules{Network: "away", Devices: []string{"tv"}}})
	env.add(Input{Name: "Show collection", Priority: 3, Rules: Rules{Kinds: []string{"episode"}, Collections: []string{"7"}, FirstEpisodeOnly: true, BingeGapMinutes: 30}, Frequency: Frequency{Mode: FrequencyDaily}})

	// Priority wins among matches; disabled never plays.
	if got := env.pick(env.req(viewer12, horror)); got != "Horror" {
		t.Fatalf("horror movie in October: %q", got)
	}
	if got := env.pick(env.req(viewer12, comedy)); got != "General" {
		t.Fatalf("comedy for viewer 12: %q", got)
	}
	if got := env.pick(env.req(viewer15, comedy)); got != "Kids" {
		t.Fatalf("PG comedy for viewer 15: %q", got)
	}
	if got := env.pick(env.req(viewer15, horror)); got != "Horror" {
		t.Fatalf("R movie exceeds the Kids rating: %q", got)
	}
	away := env.req(viewer12, comedy)
	away.Away = true
	if got := env.pick(away); got != "Away TV" {
		t.Fatalf("away TV: %q", got)
	}
	away.Device = core.DeviceMobile
	if got := env.pick(away); got != "General" {
		t.Fatalf("away mobile: %q", got)
	}
	// The date window ends.
	env.now = time.Date(2026, 11, 1, 20, 0, 0, 0, time.UTC)
	if got := env.pick(env.req(viewer12, horror)); got != "General" {
		t.Fatalf("horror in November: %q", got)
	}
	// Resumed plays never get a pre-roll.
	resumed := env.req(viewer12, comedy)
	resumed.Resume = true
	if got := env.pick(resumed); got != "" {
		t.Fatalf("resume got %q", got)
	}

	// Episodes: the collection matches the show, the binge rule skips an
	// auto-play and a play within the gap.
	if got := env.pick(env.req(viewer12, episode)); got != "Show collection" {
		t.Fatalf("first episode: %q", got)
	}
	auto := env.req(viewer12, episode)
	auto.AutoPlay = true
	if got := env.pick(auto); got != "General" {
		t.Fatalf("auto-play episode: %q", got)
	}
	env.activity.finished = env.now.Add(-10 * time.Minute)
	if got := env.pick(env.req(viewer12, episode)); got != "General" {
		t.Fatalf("episode 10 minutes after the last one: %q", got)
	}
	env.activity.finished = time.Time{}
	env.activity.stopped = env.now.Add(-45 * time.Minute)
	if got := env.pick(env.req(viewer12, episode)); got != "Show collection" {
		t.Fatalf("episode after the gap: %q", got)
	}

	// Selecting records nothing; a started play does, once, and the daily
	// frequency then holds the pre-roll back until the next local day.
	clip, ok := env.s.Pick(ctx, env.req(viewer12, episode))
	if !ok {
		t.Fatal("no pick")
	}
	if n := countPlays(t, env.s); n != 0 {
		t.Fatalf("selection recorded %d plays", n)
	}
	for range 3 {
		env.s.Started(bg, core.PrerollPlay{Viewer: viewer12, Key: clip.Key, Client: "Jellyfin Web"})
	}
	if n := countPlays(t, env.s); n != 1 {
		t.Fatalf("started recorded %d plays, want 1", n)
	}
	plays, err := env.s.Plays(ctx, mustID(t, env.s, "Show collection"), 10)
	if err != nil || len(plays) != 1 || plays[0].RatingKey != episode.Key.String() || plays[0].ViewerName != "ann" {
		t.Fatalf("play log %+v %v", plays, err)
	}
	if got := env.pick(env.req(viewer12, episode)); got != "General" {
		t.Fatalf("daily frequency same day: %q", got)
	}
	if got := env.pick(env.req(viewer15, episode)); got != "Show collection" {
		t.Fatalf("frequency is per viewer: %q", got)
	}
	env.now = env.now.Add(5 * time.Hour) // 01:00 the next day
	if got := env.pick(env.req(viewer12, episode)); got != "Show collection" {
		t.Fatalf("daily frequency next day: %q", got)
	}

	// The simulator explains every condition through the same rules.
	env.s.invalidate()
	all, _ := env.s.loadAll(ctx)
	dec := env.s.evaluate(ctx, env.req(viewer12, comedy), all, SelectPriority, true)
	if dec.Winner == nil || dec.Winner.Name != "General" {
		t.Fatalf("simulated winner %+v", dec.Winner)
	}
	want := map[string][]string{
		"Off":             {ReasonDisabled},
		"Kids":            {ReasonAudience},
		"Away TV":         {ReasonNetwork},
		"Horror":          {ReasonGenre, ReasonDateWindow},
		"Show collection": {ReasonKind, ReasonCollection},
		"General":         nil,
	}
	for _, c := range dec.Candidates {
		if got := reasonCodes(c); !slices.Equal(got, want[c.Name]) {
			t.Errorf("%s failed %v, want %v", c.Name, got, want[c.Name])
		}
		if c.Plays != (c.Name == "General") {
			t.Errorf("%s plays=%v", c.Name, c.Plays)
		}
	}
	// Every candidate lists its conditions in the one fixed order.
	order := []string{ReasonDisabled, ReasonNotReady, ReasonUnsupportedApp, ReasonResumed, ReasonKind, ReasonFirstEpisode, ReasonBingeGap,
		ReasonGenre, ReasonExcludedGenre, ReasonCollection, ReasonRating, ReasonDateWindow, ReasonAudience, ReasonNetwork, ReasonDevice,
		ReasonFrequency, ReasonLowerPriority, ReasonShuffledOut}
	for _, c := range dec.Candidates {
		last := -1
		for _, r := range c.Reasons {
			i := slices.Index(order, r.Code)
			if i <= last {
				t.Errorf("%s reasons out of order: %+v", c.Name, c.Reasons)
				break
			}
			last = i
		}
	}

	// Ties and shuffle: losers say why.
	env.add(Input{Name: "General 2", Priority: 0})
	all, _ = env.s.loadAll(ctx)
	dec = env.s.evaluate(ctx, env.req(viewer12, comedy), all, SelectPriority, true)
	winners := 0
	for _, c := range dec.Candidates {
		if c.Plays {
			winners++
		}
		if c.Name == "General 2" && !slices.Contains(reasonCodes(c), ReasonShuffledOut) && !slices.Contains(reasonCodes(c), ReasonLowerPriority) && !c.Plays {
			t.Errorf("tie loser lacks a reason: %+v", c.Reasons)
		}
	}
	if winners != 1 {
		t.Fatalf("%d winners", winners)
	}
	dec = env.s.evaluate(ctx, env.req(viewer15, comedy), all, SelectShuffle, true)
	for _, c := range dec.Candidates {
		if c.Plays || !slices.Contains(reasonCodes(c), ReasonShuffledOut) {
			continue
		}
		if c.Name != "Kids" && c.Name != "General" && c.Name != "General 2" {
			t.Errorf("non-matching %s shuffled out", c.Name)
		}
	}
}

func countPlays(t *testing.T, s *Service) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM prerolls_plays`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func mustID(t *testing.T, s *Service, name string) string {
	t.Helper()
	all, _ := s.loadAll(context.Background())
	for _, p := range all {
		if p.Name == name {
			return p.ID
		}
	}
	t.Fatalf("no pre-roll %q", name)
	return ""
}
