package catalog

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
)

func TestLexiconWordsJoinAndFold(t *testing.T) {
	got := map[string]string{}
	for _, w := range lexiconWords("Spider-Man: Far From Home — Amélie's S.W.A.T. (2019)") {
		got[w.key] = w.surface
	}
	want := map[string]string{
		"spiderman": "spider-man", "spider": "spider", "man": "man", "far": "far", "from": "from", "home": "home",
		"amelies": "amélie's", "amelie": "amélie", "swat": "s.w.a.t", "2019": "2019",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lexiconWords = %v, want %v", got, want)
	}
}

func TestEditDistanceCountsTranspositions(t *testing.T) {
	var scratch []int
	for _, tt := range []struct {
		a, b string
		max  int
		want int
	}{
		{"guilded", "gilded", 2, 1}, // insertion
		{"gildd", "gilded", 2, 1},   // deletion
		{"gidled", "gilded", 2, 1},  // transposition
		{"gelded", "gilded", 2, 1},  // substitution
		{"matirx", "matrix", 1, 1},  // transposition
		{"abcdef", "uvwxyz", 2, 3},  // gives up past max
		{"gilded", "gilded", 2, 0},
		{"amelie", "amelies", 1, 1},
	} {
		if got := editDistance([]rune(tt.a), []rune(tt.b), tt.max, &scratch); got != tt.want {
			t.Errorf("editDistance(%q, %q, %d) = %d, want %d", tt.a, tt.b, tt.max, got, tt.want)
		}
	}
}

func TestLexiconNearIsBoundedAndOrdered(t *testing.T) {
	b := newLexiconBuilder()
	b.addTitle("The Gilded Age")
	b.addTitle("The Gilded Cage")
	b.addTitle("Guided Tour")
	b.addTitle("Builder")
	lx := b.build()
	got := lx.near("guilded", 2, 4)
	var keys []string
	for _, m := range got {
		keys = append(keys, fmt.Sprintf("%s:%d", lx.key(m.idx), m.dist))
	}
	// gilded and guided are each one edit away; gilded is in more titles.
	if len(keys) < 2 || keys[0] != "gilded:1" || keys[1] != "guided:1" {
		t.Fatalf("near(guilded) = %v", keys)
	}
	if got := lx.near("gilded", 0, 4); got != nil {
		t.Fatalf("distance 0 should not search: %v", got)
	}
}

func TestSpellDistanceLeavesShortWordsAndNumbers(t *testing.T) {
	for word, want := range map[string]int{"it": 0, "up": 0, "age": 0, "1917": 0, "2049": 0, "gild": 1, "matirx": 1, "guilded": 2, "r2d2": 1} {
		if got := spellDistance(word); got != want {
			t.Errorf("spellDistance(%q) = %d, want %d", word, got, want)
		}
	}
}

