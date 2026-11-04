//go:build !linux && !darwin && !windows && !freebsd

package datafs

import "os"

// Elsewhere nothing is known about the filesystem; Probe decides.
func statKind(dir string) (string, error) {
	if _, err := os.Stat(dir); err != nil {
		return "", err
	}
	return "unknown", nil
}

func inspect(dir string) (string, string, Class, error) {
	kind, err := statKind(dir)
	return kind, "unrecognised filesystem", Unknown, err
}
