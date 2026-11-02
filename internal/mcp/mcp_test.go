package mcp_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/agent"
	"github.com/braedonsaunders/ether/internal/agent/builtin"
	"github.com/braedonsaunders/ether/internal/agent/tools"
	_ "github.com/braedonsaunders/ether/internal/auth"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/identity"
	"github.com/braedonsaunders/ether/internal/livetv"
	"github.com/braedonsaunders/ether/internal/mcp"
	"github.com/braedonsaunders/ether/internal/resolver"
	"github.com/braedonsaunders/ether/internal/store"
)

type testMembers struct{ db *sql.DB }

func (m testMembers) ActiveUser(ctx context.Context, id int64) (*core.User, error) {
	var active, isOwner int
	err := m.db.QueryRowContext(ctx, `SELECT active, is_owner FROM auth_users WHERE id=?`, id).Scan(&active, &isOwner)
	if err != nil || active != 1 {
		return nil, core.ErrUnauthorized
	}
	return &core.User{ID: id, IsOwner: isOwner == 1}, nil
}

type stubServices struct {
	blocked   []string
	approvals map[string]builtin.ApprovalInfo
}

func (s *stubServices) ServerStatus(ctx context.Context) (builtin.ServerStatus, error) {
	return builtin.ServerStatus{Version: "1.0.0", ServerName: "Ether", SetupComplete: true}, nil
}
func (s *stubServices) Health(ctx context.Context) ([]builtin.HealthCheck, error) {
	return []builtin.HealthCheck{{Name: "Database", Status: "ok", Detail: "Reachable"}}, nil
}
func (s *stubServices) History(ctx context.Context, viewerID int64, since time.Time, limit int) ([]core.HistoryEntry, error) {
	return nil, nil
}
func (s *stubServices) HistoryEntry(ctx context.Context, id int64) (resolver.HistoryRecord, error) {
	return resolver.HistoryRecord{}, core.ErrNotFound
}
func (s *stubServices) ReleaseHealth(ctx context.Context, releaseID string, titleKey core.RatingKey) (builtin.ReleaseHealth, error) {
	return builtin.ReleaseHealth{Known: releaseID == "rel-1", Blocked: len(s.blocked) > 0}, nil
}
func (s *stubServices) LookupRelease(id string) (core.Release, bool) {
	if id == "rel-1" {
		return core.Release{ID: "rel-1", Name: "Sample.Release.1080p", Indexer: "NZBGeek"}, true
	}
	return core.Release{}, false
}
func (s *stubServices) Block(ctx context.Context, req resolver.BlockRequest, reason string) (resolver.BlockEntry, error) {
	s.blocked = append(s.blocked, req.ReleaseID)
	return resolver.BlockEntry{ReleaseID: req.ReleaseID, Reason: reason}, nil
}
func (s *stubServices) PinRelease(ctx context.Context, titleKey core.RatingKey, releaseID, note string) error {
	return nil
}
func (s *stubServices) Tail(limit int, level, query string) []builtin.LogLine { return nil }
func (s *stubServices) Search(ctx context.Context, query string, types []core.MediaType, limit int) ([]core.Title, error) {
	return nil, nil
}
func (s *stubServices) Get(ctx context.Context, key core.RatingKey) (*core.Title, error) {
	return nil, core.ErrNotFound
}
func (s *stubServices) SnapshotList(ctx context.Context, offset, limit int) ([]core.Collection, error) {
	return nil, nil
}
func (s *stubServices) SnapshotItems(ctx context.Context, key core.RatingKey, offset, limit int) (core.Page, error) {
	return core.Page{}, core.ErrNotFound
}
func (s *stubServices) Generate(ctx context.Context, prompt, mediaType string) (*core.CollectionSpec, string, error) {
	return &core.CollectionSpec{Name: "Draft", MediaType: "movie"}, "", nil
}
func (s *stubServices) CreateSpec(ctx context.Context, sp core.CollectionSpec) (*core.CollectionSpec, error) {
	return &sp, nil
}
func (s *stubServices) Channels(ctx context.Context) ([]livetv.Channel, error) { return nil, nil }
func (s *stubServices) StreamSourceStatuses(ctx context.Context, channelID string) ([]livetv.StreamSourceStatus, error) {
	return nil, nil
}
func (s *stubServices) ImportSource(ctx context.Context, src livetv.Source) (livetv.Source, error) {
	return src, nil
}
func (s *stubServices) Sources(ctx context.Context) ([]builtin.SourceInfo, error) { return nil, nil }
func (s *stubServices) AddStremio(ctx context.Context, addonURL string) (builtin.SourceInfo, error) {
	return builtin.SourceInfo{Name: "stremio", Enabled: true}, nil
}
func (s *stubServices) Plugins(ctx context.Context) ([]builtin.PluginInfo, error) { return nil, nil }
func (s *stubServices) PluginHealth(ctx context.Context, id string) (builtin.PluginInfo, error) {
	return builtin.PluginInfo{}, core.ErrNotFound
}
func (s *stubServices) SetPluginEnabled(ctx context.Context, id string, enabled bool) error {
	return nil
}
func (s *stubServices) Approval(ctx context.Context, id string) (builtin.ApprovalInfo, error) {
	if a, ok := s.approvals[id]; ok {
		return a, nil
	}
	return builtin.ApprovalInfo{}, builtin.ErrApprovalNotFound
}
func (s *stubServices) Profiles(ctx context.Context) ([]builtin.ProfileInfo, error) {
	return []builtin.ProfileInfo{{Name: "Default", Ranking: core.DefaultRankingSteps}}, nil
}
func (s *stubServices) SetRanking(ctx context.Context, profile string, ranking []string) error {
	return nil
}
func (s *stubServices) AddRule(ctx context.Context, profile string, rule core.ReleaseRule) error {
	return nil
}
func (s *stubServices) Frontends(ctx context.Context) ([]builtin.FrontendInfo, error) {
	return []builtin.FrontendInfo{{ID: "plex", Name: "Plex", Enabled: true}}, nil
}
func (s *stubServices) SetFrontendEnabled(ctx context.Context, id string, enabled bool) error {
	return nil
}
func (s *stubServices) Restart(ctx context.Context, whenIdle bool) error { return nil }

