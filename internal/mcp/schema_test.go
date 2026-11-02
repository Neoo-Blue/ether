package mcp

import (
	"github.com/braedonsaunders/ether/schemas"
	"testing"
)

func TestManifestResourceUsesCanonicalSchema(t *testing.T) {
	for _, resource := range DefaultResources("") {
		if resource.URI == "ether://schemas/plugin-manifest" {
			if resource.Text != schemas.PluginManifest {
				t.Fatal("manifest resource differs from canonical schema")
			}
			return
		}
	}
	t.Fatal("manifest resource missing")
}
