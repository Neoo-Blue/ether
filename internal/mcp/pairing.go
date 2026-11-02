package mcp

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/braedonsaunders/ether/internal/agent/tools"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/identity"
	"github.com/braedonsaunders/ether/internal/store"
)

// MCP pairing: the owner clicks "Connect an AI app" in the console, which
// is already an authenticated act, so POST /pair starts a Quick Connect
// code and approves it at once. The person then exchanges the short code
// for a grant token at /ether/mcp/exchange. Only the grant authenticates.
func init() {
	store.Register("mcp_pairings", 1, `
CREATE TABLE mcp_pairings (
  code TEXT PRIMARY KEY, secret TEXT NOT NULL, scopes TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL DEFAULT '', expires INTEGER NOT NULL,
  token TEXT NOT NULL DEFAULT '');
CREATE INDEX mcp_pairings_expires ON mcp_pairings(expires);`)
}

// PairingOptions configures the pairing service.
type PairingOptions struct {
	DB           *sql.DB
	Grants       *identity.Grants
	QuickConnect *identity.QuickConnect
	// Authorize returns the owner viewer id for a console request. The app
	// implements it from the admin API's owner check.
	Authorize    func(ctx context.Context) (int64, error)
	Logger       *slog.Logger
	AllowedHosts []string
	// CodeTTL bounds how long a pairing code can be exchanged (default 10m).
	CodeTTL time.Duration
	Now     func() time.Time
}

// Pairing serves the console pairing routes and the exchange endpoint.
type Pairing struct {
	opts PairingOptions
	log  *slog.Logger

	mu       sync.Mutex
	attempts map[string][]time.Time // exchange attempts per remote address
}

// NewPairing builds the pairing service.
func NewPairing(opts PairingOptions) (*Pairing, error) {
	if opts.DB == nil || opts.Grants == nil || opts.QuickConnect == nil || opts.Authorize == nil {
		return nil, fmt.Errorf("mcp: pairing needs a database, grants, quick connect and authorize")
	}
	if opts.CodeTTL <= 0 {
		opts.CodeTTL = 10 * time.Minute
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Pairing{opts: opts, log: log, attempts: map[string][]time.Time{}}, nil
}

func (p *Pairing) now() time.Time {
	if p.opts.Now != nil {
		return p.opts.Now()
	}
	return time.Now()
}

// Handler serves the console routes, mounted by the app under
// /api/agent/mcp (owner-authenticated by the admin API):
//
//	POST   /pair            -> {code, expiresAt, endpoint}
//	GET    /clients         -> {clients: [...]}
//	DELETE /clients/{id}    -> 204
func (p *Pairing) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /pair", p.handlePair)
	mux.HandleFunc("GET /clients", p.handleClients)
	mux.HandleFunc("DELETE /clients/{id}", p.handleRevoke)
	return mux
}

// ExchangeHandler trades a pairing code for a grant token. Mount it
// LAN-only at /ether/mcp/exchange, beside the MCP endpoint. The code is
// the only credential: single-use, short-lived and rate-limited.
func (p *Pairing) ExchangeHandler() http.Handler {
	return http.HandlerFunc(p.handleExchange)
}

// Authenticate accepts only scoped grant credentials, never pairing codes.
func (p *Pairing) Authenticate(ctx context.Context, credential string) (tools.Principal, error) {
	credential = strings.TrimSpace(credential)
	if !identity.IsGrantCredential(credential) {
		return tools.Principal{}, core.ErrUnauthorized
	}
	viewer, scopes, err := p.opts.Grants.ResolveScoped(ctx, identity.SurfaceMCP, credential)
	if err != nil || viewer == nil || !viewer.IsOwner {
		return tools.Principal{}, core.ErrUnauthorized
	}
	return tools.Principal{ViewerID: viewer.ID, IsOwner: true, Surface: identity.SurfaceMCP, Scopes: scopes}, nil
}

func (p *Pairing) owner(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := p.opts.Authorize(r.Context())
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "owner sign-in required"})
		return 0, false
	}
	return id, true
}