type stubResolver struct{ *stubServices }

func (r stubResolver) History(ctx context.Context, f resolver.HistoryFilter) ([]resolver.HistoryRecord, int, error) {
	return nil, 0, nil
}

func newTestCatalogue(t *testing.T, svc *stubServices) *tools.Catalogue {
	t.Helper()
	cat := tools.NewCatalogue()
	deps := builtin.Deps{
		Status:      svc,
		Playback:    svc,
		Resolver:    stubResolver{svc},
		Logs:        svc,
		Catalog:     svc,
		Collections: svc,
		LiveTV:      svc,
		Sources:     svc,
		Plugins:     svc,
		Approvals:   svc,
		Profiles:    svc,
		Frontends:   svc,
		Server:      svc,
		OwnerUser: func(ctx context.Context) (*core.User, error) {
			return &core.User{ID: 1, Username: "owner", IsOwner: true}, nil
		},
		Routes: func() []builtin.RouteInfo {
			return []builtin.RouteInfo{{Route: "/releases", Label: "Releases"}}
		},
	}
	if err := cat.Register(builtin.All(deps)...); err != nil {
		t.Fatal(err)
	}
	return cat
}

func postRPC(t *testing.T, h http.Handler, token string, method string, params any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:32400/ether/mcp", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec, body
}

