package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/braedonsaunders/ether/internal/agent/tools"
	"github.com/braedonsaunders/ether/internal/config"
)

func TestHostFetchRejectsRedirectOutsideNetworkGrant(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://not-granted.invalid/secret", http.StatusFound)
	}))
	defer server.Close()
	man := baseManifest()
	man.Ether.Permissions.Network = []string{"127.0.0.1"}
	raw, _ := json.Marshal(man)
	p, err := env.m.PreviewInstall(ctx, Source{Kind: "studio", Manifest: raw, Files: map[string][]byte{"dist/plugin.js": []byte("// x")}})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := env.m.Install(ctx, p, InstallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	env.m.client = server.Client()
	_, err = env.m.HostFor(rec.ID).HTTPFetch(ctx, FetchRequest{URL: server.URL})
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("redirect outside grant: %v", err)
	}
}

func TestFolderHostRejectsSymlinkEscape(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	root, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "private"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	man := baseManifest()
	man.Ether.Permissions.Folders = "read"
	raw, _ := json.Marshal(man)
	p, err := env.m.PreviewInstall(ctx, Source{Kind: "studio", Manifest: raw, Files: map[string][]byte{"dist/plugin.js": []byte("// x")}})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := env.m.Install(ctx, p, InstallOptions{Grants: Grants{Folders: []string{root}}})
	if err != nil {
		t.Fatal(err)
	}
	h := env.m.HostFor(rec.ID)
	if err := os.Mkdir(filepath.Join(root, "inside"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("inside", filepath.Join(root, "safe")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.FoldersList(ctx, root, "safe"); err != nil {
		t.Fatalf("internal symlink: %v", err)
	}
	if _, err := h.FoldersList(ctx, root, "escape"); err == nil {
		t.Error("listed outside root through symlink")
	}
	if _, err := h.FoldersStat(ctx, root, "escape/private"); err == nil {
		t.Error("stat outside root through symlink")
	}
}

func TestPluginLogDoesNotExposeSavedSecrets(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	rec := env.installHello(t, "1.0.0")
	const secret = "private-value-472815"
	if _, err := env.m.Configure(ctx, rec.ID, map[string]any{"api_token": secret}); err != nil {
		t.Fatal(err)
	}
	if err := env.m.HostFor(rec.ID).Log(ctx, "info", "value: "+secret, nil); err != nil {
		t.Fatal(err)
	}
	logs, err := env.m.store.logs(ctx, rec.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range logs {
		if strings.Contains(line, secret) {
			t.Fatal("saved secret exposed in plugin logs")
		}
	}
}

func TestPluginInspectorAndNoticeRedactSavedSecrets(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	rec := env.installHello(t, "1.0.0")
	const secret = "private-value-472815"
	if _, err := env.m.Configure(ctx, rec.ID, map[string]any{"api_token": secret}); err != nil {
		t.Fatal(err)
	}
	env.fake.routes["/ether/view/test"] = func(Request) (Response, error) {
		return Response{Status: 200, Body: json.RawMessage(`{"message":"private-value-472815"}`)}, nil
	}
	if _, err := env.m.Call(ctx, rec.ID, Request{Method: "GET", Path: "/ether/view/test"}); err != nil {
		t.Fatal(err)
	}
	if err := env.m.HostFor(rec.ID).NotifyOwner(ctx, "message "+secret); err != nil {
		t.Fatal(err)
	}
	calls, err := env.m.store.calls(ctx, rec.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range calls {
		if strings.Contains(c.Response, secret) {
			t.Fatal("saved secret exposed in inspector")
		}
	}
	for _, notice := range env.notices {
		if strings.Contains(notice, secret) {
			t.Fatal("saved secret exposed in notice")
		}
	}
}

func TestPluginToolDoesNotSendSavedSecretToModel(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	rec := env.installHello(t, "1.0.0")
	const secret = "private-value-472815"
	if _, err := env.m.Configure(ctx, rec.ID, map[string]any{"api_token": secret}); err != nil {
		t.Fatal(err)
	}
	env.fake.routes["/ether/tool/leak"] = func(Request) (Response, error) {
		return Response{Status: 200, Body: json.RawMessage(`{"message":"private-value-472815"}`)}, nil
	}
	tool := env.m.pluginTool(rec, ToolMeta{Name: "leak", Title: "Leak", Description: "test", ReadOnly: true, InputSchema: json.RawMessage(`{"type":"object"}`)})
	result, err := tool.Execute(ctx, tools.Principal{}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(result.Data)
	if strings.Contains(string(raw), secret) {
		t.Fatal("saved secret exposed to model")
	}
}

func TestPluginViewsAndActionsDoNotExposeSavedSecrets(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	rec := env.installHello(t, "1.0.0")
	const secret = "private-value-472815"
	if _, err := env.m.Configure(ctx, rec.ID, map[string]any{"api_token": secret}); err != nil {
		t.Fatal(err)
	}
	env.fake.routes["/ether/view/home"] = func(Request) (Response, error) {
		return Response{Status: 200, Body: json.RawMessage(`{"spec":{"specVersion":1,"layout":"list","header":[],"body":[]},"data":{"message":"private-value-472815"}}`)}, nil
	}
	env.fake.routes["/ether/action/change"] = func(Request) (Response, error) {
		return Response{Status: 200, Body: json.RawMessage(`{"toast":"private-value-472815"}`)}, nil
	}
	for _, endpoint := range []struct{ method, path string }{{"GET", "/" + rec.ID + "/views/home"}, {"POST", "/" + rec.ID + "/actions/change"}} {
		w := httptest.NewRecorder()
		env.m.Handler().ServeHTTP(w, httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(`{}`)))
		if w.Code != 200 {
			t.Fatalf("%s: status %d", endpoint.path, w.Code)
		}
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("saved secret exposed in %s", endpoint.path)
		}
	}
}

func TestKVRejectsUnboundedKeys(t *testing.T) {
	env := newTestEnv(t)
	rec := env.installHello(t, "1.0.0")
	err := env.m.HostFor(rec.ID).KVPut(context.Background(), strings.Repeat("x", 1<<20), []byte("x"))
	if err == nil {
		t.Fatal("accepted a megabyte key with no quota charge")
	}
}

func TestPluginSourceURLIsSealedAndBoundToPlugin(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	sealer, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := sealer.UpgradeCredentials(ctx); err != nil {
		t.Fatal(err)
	}
	env.m.store.sealer = sealer
	rec := env.installHello(t, "1.0.0")
	const secretURL = "https://example.com/private-configuration/manifest.json"
	rec.SourceURL = secretURL
	if err := env.m.store.upsert(ctx, rec); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := env.m.store.db.QueryRowContext(ctx, `SELECT source_url FROM plugins_installed WHERE id=?`, rec.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, "private-configuration") {
		t.Fatal("source URL stored unsealed")
	}
	if _, _, err := config.OpenWith(sealer, stored, "plugins_installed", "source_url", "other-plugin"); err == nil {
		t.Fatal("source URL not bound to plugin")
	}
	restored, err := env.m.store.record(ctx, rec.ID)
	if err != nil || restored.SourceURL != secretURL {
		t.Fatalf("source URL not restored: %v", err)
	}
}

func TestInstallFetchDoesNotExposeSecretURL(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	const secret = "private-config-472815"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "missing", http.StatusNotFound) }))
	defer srv.Close()
	env.m.client = srv.Client()
	_, err := env.m.fetch(ctx, srv.URL+"/"+secret+"/manifest.json", false, 1024)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("fetch error exposed URL: %v", err)
	}
}

func TestInstallRedirectDoesNotSendSourceURLAsReferer(t *testing.T) {
	env := newTestEnv(t)
	const secret = "private-config-472815"
	seen := make(chan string, 1)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen <- r.Referer(); w.WriteHeader(http.StatusOK) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer source.Close()
	env.m.client = source.Client()
	if _, err := env.m.fetch(context.Background(), source.URL+"/"+secret, false, 1024); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(<-seen, secret) {
		t.Fatal("source secret sent in redirect Referer")
	}
}

func TestPluginRuntimeErrorsDoNotExposeSavedSecrets(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	rec := env.installHello(t, "1.0.0")
	const secret = "private-value-472815"
	if _, err := env.m.Configure(ctx, rec.ID, map[string]any{"api_token": secret}); err != nil {
		t.Fatal(err)
	}
	env.fake.fail = errors.New("failure: " + secret)
	_, err := env.m.Call(ctx, rec.ID, Request{Path: "/ether/view/home"})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("runtime error exposed saved secret: %v", err)
	}
}
