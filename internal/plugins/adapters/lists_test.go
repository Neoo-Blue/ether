package adapters_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/agent/tools"
	"github.com/braedonsaunders/ether/internal/catalog"
	"github.com/braedonsaunders/ether/internal/collections"
	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/personalize"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/plugins/adapters"
	"github.com/braedonsaunders/ether/internal/store"
)

type listChannelFakeRuntime struct {
	mu     sync.Mutex
	calls  []plugins.Request
	routes map[string]func(plugins.Request) (plugins.Response, error)
}

func (f *listChannelFakeRuntime) Call(_ context.Context, req plugins.Request) (plugins.Response, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	fn := f.routes[req.Path]
	if fn == nil {
		for prefix, candidate := range f.routes {
			if strings.HasPrefix(req.Path, prefix) {
				fn = candidate
				break
			}
		}
	}
	f.mu.Unlock()
	if fn != nil {
		return fn(req)
	}
	switch {
	case strings.HasPrefix(req.Path, "/ether/health"):
		return plugins.Response{Status: 200, Body: json.RawMessage(`{"status":"ok"}`)}, nil
	case strings.HasPrefix(req.Path, "/ether/test"):
		return plugins.Response{Status: 200, Body: json.RawMessage(`{"ok":true}`)}, nil
	default:
		return plugins.Response{Status: 404, Body: json.RawMessage(`{"error":"not found"}`)}, nil
	}
}

func (f *listChannelFakeRuntime) Close() error { return nil }

func (f *listChannelFakeRuntime) paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = c.Path
	}
	return out
}

func (f *listChannelFakeRuntime) resetCalls() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = nil
}

type listTestHarness struct {
	db   *sql.DB
	cat  *catalog.Catalog
	cols *collections.Service
	pers *personalize.Engine
	mgr  *plugins.Manager
	reg  *extend.Registry
	rt   *listChannelFakeRuntime
}