func TestMCPBoundaryAuthResourcesAndScopes(t *testing.T) {
	ctx := context.Background()
	db := store.OpenTest(t)
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_users(id,plex_id,is_owner,active,created_at,updated_at) VALUES(1,100,1,1,0,0)`); err != nil {
		t.Fatal(err)
	}
	svc := &stubServices{approvals: map[string]builtin.ApprovalInfo{}}
	cat := newTestCatalogue(t, svc)
	agentStore := agent.NewStore(db)

	var audits []mcp.AuditEvent
	var onApprovals []agent.Approval
	srv, err := mcp.New(mcp.Options{
		Catalogue: cat,
		Approvals: agentStore,
		OnApproval: func(ctx context.Context, a agent.Approval) {
			onApprovals = append(onApprovals, a)
			svc.approvals[a.ID] = builtin.ApprovalInfo{
				ID:          a.ID,
				Tool:        a.ToolName,
				Category:    a.Category,
				Description: a.Description,
				Status:      a.Status,
			}
		},
		Authenticate: func(ctx context.Context, token string) (tools.Principal, error) {
			switch token {
			case "tok-read":
				return tools.Principal{ViewerID: 1, IsOwner: true, Surface: "mcp", Scopes: []string{"read"}}, nil
			case "tok-full":
				return tools.Principal{ViewerID: 1, IsOwner: true, Surface: "mcp", Scopes: []string{"full"}}, nil
			default:
				return tools.Principal{}, errors.New("unauthorized")
			}
		},
		Resources: mcp.DefaultResources(""),
		Version:   "1.2.3",
		Audit: func(ctx context.Context, e mcp.AuditEvent) {
			audits = append(audits, e)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	// 1. Boundary checks: DNS rebinding Host rejected with 421; cross-origin Origin rejected with 403.
	badHostReq := httptest.NewRequest(http.MethodPost, "http://evil.example.com/ether/mcp", strings.NewReader(`{}`))
	badHostRec := httptest.NewRecorder()
	h.ServeHTTP(badHostRec, badHostReq)
	if badHostRec.Code != http.StatusMisdirectedRequest {
		t.Fatalf("expected 421 for bad Host, got %d", badHostRec.Code)
	}

	localHostReq := httptest.NewRequest(http.MethodPost, "http://attacker.local/ether/mcp", strings.NewReader(`{}`))
	localHostRec := httptest.NewRecorder()
	h.ServeHTTP(localHostRec, localHostReq)
	if localHostRec.Code != http.StatusMisdirectedRequest {
		t.Fatalf("unconfigured local hostname: %d", localHostRec.Code)
	}

	badOriginReq := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:32400/ether/mcp", strings.NewReader(`{}`))
	badOriginReq.Header.Set("Origin", "https://evil.example.com")
	badOriginRec := httptest.NewRecorder()
	h.ServeHTTP(badOriginRec, badOriginReq)
	if badOriginRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-origin Origin, got %d", badOriginRec.Code)
	}

	// 2. Missing/invalid Bearer token -> 401 with WWW-Authenticate and X-Request-ID + Cache-Control: no-store.
	unauthRec, _ := postRPC(t, h, "", "ping", nil)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", unauthRec.Code)
	}
	if !strings.Contains(unauthRec.Header().Get("WWW-Authenticate"), "Bearer") {
		t.Fatalf("expected WWW-Authenticate header, got %q", unauthRec.Header().Get("WWW-Authenticate"))
	}
	if unauthRec.Header().Get("Cache-Control") != "no-store" || unauthRec.Header().Get("X-Request-ID") == "" {
		t.Fatalf("missing Cache-Control or X-Request-ID headers: %v", unauthRec.Header())
	}

	// 3. initialize, ping, resources/list, resources/read.
	initRec, initBody := postRPC(t, h, "tok-read", "initialize", map[string]any{"protocolVersion": "2025-06-18"})
	if initRec.Code != http.StatusOK {
		t.Fatalf("initialize status = %d", initRec.Code)
	}
	initRes := initBody["result"].(map[string]any)
	if initRes["protocolVersion"] != "2025-06-18" {
		t.Fatalf("unexpected protocolVersion: %v", initRes["protocolVersion"])
	}

	_, resListBody := postRPC(t, h, "tok-read", "resources/list", nil)
	resources := resListBody["result"].(map[string]any)["resources"].([]any)
	if len(resources) != 5 {
		t.Fatalf("expected 5 static resources, got %d", len(resources))
	}

	_, schemaReadBody := postRPC(t, h, "tok-read", "resources/read", map[string]any{"uri": "ether://schemas/plugin-manifest"})
	contents := schemaReadBody["result"].(map[string]any)["contents"].([]any)
	schemaText := contents[0].(map[string]any)["text"].(string)
	var parsedSchema map[string]any
	if err := json.Unmarshal([]byte(schemaText), &parsedSchema); err != nil || parsedSchema["title"] == nil {
		t.Fatalf("expected valid embedded plugin-manifest.v1.json schema, got err=%v text=%q", err, schemaText)
	}

	// 4. Scope isolation: read token sees 16 tools and cannot call write tools.
	_, listReadBody := postRPC(t, h, "tok-read", "tools/list", nil)
	readTools := listReadBody["result"].(map[string]any)["tools"].([]any)
	if len(readTools) != 16 {
		t.Fatalf("expected 16 visible tools for read scope, got %d", len(readTools))
	}
	_, deniedCall := postRPC(t, h, "tok-read", "tools/call", map[string]any{
		"name":      "release_blocklist",
		"arguments": map[string]any{"releaseId": "rel-1"},
	})
	if deniedCall["error"] == nil {
		t.Fatalf("expected error calling write tool with read scope, got %v", deniedCall)
	}

	// 5. Full scope sees all 26 tools; calling a write tool creates a pending approval without mutating state.
	_, listFullBody := postRPC(t, h, "tok-full", "tools/list", nil)
	fullTools := listFullBody["result"].(map[string]any)["tools"].([]any)
	if len(fullTools) != 26 {
		t.Fatalf("expected 26 visible tools for full scope, got %d", len(fullTools))
	}

	_, propBody := postRPC(t, h, "tok-full", "tools/call", map[string]any{
		"name":      "release_blocklist",
		"arguments": map[string]any{"releaseId": "rel-1", "reason": "bad audio sync"},
	})
	propResult := propBody["result"].(map[string]any)
	structured := propResult["structuredContent"].(map[string]any)
	if structured["status"] != "pending_approval" {
		t.Fatalf("expected pending_approval status, got %v", structured)
	}
	approvalID, _ := structured["approvalId"].(string)
	if approvalID == "" {
		t.Fatalf("expected non-empty approvalId, got %v", structured)
	}
	if len(svc.blocked) != 0 {
		t.Fatalf("write tool mutated state before approval: %v", svc.blocked)
	}
	if len(onApprovals) != 1 || onApprovals[0].ID != approvalID {
		t.Fatalf("expected OnApproval callback for %s, got %+v", approvalID, onApprovals)
	}

	// Polling approval_status over MCP returns the pending approval.
	_, statusBody := postRPC(t, h, "tok-read", "tools/call", map[string]any{
		"name":      "approval_status",
		"arguments": map[string]any{"approvalId": approvalID},
	})
	statusStructured := statusBody["result"].(map[string]any)["structuredContent"].(map[string]any)
	if statusStructured["status"] != "pending" || statusStructured["approvalId"] != approvalID {
		t.Fatalf("unexpected approval_status: %v", statusStructured)
	}

	// 6. source_add_stremio over MCP requires console approval.
	_, secBody := postRPC(t, h, "tok-full", "tools/call", map[string]any{
		"name":      "source_add_stremio",
		"arguments": map[string]any{},
	})
	secStructured := secBody["result"].(map[string]any)["structuredContent"].(map[string]any)
	if secStructured["status"] != "pending_approval" {
		t.Fatalf("expected pending_approval, got %v", secStructured)
	}

	if len(audits) == 0 {
		t.Fatal("expected audit events to be recorded")
	}
}

func TestMCPPairingExchangeAndRevocation(t *testing.T) {
	ctx := context.Background()
	db := store.OpenTest(t)
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_users(id,plex_id,is_owner,active,created_at,updated_at) VALUES(1,100,1,1,0,0)`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	members := testMembers{db}
	grants, err := identity.NewGrants(db, members, 24*time.Hour, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	qc, err := identity.NewQuickConnect(db, 10*time.Minute, 24*time.Hour, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := mcp.NewPairing(mcp.PairingOptions{
		DB:           db,
		Grants:       grants,
		QuickConnect: qc,
		Authorize: func(ctx context.Context) (int64, error) {
			return 1, nil
		},
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}

	consoleHandler := pairing.Handler()
	exchangeHandler := pairing.ExchangeHandler()

	// 1. Pair a client with full scope and exchange the code for a grant token.
	pairReq := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:32400/pair", strings.NewReader(`{"name":"Claude Code","scope":"full"}`))
	pairRec := httptest.NewRecorder()
	consoleHandler.ServeHTTP(pairRec, pairReq)
	if pairRec.Code != http.StatusCreated {
		t.Fatalf("POST /pair status = %d: %s", pairRec.Code, pairRec.Body.String())
	}
	var pairData map[string]any
	_ = json.Unmarshal(pairRec.Body.Bytes(), &pairData)
	code, _ := pairData["code"].(string)
	if code == "" {
		t.Fatalf("expected pairing code, got %v", pairData)
	}

	exReq := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:32400/ether/mcp/exchange",
		strings.NewReader(fmt.Sprintf(`{"code":%q}`, code)))
	exReq.Header.Set("Content-Type", "application/json")
	exReq.RemoteAddr = "192.168.1.50:54321"
	exRec := httptest.NewRecorder()
	exchangeHandler.ServeHTTP(exRec, exReq)
	if exRec.Code != http.StatusOK {
		t.Fatalf("POST /ether/mcp/exchange status = %d: %s", exRec.Code, exRec.Body.String())
	}
	var exData map[string]any
	_ = json.Unmarshal(exRec.Body.Bytes(), &exData)
	token, _ := exData["token"].(string)
	if !identity.IsGrantCredential(token) {
		t.Fatalf("expected ethg_ grant credential, got %q", token)
	}

	// Single-use: exchanging the same code a second time fails with 401.
	exReq2 := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:32400/ether/mcp/exchange",
		strings.NewReader(fmt.Sprintf(`{"code":%q}`, code)))
	exReq2.Header.Set("Content-Type", "application/json")
	exReq2.RemoteAddr = "192.168.1.50:54321"
	exRec2 := httptest.NewRecorder()
	exchangeHandler.ServeHTTP(exRec2, exReq2)
	if exRec2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on second exchange of same code, got %d", exRec2.Code)
	}

	// Authenticate with the exchanged grant token.
	principal, err := pairing.Authenticate(ctx, token)
	if err != nil || !principal.IsOwner || !principal.HasScope("full") {
		t.Fatalf("Authenticate(token) = %+v, err=%v", principal, err)
	}

	// Pairing codes never authenticate, before or after exchange.
	if _, err := pairing.Authenticate(ctx, code); err == nil {
		t.Fatal("redeemed code authenticated")
	}
	pairReq2 := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:32400/pair", strings.NewReader(`{"name":"Cursor","scope":"read"}`))
	pairRec2 := httptest.NewRecorder()
	consoleHandler.ServeHTTP(pairRec2, pairReq2)
	var pairData2 map[string]any
	_ = json.Unmarshal(pairRec2.Body.Bytes(), &pairData2)
	code2 := pairData2["code"].(string)
	if _, err := pairing.Authenticate(ctx, code2); err == nil {
		t.Fatal("unredeemed code authenticated")
	}
	req2 := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:32400/ether/mcp/exchange", strings.NewReader(fmt.Sprintf(`{"code":%q}`, code2)))
	req2.Header.Set("Content-Type", "application/json")
	req2.RemoteAddr = "192.168.1.51:1234"
	rec2 := httptest.NewRecorder()
	exchangeHandler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("read exchange: %d %s", rec2.Code, rec2.Body.String())
	}
	var data2 map[string]any
	_ = json.Unmarshal(rec2.Body.Bytes(), &data2)
	token2 := data2["token"].(string)
	principal2, err := pairing.Authenticate(ctx, token2)
	if err != nil || principal2.HasScope("full") || !principal2.HasScope("read") {
		t.Fatalf("read principal: %+v %v", principal2, err)
	}

	// 3. List paired clients via GET /clients and revoke both via DELETE /clients/{id}.
	listReq := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:32400/clients", nil)
	listRec := httptest.NewRecorder()
	consoleHandler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("GET /clients status = %d", listRec.Code)
	}
	var listData map[string]any
	_ = json.Unmarshal(listRec.Body.Bytes(), &listData)
	clients := listData["clients"].([]any)
	if len(clients) != 2 {
		t.Fatalf("expected 2 paired clients, got %d", len(clients))
	}
	for _, item := range clients {
		cid := item.(map[string]any)["id"].(string)
		delReq := httptest.NewRequest(http.MethodDelete, "http://127.0.0.1:32400/clients/"+cid, nil)
		delRec := httptest.NewRecorder()
		consoleHandler.ServeHTTP(delRec, delReq)
		if delRec.Code != http.StatusNoContent {
			t.Fatalf("DELETE /clients/%s status = %d", cid, delRec.Code)
		}
	}

	// Both credentials must fail after revocation.
	if _, err := pairing.Authenticate(ctx, token); err == nil {
		t.Fatal("expected revoked token to fail Authenticate")
	}
	if _, err := pairing.Authenticate(ctx, token2); err == nil {
		t.Fatal("expected revoked read grant to fail Authenticate")
	}

	// Concurrent exchange attempts can issue only one grant.
	concurrentPair := httptest.NewRecorder()
	consoleHandler.ServeHTTP(concurrentPair, httptest.NewRequest(http.MethodPost, "http://127.0.0.1:32400/pair", strings.NewReader(`{}`)))
	var concurrentData map[string]any
	_ = json.Unmarshal(concurrentPair.Body.Bytes(), &concurrentData)
	concurrentCode := concurrentData["code"].(string)
	start := make(chan struct{})
	statuses := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func() {
			req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:32400/ether/mcp/exchange", strings.NewReader(fmt.Sprintf(`{"code":%q}`, concurrentCode)))
			req.Header.Set("Content-Type", "application/json")
			req.RemoteAddr = "10.0.0.44:1234"
			rec := httptest.NewRecorder()
			<-start
			exchangeHandler.ServeHTTP(rec, req)
			statuses <- rec.Code
		}()
	}
	close(start)
	success := 0
	for i := 0; i < 2; i++ {
		if <-statuses == http.StatusOK {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("concurrent successful exchanges = %d", success)
	}

	// Expired codes cannot be exchanged or used as bearer credentials.
	expiredPair := httptest.NewRecorder()
	consoleHandler.ServeHTTP(expiredPair, httptest.NewRequest(http.MethodPost, "http://127.0.0.1:32400/pair", strings.NewReader(`{}`)))
	var expiredData map[string]any
	_ = json.Unmarshal(expiredPair.Body.Bytes(), &expiredData)
	expiredCode := expiredData["code"].(string)
	now = now.Add(11 * time.Minute)
	expiredReq := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:32400/ether/mcp/exchange", strings.NewReader(fmt.Sprintf(`{"code":%q}`, expiredCode)))
	expiredReq.Header.Set("Content-Type", "application/json")
	expiredRec := httptest.NewRecorder()
	exchangeHandler.ServeHTTP(expiredRec, expiredReq)
	if expiredRec.Code != http.StatusUnauthorized {
		t.Fatalf("expired exchange = %d", expiredRec.Code)
	}
	if _, err := pairing.Authenticate(ctx, expiredCode); err == nil {
		t.Fatal("expired code authenticated")
	}
	hostile := httptest.NewRequest(http.MethodPost, "http://evil.example/ether/mcp/exchange", strings.NewReader(`{"code":"BADCODE"}`))
	hostile.Header.Set("Content-Type", "application/json")
	hostileRec := httptest.NewRecorder()
	exchangeHandler.ServeHTTP(hostileRec, hostile)
	if hostileRec.Code != http.StatusMisdirectedRequest {
		t.Fatalf("hostile host = %d", hostileRec.Code)
	}

	// 4. Rate limit on /ether/mcp/exchange: >10 attempts per minute from one IP -> 429.
	for i := 0; i < 10; i++ {
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:32400/ether/mcp/exchange", strings.NewReader(`{"code":"BADCODE"}`))
		r.Header.Set("Content-Type", "application/json")
		r.RemoteAddr = "10.0.0.99:12345"
		w := httptest.NewRecorder()
		exchangeHandler.ServeHTTP(w, r)
	}
	r11 := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:32400/ether/mcp/exchange", strings.NewReader(`{"code":"BADCODE"}`))
	r11.Header.Set("Content-Type", "application/json")
	r11.RemoteAddr = "10.0.0.99:12345"
	w11 := httptest.NewRecorder()
	exchangeHandler.ServeHTTP(w11, r11)
	if w11.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on 11th exchange attempt, got %d", w11.Code)
	}
}
