//go:build !windows

package datafs

import (
	"errors"
	"os"
	"syscall"
)

// TryLock takes an exclusive, non-blocking lock on f. It is released when
// f is closed or the process dies.
func TryLock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

// Held reports that TryLock failed because another open file holds the
// lock, as opposed to the filesystem not supporting locks.
func Held(err error) bool { return errors.Is(err, syscall.EWOULDBLOCK) }
