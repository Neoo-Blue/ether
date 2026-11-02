package personalize

import "github.com/braedonsaunders/ether/internal/store"

// Per-viewer lifecycle for this component's tables: how a viewer's rows
// are purged on removal and moved on merge. See store.RegisterViewerTable.
func init() {
	store.RegisterViewerTable("personalize", "personalize_discovery_hidden", "user_id", store.ViewerMergeMove, "item_key")
	store.RegisterViewerTable("personalize", "personalize_item_impressions", "user_id", store.ViewerMergeMove, "day", "item_key", "hub_id")
	store.RegisterViewerTable("personalize", "personalize_overrides", "user_id", store.ViewerMergeMove, "kind", "value")
	store.RegisterViewerTable("personalize", "personalize_plays", "user_id", store.ViewerMergeUnion)
	store.RegisterViewerTable("personalize", "personalize_row_impressions", "user_id", store.ViewerMergeMove, "day", "hub_id")
	store.RegisterViewerTable("personalize", "personalize_serves", "user_id", store.ViewerMergeUnion)
	store.RegisterViewerTable("personalize", "personalize_serve_items", "user_id", store.ViewerMergeUnion)
}
