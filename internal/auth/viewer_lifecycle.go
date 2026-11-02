package auth

import "github.com/braedonsaunders/ether/internal/store"

// Per-viewer lifecycle for this component's tables: how a viewer's rows
// are purged on removal and moved on merge. See store.RegisterViewerTable.
func init() {
	store.RegisterViewerTable("auth", "auth_tokens", "user_id", store.ViewerMergeUnion)
	store.RegisterViewerTable("auth", "auth_delegations", "user_id", store.ViewerMergeDrop)
	store.RegisterViewerTable("auth", "auth_sessions", "user_id", store.ViewerMergeDrop)
	store.RegisterViewerTable("auth", "auth_plex_defaults", "user_id", store.ViewerMergeMove)
	store.RegisterViewerTable("auth", "identity_viewer_deactivations", "viewer_id", store.ViewerMergeDrop)
}
