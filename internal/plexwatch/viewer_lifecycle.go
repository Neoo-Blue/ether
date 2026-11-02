package plexwatch

import "github.com/braedonsaunders/ether/internal/store"

// Per-viewer lifecycle for this component's tables: how a viewer's rows
// are purged on removal and moved on merge. See store.RegisterViewerTable.
func init() {
	store.RegisterViewerTable("plexwatch", "plexwatch_account", "user_id", store.ViewerMergeMove)
	store.RegisterViewerTable("plexwatch", "plexwatch_fence", "user_id", store.ViewerMergeMove)
	store.RegisterViewerTable("plexwatch", "plexwatch_pin", "user_id", store.ViewerMergeMove)
	store.RegisterViewerTable("plexwatch", "plexwatch_sent", "user_id", store.ViewerMergeMove, "guid", "kind", "at_ms", "rating")
	store.RegisterViewerTable("plexwatch", "plexwatch_unmapped", "user_id", store.ViewerMergeMove, "guid")
	store.RegisterViewerTable("plexwatch", "plexwatch_attempt", "user_id", store.ViewerMergeDrop)
	store.RegisterViewerTable("plexwatch", "plexwatch_permit", "user_id", store.ViewerMergeDrop)
}
