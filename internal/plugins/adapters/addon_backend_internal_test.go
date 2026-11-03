package adapters

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// The skipped-streams warning is throttled per add-on: one WARN, then
// DEBUG until the window passes, then WARN again with both counters.
func TestAddonBackendSkipWarnReopensAfterTheWindow(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	b := &addonBackend{plugin: "community.example"}
	b.logSkipped(0, 0)
	if buf.Len() != 0 {
		t.Fatalf("nothing skipped, yet logged: %q", buf.String())
	}
	for i := 0; i < 3; i++ {
		b.logSkipped(1, 0)
	}
	if n := strings.Count(buf.String(), "level=WARN"); n != 1 {
		t.Fatalf("3 skips in a row: %d WARN, want 1: %q", n, buf.String())
	}
	b.mu.Lock()
	b.lastSkipWarn = time.Now().Add(-skipWarnInterval - time.Minute)
	b.mu.Unlock()
	b.logSkipped(0, 2)
	if n := strings.Count(buf.String(), "level=WARN"); n != 2 {
		t.Fatalf("after the window: %d WARN, want 2: %q", n, buf.String())
	}
	if !strings.Contains(buf.String(), "skippedNoHash=1") || !strings.Contains(buf.String(), "skippedAmbiguous=2") || !strings.Contains(buf.String(), "addon=community.example") {
		t.Fatalf("warn without the counters or the add-on: %q", buf.String())
	}
}
