package collections_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/braedonsaunders/ether/internal/collections"
	"github.com/braedonsaunders/ether/internal/core"
)

// boundSealer seals a value to its context, like the config sealer: a link
// sealed for one collection does not open for another.
type boundSealer struct{ values map[string]string }

func (s *boundSealer) SealField(plain string, context ...string) (string, error) {
	key := strings.Join(context, "|")
	s.values[key] = plain
	return "sealed:" + key, nil
}

func (s *boundSealer) OpenField(stored string, context ...string) (string, bool, error) {
	key := strings.Join(context, "|")
	if stored != "sealed:"+key {
		return "", false, fmt.Errorf("context mismatch")
	}
	return s.values[key], false, nil
}

func TestMigrateLegacyStremioSourcesRewritesInPlace(t *testing.T) {
	sealer := &boundSealer{values: map[string]string{}}
	e := newEnv(t, withOpts(collections.WithSealer(sealer)))
	db := e.db.(*sql.DB)
	created, err := e.svc.CreateSpec(e.ctx, spec("Add-on picks", "movie", []core.CollectionSource{{Kind: "titles", Keys: []string{core.MovieKey(101).String()}}}))
	if err != nil {
		t.Fatal(err)
	}
	built, err := e.svc.Rebuild(e.ctx, created.ID)
	if err != nil || built.LastBuiltAt == nil {
		t.Fatalf("build: %+v %v", built, err)
	}

	// Store the collection as the retired Stremio catalog source left it:
	// a sealed add-on link bound to this collection.
	link := "https://addon.example/secret-config/manifest.json"
	sealed, _ := sealer.SealField(link, "collections_specs", "sources.url", created.ID, collections.LegacyStremioCatalogKind)
	var raw string
	if err := db.QueryRow(`SELECT spec_json FROM collections_specs WHERE id=?`, created.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var stored core.CollectionSpec
	_ = json.Unmarshal([]byte(raw), &stored)
	// A mixed collection's source without a media type read its catalog
	// for movies and series.
	stored.MediaType = collections.MediaMixed
	stored.Sources = []core.CollectionSource{
		{Kind: collections.LegacyStremioCatalogKind, URL: sealed, List: "top", MaxItems: 150, Params: map[string]string{"genre": "Action"}},
		{Kind: "titles", Keys: []string{core.MovieKey(107).String()}},
	}
	legacyJSON, _ := json.Marshal(stored)
	if _, err := db.Exec(`UPDATE collections_specs SET spec_json=? WHERE id=?`, string(legacyJSON), created.ID); err != nil {
		t.Fatal(err)
	}

	// A failing migration leaves the source exactly as it was.
	boom := errors.New("add-on unreachable")
	if err := e.svc.MigrateLegacyStremioSources(e.ctx, func(context.Context, core.CollectionSource) (core.CollectionSource, error) {
		return core.CollectionSource{}, boom
	}); !errors.Is(err, boom) {
		t.Fatalf("failing migration err = %v", err)
	}
	var after string
	_ = db.QueryRow(`SELECT spec_json FROM collections_specs WHERE id=?`, created.ID).Scan(&after)
	if after != string(legacyJSON) {
		t.Fatalf("failed migration changed the spec: %s", after)
	}

	var got []core.CollectionSource
	migrate := func(_ context.Context, src core.CollectionSource) (core.CollectionSource, error) {
		got = append(got, src)
		return core.CollectionSource{Kind: "plugin:addon.x/" + src.MediaType + "-" + src.List, Params: src.Params, MaxItems: src.MaxItems}, nil
	}
	if err := e.svc.MigrateLegacyStremioSources(e.ctx, migrate); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].URL != link || got[0].List != "top" || got[0].MediaType != "movie" || got[1].MediaType != "show" {
		t.Fatalf("migrate saw %+v, want the opened link once per media type", got)
	}
	sp, err := e.svc.GetSpec(e.ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sp.ID != created.ID || len(sp.Sources) != 3 || sp.Sources[0].Kind != "plugin:addon.x/movie-top" || sp.Sources[1].Kind != "plugin:addon.x/show-top" ||
		sp.Sources[0].URL != "" || sp.Sources[0].Params["genre"] != "Action" || sp.Sources[0].MaxItems != 150 || sp.Sources[2].Kind != "titles" {
		t.Fatalf("migrated spec = %+v", sp)
	}
	if sp.LastBuiltAt == nil || !sp.LastBuiltAt.Equal(*built.LastBuiltAt) || sp.ItemCount != built.ItemCount {
		t.Fatalf("build state not kept: %+v, was %+v", sp, built)
	}

	// Running it again finds nothing to migrate.
	got = nil
	if err := e.svc.MigrateLegacyStremioSources(e.ctx, migrate); err != nil || len(got) != 0 {
		t.Fatalf("second run: calls=%d err=%v", len(got), err)
	}
}
