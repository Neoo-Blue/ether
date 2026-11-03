package catalog_test

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/catalog"
	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/store"
)

// The drawer's schedule lists aired and announced episodes (Children never
// does), finds the last and next episode, and lists seasons with specials
// last and their aired counts.
func TestTitleShowScheduleListsAiredAndAnnouncedEpisodes(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 10, 15, 0, 0, 0, time.UTC)
	c, err := catalog.New(store.OpenTest(t), nil, config.Catalog{}, slog.New(slog.DiscardHandler), catalog.WithNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	show := core.Title{Key: core.ShowKey(700), Type: core.TypeShow, Title: "Harbor Lights", Released: day(1).AddDate(-3, 0, 0), Status: "Returning Series"}
	rows := []core.Title{show,
		{Key: core.SeasonKey(700, 0), Type: core.TypeSeason, ShowKey: show.Key, Season: 0, Title: "Specials", Released: day(1).AddDate(-2, 0, 0)},
		{Key: core.SeasonKey(700, 1), Type: core.TypeSeason, ShowKey: show.Key, Season: 1, Title: "Season 1", Released: day(1).AddDate(-1, 0, 0)},
		{Key: core.SeasonKey(700, 2), Type: core.TypeSeason, ShowKey: show.Key, Season: 2, Title: "Season 2", Released: day(3)},
		{Key: core.EpisodeKey(700, 1, 1), Type: core.TypeEpisode, ShowKey: show.Key, Season: 1, Episode: 1, Title: "Old", Released: day(1).AddDate(-1, 0, 0)},
		{Key: core.EpisodeKey(700, 0, 1), Type: core.TypeEpisode, ShowKey: show.Key, Season: 0, Episode: 1, Title: "Special", Released: day(5)},
	}
	for i, d := range []time.Time{day(3), day(10), day(17), day(24), {}} {
		rows = append(rows, core.Title{Key: core.EpisodeKey(700, 2, i+1), Type: core.TypeEpisode, ShowKey: show.Key, Season: 2, Episode: i + 1, Title: fmt.Sprintf("Part %d", i+1), Released: d})
	}
	for _, r := range rows {
		if err := c.UpsertTitle(ctx, r, r.Type == core.TypeShow); err != nil {
			t.Fatal(err)
		}
	}
	got, err := c.TitleShowSchedule(ctx, show.Key, day(1), 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Last == nil || got.Last.Season != 2 || got.Last.Episode != 2 {
		t.Fatalf("last: %+v", got.Last)
	}
	if got.Next == nil || got.Next.Episode != 3 || !got.Next.Released.Equal(day(17)) {
		t.Fatalf("next: %+v", got.Next)
	}
	var order []string
	for _, e := range got.Window {
		order = append(order, fmt.Sprintf("s%de%d", e.Season, e.Episode))
	}
	// day(10) is "today" at 15:00 UTC but airs at 00:00, so it has aired.
	if fmt.Sprint(order) != "[s2e1 s2e2 s2e3 s2e4 s2e5]" {
		t.Fatalf("window: %v", order)
	}
	if !got.Window[4].Released.IsZero() {
		t.Fatalf("undated episode got a date: %+v", got.Window[4])
	}
	if len(got.Seasons) != 3 || got.Seasons[0].Season != 1 || got.Seasons[2].Season != 0 {
		t.Fatalf("seasons: %+v", got.Seasons)
	}
	if s2 := got.Seasons[1]; s2.Episodes != 5 || s2.Aired != 2 {
		t.Fatalf("season 2 counts: %+v", s2)
	}

	eps, err := c.TitleSeasonSchedule(ctx, show.Key, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 5 || eps[0].Title != "Part 1" || eps[4].Episode != 5 {
		t.Fatalf("season 2: %+v", eps)
	}
	if _, err := c.TitleShowSchedule(ctx, core.SeasonKey(700, 2), day(1), 0); err == nil {
		t.Fatal("a season key is not a show")
	}
}
