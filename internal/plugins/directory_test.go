package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/braedonsaunders/ether/internal/extend"
	"testing"
)

func TestDirectoryNativeAndOrdinarySharedLifecycle(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		name := "native"
		if foreign {
			name = "ordinary"
		}
		t.Run(name, func(t *testing.T) {
			e := newTestEnv(t)
			ctx := context.Background()
			man := Manifest{ID: "org.example.directory", Version: "1.0.0", Name: "Film directories"}
			key := "films"
			route := "/ether/directory/films"
			body := json.RawMessage(`{"entries":[{"id":"org.example.films","name":"Film sources","manifestURL":"https://example.test/manifest.json"},{"id":"org.example.bad","name":"Bad transport","manifestURL":"file:///tmp/manifest.json"}]}`)
			if foreign {
				c := StremioCat{ID: "official", Type: "all", Name: "Official"}
				man.Resources = json.RawMessage(`["addon_catalog"]`)
				man.AddonCatalogs = []StremioCat{c}
				key = "directory-" + StremioCatalogKey(c)[len("catalog-"):]
				route = "/addon_catalog/all/official.json"
				body = json.RawMessage(`{"addons":[{"transportName":"http","transportUrl":"https://example.test/manifest.json","manifest":{"id":"org.example.films","name":"Film sources"}},{"transportName":"file","transportUrl":"file:///tmp/manifest.json","manifest":{"id":"org.example.bad","name":"Bad transport"}}]}`)
			} else {
				man.Ether = &EtherBlock{Protocol: 1, Kind: KindExtension, Summary: "Discover film sources", Runtime: RuntimeDecl{Kind: "hosted", Entry: "plugin.js"}, Capabilities: []Capability{CapPluginDirectory}, Contributes: Contributes{Directories: []DirectoryMeta{{Key: key, Label: "Films"}}}}
			}
			calls := 0
			e.fake.routes[route] = func(req Request) (Response, error) { calls++; return Response{Status: 200, Body: body}, nil }
			raw, _ := json.Marshal(man)
			preview, err := e.m.PreviewInstall(ctx, Source{Kind: "studio", Manifest: raw, Files: map[string][]byte{"plugin.js": []byte("// fixture")}})
			if err != nil {
				t.Fatal(err)
			}
			rec, err := e.m.Install(ctx, preview, InstallOptions{})
			if err != nil {
				t.Fatal(err)
			}
			ds := DescriptorsFor(rec.Manifest)
			found := false
			for _, d := range ds {
				if d.Kind == extend.KindPluginDirectory && d.ID == extend.PluginID(rec.ID, key) {
					found = true
				}
			}
			if !found {
				t.Fatal("shared directory descriptor absent")
			}
			got, err := e.m.BrowseDirectory(ctx, rec.ID, key)
			if err != nil || len(got.Entries) != 2 {
				t.Fatalf("browse %+v %v", got, err)
			}
			if got.Entries[0].ManifestURL != "https://example.test/manifest.json" || got.Entries[1].ManifestURL != "" || got.Entries[1].Unsupported == "" {
				t.Fatalf("entry validation %+v", got)
			}
			if _, err = e.m.BrowseDirectory(ctx, rec.ID, "unknown"); !errors.Is(err, ErrNotFound) || calls != 1 {
				t.Fatalf("undeclared directory fetched: %v calls=%d", err, calls)
			}
			if err = e.m.SetEnabled(ctx, rec.ID, false); err != nil {
				t.Fatal(err)
			}
			if _, err = e.m.BrowseDirectory(ctx, rec.ID, key); !errors.Is(err, ErrDisabled) || calls != 1 {
				t.Fatalf("disabled directory fetched: %v", err)
			}
			if err = e.m.SetEnabled(ctx, rec.ID, true); err != nil {
				t.Fatal(err)
			}
			if _, err = e.m.BrowseDirectory(ctx, rec.ID, key); err != nil || calls != 2 {
				t.Fatalf("reenabled %v", err)
			}
			if err = e.m.Remove(ctx, rec.ID, false); err != nil {
				t.Fatal(err)
			}
			if _, err = e.m.BrowseDirectory(ctx, rec.ID, key); !errors.Is(err, ErrNotFound) || calls != 2 {
				t.Fatalf("uninstalled directory fetched: %v", err)
			}
		})
	}
}
