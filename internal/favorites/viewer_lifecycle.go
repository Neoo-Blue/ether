package favorites

import "github.com/braedonsaunders/ether/internal/store"

// Per-viewer lifecycle for this component's tables: how a viewer's rows
// are purged on removal and moved on merge. See store.RegisterViewerTable.
func init() {
	store.RegisterViewerTable("favorites", "favorites_items", "viewer_id", store.ViewerMergeMove, "kind", "subject_id")
	store.RegisterViewerTable("favorites", "favorites_mirror", "viewer_id", store.ViewerMergeMove)
}
