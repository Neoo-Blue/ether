package experience

import "github.com/braedonsaunders/ether/internal/store"

// Per-viewer lifecycle for this component's tables: how a viewer's rows
// are purged on removal and moved on merge. See store.RegisterViewerTable.
func init() {
	store.RegisterViewerTable("experience", "experience_receipts", "viewer_id", store.ViewerMergeDrop)
	store.RegisterViewerTable("experience", "experience_row_views", "viewer_id", store.ViewerMergeDrop)
	store.RegisterViewerTable("experience", "experience_row_view_manifests", "viewer_id", store.ViewerMergeDrop)
}