func spellTestCatalog(t *testing.T) (*Catalog, context.Context) {
	t.Helper()
	c := newChunkTestCatalog(t)
	ctx := context.Background()
	c.SetNow(func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) })
	titles := []core.Title{
		{Key: core.ShowKey(1), Type: core.TypeShow, Title: "The Gilded Age", Year: 2022, Popularity: 50},
		{Key: core.MovieKey(2), Type: core.TypeMovie, Title: "Guided Missiles", Year: 2001, Popularity: 5},
		{Key: core.MovieKey(3), Type: core.TypeMovie, Title: "Amélie", OriginalTitle: "Le Fabuleux Destin d'Amélie Poulain", Year: 2001, Popularity: 40},
		{Key: core.MovieKey(4), Type: core.TypeMovie, Title: "Spider-Man", Year: 2002, Popularity: 80},
		{Key: core.MovieKey(5), Type: core.TypeMovie, Title: "Star Wars: Episode IV - A New Hope", AlternativeTitles: []string{"Star Wars"}, Year: 1977, Popularity: 90},
		{Key: core.MovieKey(6), Type: core.TypeMovie, Title: "The Matrix", Year: 1999, Popularity: 85},
		{Key: core.MovieKey(7), Type: core.TypeMovie, Title: "Schindler's List", Year: 1993, Popularity: 60},
		{Key: core.MovieKey(8), Type: core.TypeMovie, Title: "It", Year: 2017, Popularity: 70},
		{Key: core.MovieKey(9), Type: core.TypeMovie, Title: "Up", Year: 2009, Popularity: 75},
		{Key: core.MovieKey(10), Type: core.TypeMovie, Title: "1917", Year: 2019, Popularity: 65},
		{Key: core.ShowKey(11), Type: core.TypeShow, Title: "Shōgun", Year: 2024, Popularity: 55},
		{Key: core.MovieKey(12), Type: core.TypeMovie, Title: "Sousou no Frieren", AlternativeTitles: []string{"Frieren: Beyond Journey's End"}, Year: 2023, Popularity: 30},
		{Key: core.MovieKey(13), Type: core.TypeMovie, Title: "The Gilded Cage", Year: 2013, Popularity: 2},
	}
	for i := range titles {
		titles[i].Released = time.Date(titles[i].Year, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	if err := c.upsertMany(ctx, titles, upsertOpts{browsable: true}); err != nil {
		t.Fatal(err)
	}
	return c, ctx
}

func searchTitles(t *testing.T, c *Catalog, ctx context.Context, q string) []string {
	t.Helper()
	got, err := c.Search(ctx, q, nil, 10)
	if err != nil {
		t.Fatalf("Search(%q): %v", q, err)
	}
	out := make([]string, len(got))
	for i, g := range got {
		out[i] = g.Title
	}
	return out
}

func TestSearchRecoversMisspellings(t *testing.T) {
	c, ctx := spellTestCatalog(t)
	for _, tt := range []struct{ q, want string }{
		{"The Guilded Age", "The Gilded Age"}, // the owner's report: an inserted letter
		{"the gildd age", "The Gilded Age"},   // a missing letter
		{"the gidled age", "The Gilded Age"},  // swapped letters
		{"the gelded age", "The Gilded Age"},  // a wrong letter
		{"matirx", "The Matrix"},
		{"the matirx", "The Matrix"},
		{"amelie", "Amélie"},         // accent
		{"Amelie Poulain", "Amélie"}, // accent, original title
		{"spiderman", "Spider-Man"},  // hyphen
		{"spider man", "Spider-Man"}, // hyphen as a space
		{"starwars", "Star Wars: Episode IV - A New Hope"},
		{"star wars episode iv", "Star Wars: Episode IV - A New Hope"}, // punctuation
		{"new hope star wars", "Star Wars: Episode IV - A New Hope"},   // word order
		{"schindlers list", "Schindler's List"},                        // apostrophe
		{"shogun", "Shōgun"},
		{"freiren", "Sousou no Frieren"},           // alternative title words
		{"The Guilded Age 2022", "The Gilded Age"}, // with a year
	} {
		got := searchTitles(t, c, ctx, tt.q)
		if len(got) == 0 || got[0] != tt.want {
			t.Errorf("Search(%q) = %v, want %q first", tt.q, got, tt.want)
		}
	}
}

func TestSearchSpellingKeepsLiteralAndShortQueriesPrecise(t *testing.T) {
	c, ctx := spellTestCatalog(t)
	for _, tt := range []struct {
		q    string
		want []string
	}{
		{"The Gilded Age", []string{"The Gilded Age"}},
		{"Gilded", []string{"The Gilded Age", "The Gilded Cage"}},
		{"It", nil}, // checked below: It first
		{"Up", nil},
		{"1917", []string{"1917"}},
		{"1918", []string{}},
		{"Ip", []string{}},
		{"qwzxv", []string{}},
		{"Guided", []string{"Guided Missiles"}},
	} {
		got := searchTitles(t, c, ctx, tt.q)
		if tt.want == nil {
			if len(got) == 0 || got[0] != tt.q {
				t.Errorf("Search(%q) = %v, want %q first", tt.q, got, tt.q)
			}
			continue
		}
		if len(got) == 0 && len(tt.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Search(%q) = %v, want %v", tt.q, got, tt.want)
		}
	}
}

func TestSearchSpellingRanksLiteralBeforeCorrected(t *testing.T) {
	c, ctx := spellTestCatalog(t)
	// "Guilded Lily" holds the typed word literally: it stays first, and a
	// literal hit means a known word is not "corrected" away.
	extra := core.Title{Key: core.MovieKey(20), Type: core.TypeMovie, Title: "Guilded Lily", Year: 2020, Popularity: 1, Released: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}
	if err := c.upsertMany(ctx, []core.Title{extra}, upsertOpts{browsable: true}); err != nil {
		t.Fatal(err)
	}
	c.searchIx.ix.Store(nil) // rebuild with the new title
	got := searchTitles(t, c, ctx, "guilded")
	if len(got) == 0 || got[0] != "Guilded Lily" {
		t.Fatalf("Search(guilded) = %v, want the literal match first", got)
	}
}

func TestSearchIndexRefreshesAfterWrites(t *testing.T) {
	c, ctx := spellTestCatalog(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c.SetNow(func() time.Time { return now })
	if got := searchTitles(t, c, ctx, "The Guilded Age"); len(got) == 0 {
		t.Fatal("search index not built")
	}
	late := core.Title{Key: core.MovieKey(30), Type: core.TypeMovie, Title: "Oppenheimer", Year: 2023, Popularity: 9, Released: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)}
	if err := c.upsertMany(ctx, []core.Title{late}, upsertOpts{browsable: true}); err != nil {
		t.Fatal(err)
	}
	// Within the refresh interval the old index serves, and the new title is
	// searchable at once (misspelt too) from the titles written since.
	if got := searchTitles(t, c, ctx, "Openheimer"); len(got) == 0 || got[0] != "Oppenheimer" {
		t.Fatalf("fresh Search(Openheimer) = %v", got)
	}
	now = now.Add(searchIndexRefresh)
	c.searchIndex(ctx, false)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if ix := c.searchIx.ix.Load(); ix != nil && ix.lx.find("oppenheimer") >= 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("search index did not rebuild after a write")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := searchTitles(t, c, ctx, "Openheimer"); len(got) == 0 || got[0] != "Oppenheimer" {
		t.Fatalf("Search(Openheimer) = %v", got)
	}
	c.searchIx.freshMu.Lock()
	n := len(c.searchIx.fresh)
	c.searchIx.freshMu.Unlock()
	if n != 0 {
		t.Fatalf("%d fresh titles kept after the rebuild that holds them", n)
	}
}

func TestSearchSpellingTypeFilterAndUnfiltered(t *testing.T) {
	c, ctx := spellTestCatalog(t)
	got, err := c.Search(ctx, "The Guilded Age", []core.MediaType{core.TypeMovie}, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range got {
		if g.Type != core.TypeMovie {
			t.Fatalf("type filter leaked %v", g)
		}
	}
	all, err := c.SearchAll(ctx, "The Guilded Age", nil, 10)
	if err != nil || len(all) == 0 || all[0].Title != "The Gilded Age" {
		t.Fatalf("SearchAll = %v, %v", all, err)
	}
}
