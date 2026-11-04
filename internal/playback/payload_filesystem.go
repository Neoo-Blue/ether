package playback

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/datafs"
)

// Crash recovery takes a dead process's witness lock as proof that the
// process can no longer use its payloads. A filesystem whose locks cannot
// carry that proof (a network share, or one that fails the startup probe)
// runs the payload store in safe mode instead of refusing to start (#305):
// a held lock still proves a process is alive, but a free lock no longer
// proves it is dead. A live store refreshes its witness's modification
// time every payloadLeaseBeat, and another incarnation is reclaimed only
// once its witness has gone payloadLeaseTTL without a refresh. A store that
// closes cleanly back-dates its witness so the next start reclaims it at
// once. Until then its payloads stay on disk.
const (
	payloadLeaseBeat = time.Minute
	payloadLeaseTTL  = 10 * time.Minute
)

// inspectPayloadFilesystem classifies the data folder; tests stand in for
// filesystems the test machine does not have.
var inspectPayloadFilesystem = datafs.Inspect

// PayloadFilesystem is the filesystem under the playback data folder and
// whether crash recovery trusts it, for the owner's health view.
type PayloadFilesystem struct {
	Path string `json:"path"`
	Type string `json:"type"`
	// Trust is "local" (a known local filesystem), "verified" (unknown, and
	// the startup probe passed) or "safe-mode".
	Trust string `json:"trust"`
	// Reason says why safe mode is on; Fix says what to change. The kinds
	// are their stable codes for the console to translate: ReasonKind is
	// "network" or "check" (the startup probe failed); FixKind is
	// datafs.HintKind's, or "local-disk".
	Reason     string `json:"reason,omitempty"`
	ReasonKind string `json:"reasonKind,omitempty"`
	Fix        string `json:"fix,omitempty"`
	FixKind    string `json:"fixKind,omitempty"`
}

// payloadTrust decides how far crash recovery may trust a filesystem. It
// returns the reason for safe mode, or "" when the witness locks can prove
// a process dead. Only an unknown filesystem is probed.
func payloadTrust(class datafs.Class, probe func() error) string {
	switch class {
	case datafs.Local:
		return ""
	case datafs.Network:
		return "it is a network filesystem, where a file lock does not stop another machine"
	}
	if err := probe(); err != nil {
		return "the startup filesystem check failed: " + err.Error()
	}
	return ""
}

func newPayloadFilesystem(info datafs.Info, degraded string) PayloadFilesystem {
	st := PayloadFilesystem{Path: info.Path, Type: info.Type, Trust: "local"}
	switch {
	case degraded != "":
		st.Trust, st.Reason, st.ReasonKind = "safe-mode", degraded, "check"
		if info.Class == datafs.Network {
			st.ReasonKind = "network"
		}
		st.Fix, st.FixKind = datafs.Hint(info.Type), datafs.HintKind(info.Type)
		if st.Fix == "" {
			st.Fix, st.FixKind = "Put Ether's data folder on a disk in this machine (ext4, XFS, Btrfs, ZFS, f2fs, bcachefs or NTFS)", "local-disk"
		}
		slog.Warn("playback: cache cleanup is in safe mode; files left by a crash are kept for 10 minutes before they are reclaimed",
			"path", info.Path, "filesystem", info.Type, "reason", degraded, "fix", st.Fix)
	case info.Class != datafs.Local:
		st.Trust = "verified"
		slog.Info("playback: data folder filesystem passed the startup check", "path", info.Path, "filesystem", info.Type)
	}
	return st
}

// PayloadFilesystem reports the playback data folder's filesystem; false
// without a database-backed store.
func (m *Manager) PayloadFilesystem() (PayloadFilesystem, bool) {
	if m.resume == nil || m.resume.payload == nil {
		return PayloadFilesystem{}, false
	}
	return m.resume.payload.fs, true
}

// checkFilesystem refuses work once the data folder is no longer on the
// filesystem judged at open (one statfs).
func (p *payloadStore) checkFilesystem() error {
	kind, err := datafs.Kind(p.dir)
	if err != nil {
		return err
	}
	if kind != p.fsKind {
		return fmt.Errorf("%w: %s is no longer on the %s filesystem checked at startup", errPayloadFilesystem, p.dir, p.fs.Type)
	}
	return nil
}

// lockWitness takes f's witness lock. In safe mode only a lock another open
// file holds counts against taking the slot; a filesystem without working
// locks falls back to the lease.
func (p *payloadStore) lockWitness(f *os.File) error {
	err := lockPayloadWitness(f)
	if err != nil && p.degraded != "" && !datafs.Held(err) {
		return nil
	}
	return err
}

// checkWitness is verifyWitnessFile. Safe mode does not rely on file
// identity: verifyProof compares the content read through the path instead.
func (p *payloadStore) checkWitness(f *os.File) error {
	if p.degraded == "" {
		return verifyWitnessFile(f)
	}
	opened, err := f.Stat()
	if err != nil {
		return err
	}
	path, err := os.Lstat(f.Name())
	if err != nil {
		return err
	}
	if !opened.Mode().IsRegular() || !path.Mode().IsRegular() {
		return core.ErrNotAvailable
	}
	return nil
}

// leaseExpired reports, in safe mode, that incarnation's witness f has not
// been refreshed for payloadLeaseTTL and no store in this process holds it.
// With trustworthy locks the lock alone decides.
func (p *payloadStore) leaseExpired(f *os.File, incarnation string) bool {
	if p.degraded == "" {
		return true
	}
	if _, live := heldPayloadWitnesses.Load(incarnation); live {
		return false
	}
	st, err := f.Stat()
	return err == nil && time.Since(st.ModTime()) >= payloadLeaseTTL
}

// beat refreshes this store's witness lease until the witness is dropped.
func (p *payloadStore) beat(path string, stop <-chan struct{}) {
	t := time.NewTicker(payloadLeaseBeat)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			now := time.Now()
			if err := os.Chtimes(path, now, now); err != nil {
				slog.Warn("playback: could not refresh the payload witness lease", "path", path, "err", err)
			}
		}
	}
}

// dropWitnessLocked closes this store's witness once nothing it protects
// is in flight. In safe mode it first back-dates the lease so the next
// start reclaims this incarnation without waiting. p.mu is held.
func (p *payloadStore) dropWitnessLocked() {
	if p.file == nil {
		return
	}
	if p.degraded != "" {
		close(p.beatStop)
		old := time.Now().Add(-24 * time.Hour)
		_ = os.Chtimes(p.file.Name(), old, old)
	}
	p.file.Close()
	p.file = nil
	heldPayloadWitnesses.Delete(p.incarnation)
}
