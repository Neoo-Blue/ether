package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/store"
	"github.com/braedonsaunders/ether/internal/tmdb"
)

type releaseClient struct {
	TMDB
	details map[int64]*tmdb.MovieDetails
	calls   int
}

func (f *releaseClient) MovieDetails(_ context.Context, id int64) (*tmdb.MovieDetails, error) {
	f.calls++
	if d := f.details[id]; d != nil {
		return d, nil
	}
	return nil, errors.New("offline")
}
func (f *releaseClient) MovieDetailsFresh(ctx context.Context, id int64) (*tmdb.MovieDetails, error) {
	return f.MovieDetails(ctx, id)
}
func (f *releaseClient) SearchMovies(_ context.Context, _ string, _ int) (*tmdb.Paged[tmdb.Movie], error) {
	return &tmdb.Paged[tmdb.Movie]{Results: []tmdb.Movie{f.details[1].Movie, f.details[3].Movie}}, nil
}
func (f *releaseClient) MovieList(_ context.Context, _ string, _ int) (*tmdb.Paged[tmdb.Movie], error) {
	return &tmdb.Paged[tmdb.Movie]{Results: []tmdb.Movie{f.details[1].Movie, f.details[3].Movie}, TotalPages: 1}, nil
}
func (f *releaseClient) CollectionDetails(_ context.Context, _ int64) (*tmdb.CollectionDetails, error) {
	return &tmdb.CollectionDetails{ID: 10, Name: "Series", Parts: []tmdb.Movie{f.details[1].Movie, f.details[3].Movie}}, nil
}
func releaseDetail(t *testing.T, text string) *tmdb.MovieDetails {
	t.Helper()
	var d tmdb.MovieDetails
	if err := json.Unmarshal([]byte(text), &d); err != nil {
		t.Fatal(err)
	}
	return &d
}
func TestHomeReleaseGateBackfillAndCorrections(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2025, 4, 1, 12, 0, 0, 0, time.UTC)
	client := &releaseClient{details: map[int64]*tmdb.MovieDetails{
		1: releaseDetail(t, `{"id":1,"title":"A","release_date":"2025-01-01","release_dates":{"results":[{"iso_3166_1":"US","release_dates":[{"type":5,"release_date":"2025-07-01T00:00:00Z"}]},{"iso_3166_1":"CA","release_dates":[{"type":4,"release_date":"2025-04-01T00:00:00Z"}]}]}}`),
		2: releaseDetail(t, `{"id":2,"title":"B","release_date":"2025-01-01","release_dates":{"results":[]}}`),
		3: releaseDetail(t, `{"id":3,"title":"C","release_date":"2025-01-01","release_dates":{"results":[{"iso_3166_1":"US","release_dates":[{"type":4,"release_date":"2025-05-01T00:00:00Z"}]}]}}`),
		4: releaseDetail(t, `{"id":4,"title":"D","release_date":"2025-01-01"}`),
	}}
	c, err := New(db, client, config.Catalog{}, nil, WithNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	for id := int64(1); id <= 4; id++ {
		if err = c.UpsertTitle(ctx, core.Title{Key: core.MovieKey(id), Type: core.TypeMovie, IDs: core.IDs{TMDB: id}, Title: "Movie", Released: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}, true); err != nil {
			t.Fatal(err)
		}
	}
	page, err := c.Browse(ctx, core.BrowseQuery{})
	if err != nil || page.Total != 4 {
		t.Fatalf("default off: %+v %v", page, err)
	}
	c.UpdateConfig(config.Catalog{ReleaseMode: config.ModeDigital})
	page, err = c.Browse(ctx, core.BrowseQuery{})
	if err != nil || page.Total != 0 {
		t.Fatalf("unknown: %+v %v", page, err)
	}
	if client.calls != 0 {
		t.Fatal("browse fetched details")
	}
	if n, err := c.BackfillHomeReleases(ctx, 2); err != nil || n != 2 {
		t.Fatalf("bounded: %d %v", n, err)
	}
	if n, err := c.BackfillHomeReleases(ctx, 2); err != nil || n != 2 {
		t.Fatalf("remaining: %d %v", n, err)
	}
	page, err = c.Browse(ctx, core.BrowseQuery{Limit: 1})
	if err != nil || page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("page: %+v %v", page, err)
	}
	page2, err := c.Browse(ctx, core.BrowseQuery{Limit: 1, Offset: 1})
	if err != nil || page2.Total != 1 || len(page2.Items) != 0 {
		t.Fatalf("page2: %+v %v", page2, err)
	}
	for id := int64(1); id <= 4; id++ {
		got, err := c.Get(ctx, core.MovieKey(id))
		if id == 1 {
			if err != nil || got.Year != 2025 {
				t.Fatalf("visible %d: %+v %v", id, got, err)
			}
		} else if !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("hidden %d: %+v %v", id, got, err)
		}
	}
	summary := []core.Title{{Key: core.MovieKey(1), Type: core.TypeMovie}, {Key: core.MovieKey(3), Type: core.TypeMovie}, {Key: core.MovieKey(999), Type: core.TypeMovie}}
	if got := c.FilterVisibility(ctx, summary); len(got) != 1 || got[0].Key != core.MovieKey(1) {
		t.Fatalf("remote summaries: %+v", got)
	}
	duplicates := []core.Title{{Key: core.MovieKey(1), Type: core.TypeMovie}, {Key: core.MovieKey(1), Type: core.TypeMovie}}
	if err := c.hydrateHomeRelease(ctx, duplicates); err != nil {
		t.Fatal(err)
	}
	for _, title := range duplicates {
		if !title.HomeReleaseChecked || title.HomeReleased.IsZero() {
			t.Fatalf("duplicate summary lost release facts: %+v", title)
		}
	}
	// Distinct missing keys cross query batches without hiding or duplicating
	// the repeated visible title at either end of the input.
	many := []core.Title{{Key: core.MovieKey(1), Type: core.TypeMovie}}
	for id := int64(1000); id < 1500; id++ {
		many = append(many, core.Title{Key: core.MovieKey(id), Type: core.TypeMovie})
	}
	many = append(many, core.Title{Key: core.MovieKey(1), Type: core.TypeMovie})
	if err := c.hydrateHomeRelease(ctx, many); err != nil {
		t.Fatal(err)
	}
	if !many[0].HomeReleaseChecked || !many[len(many)-1].HomeReleaseChecked || many[1].HomeReleaseChecked {
		t.Fatal("batched summaries lost or invented release facts")
	}
	if err = c.UpsertTitle(ctx, core.Title{Key: core.MovieKey(1), Type: core.TypeMovie, IDs: core.IDs{TMDB: 1}, Title: "Summary", Released: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}, true); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Get(ctx, core.MovieKey(1)); err != nil {
		t.Fatalf("summary erased evidence: %v", err)
	}
	beforeCalls := client.calls
	search, err := c.Search(ctx, "A", []core.MediaType{core.TypeMovie}, 20)
	if err != nil || len(search) != 1 || search[0].Key != core.MovieKey(1) {
		t.Fatalf("search: %+v %v", search, err)
	}
	_, collection, err := c.Collection(ctx, 10)
	if err != nil || len(collection) != 1 || collection[0].Key != core.MovieKey(1) {
		t.Fatalf("collection: %+v %v", collection, err)
	}
	list, err := c.List(ctx, "popular", core.TypeMovie, 20)
	if err != nil || len(list) != 1 || list[0].Key != core.MovieKey(1) {
		t.Fatalf("home list: %+v %v", list, err)
	}
	cached, err := c.CachedTitles(ctx, []core.RatingKey{core.MovieKey(1), core.MovieKey(3)})
	if err != nil || len(cached) != 1 || cached[0].Key != core.MovieKey(1) {
		t.Fatalf("cached: %+v %v", cached, err)
	}
	if client.calls != beforeCalls {
		t.Fatal("browse paths fetched per-title details")
	}
	// Age and historical fallback admission never replace a known home date.
	if _, err := db.ExecContext(ctx, `UPDATE catalog_home_release SET fallback_admitted=1 WHERE key=?`, int64(core.MovieKey(2))); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(ctx, core.MovieKey(2)); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("unknown date admitted: %v", err)
	}
	// Corrected physical dates can admit a title, but future dates cannot.
	client.details[2] = releaseDetail(t, `{"id":2,"title":"B","release_date":"2025-01-01","release_dates":{"results":[{"iso_3166_1":"GB","release_dates":[{"type":4,"release_date":"2025-08-01T00:00:00Z"}]}]}}`)
	client.details[3] = releaseDetail(t, `{"id":3,"title":"C","release_date":"2025-01-01","release_dates":{"results":[{"iso_3166_1":"JP","release_dates":[{"type":5,"release_date":"2025-04-01T00:00:00Z"}]}]}}`)
	now = now.Add(25 * time.Hour)
	if _, err = c.BackfillHomeReleases(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Get(ctx, core.MovieKey(3)); err != nil {
		t.Fatalf("corrected future date: %v", err)
	}
	c, err = New(db, client, config.Catalog{ReleaseMode: config.ModeDigital}, nil, WithNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := c.Get(ctx, core.MovieKey(2)); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("future date admitted after restart: %+v %v", got, err)
	}
	c.UpdateConfig(config.Catalog{})
	page, err = c.Browse(ctx, core.BrowseQuery{})
	if err != nil || page.Total != 4 {
		t.Fatalf("disabled: %+v %v", page, err)
	}
}

