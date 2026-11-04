package datafs

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// TryLock takes an exclusive, non-blocking lock on f. It is released when
// f is closed or the process dies.
func TryLock(f *os.File) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{Offset: 1024})
}

// Held reports that TryLock failed because another open file holds the
// lock, as opposed to the filesystem not supporting locks.
func Held(err error) bool { return errors.Is(err, windows.ERROR_LOCK_VIOLATION) }
