package stremio

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/braedonsaunders/ether/internal/core"
)

type fakeAnime struct {
	mu    sync.Mutex
	calls []string
	ids   map[string][2]int64 // db -> id, episode
	err   error
}

func (f *fakeAnime) Lookup(_ context.Context, _ core.SearchRequest, db string) (int64, int, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, db)
	if f.err != nil {
		return 0, 0, false, f.err
	}
	v, ok := f.ids[db]
	return v[0], int(v[1]), ok, nil
}

func manifestWithPrefixes(types string, prefixes string) Manifest {
	m, err := DecodeManifest([]byte(`{"id":"x","resources":[{"name":"stream","types":` + types + `,"idPrefixes":` + prefixes + `}]}`))
	if err != nil {
		panic(err)
	}
	return m
}

func TestLookupStreamsAnimeOnlyAddonUsesMappedEntryEpisode(t *testing.T) {
	m := manifestWithPrefixes(`["series","movie"]`, `["kitsu:"]`)
	anime := &fakeAnime{ids: map[string][2]int64{"kitsu": {41982, 3}}}
	look, ok := LookupStreams(t.Context(), m, animeRequest(), anime)
	if !ok || look.Type != TypeSeries || look.ID != "kitsu:41982:3" || look.EntryEpisode != 3 || look.Packs {
		t.Fatalf("lookup %+v ok=%v", look, ok)
	}
	if look.Route() != "/stream/series/kitsu:41982:3.json" {
		t.Fatalf("route %q", look.Route())
	}
}

func TestLookupStreamsAnimeMappingOnlyConsultedWhenNeeded(t *testing.T) {
	m := manifestWithPrefixes(`["series","movie"]`, `["kitsu:"]`)
	anime := &fakeAnime{ids: map[string][2]int64{"kitsu": {41982, 3}}}
	notAnime := animeRequest()
	notAnime.Anime = false
	season := animeRequest()
	season.Episode = 0
	for _, req := range []core.SearchRequest{notAnime, season} {
		if look, ok := LookupStreams(t.Context(), m, req, anime); ok {
			t.Fatalf("asked the add-on: %+v", look)
		}
	}
	if len(anime.calls) != 0 {
		t.Fatalf("mapping loaded for a non-anime or whole-season request: %v", anime.calls)
	}
	// No mapping service at all: skipped quietly.
	if _, ok := LookupStreams(t.Context(), m, animeRequest(), nil); ok {
		t.Fatal("asked without an anime mapping")
	}
	// An unavailable or ambiguous mapping skips the add-on quietly.
	anime.err = errors.New("unavailable")
	if look, ok := LookupStreams(t.Context(), m, animeRequest(), anime); ok {
		t.Fatalf("unavailable mapping: %+v", look)
	}
	anime.err, anime.ids = nil, nil
	if look, ok := LookupStreams(t.Context(), m, animeRequest(), anime); ok {
		t.Fatalf("unmapped title: %+v", look)
	}
}

func TestLookupStreamsPrefersIMDbThenTMDBThenTVDBThenAnime(t *testing.T) {
	for _, tc := range []struct {
		types, prefixes string
		req             core.SearchRequest
		want            string
	}{
		{`["movie","series"]`, `["tt","tmdb:","kitsu:"]`, animeRequest(), "/stream/series/tt2560140:3:15.json"},
		{`["movie","series"]`, `["tmdb:","kitsu:"]`, animeRequest(), "/stream/series/tmdb:1429:3:15.json"},
		{`["movie"]`, `["tt","tmdb"]`, core.SearchRequest{Type: core.TypeMovie, IDs: core.IDs{TMDB: 603}, Title: "M"}, "/stream/movie/tmdb:603.json"},
		{`["series"]`, `["tvdb:"]`, core.SearchRequest{Type: core.TypeEpisode, IDs: core.IDs{TVDB: 81189}, Title: "S", Season: 2, Episode: 4}, "/stream/series/tvdb:81189:2:4.json"},
		{`["movie"]`, `["kitsu"]`, core.SearchRequest{Type: core.TypeMovie, IDs: core.IDs{TMDB: 5000}, Title: "Film", Anime: true}, "/stream/movie/kitsu:108.json"},
	} {
		anime := &fakeAnime{ids: map[string][2]int64{"kitsu": {108, 0}}}
		look, ok := LookupStreams(t.Context(), manifestWithPrefixes(tc.types, tc.prefixes), tc.req, anime)
		if !ok || look.Route() != tc.want {
			t.Errorf("prefixes %s: got %q ok=%v, want %s", tc.prefixes, look.Route(), ok, tc.want)
		}
	}
}

func TestLookupStreamsSeasonRequestAsksFirstEpisodeForPacks(t *testing.T) {
	m := manifestWithPrefixes(`["series"]`, `["tt"]`)
	look, ok := LookupStreams(t.Context(), m, core.SearchRequest{Type: core.TypeEpisode, IDs: core.IDs{IMDB: "0903747"}, Title: "Show", Season: 1}, nil)
	if !ok || look.ID != "tt0903747:1:1" || !look.Packs {
		t.Fatalf("lookup %+v ok=%v", look, ok)
	}
}

func TestLookupStreamsAsksNothingWithoutAUsableID(t *testing.T) {
	m := manifestWithPrefixes(`["movie","series"]`, `["tt"]`)
	for _, req := range []core.SearchRequest{
		{Type: core.TypeMovie, Title: "No IDs"},
		{Type: core.TypeMovie, IDs: core.IDs{IMDB: "not-an-id"}},
		{Type: core.TypeShow, IDs: core.IDs{IMDB: "tt1"}},
		{Type: core.TypeEpisode, IDs: core.IDs{IMDB: "tt1"}}, // no season
		{Type: core.TypeMovie, IDs: core.IDs{TMDB: 603}},     // add-on takes only IMDb
	} {
		if look, ok := LookupStreams(t.Context(), m, req, nil); ok {
			t.Errorf("%+v asked: %+v", req, look)
		}
	}
	// An add-on that serves only movies is not asked about episodes.
	movies := manifestWithPrefixes(`["movie"]`, `["tt"]`)
	if _, ok := LookupStreams(t.Context(), movies, core.SearchRequest{Type: core.TypeEpisode, IDs: core.IDs{IMDB: "tt1"}, Season: 1, Episode: 1}, nil); ok {
		t.Fatal("movie-only add-on asked for an episode")
	}
}
