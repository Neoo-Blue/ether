package collections_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/braedonsaunders/ether/internal/collections"
	"github.com/braedonsaunders/ether/internal/core"
)

func TestStremioCatalogValidation(t *testing.T) {
	base := spec("Stremio Top", "movie", []core.CollectionSource{{
		Kind:      "stremioCatalog",
		URL:       "https://cinemeta.example.test/manifest.json",
		List:      "top",
		MediaType: "movie",
		MaxItems:  100,
	}})
	collections.Normalize(&base)

	if err := collections.Validate(&base); err != nil {
		t.Fatalf("expected valid stremioCatalog spec, got %v", err)
	}

	liveSpec := base
	liveSpec.Refresh = collections.RefreshLive
	collections.Normalize(&liveSpec)
	if err := collections.Validate(&liveSpec); err == nil || !strings.Contains(err.Error(), "refresh") {
		t.Fatalf("expected RefreshLive to be rejected for stremioCatalog, got %v", err)
	}

	overMax := base
	overMax.Sources = []core.CollectionSource{{
		Kind:     "stremioCatalog",
		URL:      "https://cinemeta.example.test/manifest.json",
		List:     "top",
		MaxItems: 2001,
	}}
	collections.Normalize(&overMax)
	if err := collections.Validate(&overMax); err == nil || !strings.Contains(err.Error(), "maxItems") {
		t.Fatalf("expected maxItems > 2000 to be rejected, got %v", err)
	}

	missingList := base
	missingList.Sources = []core.CollectionSource{{
		Kind: "stremioCatalog",
		URL:  "https://cinemeta.example.test/manifest.json",
	}}
	collections.Normalize(&missingList)
	if err := collections.Validate(&missingList); err == nil || !strings.Contains(err.Error(), "list") {
		t.Fatalf("expected missing list to be rejected, got %v", err)
	}
}

func TestStremioCatalogGatherAndUnmatchedCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/secret-token/manifest.json":
			fmt.Fprint(w, `{
				"id": "org.stremio.cinemeta",
				"name": "Cinemeta",
				"version": "3.0.0",
				"resources": ["catalog"],
				"types": ["movie", "series"],
				"catalogs": [
					{"type": "movie", "id": "top", "name": "Popular Movies", "extra": [{"name": "genre", "options": ["Action", "Sci-Fi"]}]},
					{"type": "series", "id": "top", "name": "Popular Series"}
				]
			}`)
		case "/secret-token/catalog/movie/top.json":
			// Includes:
			// - tt0000101 -> matches fixture 101 (Space Saga) via IMDb
			// - tmdb:107  -> matches fixture 107 (Dream Heist) via TMDB
			// - kitsu:999 -> non-IMDb/TMDB id with a matching title "Prison Hope" (must NOT be matched by title, D7)
			// - tt0999999 -> IMDb id not in catalogue (unmatched)
			fmt.Fprint(w, `{"metas":[
				{"id":"tt0000101","type":"movie","name":"Space Saga"},
				{"id":"tmdb:107","type":"movie","name":"Dream Heist"},
				{"id":"kitsu:999","type":"movie","name":"Prison Hope"},
				{"id":"tt0999999","type":"movie","name":"Unknown Film"}
			]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	e := newEnv(t, withOpts(collections.WithStremioHTTPClient(srv.Client())))

	// Inspect manifest helper.
	summary, err := e.svc.InspectStremioManifest(e.ctx, srv.URL+"/secret-token/manifest.json")
	if err != nil {
		t.Fatalf("InspectStremioManifest: %v", err)
	}
	if summary.Name != "Cinemeta" || len(summary.Catalogs) != 2 {
		t.Fatalf("unexpected manifest summary: %+v", summary)
	}

	sp := spec("Cinemeta Top", "movie", []core.CollectionSource{{
		Kind:      "stremioCatalog",
		URL:       srv.URL + "/secret-token/manifest.json",
		List:      "top",
		MediaType: "movie",
		MaxItems:  50,
	}})
	sp.Sort = "source"
	sp.SortDesc = false

	preview, err := e.svc.Preview(e.ctx, sp, 50)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(preview.Items) != 0 || len(preview.Warnings) == 0 {
		t.Fatalf("preview should not fetch: %+v", preview)
	}

	created, err := e.svc.CreateSpec(e.ctx, sp)
	if err != nil {
		t.Fatalf("CreateSpec: %v", err)
	}
	created, err = e.svc.Rebuild(e.ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, warning := range created.Warnings {
		if strings.Contains(warning, "2 of 4 items in the Stremio catalog could not be matched to the catalogue") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing unmatched count: %v", created.Warnings)
	}
	page, err := e.svc.Items(e.ctx, owner, core.SmartKey(mustInt(t, created.ID)), 0, 50)
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	if len(page.Items) != 2 || page.Items[0].Key != core.MovieKey(101) || page.Items[1].Key != core.MovieKey(107) {
		t.Fatalf("published items = %+v", page.Items)
	}
}
