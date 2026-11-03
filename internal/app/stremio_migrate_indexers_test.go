package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/store"
)

type nopRuntime struct{}

func (nopRuntime) Call(context.Context, plugins.Request) (plugins.Response, error) {
	return plugins.Response{Status: http.StatusOK, Body: []byte(`{"streams":[]}`)}, nil
}
func (nopRuntime) Close() error { return nil }

func migrationApp(t *testing.T, indexers []config.Indexer) *App {
	t.Helper()
	cfg, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Update(func(c *config.Config) error { c.Indexers = indexers; return nil }); err != nil {
		t.Fatal(err)
	}
	mgr, err := plugins.New(plugins.Deps{DB: store.OpenTest(t), Sealer: cfg, Logger: quietLog()})
	if err != nil {
		t.Fatal(err)
	}
	mgr.RegisterRuntime("remote", func(plugins.Installed) (plugins.Runtime, error) { return nopRuntime{}, nil })
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return &App{cfg: cfg, pluginsRT: &pluginRuntime{mgr: mgr}, log: quietLog()}
}

// A Stremio add-on entered as an indexer link becomes an installed add-on
// with only its stream role on, and its row keeps id, name, priority and
// limits (so release ids, pins and blocks are unchanged) without its link.
// Running the migration again changes nothing; a second row with the same
// link reuses the add-on.
func TestLegacyStremioIndexersBecomeAddonRows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"org.example.streams","version":"1.0.0","name":"Example streams","types":["movie","series"],"resources":["stream","catalog","subtitles"],"catalogs":[{"type":"movie","id":"top"}],"idPrefixes":["tt"]}`)
	}))
	defer srv.Close()
	link := srv.URL + "/KEY123456789/manifest.json?debrid=SECRET"
	a := migrationApp(t, []config.Indexer{
		{ID: "nz", Name: "Usenet", URL: "https://indexer.example", Kind: config.IndexerKindNewznab, Enabled: true, Priority: 1},
		{ID: "stremio-1", Name: "Torrents", URL: link, Kind: config.IndexerKindStremio, Protocol: "torrent", Enabled: true, Priority: 2, DailyLimit: 40},
		{ID: "stremio-2", Name: "Same link", URL: srv.URL + "/KEY123456789/configure?debrid=SECRET", Kind: config.IndexerKindStremio, Enabled: false, Priority: 3},
	})
	ctx := context.Background()
	a.migrateLegacyStremio(ctx)

	rows := a.cfg.Get().Indexers
	// Two legacy rows of one link become one add-on row (the first keeps
	// its place); no extra row is added for the add-on.
	if len(rows) != 2 {
		t.Fatalf("rows: %+v", rows)
	}
	got := rows[1]
	if got.ID != "stremio-1" || got.Name != "Torrents" || got.Priority != 2 || got.DailyLimit != 40 || got.Kind != config.IndexerKindAddon || got.URL != "" || got.Plugin == "" {
		t.Fatalf("migrated row: %+v", got)
	}
	rec, err := a.pluginManager().Get(ctx, got.Plugin)
	if err != nil {
		t.Fatal(err)
	}
	if !rec.Manifest.HasCapability(plugins.CapStreamSource) || rec.Manifest.HasCapability(plugins.CapListSource) || rec.Manifest.HasCapability(plugins.CapSubtitles) {
		t.Fatalf("roles: off=%v", rec.RolesOff)
	}
	if rec.SourceURL != srv.URL+"/KEY123456789?debrid=SECRET" {
		t.Fatalf("link not kept with its configuration: %q", rec.SourceURL)
	}
	// The configure link and the manifest link are the same add-on.
	recs, _ := a.pluginManager().List(ctx)
	if len(recs) != 1 {
		t.Fatalf("add-ons: %d", len(recs))
	}

	a.migrateLegacyStremio(ctx)
	again := a.cfg.Get().Indexers
	for i := range again {
		if again[i].ID != rows[i].ID || again[i].Kind != rows[i].Kind || again[i].Plugin != rows[i].Plugin || again[i].URL != rows[i].URL {
			t.Fatalf("second run changed row %d: %+v -> %+v", i, rows[i], again[i])
		}
	}

	// The search manager sees the add-on row with its link in memory only.
	expanded := a.addonIndexers(ctx, again)
	if !expanded[1].Enabled || expanded[1].URL != rec.SourceURL || a.cfg.Get().Indexers[1].URL != "" {
		t.Fatalf("expanded: %+v", expanded[1])
	}
}

// An add-on whose host is down while it is migrated is recorded from what
// its legacy entry proves (it serves streams), so playback never waits on
// the migration; the hourly refresh reads its real manifest later.
func TestLegacyStremioIndexerMigratesWhileAddonIsDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	a := migrationApp(t, []config.Indexer{{ID: "stremio-9", Name: "Down", URL: srv.URL + "/cfg/manifest.json", Kind: config.IndexerKindStremio, Enabled: true}})
	ctx := context.Background()
	a.migrateLegacyStremio(ctx)
	row := a.cfg.Get().Indexers[0]
	if row.Kind != config.IndexerKindAddon || row.Plugin == "" {
		t.Fatalf("row: %+v", row)
	}
	rec, err := a.pluginManager().Get(ctx, row.Plugin)
	if err != nil || !rec.Manifest.HasCapability(plugins.CapStreamSource) {
		t.Fatalf("offline add-on: %+v %v", rec, err)
	}
}
