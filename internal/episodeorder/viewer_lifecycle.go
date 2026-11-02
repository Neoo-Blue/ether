package episodeorder

import "github.com/braedonsaunders/ether/internal/store"

// Per-viewer lifecycle for this component's tables: how a viewer's rows
// are purged on removal and moved on merge. See store.RegisterViewerTable.
func init() {
	store.RegisterViewerTable("episodeorder", "episodeorder_preferences", "user_id", store.ViewerMergeMove, "show_key")
}
