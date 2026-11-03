package adapters_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/braedonsaunders/ether/internal/stremio"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/catalog"
	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/plugins/adapters"
)

func TestMetadataNativeAndStremioUseSharedCollectionPath(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		name := "native"
		if foreign {
			name = "stremio"
		}
		t.Run(name, func(t *testing.T) {
			h := newListTestHarness(t)
			ctx := context.Background()
			h.mgr.RegisterRuntime("remote", func(plugins.Installed) (plugins.Runtime, error) { return h.rt, nil })
			adapters.AttachMetadata(h.mgr, h.cat)
			cat := plugins.StremioCat{Type: "movie", ID: "classics", Name: "Classics"}
			man := plugins.Manifest{ID: "community.metadata." + name, Version: "1.0.0", Name: "Classic titles", Types: []string{"movie"}, Resources: stremio.ParseResources(`["catalog","meta"]`), Catalogs: []plugins.StremioCat{cat}}
			listKey := plugins.StremioCatalogKey(cat)
			if !foreign {
				listKey = "classics"
				man.Ether = &plugins.EtherBlock{Protocol: 1, Kind: plugins.KindExtension, Summary: "Classic movie records", Runtime: plugins.RuntimeDecl{Kind: "hosted", Entry: "plugin.js"}, Capabilities: []plugins.Capability{plugins.CapListSource, plugins.CapMetadata}, Contributes: plugins.Contributes{Metadata: &plugins.MetadataMeta{Types: []string{"movie"}}, ListSources: []plugins.ListSourceMeta{{Key: listKey, Label: "Classics", Catalog: "movie/classics", MaxItems: 1}}}}
			}
			title := core.Title{Type: core.TypeMovie, IDs: core.IDs{TMDB: 19, IMDB: "tt0017136"}, Title: "Metropolis", Year: 1927, Released: time.Date(1927, 1, 10, 0, 0, 0, 0, time.UTC), Summary: "A city divided", PosterPath: "https://example.test/poster.jpg"}
			metadataCalls := 0
			metadataRoute := "/ether/metadata/resolve"
			metadataBody, _ := json.Marshal(map[string]any{"title": title})
			if foreign {
				metadataRoute = "/meta/movie/tt0017136.json"
				metadataBody = []byte(`{"meta":{"id":"tt0017136","type":"movie","name":"Metropolis","moviedb_id":19,"imdb_id":"tt0017136","releaseInfo":"1927","released":"1927-01-10T00:00:00Z","description":"A city divided","poster":"https://example.test/poster.jpg"}}`)
			}
			h.rt.routes[metadataRoute] = func(req plugins.Request) (plugins.Response, error) {
				metadataCalls++
				if !foreign {
					var request core.MetadataRequest
					if req.Method != "POST" || json.Unmarshal(req.Body, &request) != nil || request.ID != "tt0017136" || request.Type != core.TypeMovie {
						t.Errorf("native request: %+v", req)
					}
				}
				return plugins.Response{Status: 200, Body: metadataBody}, nil
			}
			h.rt.routes["/catalog/movie/classics.json"] = func(plugins.Request) (plugins.Response, error) {
				return plugins.Response{Status: 200, Body: json.RawMessage(`{"metas":[{"id":"tt0017136","type":"movie","name":"Metropolis"}],"hasMore":false}`)}, nil
			}
			raw, _ := json.Marshal(man)
			preview, err := h.mgr.PreviewInstall(ctx, plugins.Source{Kind: "studio", Manifest: raw, Files: map[string][]byte{"plugin.js": []byte("// test fixture")}})
			if err != nil {
				t.Fatal(err)
			}
			rec, err := h.mgr.Install(ctx, preview, plugins.InstallOptions{})
			if err != nil {
				t.Fatal(err)
			}
			provider := extend.PluginID(rec.ID, "metadata")
			kind := extend.PluginID(rec.ID, listKey)
			if _, ok := h.reg.Get(extend.KindMetadataProvider, provider); !ok {
				t.Fatal("metadata descriptor absent")
			}
			before, err := h.cat.Browse(ctx, core.BrowseQuery{Filter: core.Filter{Type: core.TypeMovie}})
			if err != nil {
				t.Fatal(err)
			}
			spec := core.CollectionSpec{Name: "Classics", MediaType: "movie", Sources: []core.CollectionSource{{Kind: kind}}}
			result, err := h.cols.Preview(ctx, spec, 10)
			if err != nil {
				t.Fatal(err)
			}
			rawPreview, _ := json.Marshal(result)
			var found bool
			// Check the actual preview and local shared catalogue rather than just
			// descriptor registration or an isolated transport request.
			for _, item := range result.Items {
				if item.Key == core.MovieKey(19) {
					found = true
				}
			}
			if !found {
				t.Fatalf("native preview missing materialized title: %s", rawPreview)
			}
			stored, err := h.cat.CachedGet(ctx, core.MovieKey(19))
			if err != nil || stored.Title != "Metropolis" || stored.IDs.IMDB != "tt0017136" || stored.PosterPath != title.PosterPath {
				t.Fatalf("shared record %+v %v", stored, err)
			}
			if metadataCalls != 1 {
				t.Fatalf("metadata calls %d", metadataCalls)
			}
			after, err := h.cat.Browse(ctx, core.BrowseQuery{Filter: core.Filter{Type: core.TypeMovie}})
			if err != nil || after.Total != before.Total {
				t.Fatalf("materialization changed global browse: before=%d after=%d err=%v", before.Total, after.Total, err)
			}
			req := core.MetadataRequest{ID: "tt0017136", Type: core.TypeMovie, IDs: core.IDs{IMDB: "tt0017136"}}
			if _, err = h.cat.MaterializeMetadata(ctx, provider, req); err != nil {
				t.Fatal(err)
			}
			if metadataCalls != 1 {
				t.Fatal("warm materialization or browse fetched provider")
			}
			// A fresh catalogue instance retains persisted provider identity and data.
			restarted, err := catalog.New(h.db, nil, config.Catalog{Language: "en-US", Region: "US", MinPopularity: 1, MinVotes: 1}, nil)
			if err != nil {
				t.Fatal(err)
			}
			adapters.AttachMetadata(h.mgr, restarted)
			if _, err = restarted.MaterializeMetadata(ctx, provider, req); err != nil {
				t.Fatal(err)
			}
			if metadataCalls != 1 {
				t.Fatal("restart lost the metadata cache")
			}
			if err = h.mgr.SetEnabled(ctx, rec.ID, false); err != nil {
				t.Fatal(err)
			}
			if _, ok := h.reg.Get(extend.KindMetadataProvider, provider); ok {
				t.Fatal("disabled descriptor remains")
			}
			if _, ok := h.cols.KindRegistry().Lookup(kind); ok {
				t.Fatal("disabled collection source remains")
			}
			if _, err = restarted.MaterializeMetadata(ctx, provider, req); !errors.Is(err, core.ErrNotAvailable) {
				t.Fatalf("disabled cached provider usable: %v", err)
			}
			if err = h.mgr.SetEnabled(ctx, rec.ID, true); err != nil {
				t.Fatal(err)
			}
			again, err := restarted.MaterializeMetadata(ctx, provider, req)
			if err != nil || again.Key != core.MovieKey(19) {
				t.Fatalf("reenable identity: %+v %v", again, err)
			}
		})
	}
}