func TestHomeReleaseBoundary(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := core.Title{Type: core.TypeMovie, Released: start, HomeReleaseChecked: true}
	if homeReleaseVisible(&t1, start.Add(120*24*time.Hour)) || !homeReleaseVisible(&t1, start.Add(120*24*time.Hour+time.Second)) {
		t.Fatal("cinema age boundary")
	}
	t1.HomeReleased = start.Add(100 * 24 * time.Hour)
	if homeReleaseVisible(&t1, start.Add(90*24*time.Hour)) || !homeReleaseVisible(&t1, t1.HomeReleased) {
		t.Fatal("dated boundary")
	}
}

func TestCachedGetManyDoesNotFetchOrAdmitFallback(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2025, 4, 1, 12, 0, 0, 0, time.UTC)
	client := &releaseClient{details: map[int64]*tmdb.MovieDetails{}}
	c, err := New(db, client, config.Catalog{ReleaseMode: config.ModeDigital}, nil, WithNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	movie := core.Title{Key: core.MovieKey(44), Type: core.TypeMovie, IDs: core.IDs{TMDB: 44}, Released: now.AddDate(-1, 0, 0)}
	if err := c.UpsertTitle(ctx, movie, true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO catalog_home_release(key,checked,earliest_ts,fallback_admitted,attempted_at) VALUES(?,1,0,0,0)`, int64(movie.Key)); err != nil {
		t.Fatal(err)
	}
	before := movie
	before.Key, before.IDs.TMDB, before.Released = core.MovieKey(45), 45, now.Add(-89*24*time.Hour)
	known := movie
	known.Key, known.IDs.TMDB = core.MovieKey(46), 46
	unknown := movie
	unknown.Key, unknown.IDs.TMDB = core.MovieKey(47), 47
	for _, title := range []core.Title{before, known, unknown} {
		if err := c.UpsertTitle(ctx, title, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO catalog_home_release(key,checked,earliest_ts,fallback_admitted,attempted_at) VALUES(?,1,0,0,0),(?,1,?,0,0)`, int64(before.Key), int64(known.Key), now.Add(-24*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	got, err := c.CachedGetMany(ctx, []core.RatingKey{movie.Key, before.Key, known.Key, unknown.Key, core.MovieKey(999)})
	if err != nil || len(got) != 3 || got[known.Key].Key != known.Key || got[movie.Key].Key != movie.Key || got[unknown.Key].Key != unknown.Key {
		t.Fatalf("eligibility: %v %v", got, err)
	}
	var admitted int
	if err := db.QueryRowContext(ctx, `SELECT fallback_admitted FROM catalog_home_release WHERE key=?`, int64(movie.Key)).Scan(&admitted); err != nil || admitted != 0 {
		t.Fatalf("fallback write: %d %v", admitted, err)
	}
	if client.calls != 0 {
		t.Fatalf("remote calls: %d", client.calls)
	}
}

func TestDigitalAgeAdmissionBrowseSearchGet(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2025, 8, 1, 0, 0, 0, 0, time.UTC)
	c, err := New(db, nil, config.Catalog{ReleaseMode: config.ModeDigital}, nil, WithNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	for i, age := range []time.Duration{121 * 24 * time.Hour, 100 * 24 * time.Hour, 100 * 24 * time.Hour} {
		title := core.Title{Key: core.MovieKey(int64(i + 701)), Type: core.TypeMovie, Title: "Admission test", Released: now.Add(-age)}
		if err := c.UpsertTitle(ctx, title, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO catalog_home_release(key,checked,earliest_ts) VALUES(?,1,?)`, int64(core.MovieKey(703)), now.Add(24*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	check := func(want int, visible map[int64]bool) {
		t.Helper()
		page, err := c.Browse(ctx, core.BrowseQuery{Limit: 20})
		if err != nil || page.Total != want || len(page.Items) != want {
			t.Fatalf("browse: %+v %v", page, err)
		}
		found, err := c.Search(ctx, "Admission test", []core.MediaType{core.TypeMovie}, 20)
		if err != nil || len(found) != want {
			t.Fatalf("search: %+v %v", found, err)
		}
		for id := int64(701); id <= 703; id++ {
			_, err := c.Get(ctx, core.MovieKey(id))
			if (err == nil) != visible[id] {
				t.Fatalf("get %d: %v", id, err)
			}
		}
	}
	check(1, map[int64]bool{701: true})
	if _, err := db.ExecContext(ctx, `INSERT INTO catalog_home_release(key,checked,earliest_ts) VALUES(?,1,?)`, int64(core.MovieKey(702)), now.Unix()); err != nil {
		t.Fatal(err)
	}
	c, err = New(db, nil, config.Catalog{ReleaseMode: config.ModeDigital}, nil, WithNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	check(2, map[int64]bool{701: true, 702: true})
	for _, mode := range []string{config.ModePlayable, config.ModeImmediate} {
		c.UpdateConfig(config.Catalog{ReleaseMode: mode})
		check(3, map[int64]bool{701: true, 702: true, 703: true})
	}
}
