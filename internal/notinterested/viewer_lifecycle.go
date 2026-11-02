package notinterested

import "github.com/braedonsaunders/ether/internal/store"

// Per-viewer lifecycle for this component's tables: how a viewer's rows
// are purged on removal and moved on merge. See store.RegisterViewerTable.
func init() {
	store.RegisterViewerTable("notinterested", "notinterested_titles", "viewer_id", store.ViewerMergeMove, "title_key")
	store.RegisterViewerTable("notinterested", "notinterested_revisions", "viewer_id", store.ViewerMergeMove)
}
