package trackprefs

import "github.com/braedonsaunders/ether/internal/store"

// Per-viewer lifecycle for this component's tables: how a viewer's rows
// are purged on removal and moved on merge. See store.RegisterViewerTable.
func init() {
	store.RegisterViewerTable("trackprefs", "trackprefs_memory", "user_id", store.ViewerMergeMove, "show_id", "kind")
	store.RegisterViewerTable("trackprefs", "trackprefs_rules", "user_id", store.ViewerMergeMove)
	store.RegisterViewerTable("trackprefs", "trackprefs_scoped", "user_id", store.ViewerMergeMove, "scope", "scope_id", "field")
}
