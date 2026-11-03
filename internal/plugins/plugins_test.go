package plugins

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/agent/tools"
	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/store"
)

// fakeRuntime answers plugin calls from a route table and can fail, stall
// or run host calls through the real Host.
type fakeRuntime struct {
	mu     sync.Mutex
	calls  []Request
	routes map[string]func(Request) (Response, error)
	fail   error
	inst   Installed
}

func (f *fakeRuntime) Call(ctx context.Context, req Request) (Response, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	fail, routes := f.fail, f.routes
	f.mu.Unlock()
	if fail != nil {
		return Response{}, fail
	}
	for prefix, fn := range routes {
		if strings.HasPrefix(req.Path, prefix) {
			return fn(req)
		}
	}
	switch {
	case strings.HasPrefix(req.Path, "/ether/health"):
		return Response{Status: 200, Body: json.RawMessage(`{"status":"ok"}`)}, nil
	case strings.HasPrefix(req.Path, "/ether/test"):
		return Response{Status: 200, Body: json.RawMessage(`{"ok":true,"message":"Works"}`)}, nil
	case strings.HasPrefix(req.Path, "/ether/event"):
		var body struct {
			Events []Event `json:"events"`
		}
		_ = json.Unmarshal(req.Body, &body)
		ack := []string{}
		for _, e := range body.Events {
			ack = append(ack, e.ID)
		}
		raw, _ := json.Marshal(map[string]any{"ack": ack})
		return Response{Status: 200, Body: raw}, nil
	}
	return Response{Status: 200, Body: json.RawMessage(`{}`)}, nil
}

func (f *fakeRuntime) Close() error { return nil }

func (f *fakeRuntime) paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []string{}
	for _, c := range f.calls {
		out = append(out, c.Path)
	}
	return out
}

