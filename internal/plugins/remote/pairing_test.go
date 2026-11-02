package remote

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/identity"
	"github.com/braedonsaunders/ether/internal/store"
)

type pairUsers struct{ db *sql.DB }

func (u pairUsers) ActiveUser(ctx context.Context, id int64) (*core.User, error) {
	var active int
	if err := u.db.QueryRowContext(ctx, `SELECT active FROM auth_users WHERE id=?`, id).Scan(&active); err != nil || active != 1 {
		return nil, core.ErrUnauthorized
	}
	return &core.User{ID: id}, nil
}
func TestPairingExchangeAndRevocation(t *testing.T) {
	db := store.OpenTest(t)
	if _, err := db.Exec(`INSERT INTO auth_users(id,plex_id,created_at,updated_at) VALUES(1,123,0,0)`); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	clock := func() time.Time { return now }
	g, err := identity.NewGrants(db, pairUsers{db}, time.Hour, clock)
	if err != nil {
		t.Fatal(err)
	}
	p := NewPairing(db, g, func(context.Context) (int64, error) { return 1, nil }, func(_ context.Context, id string) bool { return id == "example" }, nil)
	p.now = clock
	h := p.WrapOwner(http.NotFoundHandler())
	request := func(handler http.Handler, method, path string, body []byte) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewReader(body)))
		return w
	}
	issue := func() string {
		t.Helper()
		w := request(h, "POST", "/example/pair", nil)
		if w.Code != 200 {
			t.Fatalf("issue: %d %s", w.Code, w.Body)
		}
		var out struct {
			Code      string `json:"code"`
			TunnelURL string `json:"tunnelUrl"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Code == "" || out.TunnelURL == "" {
			t.Fatalf("bad pair: %s", w.Body)
		}
		return out.Code
	}
	exchange := http.HandlerFunc(p.Exchange)
	code := issue()
	body := []byte(fmt.Sprintf(`{"code":%q}`, code))
	w := request(exchange, "POST", PairExchangePath, body)
	if w.Code != 200 {
		t.Fatalf("exchange: %d %s", w.Code, w.Body)
	}
	var result struct {
		Credential string `json:"credential"`
		Plugin     string `json:"pluginId"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Plugin != "example" {
		t.Fatalf("result %s", w.Body)
	}
	if _, device, _, err := g.ResolveGrantDevice(context.Background(), "plugin", result.Credential, ""); err != nil || device.ID != "example" {
		t.Fatalf("grant %v %v", device, err)
	}
	if w = request(exchange, "POST", PairExchangePath, body); w.Code != 401 {
		t.Fatalf("replay: %d", w.Code)
	}
	w = request(h, "GET", "/example/pairings", nil)
	var listing struct {
		Pairings []struct {
			ID string `json:"id"`
		} `json:"pairings"`
	}
	if json.Unmarshal(w.Body.Bytes(), &listing) != nil || len(listing.Pairings) != 1 {
		t.Fatalf("list %d %s", w.Code, w.Body)
	}
	if w = request(h, "DELETE", "/other/pairings/"+listing.Pairings[0].ID, nil); w.Code != 404 {
		t.Fatalf("cross plugin revoke %d", w.Code)
	}
	if w = request(h, "DELETE", "/example/pairings/"+listing.Pairings[0].ID, nil); w.Code != 204 {
		t.Fatalf("revoke %d", w.Code)
	}
	if _, _, _, err := g.ResolveGrantDevice(context.Background(), "plugin", result.Credential, ""); err == nil {
		t.Fatal("revoked credential accepted")
	}
	expired := issue()
	now = now.Add(6 * time.Minute)
	if w = request(exchange, "POST", PairExchangePath, []byte(fmt.Sprintf(`{"code":%q}`, expired))); w.Code != 401 {
		t.Fatalf("expired: %d", w.Code)
	}
	for i := 0; i < 31; i++ {
		w = request(exchange, "POST", PairExchangePath, []byte(`{"code":"wrong"}`))
	}
	if w.Code != 429 {
		t.Fatalf("rate limit: %d", w.Code)
	}
}
