package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/agent/tools"
	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/notify"
)

func TestNotificationAndEventFixture(t *testing.T) {
	env := newTestEnv(t)
	sealer, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := sealer.UpgradeCredentials(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc := notify.New(env.m.store.db, notify.Options{Sealer: sealer})
	env.m.deps.Notifications = svc
	var man map[string]any
	if err := json.Unmarshal(helloManifest("1.0.0"), &man); err != nil {
		t.Fatal(err)
	}
	eth := man["ether"].(map[string]any)
	eth["kind"] = "notification_channel"
	eth["capabilities"] = []string{"notify", "events"}
	eth["contributes"] = map[string]any{
		"notification": map[string]any{"sendTest": true, "fields": []any{map[string]any{"key": "credential", "kind": "secret", "label": "Credential", "required": true}}},
		"events":       map[string]any{"subscribe": []string{"playback.started"}},
	}
	raw, _ := json.Marshal(man)
	p, err := env.m.PreviewInstall(context.Background(), Source{Kind: "studio", Manifest: raw, Files: helloFiles()})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := env.m.Install(context.Background(), p, InstallOptions{AcceptAddedPermissions: true})
	if err != nil {
		t.Fatal(err)
	}
	kind := extend.PluginID(rec.ID, "notify")
	if err := svc.Save(context.Background(), notify.Channel{ID: "fixture", Name: "Fixture", Kind: kind, Enabled: true, Events: []string{"*"}, Secrets: map[string]string{"credential": "sensitive"}}); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := env.m.store.db.QueryRow(`SELECT config FROM notify_channels WHERE id='fixture'`).Scan(&stored); err != nil || strings.Contains(stored, "sensitive") {
		t.Fatalf("channel secret was not sealed: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/fixture/test", nil)
	out := httptest.NewRecorder()
	svc.Handler().ServeHTTP(out, req)
	if out.Code != 200 {
		t.Fatalf("test notification: %d %s", out.Code, out.Body.String())
	}
	env.m.Publish(context.Background(), Event{Name: EventPlaybackStarted})
	if err := env.m.DeliverOutbox(context.Background(), rec.ID); err != nil {
		t.Fatal(err)
	}
	var foundNotify, foundEvent, foundConfigure bool
	env.fake.mu.Lock()
	for _, c := range env.fake.calls {
		if c.Path == "/ether/configure" && bytes.Contains(c.Body, []byte("sensitive")) {
			foundConfigure = true
		}
		if c.Path == "/ether/notify" {
			foundNotify = !bytes.Contains(c.Body, []byte("sensitive"))
		}
		if c.Path == "/ether/event" {
			foundEvent = true
		}
	}
	env.fake.mu.Unlock()
	if !foundNotify || !foundEvent || !foundConfigure {
		t.Fatalf("fixture notify=%v event=%v configure=%v", foundNotify, foundEvent, foundConfigure)
	}
	if err := env.m.SetShadow(context.Background(), rec.ID, true); err != nil {
		t.Fatal(err)
	}
	before := len(env.fake.paths())
	shadow := httptest.NewRecorder()
	svc.Handler().ServeHTTP(shadow, httptest.NewRequest(http.MethodPost, "/fixture/test", nil))
	entries, err := env.m.Journal(context.Background(), rec.ID, 10, true)
	if shadow.Code != 200 || len(env.fake.paths()) != before || err != nil || len(entries) != 1 || entries[0].Op != "notify" {
		t.Fatalf("shadow delivery: %d %+v %v", shadow.Code, entries, err)
	}
	var leaked int
	if err := env.m.store.db.QueryRow(`SELECT COUNT(*) FROM plugins_calls WHERE request LIKE '%sensitive%' OR response LIKE '%sensitive%'`).Scan(&leaked); err != nil || leaked != 0 {
		t.Fatalf("secret leaked: %d %v", leaked, err)
	}
	if err := env.m.SetEnabled(context.Background(), rec.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := svc.Kinds().Get(kind); ok {
		t.Fatal("kind remains registered")
	}
	if err := env.m.SetEnabled(context.Background(), rec.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, ok := svc.Kinds().Get(kind); !ok {
		t.Fatal("kind was not restored")
	}
	if err := env.m.Remove(context.Background(), rec.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := svc.Kinds().Get(kind); ok {
		t.Fatal("removed plugin kind remains registered")
	}
}

func TestEventBatchPartialAckAndLeader(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	rec := env.installHello(t, "1.0.0")
	for i := 0; i < 55; i++ {
		env.m.Publish(ctx, Event{ID: fmt.Sprintf("id-%d", i), Name: EventPlaybackStarted})
	}
	var batches []int
	env.fake.routes["/ether/event"] = func(req Request) (Response, error) {
		var v struct {
			Events []Event `json:"events"`
		}
		_ = json.Unmarshal(req.Body, &v)
		batches = append(batches, len(v.Events))
		ids := []string{}
		for _, ev := range v.Events {
			ids = append(ids, ev.ID)
		}
		if len(batches) == 1 {
			ids = ids[:1]
		}
		b, _ := json.Marshal(map[string]any{"ack": ids})
		return Response{Status: 200, Body: b}, nil
	}
	env.lead = false
	env.m.runOnce(ctx, map[string]time.Time{})
	if len(batches) != 0 {
		t.Fatal("follower delivered events")
	}
	env.lead = true
	env.m.runOnce(ctx, map[string]time.Time{})
	if len(batches) != 1 || batches[0] != 50 {
		t.Fatalf("partial batch %v", batches)
	}
	if err := env.m.DeliverOutbox(ctx, rec.ID); err != nil {
		t.Fatal(err)
	}
	if len(batches) != 3 || batches[1] != 50 || batches[2] != 4 {
		t.Fatalf("batch sizes %v", batches)
	}
	evs, _, err := env.m.store.outboxBatch(ctx, rec.ID, 100)
	if err != nil || len(evs) != 0 {
		t.Fatalf("remaining %d %v", len(evs), err)
	}
}

func TestPluginToolCatalogueValidationAndBound(t *testing.T) {
	env := newTestEnv(t)
	rec := env.installHello(t, "1.0.0")
	tl := ToolMeta{Name: "test_tool", Title: "Test tool", Description: "Test", InputSchema: json.RawMessage(`{"type":"object","required":["value"],"properties":{"value":{"type":"string"}}}`)}
	tool := env.m.pluginTool(rec, tl)
	if err := env.tools.ReplaceOwner(rec.ID, []tools.Tool{tool}); err != nil {
		t.Fatal(err)
	}
	got, ok := env.tools.Lookup(context.Background(), tools.Principal{IsOwner: true, Surface: "console"}, tool.Name)
	if !ok {
		t.Fatal("tool missing")
	}
	if _, err := got.Execute(context.Background(), tools.Principal{}, map[string]any{}); err == nil {
		t.Fatal("invalid input accepted")
	}
	called := false
	env.fake.routes["/ether/tool/test_tool"] = func(Request) (Response, error) {
		called = true
		return Response{Status: 200, Body: []byte(`{"value":"` + strings.Repeat("x", 65536) + `"}`)}, nil
	}
	result, err := got.Execute(context.Background(), tools.Principal{}, map[string]any{"value": "yes"})
	if err != nil || result.Proposal == nil || called {
		t.Fatalf("write did not propose: %+v %v", result, err)
	}
	if _, err := result.Proposal.Apply(context.Background()); err == nil || !called {
		t.Fatal("oversized output accepted")
	}
	tl.ReadOnly = true
	tool = env.m.pluginTool(rec, tl)
	if err := env.tools.ReplaceOwner(rec.ID, []tools.Tool{tool}); err != nil {
		t.Fatal(err)
	}
	mcp, ok := env.tools.Lookup(context.Background(), tools.Principal{IsOwner: true, Surface: "mcp", Scopes: []string{"read"}}, tool.Name)
	if !ok {
		t.Fatal("read tool missing from MCP catalogue")
	}
	env.fake.routes["/ether/tool/test_tool"] = func(Request) (Response, error) {
		return Response{Status: 200, Body: []byte(`{"message":"ok","api_key":"private"}`)}, nil
	}
	read, err := mcp.Execute(context.Background(), tools.Principal{}, map[string]any{"value": "yes"})
	if err != nil || read.Data["message"] != "ok" || read.Data["api_key"] != "Saved" {
		t.Fatalf("read tool: %+v %v", read, err)
	}
}

func TestPluginViewActionProposal(t *testing.T) {
	env := newTestEnv(t)
	rec := env.installHello(t, "1.0.0")
	env.fake.routes["/ether/view/home"] = func(Request) (Response, error) {
		return Response{Status: 200, Body: []byte(`{"spec":{"specVersion":1,"layout":"list","header":[],"body":[]},"data":{"count":2}}`)}, nil
	}
	env.fake.routes["/ether/action/change"] = func(Request) (Response, error) {
		return Response{Status: 200, Body: []byte(`{"refresh":true,"proposal":{"id":"plugin-task","title":"Change things","lines":["One change"]}}`)}, nil
	}
	var decisions []string
	env.fake.routes["/ether/proposal/plugin-task"] = func(req Request) (Response, error) {
		decisions = append(decisions, string(req.Body))
		return Response{Status: 200, Body: []byte(`{"refresh":true}`)}, nil
	}
	h := env.m.Handler()
	view := httptest.NewRecorder()
	h.ServeHTTP(view, httptest.NewRequest("GET", "/"+rec.ID+"/views/home", nil))
	if view.Code != 200 || !strings.Contains(view.Body.String(), `"spec"`) || !strings.Contains(view.Body.String(), `"data"`) {
		t.Fatalf("view: %d %s", view.Code, view.Body.String())
	}
	rec.Manifest.Ether.Contributes.Slots = append(rec.Manifest.Ether.Contributes.Slots, SlotMeta{Key: "panel", Route: "/ether/view/home"})
	rec.ManifestRaw, _ = json.Marshal(rec.Manifest)
	if err := env.m.store.upsert(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	slot := httptest.NewRecorder()
	h.ServeHTTP(slot, httptest.NewRequest("GET", "/"+rec.ID+"/views/panel", nil))
	if slot.Code != 200 {
		t.Fatalf("slot view: %d %s", slot.Code, slot.Body.String())
	}
	action := httptest.NewRecorder()
	h.ServeHTTP(action, httptest.NewRequest("POST", "/"+rec.ID+"/actions/change", strings.NewReader(`{}`)))
	var response struct {
		Proposal struct {
			ID string `json:"id"`
		} `json:"proposal"`
	}
	if err := json.Unmarshal(action.Body.Bytes(), &response); err != nil || action.Code != 200 || response.Proposal.ID == "" || response.Proposal.ID == "plugin-task" {
		t.Fatalf("action %d %s: %v", action.Code, action.Body.String(), err)
	}
	path := "/" + rec.ID + "/proposals/" + response.Proposal.ID
	result := httptest.NewRecorder()
	h.ServeHTTP(result, httptest.NewRequest("POST", path, strings.NewReader(`{"decision":"approved","note":"yes"}`)))
	if result.Code != 200 || len(decisions) != 1 || !strings.Contains(decisions[0], `"approved"`) {
		t.Fatalf("decision %d %s %v", result.Code, result.Body.String(), decisions)
	}
	again := httptest.NewRecorder()
	h.ServeHTTP(again, httptest.NewRequest("POST", path, strings.NewReader(`{"decision":"approved"}`)))
	if again.Code != 404 || len(decisions) != 1 {
		t.Fatalf("replay %d %v", again.Code, decisions)
	}
	second := httptest.NewRecorder()
	h.ServeHTTP(second, httptest.NewRequest("POST", "/"+rec.ID+"/actions/change", strings.NewReader(`{}`)))
	if err := json.Unmarshal(second.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	rejected := httptest.NewRecorder()
	h.ServeHTTP(rejected, httptest.NewRequest("POST", "/"+rec.ID+"/proposals/"+response.Proposal.ID, strings.NewReader(`{"decision":"rejected"}`)))
	if rejected.Code != 200 || len(decisions) != 2 || !strings.Contains(decisions[1], `"rejected"`) {
		t.Fatalf("reject: %d %v", rejected.Code, decisions)
	}
	env.fake.routes["/ether/view/home"] = func(Request) (Response, error) {
		return Response{Status: 200, Body: []byte(`{"spec":{"specVersion":1,"layout":"list","header":[],"body":[],"className":"bad"},"data":{}}`)}, nil
	}
	invalid := httptest.NewRecorder()
	h.ServeHTTP(invalid, httptest.NewRequest("GET", "/"+rec.ID+"/views/home", nil))
	if invalid.Code != http.StatusBadGateway {
		t.Fatalf("invalid view accepted: %d %s", invalid.Code, invalid.Body.String())
	}
}

func TestEventOverflowAndRetentionThroughPublish(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	rec := env.installHello(t, "1.0.0")
	now := time.Now().UTC()
	env.m.now = func() time.Time { return now }
	env.m.store.now = env.m.now
	env.m.Publish(ctx, Event{Name: EventPlaybackStarted, ID: "old"})
	now = now.Add(8 * 24 * time.Hour)
	for i := 0; i < OutboxCap+1; i++ {
		env.m.Publish(ctx, Event{Name: EventPlaybackStarted, ID: fmt.Sprintf("new-%d", i)})
	}
	evs, _, err := env.m.store.outboxBatch(ctx, rec.ID, OutboxCap+10)
	if err != nil || len(evs) != OutboxCap || evs[0].ID != "new-1" {
		t.Fatalf("overflow: %d %v", len(evs), err)
	}
	h, err := env.m.store.health(ctx, rec.ID)
	if err != nil || h.Drops != 2 {
		t.Fatalf("drop count: %d %v", h.Drops, err)
	}
	var old int
	if err := env.m.store.db.QueryRow(`SELECT COUNT(*) FROM plugins_event_log WHERE created < ?`, now.Add(-EventLogMaxAge).Unix()).Scan(&old); err != nil || old != 0 {
		t.Fatalf("retention: %d %v", old, err)
	}
}

func TestEventViewerRedactionNested(t *testing.T) {
	input := json.RawMessage(`{"items":[{"viewerId":7,"nested":{"username":"viewer","title":"Film"}}]}`)
	output := stripViewer(input)
	if bytes.Contains(output, []byte("viewer")) || bytes.Contains(output, []byte("username")) || !bytes.Contains(output, []byte("Film")) {
		t.Fatalf("redaction: %s", output)
	}
	if string(stripViewer(json.RawMessage(`not json`))) != "{}" {
		t.Fatal("invalid payload retained")
	}
}

func TestToolInputEnumStillEnforcesBounds(t *testing.T) {
	env := newTestEnv(t)
	rec := env.installHello(t, "1.0.0")
	tool := env.m.pluginTool(rec, ToolMeta{Name: "bound", Title: "Bound", Description: "Bound", ReadOnly: true, InputSchema: json.RawMessage(`{"type":"object","properties":{"x":{"type":"string","enum":["long"],"maxLength":2}}}`)})
	if _, err := tool.Execute(context.Background(), tools.Principal{}, map[string]any{"x": "long"}); err == nil {
		t.Fatal("enum bypassed length bound")
	}
}
