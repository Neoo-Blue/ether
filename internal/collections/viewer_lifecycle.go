package collections

import (
	"context"
	"database/sql"

	"github.com/braedonsaunders/ether/internal/store"
)

// Collections are shared server content, not personal data: removing a
// viewer keeps the collections they made and only drops their name from
// them (owner_user_id 0 is a server-owned collection). A merge hands
// their collections to the surviving viewer.
func init() {
	store.RegisterViewerPurge("collections", []string{"collections_specs"}, func(ctx context.Context, tx *sql.Tx, viewerID int64) error {
		_, err := tx.ExecContext(ctx, `UPDATE collections_specs SET owner_user_id=0 WHERE owner_user_id=?`, viewerID)
		return err
	})
	store.RegisterViewerMerge("collections", []string{"collections_specs"}, func(ctx context.Context, tx *sql.Tx, fromID, intoID int64) error {
		_, err := tx.ExecContext(ctx, `UPDATE collections_specs SET owner_user_id=? WHERE owner_user_id=?`, intoID, fromID)
		return err
	})
}
