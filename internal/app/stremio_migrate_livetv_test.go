package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/agent/tools"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/livetv"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/plugins/adapters"
	"github.com/braedonsaunders/ether/internal/plugins/remote"
	"github.com/braedonsaunders/ether/internal/store"
)

// A built-in Stremio Live TV source becomes the add-on's channel source in
// place, and its channels keep every owner choice across the next refresh.
func TestMigrateLegacyStremioLiveTVKeepsChannels(t *testing.T) {
	addon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/key123/manifest.json":
			fmt.Fprint(w, `{"id":"tv.example","version":"1.0.0","name":"Example TV","resources":["catalog","stream"],"types":["tv"],"catalogs":[{"type":"tv","id":"live","name":"News"}]}`)
		case "/key123/catalog/tv/live.json":
			fmt.Fprint(w, `{"metas":[{"id":"news","name":"News 24"}]}`)
		case "/key123/stream/tv/news.json":
			fmt.Fprint(w, `{"streams":[{"name":"HD","url":"https://live.example/news/hd?key=fresh"},{"name":"SD","url":"https://live.example/news/sd?key=fresh"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer addon.Close()
	ctx := context.Background()
	now := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	db := store.OpenTest(t)
	tv, err := livetv.New(livetv.Options{DB: db, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer tv.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr, err := plugins.New(plugins.Deps{DB: db, Registry: extend.New(), Tools: tools.NewCatalogue(), Logger: log, IsLeader: func() bool { return true }, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	mgr.RegisterRuntime("remote", (&remote.Factory{TrustedLAN: remote.GrantsLAN(mgr.GrantsOf), Logger: log}).RuntimeFactory())
	adapters.Attach(mgr, nil, nil, tv)
	a := &App{livetv: tv, pluginsRT: &pluginRuntime{mgr: mgr}, log: log}

	// What the built-in Stremio source stored.
	link := addon.URL + "/key123/manifest.json"
	raw, _ := json.Marshal(map[string]any{"id": "old", "name": "My sports", "kind": "stremio", "playlistUrl": link, "refreshHours": 12, "updatedAt": now})
	if _, err := db.Exec(`INSERT INTO livetv_sources(id,data) VALUES(?,?)`, "old", string(raw)); err != nil {
		t.Fatal(err)
	}
	chID := legacyStableID("old", "news")
	ch := livetv.Channel{ID: chID, SourceID: "old", GuideID: "news.us", Name: "News 24", Number: "701", Kind: "iptv", Enabled: true, Favorite: true, Rating: "TV-PG",
		StreamSources: []livetv.StreamSource{
			{ID: "stremio-" + legacyStableID(chID, "0"), Name: "HD", URL: "https://live.example/news/hd?key=old", Enabled: false},
			{ID: "stremio-" + legacyStableID(chID, "1"), Name: "SD", URL: "https://live.example/news/sd?key=old", Enabled: true, Priority: 1},
			{ID: "mine", Name: "Backup", URL: "https://backup.example/news", Enabled: true, Priority: 2},
		}}
	if _, err := tv.SaveChannel(ctx, ch); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if err := a.migrateLegacyStremioLiveTV(ctx); err != nil {
			t.Fatal(err)
		}
	}
	installed, err := mgr.List(ctx)
	if err != nil || len(installed) != 1 {
		t.Fatalf("installed = %d err=%v", len(installed), err)
	}
	rec := installed[0]
	kind := extend.PluginID(rec.ID, plugins.StremioChannelsKey)
	if !rec.Manifest.HasCapability(plugins.CapLiveTV) {
		t.Fatalf("live TV role off: %+v", rec.Manifest.EffectiveEther().Capabilities)
	}
	srcs, err := tv.Sources(ctx)
	if err != nil || len(srcs) != 1 || srcs[0].ID != "old" || srcs[0].Kind != kind || srcs[0].Name != "My sports" || strings.Contains(srcs[0].PlaylistURL, "key123") {
		t.Fatalf("sources = %+v err=%v", srcs, err)
	}

	if _, err := tv.RefreshSource(ctx, "old"); err != nil {
		t.Fatal(err)
	}
	got, err := tv.Channel(ctx, chID)
	if err != nil {
		t.Fatal(err)
	}
	in := got.StreamSources
	if got.Number != "701" || !got.Favorite || got.Rating != "TV-PG" || got.GuideID != "news.us" || len(in) != 3 ||
		in[0].Enabled || in[0].URL != "https://live.example/news/hd?key=fresh" || !in[1].Enabled || in[2].ID != "mine" {
		t.Fatalf("channel after refresh = %+v", got)
	}
	all, _ := tv.Channels(ctx)
	if len(all) != 1 {
		t.Fatalf("refresh duplicated channels: %d", len(all))
	}
}

func legacyStableID(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:10])
}
