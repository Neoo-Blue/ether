package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
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
