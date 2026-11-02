package identity

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/store"
)

// Grant surfaces added for the extensibility build (SPEC §5.12, §5.3).
const (
	// SurfaceMCP is used by paired MCP clients at /ether/mcp.
	SurfaceMCP = "mcp"
	// SurfacePlugin is used by tunnelled plugin sidecars (the plugin host
	// mints these through its own pairing flow).
	SurfacePlugin = "plugin"
)

// Scopes a grant can carry on the MCP surface.
const (
	ScopeRead = "read" // read-only tools
	ScopeFull = "full" // read tools plus write proposals
)

func init() {
	store.Register("identity_grants", 3, `ALTER TABLE identity_grants ADD COLUMN scopes TEXT NOT NULL DEFAULT '';`)
}

// NormaliseScopes validates, deduplicates and sorts scopes for storage.
// Only the MCP scopes Ether knows today are accepted; unknown scopes are
// rejected so a typo can never widen a grant.
func NormaliseScopes(scopes []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(scopes))
	for _, s := range scopes {
		s = strings.TrimSpace(s)
		switch s {
		case "":
			continue
		case ScopeRead, ScopeFull:
		default:
			return nil, fmt.Errorf("identity: unknown scope %q", s)
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out, nil
}

// ParseScopes splits the stored scope column.
func ParseScopes(stored string) []string {
	return strings.Fields(stored)
}

// IssueScoped creates a grant carrying scopes (MCP pairing mints "read" or
// "full"). Owner-only: MCP and plugin credentials are owner credentials.
func (g *Grants) IssueScoped(ctx context.Context, viewerID int64, surface string, device core.Device, scopes []string) (string, error) {
	scopes, err := NormaliseScopes(scopes)
	if err != nil {
		return "", err
	}
	if surface == "" || viewerID <= 0 {
		return "", errors.New("identity: surface and viewer required")
	}
	if _, err := g.viewers.ActiveUser(ctx, viewerID); err != nil {
		return "", err
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	token := grantPrefix + base64.RawURLEncoding.EncodeToString(raw[:])
	hash, _ := grantHash(token)
	now := g.now().UnixMilli()
	result, err := g.db.ExecContext(ctx, `INSERT INTO identity_grants(hash,viewer_id,surface,device_id,client,product,version,created,last_used,expires,scopes) SELECT ?,?,?,?,?,?,?,?,?,?,? WHERE EXISTS (SELECT 1 FROM auth_users WHERE id=? AND active=1 AND is_owner=1 AND origin<>'dlna_guest')`,
		hash, viewerID, surface, device.ID, device.Client, device.Product, device.Version, now, now, now+g.ttl.Milliseconds(), strings.Join(scopes, " "), viewerID)
	if err != nil {
		return "", err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return "", err
	}
	if n != 1 {
		return "", core.ErrUnauthorized
	}
	return token, nil
}

// ResolveScoped is Resolve plus the grant's scopes. A grant without stored
// scopes on the MCP surface resolves to read-only: narrowing, never widening.
func (g *Grants) ResolveScoped(ctx context.Context, surface, credential string) (*core.Viewer, []string, error) {
	viewer, err := g.Resolve(ctx, surface, credential)
	if err != nil {
		return nil, nil, err
	}
	hash, ok := grantHash(credential)
	if !ok {
		return nil, nil, core.ErrUnauthorized
	}
	var stored string
	err = g.db.QueryRowContext(ctx, `SELECT scopes FROM identity_grants WHERE hash=? AND surface=? AND revoked=0`, hash, surface).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, core.ErrUnauthorized
	}
	if err != nil {
		return nil, nil, err
	}
	scopes := ParseScopes(stored)
	if len(scopes) == 0 && surface == SurfaceMCP {
		scopes = []string{ScopeRead}
	}
	return viewer, scopes, nil
}

// SetScopes replaces the scopes of one grant, addressed by its credential.
// MCP pairing mints the grant through Quick Connect and sets the scope the
// owner picked in the console.
func (g *Grants) SetScopes(ctx context.Context, surface, credential string, scopes []string) error {
	scopes, err := NormaliseScopes(scopes)
	if err != nil {
		return err
	}
	hash, ok := grantHash(credential)
	if !ok || surface == "" {
		return core.ErrUnauthorized
	}
	res, err := g.db.ExecContext(ctx, `UPDATE identity_grants SET scopes=? WHERE hash=? AND surface=? AND revoked=0`, strings.Join(scopes, " "), hash, surface)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return core.ErrUnauthorized
	}
	return nil
}

// GrantRecord is one grant row as the console lists it, with scopes.
type GrantRecord struct {
	Hash     string   `json:"id"`
	Surface  string   `json:"surface"`
	DeviceID string   `json:"deviceId"`
	Client   string   `json:"client"`
	Product  string   `json:"product"`
	Version  string   `json:"version"`
	Scopes   []string `json:"scopes,omitempty"`
	Created  int64    `json:"createdAt"`
	LastUsed int64    `json:"lastUsed"`
	Expires  int64    `json:"expiresAt"`
}

// ListSurface lists a viewer's live grants on one surface, most recent first.
func (g *Grants) ListSurface(ctx context.Context, viewerID int64, surface string) ([]GrantRecord, error) {
	rows, err := g.db.QueryContext(ctx, `SELECT hash,surface,device_id,client,product,version,scopes,created,last_used,expires FROM identity_grants WHERE viewer_id=? AND surface=? AND revoked=0 AND expires>? ORDER BY last_used DESC`, viewerID, surface, g.now().UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GrantRecord{}
	for rows.Next() {
		var r GrantRecord
		var stored string
		if err := rows.Scan(&r.Hash, &r.Surface, &r.DeviceID, &r.Client, &r.Product, &r.Version, &stored, &r.Created, &r.LastUsed, &r.Expires); err != nil {
			return nil, err
		}
		r.Scopes = ParseScopes(stored)
		if len(r.Scopes) == 0 && r.Surface == SurfaceMCP {
			r.Scopes = []string{ScopeRead}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RevokeSurface revokes one grant on a surface and reports whether a live
// grant was revoked.
func (g *Grants) RevokeSurface(ctx context.Context, viewerID int64, surface, hash string) (bool, error) {
	if len(hash) != 64 || surface == "" {
		return false, fmt.Errorf("identity: invalid grant reference")
	}
	res, err := g.db.ExecContext(ctx, `UPDATE identity_grants SET revoked=1 WHERE viewer_id=? AND surface=? AND hash=? AND revoked=0`, viewerID, surface, hash)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 1 {
		g.gen.Add(1)
		return true, nil
	}
	return false, nil
}