func newListTestHarness(t *testing.T) *listTestHarness {
	t.Helper()
	ctx := context.Background()
	db := store.OpenTest(t)
	catCfg := config.Catalog{Language: "en-US", Region: "US", MinPopularity: 1, MinVotes: 1}
	cat, err := catalog.New(db, nil, catCfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	seed := []core.Title{
		{
			Key:        core.MovieKey(101),
			Type:       core.TypeMovie,
			IDs:        core.IDs{TMDB: 101, IMDB: "tt0133093"},
			Title:      "The Matrix",
			Year:       1999,
			Released:   time.Date(1999, 3, 31, 0, 0, 0, 0, time.UTC),
			Rating:     8.7,
			Popularity: 90,
			VoteCount:  10000,
		},
		{
			Key:        core.MovieKey(102),
			Type:       core.TypeMovie,
			IDs:        core.IDs{TMDB: 102, IMDB: "tt0234215"},
			Title:      "The Matrix Reloaded",
			Year:       2003,
			Released:   time.Date(2003, 5, 15, 0, 0, 0, 0, time.UTC),
			Rating:     7.2,
			Popularity: 75,
			VoteCount:  6000,
		},
		{
			Key:        core.MovieKey(103),
			Type:       core.TypeMovie,
			IDs:        core.IDs{TMDB: 103, IMDB: "tt0242653"},
			Title:      "The Matrix Revolutions",
			Year:       2003,
			Released:   time.Date(2003, 11, 5, 0, 0, 0, 0, time.UTC),
			Rating:     6.7,
			Popularity: 70,
			VoteCount:  5500,
		},
		{
			Key:        core.ShowKey(201),
			Type:       core.TypeShow,
			IDs:        core.IDs{TMDB: 201, IMDB: "tt0903747"},
			Title:      "Breaking Bad",
			Year:       2008,
			Released:   time.Date(2008, 1, 20, 0, 0, 0, 0, time.UTC),
			Rating:     9.5,
			Popularity: 99,
			VoteCount:  15000,
		},
	}
	for _, title := range seed {
		if err := cat.UpsertTitle(ctx, title, true); err != nil {
			t.Fatal(err)
		}
	}

	cols, err := collections.New(
		db,
		cat,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		collections.WithoutDefaults(),
		collections.WithIndexTTL(time.Hour),
		collections.WithLiveTTL(time.Hour),
	)
	if err != nil {
		t.Fatal(err)
	}
	pers, err := personalize.New(personalize.Options{
		DB:          db,
		Catalog:     cat,
		Collections: cols,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	reg := extend.New()
	rt := &listChannelFakeRuntime{routes: map[string]func(plugins.Request) (plugins.Response, error){}}

	mgr, err := plugins.New(plugins.Deps{
		DB:       db,
		Registry: reg,
		Tools:    tools.NewCatalogue(),
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		IsLeader: func() bool { return true },
		Now:      func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.RegisterRuntime("hosted", func(_ plugins.Installed) (plugins.Runtime, error) {
		return rt, nil
	})
	adapters.Attach(mgr, cols, pers, nil)

	return &listTestHarness{
		db:   db,
		cat:  cat,
		cols: cols,
		pers: pers,
		mgr:  mgr,
		reg:  reg,
		rt:   rt,
	}
}

func installListPlugin(t *testing.T, mgr *plugins.Manager, maxItems int) plugins.Record {
	t.Helper()
	man := map[string]any{
		"id":      "community.lists.curated",
		"version": "1.0.0",
		"name":    "Curated Lists",
		"ether": map[string]any{
			"protocol":     1,
			"kind":         "extension",
			"summary":      "$t:summary",
			"runtime":      map[string]any{"kind": "hosted", "entry": "dist/plugin.js"},
			"capabilities": []string{"list_source"},
			"strings":      map[string]any{"en": "i18n/en.json"},
			"contributes": map[string]any{
				"listSources": []map[string]any{
					{
						"key":      "staff_picks",
						"label":    "$t:listLabel",
						"hint":     "$t:listHint",
						"catalog":  "movie/staff_picks",
						"refresh":  "6h",
						"maxItems": maxItems,
						"params": []map[string]any{
							{"key": "genre", "kind": "text", "label": "Genre", "required": true},
						},
					},
				},
			},
		},
	}
	rawMan, err := json.Marshal(man)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"dist/plugin.js": []byte("// curated list plugin"),
		"i18n/en.json":   []byte(`{"summary":"Curated movie shelves","listLabel":"Staff picks","listHint":"Hand-picked movies"}`),
	}
	preview, err := mgr.PreviewInstall(context.Background(), plugins.Source{
		Kind:     "studio",
		Manifest: rawMan,
		Files:    files,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := mgr.Install(context.Background(), preview, plugins.InstallOptions{AcceptAddedPermissions: true})
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestListSourceLifecycleAndMatching(t *testing.T) {
	h := newListTestHarness(t)
	ctx := context.Background()

	rec := installListPlugin(t, h.mgr, 50)
	kindID := extend.PluginID(rec.ID, "staff_picks")

	// Collection source kind and Home row source kind are registered on install.
	sk, ok := h.cols.KindRegistry().Lookup(kindID)
	if !ok {
		t.Fatalf("expected collection SourceKind %s to be registered", kindID)
	}
	kd := sk.Describe()
	if len(kd.Params) != 1 || kd.Params[0].Name != "genre" || !kd.Params[0].Required {
		t.Fatalf("unexpected source kind params: %+v", kd.Params)
	}
	if _, ok := h.pers.RowKindRegistry().Lookup(kindID); !ok {
		t.Fatalf("expected personalize RowSourceKind %s to be registered", kindID)
	}
	// Extend descriptors exist for both collectionSource and rowSource.
	if _, ok := h.reg.Get(extend.KindCollectionSource, kindID); !ok {
		t.Fatalf("expected extend collectionSource descriptor for %s", kindID)
	}
	if _, ok := h.reg.Get(extend.KindRowSource, kindID); !ok {
		t.Fatalf("expected extend rowSource descriptor for %s", kindID)
	}

	// Required parameter validation rejects missing genre.
	_, err := h.cols.Preview(ctx, core.CollectionSpec{
		Name:      "Missing Param Preview",
		MediaType: "movie",
		Sources:   []core.CollectionSource{{Kind: kindID}},
	}, 10)
	if err == nil {
		t.Fatal("expected validation error when required param is missing")
	}

	// Configure route with page 1 (skip=0) and page 2 (skip=2), including
	// IMDB id, TMDB id, and an unmatched external item.
	h.rt.routes["/catalog/movie/staff_picks/genre=scifi.json"] = func(_ plugins.Request) (plugins.Response, error) {
		return plugins.Response{
			Status: 200,
			Body: json.RawMessage(`{
				"metas": [
					{"id": "tt0133093", "type": "movie", "name": "The Matrix"},
					{"id": "tmdb:102", "type": "movie", "name": "The Matrix Reloaded"}
				]
			}`),
		}, nil
	}
	h.rt.routes["/catalog/movie/staff_picks/genre=scifi&skip=2.json"] = func(_ plugins.Request) (plugins.Response, error) {
		return plugins.Response{
			Status: 200,
			Body: json.RawMessage(`{
				"metas": [
					{"imdb_id": "tt9999999", "type": "movie", "name": "Unmatched Future Film", "year": 2029},
					{"tmdb_id": 103, "type": "movie", "name": "The Matrix Revolutions"}
				],
				"hasMore": false
			}`),
		}, nil
	}

	spec := core.CollectionSpec{
		Name:      "Sci-Fi Picks",
		MediaType: "movie",
		Sources: []core.CollectionSource{
			{Kind: kindID, Params: map[string]string{"genre": "scifi"}},
		},
		Sort:    "source",
		Refresh: "daily",
		Enabled: true,
		Targets: core.CollectionTargets{HomeRow: true, CollectionsTab: true, Users: core.AllUsers()},
	}

	prev, err := h.cols.Preview(ctx, spec, 10)
	if err != nil {
		t.Fatalf("Preview failed: %v", err)
	}
	if prev.Total != 2 {
		t.Fatalf("expected 2 preview titles from first page, got %d (warnings: %v)", prev.Total, prev.Warnings)
	}
	if prev.Items[0].Title != "The Matrix" || prev.Items[1].Title != "The Matrix Reloaded" {
		t.Fatalf("unexpected matched titles: %+v", prev.Items)
	}

	// Create a persisted spec collection and verify reading its items on the
	// request path serves from the cached collection without calling the plugin.
	created, err := h.cols.CreateSpec(ctx, spec)
	if err != nil {
		t.Fatalf("CreateSpec failed: %v", err)
	}
	h.rt.resetCalls()
	rows, err := h.cols.HomeRowsFor(ctx, &core.User{ID: 1, IsOwner: true}, core.HubContext{Now: time.Now()})
	if err != nil || len(rows) != 1 || len(rows[0].Hub.Items) != 3 {
		t.Fatalf("cached HomeRowsFor failed: rows=%+v err=%v", rows, err)
	}
	if gotCalls := h.rt.paths(); len(gotCalls) != 0 {
		t.Fatalf("expected zero plugin calls on request-path HomeRowsFor read, got %v (spec id=%s)", gotCalls, created.ID)
	}

	// Disabling the plugin unregisters both the collection kind and Home row kind.
	if err := h.mgr.SetEnabled(ctx, rec.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.cols.KindRegistry().Lookup(kindID); ok {
		t.Fatal("expected collection SourceKind to be unregistered on disable")
	}
	if _, ok := h.pers.RowKindRegistry().Lookup(kindID); ok {
		t.Fatal("expected RowSourceKind to be unregistered on disable")
	}

	// Re-enabling restores both registrations; removing deletes them.
	if err := h.mgr.SetEnabled(ctx, rec.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.cols.KindRegistry().Lookup(kindID); !ok {
		t.Fatal("expected collection SourceKind to be re-registered on enable")
	}
	if err := h.mgr.Remove(ctx, rec.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.cols.KindRegistry().Lookup(kindID); ok {
		t.Fatal("expected collection SourceKind to be unregistered on remove")
	}
	if _, ok := h.pers.RowKindRegistry().Lookup(kindID); ok {
		t.Fatal("expected RowSourceKind to be unregistered on remove")
	}
}

func TestListSourceMaxItemsCapAndHealthFailure(t *testing.T) {
	h := newListTestHarness(t)
	ctx := context.Background()

	// Install with maxItems=2 and verify paging stops once 2 items are gathered.
	rec := installListPlugin(t, h.mgr, 2)
	kindID := extend.PluginID(rec.ID, "staff_picks")

	h.rt.routes["/catalog/movie/staff_picks/genre=action.json"] = func(_ plugins.Request) (plugins.Response, error) {
		return plugins.Response{
			Status: 200,
			Body: json.RawMessage(`{
				"metas": [
					{"id": "tt0133093", "type": "movie"},
					{"id": "tt0234215", "type": "movie"},
					{"id": "tt0242653", "type": "movie"}
				]
			}`),
		}, nil
	}

	prev, err := h.cols.Preview(ctx, core.CollectionSpec{
		Name:      "Action Cap Preview",
		MediaType: "movie",
		Sources:   []core.CollectionSource{{Kind: kindID, Params: map[string]string{"genre": "action"}}},
	}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if prev.Total != 2 {
		t.Fatalf("expected maxItems=2 cap to limit results to 2, got %d", prev.Total)
	}

	// When the plugin returns HTTP 500, health failure is recorded and 5 consecutive
	// failures auto-disable the plugin and unregister its source kinds.
	h.rt.routes["/catalog/movie/staff_picks/genre=broken.json"] = func(_ plugins.Request) (plugins.Response, error) {
		return plugins.Response{Status: 500, Body: json.RawMessage(`{"error":"upstream down"}`)}, nil
	}
	for i := 0; i < 5; i++ {
		_, err := h.cols.Preview(ctx, core.CollectionSpec{
			Name:      "Broken Preview",
			MediaType: "movie",
			Sources:   []core.CollectionSource{{Kind: kindID, Params: map[string]string{"genre": "broken"}}},
		}, 10)
		if err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
	}
	updated, err := h.mgr.Get(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled {
		t.Fatal("expected plugin to be auto-disabled after 5 consecutive catalog failures")
	}
	health, err := h.mgr.HealthOf(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(health.Failures) < 5 || health.Status != "disabled" {
		t.Fatalf("expected at least 5 recorded failures and disabled status, got %+v", health)
	}
	if _, ok := h.cols.KindRegistry().Lookup(kindID); ok {
		t.Fatal("expected auto-disabled plugin to unregister its collection SourceKind")
	}
}
