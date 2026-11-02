package core

import "context"

// LibraryExclusion is one ordered visibility rule. Slice order is evaluation
// order: the first match wins. Action is "show" or "hide"; an empty Action is
// "hide", so a saved exclusion group stays a hide rule. Conditions inside a
// rule combine with Match "all" or "any". Watch history and metadata are kept.
//
// Scope limits where a hide rule applies. Empty or "everywhere" hides the
// title from browsing and search. "discovery" hides it only from browsing,
// Home and hub rows, recommendations, New Releases, Trending and catalogue
// collections; search, direct lookup and playback still reach it.
type LibraryExclusion struct {
	// Name is optional. Clients label a rule from its conditions.
	Name      string           `json:"name"`
	Action    string           `json:"action,omitempty"` // show, hide; empty means hide
	MediaType string           `json:"mediaType"`        // movie, show, mixed
	Match     string           `json:"match"`            // all, any
	Rules     []CollectionRule `json:"rules"`
	Scope     string           `json:"scope,omitempty"` // everywhere, discovery; empty means everywhere
}

// Visibility rule scopes.
const (
	VisibilityEverywhere = "everywhere"
	VisibilityDiscovery  = "discovery"
)

type explicitSearchKey struct{}

// WithExplicitSearch marks a request as a viewer or owner typing a search.
// Catalogue visibility then applies only rules scoped everywhere, so a title
// hidden from browsing alone can still be found.
func WithExplicitSearch(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, explicitSearchKey{}, true)
}

// ExplicitSearch reports that ctx belongs to an explicit search.
func ExplicitSearch(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(explicitSearchKey{}).(bool)
	return v
}
