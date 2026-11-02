package admin

import (
	"testing"

	"github.com/braedonsaunders/ether/internal/config"
)

func TestSubtitleProviderValidation(t *testing.T) {
	cfg := config.Defaults(t.TempDir())
	cfg.Subtitles.Providers = []config.SubtitleProvider{{ID: "s", Kind: "stremio", URL: "https://example.test/key/manifest.json", Enabled: true}}
	for _, e := range ValidateConfig(cfg) {
		if e.Field == "subtitles.providers[0].url" {
			t.Fatalf("valid URL rejected: %v", e)
		}
	}
	cfg.Subtitles.Providers[0].URL = "file:///tmp/manifest.json"
	found := false
	for _, e := range ValidateConfig(cfg) {
		if e.Field == "subtitles.providers[0].url" {
			found = true
		}
	}
	if !found {
		t.Fatal("invalid add-on URL accepted")
	}
}
