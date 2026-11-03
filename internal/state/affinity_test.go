package state

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
)

func TestFamiliarLevelsMoviesAndShows(t *testing.T) {
	s, _ := newTestState(t)
	ctx := context.Background()
	mine, theirs, untouched := core.MovieKey(10), core.MovieKey(11), core.MovieKey(12)
	myShow, theirShow, otherShow := core.ShowKey(20), core.ShowKey(21), core.ShowKey(22)
	if err := s.Scrobble(ctx, 1, mine); err != nil {
		t.Fatal(err)
	}
	if err := s.Scrobble(ctx, 2, theirs); err != nil {
		t.Fatal(err)
	}
	// Started, not finished, counts too; so does another user's episode.
	if err := s.Progress(ctx, 1, core.EpisodeKey(20, 2, 3), 5*time.Minute, 40*time.Minute, "paused"); err != nil {
		t.Fatal(err)
	}
	if err := s.Scrobble(ctx, 2, core.EpisodeKey(21, 1, 1)); err != nil {
		t.Fatal(err)
	}
	// A neighbouring show's episode must not leak into show 22's range.
	if err := s.Scrobble(ctx, 2, core.EpisodeKey(23, 1, 1)); err != nil {
		t.Fatal(err)
	}
	got, err := s.Familiar(ctx, 1, []core.RatingKey{mine, theirs, untouched, myShow, theirShow, otherShow})
	if err != nil {
		t.Fatal(err)
	}
	want := map[core.RatingKey]int{mine: core.AffinityViewer, theirs: core.AffinityServer, myShow: core.AffinityViewer, theirShow: core.AffinityServer}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Familiar = %v, want %v", got, want)
	}
	if got, err := s.Familiar(ctx, 1, nil); err != nil || len(got) != 0 {
		t.Fatalf("no keys = %v, %v", got, err)
	}
}
