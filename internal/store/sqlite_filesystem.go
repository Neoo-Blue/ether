package store

import (
	"fmt"
	"path/filepath"

	"github.com/braedonsaunders/ether/internal/datafs"
)

// checkSQLiteFilesystem refuses a database file on a filesystem where
// SQLite's POSIX locks and write-ahead-log shared memory are unreliable:
// network shares and Unraid user shares. This is the one data-folder
// filesystem Ether will not start on (#305); the playback cache only
// degrades. A folder that cannot be examined is left to the open to report.
func checkSQLiteFilesystem(path string) error {
	info, err := datafs.Inspect(filepath.Dir(path))
	if err != nil {
		return nil
	}
	why := datafs.SQLiteUnsafe(info)
	if why == "" {
		return nil
	}
	fix := datafs.Hint(info.Type)
	if fix == "" {
		fix = "Put Ether's data folder on a disk in this machine"
	}
	return fmt.Errorf("store: the database %s is on %s, and %s. %s, or run Ether with PostgreSQL", path, info.Type, why, fix)
}
