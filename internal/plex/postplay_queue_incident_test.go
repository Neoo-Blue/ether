package plex

import (
	"context"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
)

func TestContinuousQueueSkipsSpecialsFutureAndUnavailable(t *testing.T) {
	f := newFixture()
	defer f.srv.Close()
	special := bbE1
	special.Key = core.EpisodeKey(breakingBad.Key.TMDB(), 0, 1)
	special.Season = 0
	future := bbE1
	future.Key = core.EpisodeKey(breakingBad.Key.TMDB(), 2, 1)
	future.Season = 2
	future.Released = time.Now().Add(48 * time.Hour)
	unavailable := bbE1
	unavailable.Key = core.EpisodeKey(breakingBad.Key.TMDB(), 2, 2)
	unavailable.Season = 2
	f.catalog.titles[special.Key] = special
	f.catalog.titles[future.Key] = future
	f.catalog.titles[unavailable.Key] = unavailable
	f.catalog.children[breakingBad.Key] = []core.RatingKey{bbS1.Key, core.SeasonKey(breakingBad.Key.TMDB(), 2), core.SeasonKey(breakingBad.Key.TMDB(), 0)}
	f.catalog.children[core.SeasonKey(breakingBad.Key.TMDB(), 2)] = []core.RatingKey{future.Key, unavailable.Key}
	f.catalog.children[core.SeasonKey(breakingBad.Key.TMDB(), 0)] = []core.RatingKey{special.Key}
	f.srv.d.FilterAvailable = func(_ context.Context, titles []core.Title) []core.Title {
		var out []core.Title
		for _, title := range titles {
			if title.Key != unavailable.Key {
				out = append(out, title)
			}
		}
		return out
	}
	keys, err := f.srv.continuousKeys(context.Background(), &Request{User: &ownerUser}, "/library/metadata/"+bbE2.Key.String(), true)
	if err != nil || len(keys) != 1 || keys[0] != bbE2.Key {
		t.Fatalf("got %v, %v", keys, err)
	}
}
