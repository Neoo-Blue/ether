package plugins

import (
	"context"
	"encoding/json"
	"testing"
)

func TestViewerGrantedRespectsLiveGrantAndRevocation(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.m.deps.Backends.ViewerNames = func(_ context.Context, ids []int64) (json.RawMessage, error) { return json.Marshal(ids) }
	man := baseManifest()
	man.Ether.Permissions.Viewers.WatchHistory = "granted_viewers"
	raw, _ := json.Marshal(man)
	p, e := env.m.PreviewInstall(ctx, Source{Kind: "studio", Manifest: raw, Files: map[string][]byte{"dist/plugin.js": []byte("// fixture")}})
	if e != nil {
		t.Fatal(e)
	}
	rec, e := env.m.Install(ctx, p, InstallOptions{Grants: Grants{Viewers: []int64{3, 4}}})
	if e != nil {
		t.Fatal(e)
	}
	h := env.m.HostFor(rec.ID)
	got, e := h.ViewerGranted(ctx)
	if e != nil || string(got) != "[3,4]" {
		t.Fatalf("got %s %v", got, e)
	}
	if e = env.m.SetGrants(ctx, rec.ID, Grants{Viewers: []int64{4}, Revoked: []string{"viewers.watch_history"}}); e != nil {
		t.Fatal(e)
	}
	got, e = h.ViewerGranted(ctx)
	if e != nil || string(got) != "[]" {
		t.Fatalf("revoked %s %v", got, e)
	}
}
