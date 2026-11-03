package admin

import (
	"strings"
	"testing"

	"github.com/braedonsaunders/ether/internal/config"
)

func TestStremioIndexerURLRedactionRoundTrip(t *testing.T) {
	full := "https://addon.example.com/eyJkZWJyaWRLZXkiOiJzZWNyZXQifQ/manifest.json"
	prev := config.Config{Indexers: []config.Indexer{
		{ID: "s1", Name: "Addon", Kind: config.IndexerKindStremio, URL: full, Enabled: true},
		{ID: "n1", Name: "News", URL: "https://idx.example.com/api", APIKey: "k1", Enabled: true},
	}}
	red := RedactConfig(prev)
	if strings.Contains(red.Indexers[0].URL, "eyJkZWJyaWQ") || strings.Contains(red.Indexers[0].URL, "manifest") {
		t.Fatalf("redacted URL leaks path: %q", red.Indexers[0].URL)
	}
	if red.Indexers[0].URL != "https://addon.example.com (saved)" {
		t.Fatalf("redacted URL = %q", red.Indexers[0].URL)
	}
	if red.Indexers[1].URL != "https://idx.example.com/api" {
		t.Fatalf("newznab URL must stay visible, got %q", red.Indexers[1].URL)
	}
	// Round trip: the redacted config sent back restores the full URL.
	out, errs := UnredactConfig(red, prev)
	if len(errs) != 0 {
		t.Fatalf("unredact errors: %v", errs)
	}
	if out.Indexers[0].URL != full {
		t.Fatalf("restored URL = %q", out.Indexers[0].URL)
	}
	// A redacted marker moved to a different host must not restore.
	moved := red
	moved.Indexers = append([]config.Indexer(nil), red.Indexers...)
	moved.Indexers[0].URL = "https://evil.example.com (saved)"
	out, errs = UnredactConfig(moved, prev)
	if out.Indexers[0].URL != "" || len(errs) == 0 {
		t.Fatalf("moved host restored: %q errs=%v", out.Indexers[0].URL, errs)
	}
	// A new entry carrying only the marker gets nothing.
	fresh := config.Config{Indexers: []config.Indexer{{ID: "s2", Kind: config.IndexerKindStremio, URL: "https://addon.example.com (saved)"}}}
	out, errs = UnredactConfig(fresh, prev)
	if out.Indexers[0].URL != "" || len(errs) == 0 {
		t.Fatalf("new entry restored: %q errs=%v", out.Indexers[0].URL, errs)
	}
	// A freshly pasted full URL is kept as-is.
	pasted := red
	pasted.Indexers = append([]config.Indexer(nil), red.Indexers...)
	pasted.Indexers[0].URL = "https://addon.example.com/newkey/manifest.json"
	out, _ = UnredactConfig(pasted, prev)
	if out.Indexers[0].URL != pasted.Indexers[0].URL {
		t.Fatalf("pasted URL changed: %q", out.Indexers[0].URL)
	}
}
