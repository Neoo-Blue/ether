package catalog

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/store"
)

func TestPersonByTMDB(t *testing.T) {
	ctx := context.Background()
	db := store.OpenTest(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c, err := New(db, nil, config.Catalog{}, nil, WithNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	upsert := func(title core.Title) {
		t.Helper()
		if err := c.UpsertTitle(ctx, title, true); err != nil {
			t.Fatal(err)
		}
	}
	// Person 7 acts (with portrait) and directs; person 9 only directs,
	// with the portrait on a second title.
	upsert(core.Title{Key: core.MovieKey(1), Type: core.TypeMovie, IDs: core.IDs{TMDB: 1}, Title: "Film", Released: now.AddDate(-1, 0, 0),
		Cast:      []core.Person{{TMDB: 7, Name: "Ringo Starr", ThumbPath: "/ringo.jpg"}},
		Directors: []core.Person{{TMDB: 9, Name: "Dee Rector"}}})
	upsert(core.Title{Key: core.MovieKey(2), Type: core.TypeMovie, IDs: core.IDs{TMDB: 2}, Title: "Film Two", Released: now.AddDate(-1, 0, 0),
		Directors: []core.Person{{TMDB: 7, Name: "Ringo Starr", ThumbPath: "/ringo-dir.jpg"}, {TMDB: 9, Name: "Dee Rector", ThumbPath: "/dee.jpg"}}})

	person, kinds, thumbKey, err := c.PersonByTMDB(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if person.Name != "Ringo Starr" || strings.Join(kinds, ",") != "cast,director" {
		t.Fatalf("person 7 = %+v kinds %v", person, kinds)
	}
	// The portrait comes from the primary (cast) credit, on its title.
	if person.ThumbPath != "/ringo.jpg" || thumbKey != core.MovieKey(1) {
		t.Fatalf("person 7 thumb = %q on %v, want /ringo.jpg on movie 1", person.ThumbPath, thumbKey)
	}

	person, kinds, thumbKey, err = c.PersonByTMDB(ctx, 9)
	if err != nil {
		t.Fatal(err)
	}
	if person.Name != "Dee Rector" || strings.Join(kinds, ",") != "director" || person.ThumbPath != "/dee.jpg" || thumbKey != core.MovieKey(2) {
		t.Fatalf("person 9 = %+v kinds %v thumbKey %v", person, kinds, thumbKey)
	}

	if _, _, _, err := c.PersonByTMDB(ctx, 404404); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("unknown person err = %v, want ErrNotFound", err)
	}
	if _, _, _, err := c.PersonByTMDB(ctx, 0); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("person 0 err = %v, want ErrNotFound", err)
	}
}
