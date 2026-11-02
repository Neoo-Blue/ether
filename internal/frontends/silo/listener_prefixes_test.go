package silo

import "testing"

// On the shared main port Silo is reachable under /silo and, so a client
// given the bare main address works, at the root /api/v2 (and /api/v1 for
// the health probe). These prefixes are what the app's shared router keys
// on; the first shared prefix is the canonical connect address.
func TestSiloDeclaresMainPortRootPrefixes(t *testing.T) {
	i := &instance{}
	var shared []string
	dedicated := 0
	for _, spec := range i.Listeners() {
		if spec.Shared {
			shared = append(shared, spec.PathPrefix)
		} else {
			dedicated++
		}
	}
	want := []string{"/silo", "/api/v2", "/api/v1"}
	if dedicated != 1 || len(shared) != len(want) {
		t.Fatalf("listeners: dedicated=%d shared=%v", dedicated, shared)
	}
	for n, prefix := range want {
		if shared[n] != prefix {
			t.Fatalf("shared prefixes %v, want %v", shared, want)
		}
	}
}
