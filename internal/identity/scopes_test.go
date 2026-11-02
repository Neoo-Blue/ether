package identity_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	_ "github.com/braedonsaunders/ether/internal/auth"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/identity"
	"github.com/braedonsaunders/ether/internal/store"
)

func TestNormaliseScopes(t *testing.T) {
	got, err := identity.NormaliseScopes([]string{" full ", "read", "full", ""})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{identity.ScopeFull, identity.ScopeRead}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if _, err := identity.NormaliseScopes([]string{"admin"}); err == nil {
		t.Fatal("expected error for unknown scope")
	}
}

func TestScopedGrantsLifecycle(t *testing.T) {
	ctx := context.Background()
	db := store.OpenTest(t)
	// Viewer 1 is owner; viewer 2 is non-owner household member.
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_users(id,plex_id,is_owner,active,created_at,updated_at) VALUES(1,100,1,1,0,0),(2,200,0,1,0,0)`); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(20000, 0)
	g, err := identity.NewGrants(db, members{db}, time.Hour, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	// Non-owner cannot issue scoped MCP grants.
	if _, err := g.IssueScoped(ctx, 2, identity.SurfaceMCP, core.Device{ID: "dev-2", Client: "Claude"}, []string{identity.ScopeRead}); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for non-owner, got %v", err)
	}

	// Owner issues a full-scope MCP grant.
	tokFull, err := g.IssueScoped(ctx, 1, identity.SurfaceMCP, core.Device{ID: "dev-1", Client: "Claude Desktop"}, []string{identity.ScopeFull})
	if err != nil {
		t.Fatal(err)
	}
	viewer, scopes, err := g.ResolveScoped(ctx, identity.SurfaceMCP, tokFull)
	if err != nil || viewer.ID != 1 || !slices.Equal(scopes, []string{identity.ScopeFull}) {
		t.Fatalf("ResolveScoped full: viewer=%+v scopes=%v err=%v", viewer, scopes, err)
	}

	// A grant on SurfaceMCP without stored scopes narrows to read-only.
	tokUnscoped, err := g.Issue(ctx, 1, nil, identity.SurfaceMCP, core.Device{ID: "dev-qc", Client: "Cursor"})
	if err != nil {
		t.Fatal(err)
	}
	_, scopes, err = g.ResolveScoped(ctx, identity.SurfaceMCP, tokUnscoped)
	if err != nil || !slices.Equal(scopes, []string{identity.ScopeRead}) {
		t.Fatalf("expected default [read] scope, got %v (err=%v)", scopes, err)
	}

	// SetScopes upgrades or narrows the grant's scopes.
	if err := g.SetScopes(ctx, identity.SurfaceMCP, tokUnscoped, []string{identity.ScopeFull}); err != nil {
		t.Fatal(err)
	}
	_, scopes, err = g.ResolveScoped(ctx, identity.SurfaceMCP, tokUnscoped)
	if err != nil || !slices.Equal(scopes, []string{identity.ScopeFull}) {
		t.Fatalf("after SetScopes: got %v (err=%v)", scopes, err)
	}

	// ListSurface returns both live grants on SurfaceMCP.
	list, err := g.ListSurface(ctx, 1, identity.SurfaceMCP)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 grants on mcp surface, got %d", len(list))
	}

	// RevokeSurface revokes one grant by hash and leaves the other active.
	revoked, err := g.RevokeSurface(ctx, 1, identity.SurfaceMCP, list[0].Hash)
	if err != nil || !revoked {
		t.Fatalf("RevokeSurface: revoked=%v err=%v", revoked, err)
	}
	listAfter, err := g.ListSurface(ctx, 1, identity.SurfaceMCP)
	if err != nil || len(listAfter) != 1 {
		t.Fatalf("expected 1 grant after revoke, got %d (err=%v)", len(listAfter), err)
	}
}
