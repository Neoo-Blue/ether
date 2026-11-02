package playback

import "github.com/braedonsaunders/ether/internal/store"

// Per-viewer lifecycle for this component's tables: how a viewer's rows
// are purged on removal and moved on merge. See store.RegisterViewerTable.
func init() {
	store.RegisterViewerTable("playback", "playback_index", "user_id", store.ViewerMergeMove, "rating_key", "release_id")
	store.RegisterViewerTable("playback", "playback_probe", "user_id", store.ViewerMergeMove, "rating_key", "release_id")
	store.RegisterViewerTable("playback", "playback_resume", "user_id", store.ViewerMergeMove, "rating_key")
	store.RegisterViewerTable("playback", "playback_live_sessions", "user_id", store.ViewerMergeDrop)
	store.RegisterViewerTableEarly("playback", "playback_payload_owners", "viewer_id", store.ViewerMergeDrop)
}