func (p *Pairing) handlePair(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := p.owner(w, r)
	if !ok {
		return
	}
	var body struct {
		Name  string `json:"name"`
		Scope string `json:"scope"`
	}
	// An empty body is fine: the drawer posts {}.
	_ = json.NewDecoder(r.Body).Decode(&body)
	name := strings.TrimSpace(body.Name)
	if len(name) > 80 {
		name = name[:80]
	}
	if name == "" {
		name = "AI app"
	}
	scope := strings.TrimSpace(body.Scope)
	if scope == "" {
		scope = identity.ScopeFull
	}
	scopes, err := identity.NormaliseScopes([]string{scope})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "scope must be read or full"})
		return
	}
	ctx := r.Context()
	deviceID := "mcp-" + randomHex(6)
	code, secret, err := p.opts.QuickConnect.Initiate(ctx, identity.SurfaceMCP, core.Device{
		ID: deviceID, Client: name, Product: "MCP", DeviceName: name,
	})
	if err != nil {
		p.log.Warn("mcp pairing initiate failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "could not start pairing"})
		return
	}
	if err := p.opts.QuickConnect.ApproveSurface(ctx, identity.SurfaceMCP, code, ownerID); err != nil {
		p.log.Warn("mcp pairing approve failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "could not start pairing"})
		return
	}
	expires := p.now().Add(p.opts.CodeTTL)
	if _, err := p.opts.DB.ExecContext(ctx, `DELETE FROM mcp_pairings WHERE expires<=? AND token=''`, p.now().UnixMilli()); err != nil {
		p.log.Warn("mcp pairing cleanup failed", "err", err)
	}
	if _, err := p.opts.DB.ExecContext(ctx, `INSERT INTO mcp_pairings(code,secret,scopes,name,expires,token) VALUES(?,?,?,?,?,'')`,
		code, secret, strings.Join(scopes, " "), name, expires.UnixMilli()); err != nil {
		p.log.Warn("mcp pairing store failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "could not start pairing"})
		return
	}
	endpoint := absoluteURL(r, "/ether/mcp")
	exchange := absoluteURL(r, "/ether/mcp/exchange")
	writeJSON(w, http.StatusCreated, map[string]any{
		"code":      code,
		"expiresAt": expires.UTC().Format(time.RFC3339),
		"endpoint":  endpoint,
		"exchange":  exchange,
		"command":   fmt.Sprintf("curl -s -X POST %s -H 'Content-Type: application/json' -d '{\"code\":\"%s\"}'", exchange, code),
	})
}

func (p *Pairing) handleExchange(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Request-ID", resolveRequestID(r))
	if v := checkBoundary(r, p.opts.AllowedHosts, nil); v != nil {
		writeJSON(w, v.status, map[string]any{"error": v.message})
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "use POST"})
		return
	}
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]any{"error": "Content-Type must be application/json"})
		return
	}
	if !p.allowAttempt(r.RemoteAddr) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "too many attempts; wait a minute"})
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "send {\"code\": \"...\"}"})
		return
	}
	code := strings.ToUpper(strings.TrimSpace(body.Code))
	if code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "a pairing code is required"})
		return
	}
	ctx := r.Context()
	var secret, scopesRaw, name string
	// Consume the code atomically before minting a grant. Concurrent exchanges
	// cannot both obtain a token, even across server instances.
	err := p.opts.DB.QueryRowContext(ctx, `DELETE FROM mcp_pairings WHERE code=? AND expires>? AND token='' RETURNING secret,scopes,name`, code, p.now().UnixMilli()).Scan(&secret, &scopesRaw, &name)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unknown, expired or already-used code"})
		return
	}
	if err != nil {
		p.log.Warn("mcp exchange lookup failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "exchange failed"})
		return
	}
	token, err := p.opts.QuickConnect.PollSurface(ctx, identity.SurfaceMCP, secret)
	if errors.Is(err, identity.ErrQuickConnectPending) {
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "pending"})
		return
	}
	if err != nil {
		_, _ = p.opts.DB.ExecContext(ctx, `DELETE FROM mcp_pairings WHERE code=?`, code)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unknown or already-used code"})
		return
	}
	scopes := identity.ParseScopes(scopesRaw)
	if err := p.opts.Grants.SetScopes(ctx, identity.SurfaceMCP, token, scopes); err != nil {
		p.log.Warn("mcp exchange set scopes failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "exchange failed"})
		return
	}
	_, _ = p.opts.DB.ExecContext(ctx, `DELETE FROM mcp_pairings WHERE code=?`, code)
	endpoint := absoluteURL(r, "/ether/mcp")
	writeJSON(w, http.StatusOK, map[string]any{
		"token":    token,
		"endpoint": endpoint,
		"name":     name,
		"scopes":   scopes,
		"claudeCodeCommand": fmt.Sprintf(
			`claude mcp add --transport http ether %s --header "Authorization: Bearer %s"`, endpoint, token),
		"note": "Keep the token like a password. The owner can revoke this app at any time in the console under Ask Ether, Connect an AI app.",
	})
}

func (p *Pairing) handleClients(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := p.owner(w, r)
	if !ok {
		return
	}
	records, err := p.opts.Grants.ListSurface(r.Context(), ownerID, identity.SurfaceMCP)
	if err != nil {
		p.log.Warn("mcp clients list failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "could not list paired apps"})
		return
	}
	clients := make([]any, 0, len(records))
	for _, rec := range records {
		name := rec.Client
		if name == "" {
			name = "AI app"
		}
		clients = append(clients, map[string]any{
			"id":         rec.Hash,
			"name":       name,
			"scopes":     rec.Scopes,
			"createdAt":  time.UnixMilli(rec.Created).UTC().Format(time.RFC3339),
			"lastSeenAt": time.UnixMilli(rec.LastUsed).UTC().Format(time.RFC3339),
			"expiresAt":  time.UnixMilli(rec.Expires).UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"clients": clients})
}

func (p *Pairing) handleRevoke(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := p.owner(w, r)
	if !ok {
		return
	}
	revoked, err := p.opts.Grants.RevokeSurface(r.Context(), ownerID, identity.SurfaceMCP, r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid client id"})
		return
	}
	if !revoked {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Paired app not found."})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// allowAttempt rate-limits exchanges: 10 per address per minute. Codes are
// short, so guessing must be slow.
func (p *Pairing) allowAttempt(addr string) bool {
	host := addr
	if i := strings.LastIndex(addr, ":"); i > 0 {
		host = addr[:i]
	}
	now := p.now()
	p.mu.Lock()
	defer p.mu.Unlock()
	recent := p.attempts[host][:0]
	for _, t := range p.attempts[host] {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) >= 10 {
		p.attempts[host] = recent
		return false
	}
	p.attempts[host] = append(recent, now)
	return true
}

func absoluteURL(r *http.Request, path string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host + path
}

func randomHex(bytes int) string {
	b := make([]byte, bytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
