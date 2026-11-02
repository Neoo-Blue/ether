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
