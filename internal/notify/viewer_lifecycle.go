package notify

import "github.com/braedonsaunders/ether/internal/store"

// Per-viewer lifecycle for this component's tables: how a viewer's rows
// are purged on removal and moved on merge. See store.RegisterViewerTable.
func init() {
	store.RegisterViewerTable("viewer_alerts", "viewer_alert_preferences", "viewer_id", store.ViewerMergeMove)
	store.RegisterViewerTable("viewer_alerts", "viewer_episode_alerts", "viewer_id", store.ViewerMergeMove, "episode_key")
	store.RegisterViewerTable("viewer_alerts", "viewer_episode_scan", "viewer_id", store.ViewerMergeMove)
	store.RegisterViewerTable("viewer_alerts", "viewer_episode_seen", "viewer_id", store.ViewerMergeMove, "episode_key")
	store.RegisterViewerTable("viewer_alerts", "viewer_push_subscriptions", "viewer_id", store.ViewerMergeUnion)
	store.RegisterViewerTable("viewer_alerts", "viewer_push_claims", "viewer_id", store.ViewerMergeMove, "episode_key", "endpoint_hash")
}
