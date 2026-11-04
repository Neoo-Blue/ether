package playback

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/datafs"
	"github.com/braedonsaunders/ether/internal/store"
)

// Crash recovery trusts a free witness lock only where locks prove a
// process dead: a known local filesystem, or an unknown one that passes the
// startup probe. Anything else runs in safe mode and Ether still starts
// (#305). In safe mode a live or recently refreshed incarnation is never
// reclaimed; a stale or cleanly closed one is.
func TestPayloadFilesystemTrustAndSafeMode(t *testing.T) {
	probeFailed := errors.New("exclusive file lock: a second open file was granted the same lock")
	for _, tc := range []struct {
		class            datafs.Class
		probe            error
		probed, safeMode bool
	}{
		{datafs.Local, nil, false, false},
		{datafs.Network, nil, false, true},
		{datafs.Unknown, nil, true, false},
		{datafs.Unknown, probeFailed, true, true},
	} {
		probed := false
		reason := payloadTrust(tc.class, func() error { probed = true; return tc.probe })
		if probed != tc.probed || (reason != "") != tc.safeMode {
			t.Errorf("%v probe=%v: probed=%v reason=%q", tc.class, tc.probe, probed, reason)
		}
		if tc.probe != nil && !strings.Contains(reason, tc.probe.Error()) {
			t.Errorf("safe-mode reason %q does not name the failed check", reason)
		}
	}
	scratch := t.TempDir()
	if err := datafs.Probe(scratch); err != nil {
		t.Fatalf("probe failed on the test machine's disk: %v", err)
	}
	if left, _ := os.ReadDir(scratch); len(left) != 0 {
		t.Fatalf("probe left %d entries behind", len(left))
	}

	saved := inspectPayloadFilesystem
	inspectPayloadFilesystem = func(dir string) (datafs.Info, error) {
		info, err := saved(dir)
		info.Type, info.Class = "nfs4", datafs.Network
		return info, err
	}
	t.Cleanup(func() { inspectPayloadFilesystem = saved })
	ctx := context.Background()
	db, dir := store.OpenTest(t), t.TempDir()
	open := func() *payloadStore {
		t.Helper()
		p, err := openPayloadStore(ctx, db, dir)
		if err != nil {
			t.Fatalf("safe mode must still open: %v", err)
		}
		t.Cleanup(p.close)
		return p
	}
	a := open()
	if a.fs.Trust != "safe-mode" || a.fs.Type != "nfs4" || a.fs.Path != dir || !strings.Contains(a.fs.Fix, "network share (nfs4)") {
		t.Fatalf("status %+v", a.fs)
	}
	hash := nzbHash([]byte("safe-mode"))
	payload := filepath.Join(dir, hash+".nzb")
	if err := os.WriteFile(payload, []byte("nzb"), 0o600); err != nil {
		t.Fatal(err)
	}
	payloadReserve(t, a, hash)

	// A live incarnation in this process is never taken over.
	b := open()
	if b.incarnation == a.incarnation || payloadRows(t, a, "playback_payload_owners", a.incarnation) != 1 {
		t.Fatal("second store reclaimed a live incarnation")
	}
	if err := a.verifyProof(a.file, a.incarnation); err != nil {
		t.Fatalf("live store lost its witness: %v", err)
	}

	// a crashes: its lock is free, but its lease is fresh.
	a.mu.Lock()
	close(a.beatStop)
	witness := a.file.Name()
	a.file.Close()
	a.file = nil
	heldPayloadWitnesses.Delete(a.incarnation)
	a.mu.Unlock()
	b.cursor = 0
	if err := b.reconcileSlots(ctx); err != nil {
		t.Fatal(err)
	}
	if payloadRows(t, b, "playback_payload_owners", a.incarnation) != 1 {
		t.Fatal("free lock reclaimed an incarnation whose lease is fresh")
	}
	stale := time.Now().Add(-payloadLeaseTTL - time.Minute)
	if err := os.Chtimes(witness, stale, stale); err != nil {
		t.Fatal(err)
	}
	b.cursor = 0
	if err := b.reconcileSlots(ctx); err != nil {
		t.Fatal(err)
	}
	if payloadRows(t, b, "playback_payload_incarnations", a.incarnation) != 0 {
		t.Fatal("stale lease was not reclaimed")
	}
	if _, err := os.Stat(payload); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale incarnation's payload kept: %v", err)
	}

	// A clean close back-dates the lease: reclaimed without the wait.
	b.close()
	c := open()
	c.cursor = 0
	if err := c.reconcileSlots(ctx); err != nil {
		t.Fatal(err)
	}
	if payloadRows(t, c, "playback_payload_incarnations", b.incarnation) != 0 {
		t.Fatal("cleanly closed incarnation was not reclaimed")
	}
}
