package trakt

import "github.com/braedonsaunders/ether/internal/store"

// Per-viewer lifecycle for this component's tables: how a viewer's rows
// are purged on removal and moved on merge. See store.RegisterViewerTable.
func init() {
	store.RegisterViewerTable("trakt", "trakt_tokens", "user_id", store.ViewerMergeMove)
	store.RegisterViewerTable("trakt", "trakt_import_queue", "user_id", store.ViewerMergeDrop)
}
