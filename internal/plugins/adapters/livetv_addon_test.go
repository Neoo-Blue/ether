package adapters_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/agent/tools"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/livetv"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/plugins/adapters"
	"github.com/braedonsaunders/ether/internal/plugins/remote"
	"github.com/braedonsaunders/ether/internal/store"
)

// addonTV is Live TV and the plugin host wired as in the server, with a
// real URL runtime: the add-on is an httptest server on loopback, which
// InstallAddon trusts as a LAN add-on the owner pasted.
type addonTV struct {
	tv   *livetv.Service
	mgr  *plugins.Manager
	reg  *extend.Registry
	rec  plugins.Record
	kind string
	link string
}

func newAddonTV(t *testing.T, now time.Time, handler http.HandlerFunc) *addonTV {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	db := store.OpenTest(t)
	tv, err := livetv.New(livetv.Options{DB: db, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tv.Close() })
	reg := extend.New()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr, err := plugins.New(plugins.Deps{DB: db, Registry: reg, Tools: tools.NewCatalogue(), Logger: log, IsLeader: func() bool { return true }, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	factory := &remote.Factory{TrustedLAN: remote.GrantsLAN(mgr.GrantsOf), Logger: log}
	mgr.RegisterRuntime("remote", factory.RuntimeFactory())
	adapters.Attach(mgr, nil, nil, tv)
	link := srv.URL + "/secret/manifest.json"
	rec, err := mgr.InstallAddon(context.Background(), plugins.AddonInstall{Link: link, Roles: []plugins.Capability{plugins.CapLiveTV}})
	if err != nil {
		t.Fatal(err)
	}
	return &addonTV{tv: tv, mgr: mgr, reg: reg, rec: rec, kind: extend.PluginID(rec.ID, plugins.StremioChannelsKey), link: link}
}

var owner = &core.User{IsOwner: true}

// legacyID is how the built-in Stremio source minted ids.
func legacyID(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:10])
}

