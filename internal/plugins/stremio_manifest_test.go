package plugins

import (
	"context"
	"encoding/json"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/stremio"
	"strings"
	"testing"
)

func TestPlainStremioCopyAndNativeValidation(t *testing.T) {
	m := Manifest{ID: "org.Example.Addon", Name: strings.Repeat("Long name ", 10), Version: "1.0.0", Description: strings.Repeat("Simply watch — enjoy! ", 30), Resources: stremio.ParseResources(`["catalog","meta","subtitles",{"name":"stream","types":["movie"],"idPrefixes":["tt"]}]`), Types: []string{"movie"}, Catalogs: []StremioCat{{Type: "movie", ID: "top", Name: "Popular"}}}
	raw, _ := json.Marshal(m)
	if err := m.Validate(); err != nil {
		t.Fatalf("foreign copy rejected: %v", err)
	}
	ds := DescriptorsFor(m)
	counts := map[extend.Kind]int{}
	for _, d := range ds {
		counts[d.Kind]++
	}
	for _, kind := range []extend.Kind{extend.KindCollectionSource, extend.KindRowSource, extend.KindSourceKind, extend.KindSubtitleProvider, extend.KindMetadataProvider} {
		if counts[kind] != 1 {
			t.Fatalf("missing native contribution %s: %+v", kind, ds)
		}
	}
	after, _ := json.Marshal(m)
	if string(raw) != string(after) {
		t.Fatal("descriptor derivation modified upstream manifest")
	}
	native := baseManifest()
	native.Description = m.Description
	if !hasField(native.Validate(), "description") {
		t.Fatal("Ether-authored copy lint bypassed")
	}
}
func TestStremioResourceLocalFilters(t *testing.T) {
	m := Manifest{Types: []string{"movie"}, IDPrefixes: []string{"tmdb:"}, Resources: stremio.ParseResources(`["meta",{"name":"stream","types":["series"]},{"name":"subtitles","types":["movie"],"idPrefixes":["tt"]}]`)}
	for _, c := range []struct {
		r, typ, id string
		want       bool
	}{{"meta", "movie", "tmdb:1", true}, {"meta", "movie", "tt1", false}, {"meta", "series", "tmdb:1", false}, {"stream", "series", "tt1:1:1", true}, {"stream", "movie", "tt1", false}, {"subtitles", "movie", "tt1", true}, {"subtitles", "movie", "tmdb:1", false}} {
		if got := m.AcceptsResource(c.r, c.typ, c.id); got != c.want {
			t.Errorf("%+v got %v", c, got)
		}
	}
}
func TestStremioCatalogRequiredExtrasAndLifecycle(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	raw := json.RawMessage(`{"id":"org.example.catalog","version":"1.0.0","name":"Foreign catalogue","description":"just test it!","types":["movie"],"resources":["catalog","meta"],"catalogs":[{"id":"top","type":"movie","name":"Popular","extra":[{"name":"genre","isRequired":true,"options":["Action","Drama"]},{"name":"skip"}]}]}`)
	preview, err := e.m.PreviewInstall(ctx, Source{Kind: "studio", Manifest: raw})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := e.m.Install(ctx, preview, InstallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	e.fake.routes["/catalog/movie/top/genre=Action&skip=100.json"] = func(req Request) (Response, error) {
		return Response{Status: 200, Body: json.RawMessage(`{"metas":[{"id":"tt1","type":"movie","name":"Test"}]}`)}, nil
	}
	if _, err = e.m.StremioResource(ctx, rec.ID, "catalog", "movie", "top", nil); !hasField(err, "genre") {
		t.Fatalf("missing required extra accepted: %v", err)
	}
	if _, err = e.m.StremioResource(ctx, rec.ID, "catalog", "movie", "top", map[string]string{"genre": "Unknown"}); !hasField(err, "genre") {
		t.Fatalf("undeclared option accepted: %v", err)
	}
	response, err := e.m.StremioResource(ctx, rec.ID, "catalog", "movie", "top", map[string]string{"genre": "Action", "skip": "100"})
	if err != nil || response.Status != 200 {
		t.Fatalf("browse: %+v %v", response, err)
	}
	if err = e.m.SetEnabled(ctx, rec.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err = e.m.StremioResource(ctx, rec.ID, "catalog", "movie", "top", map[string]string{"genre": "Action"}); err != ErrDisabled {
		t.Fatalf("disabled fetch: %v", err)
	}
	if err = e.m.SetEnabled(ctx, rec.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err = e.m.StremioResource(ctx, rec.ID, "catalog", "movie", "top", map[string]string{"genre": "Action", "skip": "100"}); err != nil {
		t.Fatal(err)
	}
}

func TestStremioDirectoryTypesUseTheirOwnDeclarations(t *testing.T) {
	m := Manifest{Types: []string{"movie", "series"}, Resources: stremio.ParseResources(`["catalog","addon_catalog"]`), Catalogs: []StremioCat{{Type: "movie", ID: "top"}}, AddonCatalogs: []StremioCat{{Type: "all", ID: "official"}, {Type: "channel", ID: "community"}}}
	if !m.AcceptsResource("addon_catalog", "all", "official") || !m.AcceptsResource("addon_catalog", "channel", "community") {
		t.Fatal("directory types incorrectly restricted to title types")
	}
	if m.AcceptsResource("addon_catalog", "movie", "undeclared") || m.AcceptsResource("catalog", "movie", "undeclared") {
		t.Fatal("undeclared catalogue accepted")
	}
}
