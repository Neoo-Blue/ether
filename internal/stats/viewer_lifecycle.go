package stats

import (
	"context"
	"database/sql"

	"github.com/braedonsaunders/ether/internal/store"
)

// Per-viewer lifecycle for this component's tables: how a viewer's rows
// are purged on removal and moved on merge. See store.RegisterViewerTable.
// The newsletter settings row is a server-wide singleton that merely
// names a viewer; removal clears the name, a merge retargets it.
func init() {
	store.RegisterViewerTable("stats", "stats_plays", "user_id", store.ViewerMergeUnion)
	store.RegisterViewerPurge("stats", []string{"stats_newsletter_settings"}, func(ctx context.Context, tx *sql.Tx, viewerID int64) error {
		_, err := tx.ExecContext(ctx, `UPDATE stats_newsletter_settings SET user_id=0 WHERE user_id=?`, viewerID)
		return err
	})
	store.RegisterViewerMerge("stats", []string{"stats_newsletter_settings"}, func(ctx context.Context, tx *sql.Tx, fromID, intoID int64) error {
		_, err := tx.ExecContext(ctx, `UPDATE stats_newsletter_settings SET user_id=? WHERE user_id=?`, intoID, fromID)
		return err
	})
}
