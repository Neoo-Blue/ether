package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/identity"
	"github.com/braedonsaunders/ether/internal/resolver"
)

func TestMCPBuiltinToolsAndEndToEndWorkflow(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfg, err := config.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Update(func(c *config.Config) error {
		c.SetupComplete = true
		c.Frontends = map[string]config.Frontend{"plex": {Enabled: false}}
		c.Transcode.FFmpegPath = "/not-installed/ffmpeg"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	a, err := New(ctx, Options{
		DataDir: dir,
		Version: "2.0.0-test",
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)

	// 1. Verify all 26 built-in tools are registered in both the tool catalogue
	// and the descriptor registry (extend.KindTool).
	toolDescs := a.extendRT.registry.List(extend.KindTool)
	if len(toolDescs) != 26 {
		t.Fatalf("expected 26 extend.KindTool descriptors, got %d", len(toolDescs))
	}

	// Create local owner account and issue an owner console token.
	local, err := identity.NewLocal(a.db)
	if err != nil {
		t.Fatal(err)
	}
	ownerID, err := local.CreateOwner(ctx, "owner", "correct-horse-battery-staple")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.users.RefreshViewers(ctx); err != nil {
		t.Fatal(err)
	}
	grants, err := identity.NewGrants(a.db, a.users, time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	ownerToken, err := grants.Issue(ctx, ownerID, nil, "owner", core.Device{ID: "console-test"})
	if err != nil {
		t.Fatal(err)
	}

	// Direct or tokenless setup access must not mint owner grants.
	pairingHandler, err := a.buildMCPPairing()
	if err != nil {
		t.Fatal(err)
	}
	denied := httptest.NewRecorder()
	pairingHandler.Handler().ServeHTTP(denied, httptest.NewRequest(http.MethodPost, "http://localhost/pair", strings.NewReader(`{}`)))
	if denied.Code != http.StatusForbidden {
		t.Fatalf("missing owner access: %d", denied.Code)
	}

	// Seed a title, playback history, resolver history trace, and a server log entry.
	movieKey := core.MovieKey(550)
	title := core.Title{
		Key:           movieKey,
		Type:          core.TypeMovie,
		IDs:           core.IDs{TMDB: 550, IMDB: "tt0113277"},
		Title:         "Heat",
		Year:          1995,
		Summary:       "A group of professional bank robbers start to feel the heat from LAPD.",
		ContentRating: "R",
		Rating:        8.3,
		Popularity:    90,
		VoteCount:     5000,
		Released:      time.Date(1995, 12, 15, 0, 0, 0, 0, time.UTC),
	}
	if err := a.catalog.UpsertTitle(ctx, title, true); err != nil {
		t.Fatal(err)
	}
	if err := a.state.Scrobble(ctx, ownerID, movieKey); err != nil {
		t.Fatal(err)
	}

	trace := resolver.Trace{
		At:             time.Now().Add(-2 * time.Hour).UTC(),
		TitleKey:       movieKey,
		TitleName:      "Heat (1995)",
		Profile:        "Balanced",
		Winner:         "rel-heat-bad",
		SearchDuration: 110 * time.Millisecond,
		TotalDuration:  290 * time.Millisecond,
		Candidates: []resolver.CandidateTrace{
			{
				ReleaseID:     "rel-heat-bad",
				Name:          "Heat.1995.2160p.WEB-DL.DDP5.1.H.265-BADGRP",
				Indexer:       "NZBGeek",
				Score:         94,
				Size:          16 << 30,
				Height:        2160,
				Attempted:     true,
				HealthChecked: true,
				Healthy:       false,
				Reason:        "missing articles on primary provider during stream",
			},
			{
				ReleaseID:     "rel-heat-good",
				Name:          "Heat.1995.1080p.BluRay.x264-SPARKS",
				Indexer:       "DrunkenSlug",
				Score:         88,
				Size:          9 << 30,
				Height:        1080,
				Attempted:     false,
				HealthChecked: true,
				Healthy:       true,
			},
		},
	}
	rawTrace, _ := json.Marshal(trace)
	if _, err := a.db.ExecContext(ctx,
		`INSERT INTO resolver_history(resolved_at, rating_key, title_name, profile, release_id, release_name, indexer, size, error, failover, pinned, candidates, trace)
		 VALUES (?,?,?,?,?,?,?,?,?,0,0,2,?)`,
		trace.At.Unix(), int64(movieKey), trace.TitleName, trace.Profile,
		"rel-heat-bad", "Heat.1995.2160p.WEB-DL.DDP5.1.H.265-BADGRP", "NZBGeek", int64(16<<30),
		"article missing during playback", string(rawTrace)); err != nil {
		t.Fatal(err)
	}
	a.log.Warn("stream stalled on missing article", "title", "Heat", "release", "rel-heat-bad")

	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)

	// 2. Pair an MCP client via the console API and exchange the pairing code.
	pairReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/ether/api/agent/mcp/pair",
		strings.NewReader(`{"name":"Claude Desktop","scope":"full"}`))
	pairReq.Header.Set("Authorization", "Bearer "+ownerToken)
	pairReq.Header.Set("Content-Type", "application/json")
	pairReq.Header.Set("X-Ether-Request", "1")
	pairResp, err := http.DefaultClient.Do(pairReq)
	if err != nil {
		t.Fatal(err)
	}
	defer pairResp.Body.Close()
	if pairResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(pairResp.Body)
		t.Fatalf("POST /ether/api/agent/mcp/pair status = %d: %s", pairResp.StatusCode, string(body))
	}
	var pairPayload struct {
		Code     string `json:"code"`
		Endpoint string `json:"endpoint"`
		Exchange string `json:"exchange"`
	}
	if err := json.NewDecoder(pairResp.Body).Decode(&pairPayload); err != nil {
		t.Fatal(err)
	}
	if pairPayload.Code == "" {
		t.Fatal("empty pairing code")
	}

	exResp, err := http.Post(srv.URL+"/ether/mcp/exchange", "application/json",
		strings.NewReader(fmt.Sprintf(`{"code":%q}`, pairPayload.Code)))
	if err != nil {
		t.Fatal(err)
	}
	defer exResp.Body.Close()
	if exResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(exResp.Body)
		t.Fatalf("POST /ether/mcp/exchange status = %d: %s", exResp.StatusCode, string(body))
	}
	var exPayload struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(exResp.Body).Decode(&exPayload); err != nil {
		t.Fatal(err)
	}
	mcpToken := exPayload.Token
	if !identity.IsGrantCredential(mcpToken) {
		t.Fatalf("expected ethg_ token, got %q", mcpToken)
	}

	mcpCall := func(toolName string, args map[string]any) map[string]any {
		t.Helper()
		rpcBody, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      toolName,
				"arguments": args,
			},
		})
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/ether/mcp", bytes.NewReader(rpcBody))
		req.Header.Set("Authorization", "Bearer "+mcpToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("MCP tools/call %s HTTP %d: %s", toolName, resp.StatusCode, string(b))
		}
		var out struct {
			Result struct {
				StructuredContent map[string]any `json:"structuredContent"`
				IsError           bool           `json:"isError"`
			} `json:"result"`
			Error *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		if out.Error != nil {
			t.Fatalf("MCP tools/call %s RPC error %d: %s", toolName, out.Error.Code, out.Error.Message)
		}
		if out.Result.IsError {
			t.Fatalf("MCP tools/call %s returned tool error: %v", toolName, out.Result.StructuredContent)
		}
		return out.Result.StructuredContent
	}

	// 3. Answer "why did Heat buffer last night" using read tools over /ether/mcp.
	searchRes := mcpCall("catalog_search", map[string]any{"query": "Heat"})
	results, _ := searchRes["results"].([]any)
	if len(results) == 0 {
		t.Fatalf("catalog_search returned no results: %v", searchRes)
	}
	foundKey := results[0].(map[string]any)["key"].(string)
	if foundKey != fmt.Sprint(int64(movieKey)) {
		t.Fatalf("catalog_search key = %q, want %d", foundKey, movieKey)
	}

	histRes := mcpCall("playback_history", map[string]any{"sinceHours": 24})
	entries, _ := histRes["entries"].([]any)
	if len(entries) == 0 {
		t.Fatalf("playback_history returned no entries: %v", histRes)
	}

	traceRes := mcpCall("resolve_trace", map[string]any{"titleKey": foundKey})
	if traceRes["releaseId"] != "rel-heat-bad" {
		t.Fatalf("resolve_trace winner = %v, want rel-heat-bad", traceRes["releaseId"])
	}

	relHealth := mcpCall("release_health", map[string]any{"releaseId": "rel-heat-bad", "titleKey": foundKey})
	if relHealth["status"] != "unhealthy" {
		t.Fatalf("release_health status = %v, want unhealthy (%v)", relHealth["status"], relHealth)
	}

	logsRes := mcpCall("logs_tail", map[string]any{"query": "rel-heat-bad"})
	logLines, _ := logsRes["lines"].([]any)
	if len(logLines) == 0 {
		t.Fatalf("expected logs_tail to find stall log line, got %v", logsRes)
	}

	// 4. Call release_blocklist over MCP: must return pending_approval without blocklisting yet.
	propRes := mcpCall("release_blocklist", map[string]any{
		"releaseId": "rel-heat-bad",
		"reason":    "buffered last night due to missing articles",
	})
	if propRes["status"] != "pending_approval" {
		t.Fatalf("expected pending_approval from release_blocklist, got %v", propRes)
	}
	approvalID, _ := propRes["approvalId"].(string)
	if approvalID == "" {
		t.Fatalf("missing approvalId in %v", propRes)
	}
	if blocked, err := a.resolver.ReleaseBlocked(ctx, "rel-heat-bad"); err != nil || blocked {
		t.Fatalf("release should not be blocked before owner approval (blocked=%v err=%v)", blocked, err)
	}

	// 5. Approve the proposal in the console and verify approval_status + blocklist effect.
	approveReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/ether/api/agent/approvals/"+approvalID,
		strings.NewReader(`{"decision":"approved","note":"Block bad release"}`))
	approveReq.Header.Set("Authorization", "Bearer "+ownerToken)
	approveReq.Header.Set("Content-Type", "application/json")
	approveReq.Header.Set("X-Ether-Request", "1")
	approveResp, err := http.DefaultClient.Do(approveReq)
	if err != nil {
		t.Fatal(err)
	}
	defer approveResp.Body.Close()
	if approveResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(approveResp.Body)
		t.Fatalf("POST /ether/api/agent/approvals/%s status = %d: %s", approvalID, approveResp.StatusCode, string(b))
	}

	apprStatus := mcpCall("approval_status", map[string]any{"approvalId": approvalID})
	if apprStatus["status"] != "approved" {
		t.Fatalf("approval_status after approval = %v, want approved", apprStatus)
	}
	if blocked, err := a.resolver.ReleaseBlocked(ctx, "rel-heat-bad"); err != nil || !blocked {
		t.Fatalf("release should be blocked after owner approval (blocked=%v err=%v)", blocked, err)
	}
	// MCP secret-requiring writes wait for approval and then present a secret card.
	sourceCount := len(a.cfg.Get().Indexers)
	secretProposal := mcpCall("source_add_stremio", map[string]any{})
	if secretProposal["status"] != "pending_approval" {
		t.Fatalf("secret proposal: %v", secretProposal)
	}
	secretApproval := secretProposal["approvalId"].(string)
	var threadID string
	if err := a.db.QueryRowContext(ctx, `SELECT thread_id FROM agent_approvals WHERE id=?`, secretApproval).Scan(&threadID); err != nil {
		t.Fatal(err)
	}
	approveSecret, _ := http.NewRequest(http.MethodPost, srv.URL+"/ether/api/agent/approvals/"+secretApproval, strings.NewReader(`{"decision":"approved"}`))
	approveSecret.Header.Set("Authorization", "Bearer "+ownerToken)
	approveSecret.Header.Set("Content-Type", "application/json")
	approveSecret.Header.Set("X-Ether-Request", "1")
	approveSecretResp, err := http.DefaultClient.Do(approveSecret)
	if err != nil {
		t.Fatal(err)
	}
	approveSecretResp.Body.Close()
	if approveSecretResp.StatusCode != http.StatusOK {
		t.Fatalf("secret approval: %d", approveSecretResp.StatusCode)
	}
	pendingSecretStatus := mcpCall("approval_status", map[string]any{"approvalId": secretApproval})
	if pendingSecretStatus["status"] != "pending_secret" {
		t.Fatalf("source added prematurely: %v", pendingSecretStatus)
	}
	var requestID string
	if err := a.db.QueryRowContext(ctx, `SELECT id FROM agent_secret_requests WHERE thread_id=? AND status='pending'`, threadID).Scan(&requestID); err != nil {
		t.Fatal(err)
	}
	if len(a.cfg.Get().Indexers) != sourceCount {
		t.Fatal("source added before secret submission")
	}
	const secretURL = "https://example.invalid/key123/manifest.json"
	secretReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/ether/api/agent/secrets/"+requestID, strings.NewReader(`{"value":"`+secretURL+`"}`))
	secretReq.Header.Set("Authorization", "Bearer "+ownerToken)
	secretReq.Header.Set("Content-Type", "application/json")
	secretReq.Header.Set("X-Ether-Request", "1")
	secretResp, err := http.DefaultClient.Do(secretReq)
	if err != nil {
		t.Fatal(err)
	}
	secretResp.Body.Close()
	if secretResp.StatusCode != http.StatusOK {
		t.Fatalf("secret submit: %d", secretResp.StatusCode)
	}
	if len(a.cfg.Get().Indexers) != sourceCount+1 {
		t.Fatal("source was not added after secret submission")
	}
	storedSecretStatus := mcpCall("approval_status", map[string]any{"approvalId": secretApproval})
	if storedSecretStatus["status"] != "approved" {
		t.Fatalf("source approval after submit: %v", storedSecretStatus)
	}
	var leaked int
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_messages WHERE thread_id=? AND parts LIKE ?`, threadID, "%key123%").Scan(&leaked); err != nil || leaked != 0 {
		t.Fatalf("secret in transcript: %d %v", leaked, err)
	}

}
