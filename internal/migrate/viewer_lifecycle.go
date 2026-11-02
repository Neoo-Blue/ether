package migrate

import "github.com/braedonsaunders/ether/internal/store"

// Per-viewer lifecycle for this component's tables: how a viewer's rows
// are purged on removal and moved on merge. See store.RegisterViewerTable.
func init() {
	store.RegisterViewerTable("migrate", "migrate_objects", "viewer", store.ViewerMergeMove, "source", "kind", "external")
	store.RegisterViewerTable("migrate", "migrate_watchlist", "viewer", store.ViewerMergeMove, "key")
}
