package state

import "github.com/braedonsaunders/ether/internal/store"

// Per-viewer lifecycle for this component's tables: how a viewer's rows
// are purged on removal and moved on merge. See store.RegisterViewerTable.
func init() {
	store.RegisterViewerTable("state", "state_watch", "user_id", store.ViewerMergeMove, "rating_key")
	store.RegisterViewerTable("state", "state_history", "user_id", store.ViewerMergeUnion)
	store.RegisterViewerTable("state", "state_playlists", "user_id", store.ViewerMergeUnion)
	store.RegisterViewerTable("state", "state_playlist_markers", "user_id", store.ViewerMergeMove, "marker")
	store.RegisterViewerTable("state", "state_last_source", "user_id", store.ViewerMergeMove, "rating_key")
	store.RegisterViewerTable("state", "state_sync_journal", "user_id", store.ViewerMergeDrop)
	store.RegisterViewerTable("state", "state_sync_lock", "user_id", store.ViewerMergeDrop)
}
