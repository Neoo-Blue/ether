package datafs

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// probeDir holds the probe's scratch folders inside the probed folder.
const probeDir = ".fsprobe"

// ProbeError names the semantic a filesystem failed.
type ProbeError struct {
	Step string
	Err  error
}

func (e *ProbeError) Error() string { return e.Step + ": " + e.Err.Error() }
func (e *ProbeError) Unwrap() error { return e.Err }

// Probe tests, in a scratch folder inside dir, what the payload store's
// crash recovery relies on: write and fsync, read back, atomic rename over
// an existing file, stable and distinct file identity, exclusive locks that
// a second open file cannot take and that closing releases, in-place
// rewrite, and remove. nil means every check passed. The scratch folder is
// removed; one left by a crash is removed by the next probe.
//
// A local probe cannot see another machine, so it never makes a network
// filesystem trustworthy; callers do not probe those.
func Probe(dir string) (err error) {
	root := filepath.Join(dir, probeDir)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return &ProbeError{"create a test folder", err}
	}
	clearStaleProbes(root)
	tmp, err := os.MkdirTemp(root, "probe-")
	if err != nil {
		return &ProbeError{"create a test folder", err}
	}
	defer func() {
		if rerr := os.RemoveAll(tmp); rerr != nil && err == nil {
			err = &ProbeError{"remove the test folder", rerr}
		}
		_ = os.Remove(root) // fails while another probe is running; fine
	}()
	return probe(tmp)
}

// clearStaleProbes removes scratch folders an interrupted probe left. Only
// probe folders live here, so the listing is bounded by crashed probes.
func clearStaleProbes(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-time.Hour)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
			_ = os.RemoveAll(filepath.Join(root, e.Name()))
		}
	}
}

func probe(dir string) error {
	a, b, c := filepath.Join(dir, "a"), filepath.Join(dir, "b"), filepath.Join(dir, "c")
	if err := writeSynced(a, "witness-1"); err != nil {
		return &ProbeError{"write and fsync a file", err}
	}
	if err := expectContent(a, "witness-1"); err != nil {
		return &ProbeError{"read a file back", err}
	}
	if err := writeSynced(b, "witness-2"); err != nil {
		return &ProbeError{"write and fsync a file", err}
	}
	if err := os.Rename(b, a); err != nil {
		return &ProbeError{"rename over an existing file", err}
	}
	if err := expectContent(a, "witness-2"); err != nil {
		return &ProbeError{"rename over an existing file", err}
	}
	if _, err := os.Lstat(b); !errors.Is(err, fs.ErrNotExist) {
		return &ProbeError{"rename over an existing file", fmt.Errorf("the source still exists (%v)", err)}
	}

	f1, err := os.OpenFile(a, os.O_RDWR, 0)
	if err != nil {
		return &ProbeError{"open a file twice", err}
	}
	defer f1.Close()
	f2, err := os.OpenFile(a, os.O_RDWR, 0)
	if err != nil {
		return &ProbeError{"open a file twice", err}
	}
	defer f2.Close()
	if err := writeSynced(c, "other"); err != nil {
		return &ProbeError{"write and fsync a file", err}
	}
	if err := sameIdentity(f1, f2, a, c); err != nil {
		return &ProbeError{"stable file identity", err}
	}

	if err := TryLock(f1); err != nil {
		return &ProbeError{"exclusive file lock", fmt.Errorf("the lock was refused (%w)", err)}
	}
	if err := TryLock(f2); err == nil {
		return &ProbeError{"exclusive file lock", errors.New("a second open file was granted the same lock")}
	}
	f1.Close()
	if err := TryLock(f2); err != nil {
		return &ProbeError{"exclusive file lock", fmt.Errorf("closing the holder did not release the lock (%w)", err)}
	}

	if err := rewrite(f2, "witness-3"); err != nil {
		return &ProbeError{"rewrite a file in place", err}
	}
	if err := expectContent(a, "witness-3"); err != nil {
		return &ProbeError{"rewrite a file in place", err}
	}
	if err := os.Remove(c); err != nil {
		return &ProbeError{"remove a file", err}
	}
	if _, err := os.Lstat(c); !errors.Is(err, fs.ErrNotExist) {
		return &ProbeError{"remove a file", fmt.Errorf("the file still exists (%v)", err)}
	}
	return nil
}

func writeSynced(path, value string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(value); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func expectContent(path, want string) error {
	got, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(got, []byte(want)) {
		return fmt.Errorf("read %q, wrote %q", got, want)
	}
	return nil
}

// sameIdentity: two opens of a and its path are one file; c is another.
func sameIdentity(f1, f2 *os.File, a, c string) error {
	s1, err := f1.Stat()
	if err != nil {
		return err
	}
	s2, err := f2.Stat()
	if err != nil {
		return err
	}
	byPath, err := os.Lstat(a)
	if err != nil {
		return err
	}
	other, err := os.Lstat(c)
	if err != nil {
		return err
	}
	if !os.SameFile(s1, s2) || !os.SameFile(s1, byPath) {
		return errors.New("two opens of one file report different identities")
	}
	if os.SameFile(s1, other) {
		return errors.New("two different files report the same identity")
	}
	return nil
}

func rewrite(f *os.File, value string) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.WriteAt([]byte(value), 0); err != nil {
		return err
	}
	return f.Sync()
}