func TestAddonChannelsImportRefreshAndXMLTVGuide(t *testing.T) {
	now := time.Date(2026, 11, 2, 9, 30, 0, 0, time.UTC)
	var guideFetches atomic.Int32
	a := newAddonTV(t, now, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/secret/manifest.json":
			fmt.Fprint(w, `{"id":"tv.example","version":"1.0.0","name":"Example TV","resources":["catalog","stream"],"types":["tv"],"catalogs":[{"type":"tv","id":"live","name":"News"}],"behaviorHints":{"epgUrl":"/guide.xml"}}`)
		case "/secret/catalog/tv/live.json":
			fmt.Fprint(w, `{"metas":[{"id":"channel1","name":"Example","logo":"https://img.example/logo.png"},{"id":"channel2","name":"Second"}]}`)
		case "/secret/stream/tv/channel1.json":
			fmt.Fprint(w, `{"streams":[{"name":"HD","url":"https://stream.example/one?key=secret","behaviorHints":{"proxyHeaders":{"request":{"referer":"https://stream.example/","Host":"evil.example"}}}},{"name":"SD\nbackup","url":"https://stream.example/two"},{"url":"https://stream.example/one?key=secret"},{"url":"rtmp://stream.example/x"},{"infoHash":"0123456789abcdef0123456789abcdef01234567"}]}`)
		case "/secret/stream/tv/channel2.json":
			fmt.Fprint(w, `{"streams":[{"url":"https://stream.example/second"}]}`)
		case "/guide.xml":
			guideFetches.Add(1)
			fmt.Fprint(w, `<tv><channel id="channel1"><display-name>Example</display-name></channel><programme channel="channel1" start="20261102090000 +0000" stop="20261102100000 +0000"><title>News</title></programme><programme channel="elsewhere" start="20261102090000 +0000" stop="20261102100000 +0000"><title>Other</title></programme></tv>`)
		default:
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	if !a.rec.Manifest.HasCapability(plugins.CapLiveTV) {
		t.Fatalf("the tv add-on has no Live TV role: %+v", a.rec.Manifest.EffectiveEther().Capabilities)
	}
	kind, ok := a.tv.KindRegistry().Lookup(a.kind)
	if !ok {
		t.Fatalf("kind %s not registered", a.kind)
	}
	if d := kind.Describe(); d.Editor != "addon" || d.Label.Resolved != "Example TV" {
		t.Fatalf("describe = %+v", d)
	}
	src, err := a.tv.ImportSource(ctx, livetv.Source{ID: "s", Name: "Example TV", Kind: a.kind})
	if err != nil {
		t.Fatal(err)
	}
	if src.Kind != a.kind || src.RefreshHours != 6 || strings.Contains(src.PlaylistURL, "secret") {
		t.Fatalf("source = %+v", src)
	}
	cs, err := a.tv.SourceChannels(ctx, "s")
	if err != nil || len(cs) != 2 {
		t.Fatalf("channels=%+v err=%v", cs, err)
	}
	var c livetv.Channel
	for _, ch := range cs {
		if ch.GuideID == "channel1" {
			c = ch
		}
	}
	if c.ID != legacyID("s", "channel1") || c.Name != "Example" || c.Group != "News" || c.Logo != "https://img.example/logo.png" || c.Number == "" {
		t.Fatalf("channel = %+v", c)
	}
	in := c.StreamSources
	if len(in) != 2 || in[0].ID != "stremio-"+legacyID(c.ID, "0") || in[1].ID != "stremio-"+legacyID(c.ID, "1") ||
		in[0].Name != "HD" || in[1].Name != "SD" || in[1].URL != "https://stream.example/two" {
		t.Fatalf("inputs = %+v", in)
	}
	if len(in[0].Headers) != 1 || in[0].Headers["Referer"] != "https://stream.example/" {
		t.Fatalf("proxy headers = %+v", in[0].Headers)
	}
	guide, err := a.tv.Guide(ctx, owner, now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	titles := 0
	for _, p := range guide {
		if p.ChannelID == c.ID && p.Item.Title == "News" {
			titles++
		}
	}
	if titles != 1 || guideFetches.Load() != 1 {
		t.Fatalf("guide programmes=%d fetches=%d %+v", titles, guideFetches.Load(), guide)
	}

	// The owner switches the first input off; a refresh keeps that.
	c.StreamSources[0].Enabled = false
	if _, err := a.tv.SaveChannel(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := a.tv.RefreshSource(ctx, "s"); err != nil {
		t.Fatal(err)
	}
	after, _ := a.tv.Channel(ctx, c.ID)
	if len(after.StreamSources) != 2 || after.StreamSources[0].Enabled || after.StreamURL != "https://stream.example/two" {
		t.Fatalf("refresh lost owner choices: %+v", after)
	}
	if _, err := a.tv.ImportSource(ctx, src); err != nil {
		t.Fatal(err)
	}
}

func TestAddonChannelsNativeGuide(t *testing.T) {
	now := time.Date(2026, 11, 2, 9, 30, 0, 0, time.UTC)
	var days atomic.Int32
	a := newAddonTV(t, now, func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/secret/manifest.json":
			fmt.Fprint(w, `{"id":"tv.guide.example","version":"1.0.0","name":"Guide TV","resources":["catalog","stream"],"types":["tv"],"catalogs":[{"type":"tv","id":"live","extra":[{"name":"date"}]}],"behaviorHints":{"epgProvider":true}}`)
		case strings.Contains(p, "skip="):
			fmt.Fprint(w, `{"metasDetailed":[]}`)
		case strings.Contains(p, "date=2026-11-02"):
			days.Add(1)
			fmt.Fprint(w, `{"metasDetailed":[{"id":"c","name":"Channel","videos":[{"id":"p","title":"News","startTime":"2026-11-02T09:00:00Z","endTime":"2026-11-02T10:00:00Z"},{"id":"q","title":"Long ago","startTime":"2026-10-01T09:00:00Z","endTime":"2026-10-01T10:00:00Z"}]},{"id":"unknown","name":"Not imported","videos":[{"id":"r","title":"X","startTime":"2026-11-02T09:00:00Z","endTime":"2026-11-02T10:00:00Z"}]}]}`)
		case strings.Contains(p, "date="):
			days.Add(1)
			if strings.Contains(p, "date=2026-11-05") {
				http.Error(w, "busy", http.StatusInternalServerError)
				return
			}
			fmt.Fprint(w, `{"metasDetailed":[]}`)
		case strings.HasPrefix(p, "/secret/catalog/"):
			fmt.Fprint(w, `{"metas":[{"id":"c","name":"Channel"}]}`)
		case strings.HasPrefix(p, "/secret/stream/"):
			fmt.Fprint(w, `{"streams":[{"url":"https://example.test/live"}]}`)
		default:
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	if _, err := a.tv.ImportSource(ctx, livetv.Source{ID: "native", Name: "Native", Kind: a.kind}); err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, at := range []time.Time{now, time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)} {
		guide, err := a.tv.Guide(ctx, owner, at.Add(-2*time.Hour), at.Add(2*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range guide {
			titles = append(titles, p.Item.Title)
		}
	}
	// One page per day of the window (Nov 1 to Nov 16); a failed day is skipped.
	if len(titles) != 1 || titles[0] != "News" || days.Load() != 16 {
		t.Fatalf("programmes=%v days=%d", titles, days.Load())
	}
}

func TestAddonChannelsOneFailedChannelDoesNotFailImport(t *testing.T) {
	now := time.Date(2026, 11, 2, 9, 30, 0, 0, time.UTC)
	var broken atomic.Bool
	a := newAddonTV(t, now, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/secret/manifest.json":
			fmt.Fprint(w, `{"id":"tv.flaky.example","version":"1.0.0","name":"Flaky TV","resources":["catalog","stream"],"types":["tv"],"catalogs":[{"type":"tv","id":"a","name":"A"},{"type":"tv","id":"needs","extra":[{"name":"genre","isRequired":true}]}]}`)
		case "/secret/catalog/tv/a.json":
			fmt.Fprint(w, `{"metas":[{"id":"one","name":"One"},{"id":"two","name":"Two"},{"id":"three","name":"Three"}]}`)
		case "/secret/stream/tv/one.json":
			fmt.Fprint(w, `{"streams":[{"url":"https://example.test/one"}]}`)
		case "/secret/stream/tv/two.json":
			if broken.Load() {
				http.Error(w, "down", http.StatusBadGateway)
				return
			}
			fmt.Fprint(w, `{"streams":[{"url":"https://example.test/two"}]}`)
		case "/secret/stream/tv/three.json":
			http.Error(w, "down", http.StatusBadGateway)
		case "/secret/catalog/tv/a/skip=3.json":
			fmt.Fprint(w, `{"metas":[]}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	if _, err := a.tv.ImportSource(ctx, livetv.Source{ID: "s", Name: "Flaky", Kind: a.kind}); err != nil {
		t.Fatal(err)
	}
	names := func() map[string]livetv.Channel {
		cs, _ := a.tv.SourceChannels(ctx, "s")
		out := map[string]livetv.Channel{}
		for _, c := range cs {
			out[c.Name] = c
		}
		return out
	}
	got := names()
	if len(got) != 2 || got["Three"].ID != "" {
		t.Fatalf("a new channel without streams was imported, or a good one dropped: %+v", got)
	}
	two := got["Two"]
	two.Number, two.Favorite = "42", true
	if _, err := a.tv.SaveChannel(ctx, two); err != nil {
		t.Fatal(err)
	}
	broken.Store(true)
	if _, err := a.tv.RefreshSource(ctx, "s"); err != nil {
		t.Fatal(err)
	}
	got = names()
	if c := got["Two"]; c.Number != "42" || !c.Favorite || len(c.StreamSources) != 1 || c.StreamURL != "https://example.test/two" {
		t.Fatalf("a channel whose lookup failed lost its inputs or owner choices: %+v", c)
	}
}

func TestAddonChannelsWithNoStreamsAnywhereFailsImport(t *testing.T) {
	now := time.Date(2026, 11, 2, 9, 30, 0, 0, time.UTC)
	a := newAddonTV(t, now, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/secret/manifest.json":
			fmt.Fprint(w, `{"id":"tv.down.example","version":"1.0.0","name":"Down TV","resources":["catalog","stream"],"types":["tv"],"catalogs":[{"type":"tv","id":"a"}]}`)
		case "/secret/catalog/tv/a.json":
			fmt.Fprint(w, `{"metas":[{"id":"one","name":"One"}]}`)
		default:
			http.Error(w, "down", http.StatusBadGateway)
		}
	})
	if _, err := a.tv.ImportSource(context.Background(), livetv.Source{ID: "s", Name: "Down", Kind: a.kind}); err == nil {
		t.Fatal("an import with no readable streams succeeded")
	}
}
