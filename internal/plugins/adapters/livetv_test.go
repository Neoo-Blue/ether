package adapters_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/agent/tools"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/livetv"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/plugins/adapters"
	"github.com/braedonsaunders/ether/internal/store"
)

type livetvTestHarness struct {
	db  *sql.DB
	tv  *livetv.Service
	mgr *plugins.Manager
	reg *extend.Registry
	rt  *listChannelFakeRuntime
	now time.Time
}

func newLiveTVTestHarness(t *testing.T) *livetvTestHarness {
	t.Helper()
	db := store.OpenTest(t)
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

	tv, err := livetv.New(livetv.Options{
		DB:  db,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tv.Close() })

	reg := extend.New()
	rt := &listChannelFakeRuntime{routes: map[string]func(plugins.Request) (plugins.Response, error){}}

	mgr, err := plugins.New(plugins.Deps{
		DB:       db,
		Registry: reg,
		Tools:    tools.NewCatalogue(),
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		IsLeader: func() bool { return true },
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.RegisterRuntime("hosted", func(_ plugins.Installed) (plugins.Runtime, error) {
		return rt, nil
	})
	adapters.Attach(mgr, nil, nil, tv)

	return &livetvTestHarness{
		db:  db,
		tv:  tv,
		mgr: mgr,
		reg: reg,
		rt:  rt,
		now: now,
	}
}

func installLiveTVPlugin(t *testing.T, mgr *plugins.Manager) plugins.Record {
	t.Helper()
	man := map[string]any{
		"id":      "community.livetv.fast",
		"version": "1.0.0",
		"name":    "FAST channels",
		"ether": map[string]any{
			"protocol":     1,
			"kind":         "livetv_source",
			"summary":      "Live fast channels for the household",
			"runtime":      map[string]any{"kind": "hosted", "entry": "dist/plugin.js"},
			"capabilities": []string{"livetv_channels"},
			"contributes": map[string]any{
				"channels": map[string]any{
					"refresh": "6h",
					"test":    true,
				},
			},
		},
	}
	rawMan, err := json.Marshal(man)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"dist/plugin.js": []byte("// fast channels plugin"),
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

func TestLiveTVChannelSourceImportGuideAndLazyStreamOpen(t *testing.T) {
	h := newLiveTVTestHarness(t)
	ctx := context.Background()

	rec := installLiveTVPlugin(t, h.mgr)
	kindID := extend.PluginID(rec.ID, "channels")

	sk, ok := h.tv.KindRegistry().Lookup(kindID)
	if !ok {
		t.Fatalf("expected Live TV SourceKind %s to be registered", kindID)
	}
	sd := sk.Describe()
	if sd.Editor != "plugin" || !sd.YieldsChannels || !sd.YieldsGuide {
		t.Fatalf("unexpected SourceDescribe: %+v", sd)
	}
	desc, ok := h.reg.Get(extend.KindLiveTVSource, kindID)
	if !ok {
		t.Fatalf("expected extend descriptor for %s", kindID)
	}
	meta, err := extend.MetaOf[map[string]any](desc)
	if err != nil {
		t.Fatal(err)
	}
	if meta["yieldsChannels"] != true || meta["yieldsGuide"] != true {
		t.Fatalf("unexpected Live TV descriptor meta: %+v", meta)
	}

	progStart := h.now.Add(-30 * time.Minute).Format(time.RFC3339)
	progEnd := h.now.Add(30 * time.Minute).Format(time.RFC3339)

	h.rt.routes["/ether/channels.json"] = func(_ plugins.Request) (plugins.Response, error) {
		return plugins.Response{
			Status: 200,
			Body: json.RawMessage(`{
				"channels": [
					{
						"id": "scifi-hd",
						"name": "Sci-Fi 24/7",
						"number": 42,
						"group": "Movies",
						"logo": "https://cdn.example/scifi.png",
						"streams": [
							{"id": "scifi-primary", "name": "Primary CDN"},
							{"openId": "scifi-backup", "name": "Backup Open"}
						]
					}
				]
			}`),
		}, nil
	}
	h.rt.routes["/ether/guide/scifi-hd.json"] = func(req plugins.Request) (plugins.Response, error) {
		if !strings.Contains(req.Path, "from=") || !strings.Contains(req.Path, "to=") {
			t.Fatalf("expected from and to query parameters on guide request, got %s", req.Path)
		}
		payload := map[string]any{
			"programmes": []map[string]any{
				{
					"title":   "Galactic Odyssey",
					"summary": "Deep space exploration.",
					"rating":  "TV-PG",
					"start":   progStart,
					"end":     progEnd,
				},
			},
		}
		raw, _ := json.Marshal(payload)
		return plugins.Response{Status: 200, Body: raw}, nil
	}

	src, err := h.tv.ImportSource(ctx, livetv.Source{
		Name:        "FAST Lineup",
		PlaylistURL: kindID,
	})
	if err != nil {
		t.Fatalf("ImportSource failed: %v", err)
	}
	if src.RefreshHours != 6 {
		t.Fatalf("expected RefreshHours=6 from plugin manifest refresh=6h, got %d", src.RefreshHours)
	}
	if src.ChannelCount != 1 {
		t.Fatalf("expected 1 imported channel, got %d", src.ChannelCount)
	}

	channels, err := h.tv.Channels(ctx)
	if err != nil {
		t.Fatalf("Channels failed: %v", err)
	}
	if len(channels) != 1 {
		t.Fatalf("expected 1 persisted channel, got %d", len(channels))
	}
	ch := channels[0]
	if ch.Name != "Sci-Fi 24/7" || ch.Number != "42" || ch.Group != "Movies" {
		t.Fatalf("unexpected channel metadata: %+v", ch)
	}
	if len(ch.StreamSources) != 2 {
		t.Fatalf("expected 2 stream sources, got %+v", ch.StreamSources)
	}
	if ch.StreamSources[0].URL != "plugin://"+rec.ID+"/stream/tv/scifi-primary" {
		t.Fatalf("unexpected primary stream URL: %s", ch.StreamSources[0].URL)
	}
	if ch.StreamSources[1].URL != "plugin://"+rec.ID+"/open/scifi-backup" {
		t.Fatalf("unexpected backup stream URL: %s", ch.StreamSources[1].URL)
	}

	// Verify EPG guide programmes were imported and matched to the channel.
	owner := &core.User{ID: 1, IsOwner: true}
	progs, err := h.tv.Guide(ctx, owner, h.now.Add(-time.Hour), h.now.Add(time.Hour))
	if err != nil {
		t.Fatalf("Guide failed: %v", err)
	}
	if len(progs) != 1 || progs[0].ChannelID != ch.ID || progs[0].Item.Title != "Galactic Odyssey" {
		t.Fatalf("expected imported EPG programme on channel %s, got %+v", ch.ID, progs)
	}

	// Lazy tune-time stream resolution via /stream/tv/{id}.json and /ether/open/{id}.
	h.rt.routes["/stream/tv/scifi-primary.json"] = func(_ plugins.Request) (plugins.Response, error) {
		return plugins.Response{
			Status: 200,
			Body: json.RawMessage(`{
				"streams": [
					{
						"url": "https://stream.example/scifi/master.m3u8",
						"behaviorHints": {
							"proxyHeaders": {
								"request": {"X-Auth-Token": "tok-123"}
							}
						}
					}
				]
			}`),
		}, nil
	}
	h.rt.routes["/ether/open/scifi-backup"] = func(_ plugins.Request) (plugins.Response, error) {
		return plugins.Response{
			Status: 200,
			Body: json.RawMessage(`{
				"url": "https://backup.example/scifi/index.m3u8",
				"headers": {"Referer": "https://fast.example/"}
			}`),
		}, nil
	}

	u1, hdr1, err := h.tv.ResolveStreamSource(ctx, ch, ch.StreamSources[0])
	if err != nil {
		t.Fatalf("ResolveStreamSource primary failed: %v", err)
	}
	if u1 != "https://stream.example/scifi/master.m3u8" || hdr1.Get("X-Auth-Token") != "tok-123" {
		t.Fatalf("unexpected resolved primary stream: url=%q hdr=%v", u1, hdr1)
	}

	u2, hdr2, err := h.tv.ResolveStreamSource(ctx, ch, ch.StreamSources[1])
	if err != nil {
		t.Fatalf("ResolveStreamSource backup failed: %v", err)
	}
	if u2 != "https://backup.example/scifi/index.m3u8" || hdr2.Get("Referer") != "https://fast.example/" {
		t.Fatalf("unexpected resolved backup stream: url=%q hdr=%v", u2, hdr2)
	}

	// Disabling unregisters the Live TV source kind; re-enabling restores it; removing deletes it.
	if err := h.mgr.SetEnabled(ctx, rec.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.tv.KindRegistry().Lookup(kindID); ok {
		t.Fatal("expected Live TV SourceKind to be unregistered on disable")
	}
	if err := h.mgr.SetEnabled(ctx, rec.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.tv.KindRegistry().Lookup(kindID); !ok {
		t.Fatal("expected Live TV SourceKind to be re-registered on enable")
	}
	if err := h.mgr.Remove(ctx, rec.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.tv.KindRegistry().Lookup(kindID); ok {
		t.Fatal("expected Live TV SourceKind to be unregistered on remove")
	}
}

func TestLiveTVHealthFailureOnOpenAndFetch(t *testing.T) {
	h := newLiveTVTestHarness(t)
	ctx := context.Background()

	rec := installLiveTVPlugin(t, h.mgr)
	kindID := extend.PluginID(rec.ID, "channels")

	h.rt.routes["/ether/channels.json"] = func(_ plugins.Request) (plugins.Response, error) {
		return plugins.Response{Status: 502, Body: json.RawMessage(`{"error":"bad gateway"}`)}, nil
	}
	if _, err := h.tv.ImportSource(ctx, livetv.Source{Name: "Broken", PlaylistURL: kindID}); err == nil {
		t.Fatal("expected ImportSource error when /ether/channels.json returns HTTP 502")
	}

	health, err := h.mgr.HealthOf(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(health.Failures) != 1 || health.Status != "error" {
		t.Fatalf("expected 1 recorded health failure after 502, got %+v", health)
	}

	// Stream open failure also records a plugin health failure and returns core.ErrNotAvailable.
	h.rt.routes["/ether/open/broken-stream"] = func(_ plugins.Request) (plugins.Response, error) {
		return plugins.Response{Status: 500, Body: json.RawMessage(`{"error":"failed"}`)}, nil
	}
	_, _, err = h.tv.ResolveStreamSource(ctx, livetv.Channel{ID: "ch1"}, livetv.StreamSource{
		ID:  "s1",
		URL: "plugin://" + rec.ID + "/ether/open/broken-stream",
	})
	if !errors.Is(err, core.ErrNotAvailable) {
		t.Fatalf("expected ErrNotAvailable on failed plugin stream open, got %v", err)
	}
	health, err = h.mgr.HealthOf(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(health.Failures) != 2 {
		t.Fatalf("expected 2 recorded health failures, got %+v", health)
	}
}
