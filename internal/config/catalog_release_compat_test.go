package config

import (
	"encoding/json"
	"testing"
)

// Legacy combined playable/date gating remains unchanged until the owner
// explicitly chooses a replacement release mode in the console.
func TestCatalogReleaseCompatibility(t *testing.T) {
	for _, gate := range []bool{false, true} {
		cfg := Defaults(t.TempDir())
		raw := []byte(`{"catalog":{"gateMoviesByHomeRelease":false,"showUpcomingEpisodes":true},"availability":{"mode":"playable"}}`)
		if gate {
			raw = []byte(`{"catalog":{"gateMoviesByHomeRelease":true,"showUpcomingEpisodes":true},"availability":{"mode":"playable"}}`)
		}
		if err := json.Unmarshal(raw, &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg.Catalog.GateMoviesByHomeRelease != gate || !cfg.Catalog.ShowUpcomingEpisodes || cfg.Catalog.ShowUpcomingMovies {
			t.Fatalf("legacy settings changed: %+v", cfg.Catalog)
		}
		if cfg.Availability.EffectiveMode() != ModePlayable {
			t.Fatal("release mode changed")
		}
	}
	cfg := Defaults(t.TempDir())
	if cfg.Catalog.ShowUpcomingEpisodes || cfg.Catalog.ShowUpcomingMovies {
		t.Fatal("upcoming content must be opt-in")
	}
}
