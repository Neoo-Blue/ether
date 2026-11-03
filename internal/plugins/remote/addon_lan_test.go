package remote_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/plugins/remote"
	"github.com/braedonsaunders/ether/internal/store"
)

// A Stremio add-on the owner adds from a private address (a self-hosted
// add-on on the LAN) is reachable through the guarded runtime, with the
// configuration in its link kept.
func TestLANAddonInstalledByLinkIsReachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cfg/manifest.json":
			fmt.Fprint(w, `{"id":"org.lan.streams","version":"1.0.0","name":"LAN","types":["movie"],"resources":["stream"],"idPrefixes":["tt"]}`)
		case "/cfg/stream/movie/tt1.json":
			if r.URL.Query().Get("k") != "v" {
				http.Error(w, "lost config", 400)
				return
			}
			fmt.Fprint(w, `{"streams":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	mgr, err := plugins.New(plugins.Deps{DB: store.OpenTest(t)})
	if err != nil {
		t.Fatal(err)
	}
	f := &remote.Factory{TrustedLAN: remote.GrantsLAN(mgr.GrantsOf)}
	mgr.RegisterRuntime("remote", f.RuntimeFactory())
	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatal(err)
	}
	rec, err := mgr.InstallAddon(ctx, plugins.AddonInstall{Link: srv.URL + "/cfg/manifest.json?k=v"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := mgr.Fetcher(rec.ID).Fetch(ctx, "/stream/movie/tt1.json", 1<<20)
	if err != nil || string(body) != `{"streams":[]}` {
		t.Fatalf("body=%q err=%v", body, err)
	}
}
