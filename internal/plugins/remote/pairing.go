package remote

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/identity"
)

const PairExchangePath = "/ether/plugins/pair"

type pendingPair struct {
	plugin  string
	viewer  int64
	expires time.Time
}

// Pairing implements owner-initiated, one-use sidecar pairing. Codes are
// intentionally ephemeral; restarting the server invalidates pending codes.
type Pairing struct {
	// TunnelURL derives the externally reachable URL from the owner request.
	TunnelURL func(*http.Request) string
	// Exists allows owners to inspect and revoke pairings of disabled plugins.
	Exists   func(context.Context, string) bool
	db       *sql.DB
	grants   *identity.Grants
	owner    func(context.Context) (int64, error)
	enabled  func(context.Context, string) bool
	tunnel   *TunnelServer
	now      func() time.Time
	mu       sync.Mutex
	pending  map[[32]byte]pendingPair
	attempts int
	window   time.Time
}

func NewPairing(db *sql.DB, grants *identity.Grants, owner func(context.Context) (int64, error), enabled func(context.Context, string) bool, tunnel *TunnelServer) *Pairing {
	return &Pairing{db: db, grants: grants, owner: owner, enabled: enabled, tunnel: tunnel, now: time.Now, pending: make(map[[32]byte]pendingPair)}
}
func pairJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func pairError(w http.ResponseWriter, status int) {
	pairJSON(w, status, map[string]string{"error": http.StatusText(status)})
}

// WrapOwner is mounted under the already authenticated owner API.
func (p *Pairing) WrapOwner(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/plugins/")
		path = strings.TrimPrefix(path, "/")
		parts := strings.Split(path, "/")
		if len(parts) < 2 || (parts[1] != "pair" && parts[1] != "pairings") {
			next.ServeHTTP(w, r)
			return
		}
		id := parts[0]
		exists := p.enabled
		if p.Exists != nil {
			exists = p.Exists
		}
		if id == "" || !exists(r.Context(), id) {
			pairError(w, http.StatusNotFound)
			return
		}
		if p.grants == nil || p.db == nil || p.owner == nil {
			pairError(w, http.StatusServiceUnavailable)
			return
		}
		switch {
		case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "pair":
			if !p.enabled(r.Context(), id) {
				pairError(w, http.StatusConflict)
				return
			}
			viewer, err := p.owner(r.Context())
			if err != nil || viewer <= 0 {
				pairError(w, http.StatusUnauthorized)
				return
			}
			var raw [10]byte
			if _, err = rand.Read(raw[:]); err != nil {
				pairError(w, 500)
				return
			}
			code := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw[:])
			now := p.now()
			hash := sha256.Sum256([]byte(code))
			p.mu.Lock()
			for k, v := range p.pending {
				if !v.expires.After(now) {
					delete(p.pending, k)
				}
			}
			if len(p.pending) >= 256 {
				p.mu.Unlock()
				pairError(w, 429)
				return
			}
			p.pending[hash] = pendingPair{id, viewer, now.Add(5 * time.Minute)}
			p.mu.Unlock()
			scheme := "ws"
			if r.TLS != nil {
				scheme = "wss"
			}
			tunnelURL := scheme + "://" + r.Host + TunnelPath
			if p.TunnelURL != nil {
				tunnelURL = p.TunnelURL(r)
			}
			pairJSON(w, 200, map[string]any{"code": code, "expiresAt": now.Add(5 * time.Minute).UTC(), "tunnelUrl": tunnelURL, "exchangePath": PairExchangePath})
		case r.Method == http.MethodGet && len(parts) == 2 && parts[1] == "pairings":
			rows, err := p.db.QueryContext(r.Context(), `SELECT hash,created,last_used,expires FROM identity_grants WHERE surface='plugin' AND device_id=? AND revoked=0 AND expires>? ORDER BY created DESC`, id, p.now().UnixMilli())
			if err != nil {
				pairError(w, 500)
				return
			}
			defer rows.Close()
			type item struct {
				ID       string `json:"id"`
				Created  int64  `json:"created"`
				LastUsed int64  `json:"lastUsed"`
				Expires  int64  `json:"expires"`
			}
			list := []item{}
			for rows.Next() {
				var x item
				if rows.Scan(&x.ID, &x.Created, &x.LastUsed, &x.Expires) != nil {
					pairError(w, 500)
					return
				}
				list = append(list, x)
			}
			if rows.Err() != nil {
				pairError(w, 500)
				return
			}
			pairJSON(w, 200, map[string]any{"pairings": list})
		case r.Method == http.MethodDelete && len(parts) == 3 && parts[1] == "pairings":
			res, err := p.db.ExecContext(r.Context(), `UPDATE identity_grants SET revoked=1 WHERE surface='plugin' AND device_id=? AND hash=? AND revoked=0`, id, parts[2])
			if err != nil {
				pairError(w, 500)
				return
			}
			n, _ := res.RowsAffected()
			if n == 0 {
				pairError(w, 404)
				return
			}
			if p.tunnel != nil {
				_ = p.tunnel.RuntimeFor(id).Close()
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			pairError(w, 404)
		}
	})
}

// Exchange accepts a code once. A process-wide attempt budget protects even
// unknown codes, and the high-entropy code prevents distributed guessing.
func (p *Pairing) Exchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		pairError(w, 405)
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		pairError(w, 400)
		return
	}
	now := p.now()
	hash := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(in.Code))))
	p.mu.Lock()
	if p.window.IsZero() || now.Sub(p.window) >= time.Minute {
		p.window = now
		p.attempts = 0
	}
	p.attempts++
	if p.attempts > 30 {
		p.mu.Unlock()
		pairError(w, 429)
		return
	}
	entry, ok := p.pending[hash]
	if ok {
		delete(p.pending, hash)
	}
	p.mu.Unlock()
	if !ok || !entry.expires.After(now) || !p.enabled(r.Context(), entry.plugin) || p.grants == nil {
		pairError(w, 401)
		return
	}
	credential, err := p.grants.Issue(r.Context(), entry.viewer, nil, "plugin", core.Device{ID: entry.plugin, Client: "Ether sidecar"})
	if err != nil {
		if errors.Is(err, core.ErrUnauthorized) {
			pairError(w, 401)
		} else {
			pairError(w, 500)
		}
		return
	}
	pairJSON(w, 200, map[string]string{"credential": credential, "pluginId": entry.plugin})
}