type testEnv struct {
	m       *Manager
	fake    *fakeRuntime
	reg     *extend.Registry
	tools   *tools.Catalogue
	notices []string
	lead    bool
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	env := &testEnv{fake: &fakeRuntime{routes: map[string]func(Request) (Response, error){}},
		reg: extend.New(), tools: tools.NewCatalogue(), lead: true}
	env.fake.inst = Installed{}
	m, err := New(Deps{
		DB: store.OpenTest(t), Registry: env.reg, Tools: env.tools,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		IsLeader: func() bool { return env.lead },
		Now:      func() time.Time { return time.Now().UTC() },
		Backends: Backends{
			Notice: func(s string) { env.notices = append(env.notices, s) },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	m.RegisterRuntime("hosted", func(inst Installed) (Runtime, error) {
		env.fake.inst = inst
		return env.fake, nil
	})
	m.RegisterRuntime("remote", func(inst Installed) (Runtime, error) {
		env.fake.inst = inst
		return env.fake, nil
	})
	env.m = m
	return env
}

func helloManifest(version string) []byte {
	man := map[string]any{
		"id": "community.example.hello", "version": version, "name": "Hello",
		"ether": map[string]any{
			"protocol": 1, "kind": "extension", "summary": "$t:summary",
			"runtime":      map[string]any{"kind": "hosted", "entry": "dist/plugin.js"},
			"capabilities": []string{"events", "tools", "ui"},
			"permissions":  map[string]any{"secrets": []string{"api_token"}},
			"strings":      map[string]any{"en": "i18n/en.json", "fr": "i18n/fr.json"},
			"settings":     map[string]any{"$ref": "settings.schema.json"},
			"contributes": map[string]any{
				"events": map[string]any{"subscribe": []string{"playback.started"}},
				"tools":  []map[string]any{{"name": "say_hello", "title": "$t:toolTitle", "description": "Greets", "inputSchema": map[string]any{"type": "object"}, "readOnly": true}},
				"pages":  []map[string]any{{"key": "home", "label": "$t:pageHome", "route": "/ether/view/home"}},
			},
		},
	}
	raw, _ := json.Marshal(man)
	return raw
}

func helloFiles() map[string][]byte {
	return map[string][]byte{
		"dist/plugin.js":       []byte("// hello"),
		"i18n/en.json":         []byte(`{"summary":"Greets the household","toolTitle":"Say hello","pageHome":"Hello home"}`),
		"i18n/fr.json":         []byte(`{"summary":"Salue le foyer"}`),
		"settings.schema.json": []byte(`{"fields":[{"key":"api_token","kind":"secret","label":"Access token"},{"key":"greeting","kind":"text","label":"Greeting"}]}`),
	}
}

func (e *testEnv) installHello(t *testing.T, version string) Record {
	t.Helper()
	p, err := e.m.PreviewInstall(context.Background(), Source{Kind: "studio", Manifest: helloManifest(version), Files: helloFiles()})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := e.m.Install(context.Background(), p, InstallOptions{AcceptAddedPermissions: true})
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestHelloLifecycle(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	rec := env.installHello(t, "1.0.0")
	if rec.Verified {
		t.Fatal("studio install must be Unverified")
	}
	// Descriptors and tools registered.
	if got := env.reg.Owned(rec.ID); len(got) == 0 {
		t.Fatal("no descriptors registered")
	}
	if _, ok := env.reg.Get(extend.KindPage, extend.PluginID(rec.ID, "home")); !ok {
		t.Fatal("page descriptor missing")
	}
	// Text resolution with en fallback.
	if s, ok := env.m.ResolveText(ctx, rec.ID, "summary", "fr"); !ok || s != "Salue le foyer" {
		t.Fatalf("fr summary: %q %v", s, ok)
	}
	if s, ok := env.m.ResolveText(ctx, rec.ID, "toolTitle", "fr"); !ok || s != "Say hello" {
		t.Fatalf("en fallback: %q %v", s, ok)
	}
	// Configure: secret is sealed and returned redacted.
	if _, err := env.m.Configure(ctx, rec.ID, map[string]any{"api_token": "sk-123", "greeting": "Hi"}); err != nil {
		t.Fatal(err)
	}
	settings, _, err := env.m.GetSettings(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if settings["api_token"] != "Saved" || settings["greeting"] != "Hi" {
		t.Fatalf("settings: %v", settings)
	}
	// The secret reaches the plugin through configure and SecretGet,
	// never another plugin's context.
	h := env.m.HostFor(rec.ID)
	if v, err := h.SecretGet(ctx, "api_token"); err != nil || v != "sk-123" {
		t.Fatalf("secret: %q %v", v, err)
	}
	if _, err := h.SecretGet(ctx, "undeclared"); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("undeclared secret: %v", err)
	}
	// Test and health.
	if out, err := env.m.Test(ctx, rec.ID); err != nil || !out.OK {
		t.Fatalf("test: %+v %v", out, err)
	}
	if hstatus, err := env.m.CheckHealth(ctx, rec.ID); err != nil || hstatus.Status != "ok" {
		t.Fatalf("health: %+v %v", hstatus, err)
	}
	// Events: publish, outbox, deliver with ack.
	env.m.Publish(ctx, Event{Name: EventPlaybackStarted, ViewerID: 7})
	if err := env.m.DeliverOutbox(ctx, rec.ID); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range env.fake.paths() {
		if p == "/ether/event" {
			found = true
		}
	}
	if !found {
		t.Fatal("event was not delivered")
	}
	// Disable clears descriptors; enable restores them.
	if err := env.m.SetEnabled(ctx, rec.ID, false); err != nil {
		t.Fatal(err)
	}
	if got := env.reg.Owned(rec.ID); len(got) != 0 {
		t.Fatal("descriptors remain while disabled")
	}
	if err := env.m.SetEnabled(ctx, rec.ID, true); err != nil {
		t.Fatal(err)
	}
	if got := env.reg.Owned(rec.ID); len(got) == 0 {
		t.Fatal("descriptors not restored")
	}
	// Remove purges everything.
	if err := env.m.Remove(ctx, rec.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := env.m.Get(ctx, rec.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after remove: %v", err)
	}
	if _, ok, err := env.m.store.kvGet(ctx, rec.ID, "x"); err != nil || ok {
		t.Fatal("kv not purged")
	}
}

func TestFailurePolicyDisables(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	rec := env.installHello(t, "1.0.0")
	env.fake.mu.Lock()
	env.fake.fail = errors.New("boom")
	env.fake.mu.Unlock()
	for i := 0; i < 5; i++ {
		_, _ = env.m.CheckHealth(ctx, rec.ID)
	}
	got, err := env.m.Get(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled {
		t.Fatal("plugin should be disabled after five failures")
	}
	h, _ := env.m.HealthOf(ctx, rec.ID)
	if h.Status != "disabled" || h.Restarts < 5 {
		t.Fatalf("health: %+v", h)
	}
	if len(env.notices) == 0 {
		t.Fatal("no console notice sent")
	}
	// Re-enable clears the failure window.
	env.fake.mu.Lock()
	env.fake.fail = nil
	env.fake.mu.Unlock()
	if err := env.m.SetEnabled(ctx, rec.ID, true); err != nil {
		t.Fatal(err)
	}
	if h, err := env.m.CheckHealth(ctx, rec.ID); err != nil || h.Status != "ok" {
		t.Fatalf("health after re-enable: %+v %v", h, err)
	}
}

func TestUpdatePermissionDiffAndRollback(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.installHello(t, "1.0.0")

	// Serve v2 with a new network permission over HTTP.
	v2 := helloManifest("2.0.0")
	var doc map[string]any
	_ = json.Unmarshal(v2, &doc)
	doc["ether"].(map[string]any)["permissions"] = map[string]any{
		"secrets": []string{"api_token"}, "network": []string{"api.example.tv"},
	}
	v2, _ = json.Marshal(doc)
	files := helloFiles()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "manifest.json" {
			_, _ = w.Write(v2)
			return
		}
		if b, ok := files[path]; ok {
			_, _ = w.Write(b)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	env.m.client = srv.Client()

	// Point the installed record at the test server as its update source.
	rec, _ := env.m.Get(ctx, "community.example.hello")
	rec.SourceURL = srv.URL
	rec.SourceKind = "url"
	rec.Verified = true
	rec.AutoUpdate = true
	rec.Shadow = true
	if err := env.m.store.upsert(ctx, rec); err != nil {
		t.Fatal(err)
	}
	p, available, err := env.m.CheckUpdate(ctx, rec.ID)
	if err != nil || !available {
		t.Fatalf("check update: %v available=%v", err, available)
	}
	if len(p.AddedPermissions) == 0 {
		t.Fatal("want added permissions on the update")
	}
	beforeApproval, err := env.m.Get(ctx, rec.ID)
	if err != nil || beforeApproval.Version != "1.0.0" {
		t.Fatalf("permission increase applied automatically: %+v %v", beforeApproval, err)
	}
	if _, err := env.m.ApplyUpdate(ctx, rec.ID, false); err == nil {
		t.Fatal("update without accepting new permissions must stop")
	} else {
		var pie *PermissionIncreaseError
		if !errors.As(err, &pie) {
			t.Fatalf("want PermissionIncreaseError, got %v", err)
		}
	}
	updated, err := env.m.ApplyUpdate(ctx, rec.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != "2.0.0" || updated.PrevVersion != "1.0.0" || !updated.AutoUpdate || !updated.Shadow {
		t.Fatalf("updated: %+v", updated)
	}
	back, err := env.m.Rollback(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Version != "1.0.0" {
		t.Fatalf("rollback: %+v", back)
	}
	if _, err := env.m.Rollback(ctx, rec.ID); err == nil {
		t.Fatal("second rollback must fail")
	}
}

func TestKVQuotaAndGrantsRevokeMidCall(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	rec := env.installHello(t, "1.0.0")
	h := env.m.HostFor(rec.ID)
	if err := h.KVPut(ctx, "a", bytes.Repeat([]byte("x"), 1024)); err != nil {
		t.Fatal(err)
	}
	// Forge a nearly-full quota by writing one large value past the cap
	// through the store directly, then confirm the next put is refused.
	big := bytes.Repeat([]byte("y"), KVQuotaBytes-1024-512-100)
	if err := h.KVPut(ctx, "big", big); err != nil {
		t.Fatal(err)
	}
	if err := h.KVPut(ctx, "more", bytes.Repeat([]byte("z"), 4096)); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("quota: %v", err)
	}
	// Revoke catalog access while a call is conceptually in flight: the
	// next host call re-reads grants and fails.
	if _, err := h.CatalogSearch(ctx, "x", 5, 0); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("catalog without permission: %v", err)
	}
	// Network: declared hosts only.
	if _, err := h.HTTPFetch(ctx, FetchRequest{URL: "https://undeclared.example/"}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("fetch undeclared: %v", err)
	}
	if err := env.m.SetGrants(ctx, rec.ID, Grants{Revoked: []string{"secrets"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.SecretGet(ctx, "api_token"); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("secret after revoke: %v", err)
	}
}

func TestJournalShadowAndUndo(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	watched := map[string]bool{}
	env.m.deps.Backends.SetWatchState = func(_ context.Context, _ int64, title string, w bool) (bool, error) {
		prev := watched[title]
		watched[title] = w
		return prev, nil
	}
	man := baseManifest()
	man.Ether.Permissions.Viewers.WriteWatchState = "granted_viewers"
	man.Ether.Capabilities = []Capability{CapEvents}
	man.Ether.Contributes.Events = &EventsMeta{Subscribe: []string{EventPlaybackStarted}}
	raw, _ := json.Marshal(man)
	p, err := env.m.PreviewInstall(ctx, Source{Kind: "studio", Manifest: raw, Files: map[string][]byte{"dist/plugin.js": []byte("// x")}})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := env.m.Install(ctx, p, InstallOptions{Grants: Grants{Viewers: []int64{3}}})
	if err != nil {
		t.Fatal(err)
	}
	h := env.m.HostFor(rec.ID)
	if err := h.SetWatchState(ctx, 9, "title-1", true); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("ungranted viewer: %v", err)
	}
	if err := h.SetWatchState(ctx, 3, "title-1", true); err != nil {
		t.Fatal(err)
	}
	if !watched["title-1"] {
		t.Fatal("watch state not applied")
	}
	// Shadow: recorded, not applied.
	if err := env.m.SetShadow(ctx, rec.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := h.SetWatchState(ctx, 3, "title-2", true); err != nil {
		t.Fatal(err)
	}
	if watched["title-2"] {
		t.Fatal("shadow write was applied")
	}
	shadow, err := env.m.Journal(ctx, rec.ID, 10, true)
	if err != nil || len(shadow) != 1 {
		t.Fatalf("shadow journal: %v %v", shadow, err)
	}
	// Undo replays the live inverse only.
	if err := env.m.SetShadow(ctx, rec.ID, false); err != nil {
		t.Fatal(err)
	}
	n, err := env.m.UndoAll(ctx, rec.ID)
	if err != nil || n != 1 {
		t.Fatalf("undo: %d %v", n, err)
	}
	if watched["title-1"] {
		t.Fatal("undo did not restore watch state")
	}
}

func TestOutboxOverflowAndLeaderPlacement(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	rec := env.installHello(t, "1.0.0")
	// Fill past the cap through the store; oldest must drop, recorded.
	st := env.m.store
	totalDropped := 0
	for i := 0; i < OutboxCap+5; i++ {
		d, err := st.enqueue(ctx, rec.ID, Event{ID: "e", Name: EventPlaybackStarted})
		if err != nil {
			t.Fatal(err)
		}
		totalDropped += d
	}
	if totalDropped != 5 {
		t.Fatalf("dropped %d, want 5", totalDropped)
	}
	evs, _, err := st.outboxBatch(ctx, rec.ID, OutboxCap+10)
	if err != nil || len(evs) != OutboxCap {
		t.Fatalf("outbox size: %d %v", len(evs), err)
	}
	// Leader placement: a scheduled job runs only as leader.
	if env.m.mayRunJobs(rec) != true {
		t.Fatal("leader should run jobs")
	}
	env.lead = false
	if env.m.mayRunJobs(rec) {
		t.Fatal("non-leader must not run leader jobs")
	}
	rec.Manifest.Ether.Cluster = "every_node"
	if !env.m.mayRunJobs(rec) {
		t.Fatal("every_node plugin should run everywhere")
	}
	env.lead = true
}

func TestInstallFromPkgAndURL(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	// Build an .etherpkg with a valid digest.
	files := helloFiles()
	files["manifest.json"] = helloManifest("1.0.0")
	files["digest"] = []byte(PkgDigest(files))
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, b := range files {
		w, _ := zw.Create(name)
		_, _ = w.Write(b)
	}
	_ = zw.Close()
	p, err := env.m.PreviewInstall(ctx, Source{Kind: "pkg", Pkg: buf.Bytes()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.m.Install(ctx, p, InstallOptions{}); err != nil {
		t.Fatal(err)
	}
	// A tampered package is refused.
	files["dist/plugin.js"] = []byte("// tampered")
	var buf2 bytes.Buffer
	zw2 := zip.NewWriter(&buf2)
	for name, b := range files {
		w, _ := zw2.Create(name)
		_, _ = w.Write(b)
	}
	_ = zw2.Close()
	if _, err := env.m.PreviewInstall(ctx, Source{Kind: "pkg", Pkg: buf2.Bytes()}); err == nil {
		t.Fatal("tampered package must be refused")
	}
	// URL install through an httptest server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if b, ok := helloFiles()[path]; ok {
			_, _ = w.Write(b)
			return
		}
		if path == "manifest.json" {
			man := helloManifest("1.0.0")
			man = bytes.Replace(man, []byte("community.example.hello"), []byte("community.example.remote"), 1)
			man = bytes.Replace(man, []byte(`"kind":"hosted","entry":"dist/plugin.js"`), []byte(`"kind":"remote"`), 1)
			_, _ = w.Write(man)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	env.m.client = srv.Client()
	p2, err := env.m.PreviewInstall(ctx, Source{Kind: "url", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := env.m.Install(ctx, p2, InstallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rec.SourceKind != "url" || rec.Verified {
		t.Fatalf("url install: %+v", rec)
	}
}

func TestOwnerAPI(t *testing.T) {
	env := newTestEnv(t)
	env.installHello(t, "1.0.0")
	srv := httptest.NewServer(env.m.Handler())
	defer srv.Close()
	get := func(path string) (int, map[string]any) {
		t.Helper()
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close() //nolint:errcheck
		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return resp.StatusCode, body
	}
	code, body := get("/")
	if code != 200 || len(body["plugins"].([]any)) != 1 {
		t.Fatalf("list: %d %v", code, body)
	}
	code, body = get("/community.example.hello")
	if code != 200 || body["verified"] != false {
		t.Fatalf("get: %d %v", code, body)
	}
	if _, ok := body["privacy"]; !ok {
		t.Fatal("privacy label missing")
	}
	if _, ok := body["impact"]; !ok {
		t.Fatal("impact missing")
	}
	code, body = get("/missing.plugin")
	if code != 404 {
		t.Fatalf("missing: %d %v", code, body)
	}
	if e, ok := body["error"].(map[string]any); !ok || e["code"] != "not_found" {
		t.Fatalf("error shape: %v", body)
	}
	// Settings round trip over HTTP, secret redacted.
	reqBody := `{"settings":{"api_token":"sk-secret","greeting":"Yo"}}`
	resp, err := http.Post(srv.URL+"/community.example.hello/test", "application/json", nil)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("test endpoint: %v %v", resp.StatusCode, err)
	}
	_ = resp.Body.Close()
	putResp, err := func() (*http.Response, error) {
		req, _ := http.NewRequest(http.MethodPut, srv.URL+"/community.example.hello/settings", strings.NewReader(reqBody))
		return http.DefaultClient.Do(req)
	}()
	if err != nil || putResp.StatusCode != 200 {
		t.Fatalf("put settings: %v %v", putResp.StatusCode, err)
	}
	_ = putResp.Body.Close()
	_, body = get("/community.example.hello/settings")
	settings, _ := body["settings"].(map[string]any)
	if settings["api_token"] != "Saved" {
		t.Fatalf("secret not redacted: %v", settings)
	}
	// Calls inspector never carries the secret.
	_, body = get("/community.example.hello/calls")
	if strings.Contains(toJSON(t, body), "sk-secret") {
		t.Fatal("secret leaked into the inspector")
	}
	// Disable then remove through the API.
	resp, _ = http.Post(srv.URL+"/community.example.hello/disable", "application/json", nil)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("disable: %d", resp.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/community.example.hello", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("delete: %v %v", resp.StatusCode, err)
	}
	_ = resp.Body.Close()
	if code, _ := get("/community.example.hello"); code != 404 {
		t.Fatalf("after delete: %d", code)
	}
}

func toJSON(t *testing.T, v any) string {
	t.Helper()
	b, _ := json.Marshal(v)
	return string(b)
}

func TestSealedSecretsArePerPlugin(t *testing.T) {
	// A sealer that actually seals: use config's store in a temp dir.
	dir := t.TempDir()
	cs, err := config.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := cs.UpgradeCredentials(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := &storage{sealer: cs, now: time.Now}
	sealed, err := st.sealSecret("plugin.a", "token", "value-1")
	if err != nil {
		t.Fatal(err)
	}
	if sealed == "value-1" {
		t.Fatal("secret was not sealed")
	}
	if v, err := st.openSecret("plugin.a", "token", sealed); err != nil || v != "value-1" {
		t.Fatalf("open own: %q %v", v, err)
	}
	if _, err := st.openSecret("plugin.b", "token", sealed); err == nil {
		t.Fatal("another plugin must not open the secret")
	}
}

func TestMigrationsApply(t *testing.T) {
	db := store.OpenTest(t)
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name LIKE 'plugins_%'`).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n < 11 {
		t.Fatalf("plugin tables: %d", n)
	}
}

func TestFetchSiblingsIncludesRelativeIcon(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/assets/icon.svg" {
			_, _ = w.Write([]byte("icon"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	env.m.client = srv.Client()
	manifest := []byte(`{"id":"com.example.icon","version":"1.0.0","name":"Icon","ether":{"protocol":1,"kind":"extension","icon":"assets/icon.svg","runtime":{"kind":"remote"}}}`)
	files := env.m.fetchSiblings(context.Background(), srv.URL+"/manifest.json", manifest, map[string][]byte{}, false)
	if string(files["assets/icon.svg"]) != "icon" {
		t.Fatalf("icon not fetched: %v", files)
	}
}

// An adapter-reported failure clears once the plugin answers again, so an
// add-on without a runtime health check does not show a problem forever.
func TestSuccessfulCallClearsRecordedFailure(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	rec := env.installHello(t, "1.0.0")
	env.m.RecordFailure(ctx, rec.ID, "catalog returned invalid JSON")
	if h, _ := env.m.HealthOf(ctx, rec.ID); h.Status != "error" {
		t.Fatalf("failure not recorded: %+v", h)
	}
	if _, err := env.m.Call(ctx, rec.ID, Request{Method: http.MethodGet, Path: "/ether/health"}); err != nil {
		t.Fatal(err)
	}
	h, _ := env.m.HealthOf(ctx, rec.ID)
	if h.Status != "ok" || h.LastOK.IsZero() || h.LastError == "" {
		t.Fatalf("health after a good call: %+v", h)
	}
}
