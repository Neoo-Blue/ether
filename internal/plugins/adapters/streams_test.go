package adapters_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/agent/tools"
	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/debrid"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/plugins/adapters"
	"github.com/braedonsaunders/ether/internal/resolver"
	"github.com/braedonsaunders/ether/internal/store"
)

type fakePluginRuntime struct {
	mu      sync.Mutex
	calls   []plugins.Request
	handler func(ctx context.Context, req plugins.Request) (plugins.Response, error)
}

func (f *fakePluginRuntime) Call(ctx context.Context, req plugins.Request) (plugins.Response, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	h := f.handler
	f.mu.Unlock()
	if h != nil {
		return h(ctx, req)
	}
	return plugins.Response{Status: 200, Body: json.RawMessage(`{}`)}, nil
}

func (f *fakePluginRuntime) Close() error { return nil }

type streamTestEnv struct {
	t       *testing.T
	mgr     *plugins.Manager
	reg     *extend.Registry
	sources *adapters.StreamSources
	engine  *adapters.Engine
	media   *debrid.Engine
	debSvc  *debrid.Service
	debIdx  *debrid.Indexer
	res     *resolver.Service
	rts     map[string]*fakePluginRuntime
	mu      sync.Mutex
}

func newStreamTestEnv(t *testing.T) *streamTestEnv {
	t.Helper()
	db := store.OpenTest(t)
	reg := extend.New()
	cat := tools.NewCatalogue()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	env := &streamTestEnv{
		t:   t,
		reg: reg,
		rts: map[string]*fakePluginRuntime{},
	}

	mgr, err := plugins.New(plugins.Deps{
		DB:       db,
		Registry: reg,
		Tools:    cat,
		Logger:   log,
	})
	if err != nil {
		t.Fatal(err)
	}
	factory := func(inst plugins.Installed) (plugins.Runtime, error) {
		env.mu.Lock()
		rt := env.rts[inst.ID]
		if rt == nil {
			rt = &fakePluginRuntime{}
			env.rts[inst.ID] = rt
		}
		env.mu.Unlock()
		return rt, nil
	}
	mgr.RegisterRuntime("hosted", factory)
	mgr.RegisterRuntime("remote", factory)
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	env.mgr = mgr

	cfg := config.Config{
		Quality: config.Quality{
			Default: "4K",
			Profiles: []core.QualityProfile{
				{
					Name:             "4K",
					MaxHeight:        2160,
					MinHeight:        720,
					AllowHDR:         true,
					AllowDolbyVision: true,
					PreferredSources: []core.Source{core.SourceRemux, core.SourceBluRay, core.SourceWEBDL, core.SourceWEBRip},
					PreferredCodecs:  []string{"hevc", "h264"},
					PreferredAudio:   []string{"truehd", "dtshd", "eac3", "dts", "ac3", "aac"},
				},
			},
		},
	}
	debSvc := debrid.NewService(cfg.Debrid, debrid.Options{DB: db, Logger: log})
	ss := adapters.NewStreamSources(nil)
	ss.Bind(mgr, debSvc)
	eng := ss.WrapEngine(nil)
	media := debrid.NewEngine(debSvc, debrid.EngineOptions{
		Inner:  eng,
		Health: eng,
		Logger: log,
	})
	t.Cleanup(func() { media.Close() })

	debIdx := debrid.NewIndexer(ss, debSvc)
	debIdx.SetUsenetReady(func() bool {
		return ss.HasEnabledPlugins(context.Background())
	})
	res := resolver.New(debIdx, media, db, cfg, resolver.Options{Logger: log})
	t.Cleanup(func() { res.Close() })

	env.sources = ss
	env.engine = eng
	env.media = media
	env.debSvc = debSvc
	env.debIdx = debIdx
	env.res = res
	return env
}

func (e *streamTestEnv) installSourcePlugin(
	id, name, class string,
	foldersPerm string,
	limits map[string]any,
	handler func(ctx context.Context, req plugins.Request) (plugins.Response, error),
) plugins.Record {
	e.t.Helper()
	e.mu.Lock()
	e.rts[id] = &fakePluginRuntime{handler: handler}
	e.mu.Unlock()

	srcMeta := map[string]any{"class": class}
	if limits != nil {
		srcMeta["limits"] = limits
	}
	perms := map[string]any{}
	if foldersPerm != "" {
		perms["folders"] = foldersPerm
	}
	man := map[string]any{
		"id":      id,
		"version": "1.0.0",
		"name":    name,
		"ether": map[string]any{
			"protocol":     1,
			"kind":         "source",
			"summary":      "Provides streams for titles",
			"capabilities": []string{"stream_source"},
			"runtime":      map[string]any{"kind": "hosted", "entry": "dist/plugin.js"},
			"permissions":  perms,
			"contributes": map[string]any{
				"source": srcMeta,
			},
		},
	}
	raw, err := json.Marshal(man)
	if err != nil {
		e.t.Fatal(err)
	}
	prev, err := e.mgr.PreviewInstall(context.Background(), plugins.Source{
		Kind:     "studio",
		Manifest: raw,
		Files:    map[string][]byte{"dist/plugin.js": []byte("// plugin")},
	})
	if err != nil {
		e.t.Fatal(err)
	}
	rec, err := e.mgr.Install(context.Background(), prev, plugins.InstallOptions{AcceptAddedPermissions: true})
	if err != nil {
		e.t.Fatal(err)
	}
	return rec
}

func TestStreamSourceSearchRanksByOwnerLadderAndPlays(t *testing.T) {
	env := newStreamTestEnv(t)
	ctx := context.Background()

	// Create a real local file to back the lazy /ether/open resolution so we
	// can verify the winning release both ranks by Ether's ladder and opens.
	mediaDir := t.TempDir()
	winPath := filepath.Join(mediaDir, "Arrival.2016.2160p.UHD.BluRay.REMUX.HEVC.TrueHD.7.1.Atmos-GRP.mkv")
	payloadBytes := bytesRepeat("ETHER-VIDEO-STREAM-2160P-", 1024)
	if err := os.WriteFile(winPath, payloadBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	// The plugin deliberately returns the 720p release FIRST and the 2160p
	// Remux SECOND. Ether must ignore the plugin's ordering, parse both via
	// releaseparse, and rank the 2160p Remux first on a 4K-capable client.
	rec := env.installSourcePlugin(
		"community.example.fixture",
		"Fixture Source",
		"direct",
		"read",
		nil,
		func(ctx context.Context, req plugins.Request) (plugins.Response, error) {
			switch {
			case strings.HasPrefix(req.Path, "/stream/movie/"):
				body, _ := json.Marshal(map[string]any{
					"streams": []map[string]any{
						{
							"name": "Fixture 720p",
							"ether": map[string]any{
								"id":          "low-720p",
								"releaseName": "Arrival.2016.720p.WEBRip.x264.AAC-LOW",
								"size":        int64(2_500_000_000),
								"languages":   []string{"en"},
								"lazy":        true,
							},
						},
						{
							"name": "Fixture 4K",
							"ether": map[string]any{
								"id":          "uhd-2160p",
								"releaseName": "Arrival.2016.2160p.UHD.BluRay.REMUX.HEVC.TrueHD.7.1.Atmos-GRP",
								"size":        int64(45_000_000_000),
								"languages":   []string{"en", "fr"},
								"lazy":        true,
							},
						},
					},
				})
				return plugins.Response{Status: 200, Body: body}, nil
			case req.Path == "/ether/open/uhd-2160p":
				body, _ := json.Marshal(map[string]any{
					"file": winPath,
				})
				return plugins.Response{Status: 200, Body: body}, nil
			case req.Path == "/ether/open/low-720p":
				body, _ := json.Marshal(map[string]any{
					"file": winPath,
				})
				return plugins.Response{Status: 200, Body: body}, nil
			default:
				return plugins.Response{Status: 404}, nil
			}
		},
	)
	if err := env.mgr.SetGrants(ctx, rec.ID, plugins.Grants{Folders: []string{mediaDir}}); err != nil {
		t.Fatal(err)
	}

	title := &core.Title{
		Key:     core.MovieKey(329865),
		Type:    core.TypeMovie,
		Title:   "Arrival",
		Year:    2016,
		Runtime: 116 * time.Minute,
		IDs:     core.IDs{IMDB: "tt2543164", TMDB: 329865},
	}

	res, err := env.res.Resolve(ctx, core.ResolveRequest{
		Title: title,
		Client: core.ClientCaps{
			MaxHeight:   2160,
			HDR10:       true,
			DolbyVision: true,
			VideoCodecs: []string{"h264", "hevc"},
			AudioCodecs: []string{"truehd", "eac3", "ac3", "aac"},
		},
	})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if !strings.Contains(res.Release.Name, "2160p") {
		t.Fatalf("expected 2160p release to win Ether's ladder, got %q", res.Release.Name)
	}
	if len(res.Release.Info.Languages) < 2 {
		t.Fatalf("expected ether.languages merged into ReleaseInfo, got %v", res.Release.Info.Languages)
	}

	src, err := env.media.Open(ctx, res.Release.ID, res.NZB)
	if err != nil {
		t.Fatalf("Open winning release failed: %v", err)
	}
	defer src.Close() //nolint:errcheck

	buf := make([]byte, len(payloadBytes))
	n, err := src.ReadAt(buf, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("ReadAt failed: %v", err)
	}
	if string(buf[:n]) != string(payloadBytes) {
		t.Fatalf("unexpected played bytes: got %d bytes", n)
	}
}

func TestLazyStreamTimeoutFallsBackToNextCandidate(t *testing.T) {
	env := newStreamTestEnv(t)
	ctx := context.Background()

	mediaDir := t.TempDir()
	backupPath := filepath.Join(mediaDir, "Dune.2021.1080p.BluRay.x264.DTS-BACKUP.mkv")
	if err := os.WriteFile(backupPath, bytesRepeat("BACKUP-STREAM-", 512), 0o600); err != nil {
		t.Fatal(err)
	}

	rec := env.installSourcePlugin(
		"community.example.lazy",
		"Lazy Source",
		"direct",
		"read",
		nil,
		func(ctx context.Context, req plugins.Request) (plugins.Response, error) {
			switch {
			case strings.HasPrefix(req.Path, "/stream/movie/"):
				body, _ := json.Marshal(map[string]any{
					"streams": []map[string]any{
						{
							"name": "Stalling 4K",
							"ether": map[string]any{
								"id":          "stall-4k",
								"releaseName": "Dune.2021.2160p.UHD.BluRay.REMUX.HEVC.TrueHD.7.1.Atmos-STALL",
								"size":        int64(50_000_000_000),
								"lazy":        true,
							},
						},
						{
							"name": "Working 1080p",
							"ether": map[string]any{
								"id":          "fast-1080p",
								"releaseName": "Dune.2021.1080p.BluRay.x264.DTS-BACKUP",
								"size":        int64(12_000_000_000),
								"lazy":        true,
							},
						},
					},
				})
				return plugins.Response{Status: 200, Body: body}, nil
			case req.Path == "/ether/open/stall-4k":
				// Block until the 3s /ether/open deadline expires.
				<-ctx.Done()
				return plugins.Response{}, ctx.Err()
			case req.Path == "/ether/open/fast-1080p":
				body, _ := json.Marshal(map[string]any{"file": backupPath})
				return plugins.Response{Status: 200, Body: body}, nil
			default:
				return plugins.Response{Status: 404}, nil
			}
		},
	)
	if err := env.mgr.SetGrants(ctx, rec.ID, plugins.Grants{Folders: []string{mediaDir}}); err != nil {
		t.Fatal(err)
	}

	title := &core.Title{
		Key:     core.MovieKey(438631),
		Type:    core.TypeMovie,
		Title:   "Dune",
		Year:    2021,
		Runtime: 155 * time.Minute,
		IDs:     core.IDs{IMDB: "tt1160419", TMDB: 438631},
	}

	res, err := env.res.Resolve(ctx, core.ResolveRequest{
		Title: title,
		Client: core.ClientCaps{
			MaxHeight:   2160,
			VideoCodecs: []string{"h264", "hevc"},
		},
	})
	if err != nil {
		t.Fatalf("expected fallback candidate to succeed after lazy timeout, got: %v", err)
	}
	if !strings.Contains(res.Release.Name, "1080p") {
		t.Fatalf("expected 1080p fallback release to win after 4K lazy open timed out, got %q", res.Release.Name)
	}
}

func TestFoldersPluginLocalFilePlaysAsTopReleaseAndEnforcesContainment(t *testing.T) {
	env := newStreamTestEnv(t)
	ctx := context.Background()

	grantedDir := t.TempDir()
	outsideDir := t.TempDir()

	localMovie := filepath.Join(grantedDir, "Blade Runner 2049 (2017).mkv")
	if err := os.WriteFile(localMovie, bytesRepeat("LOCAL-FOLDER-MOVIE-", 1024), 0o600); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(outsideDir, "secret.mkv")
	if err := os.WriteFile(outsideFile, bytesRepeat("OUTSIDE-", 512), 0o600); err != nil {
		t.Fatal(err)
	}
	escapeLink := filepath.Join(grantedDir, "escape.mkv")
	_ = os.Symlink(outsideFile, escapeLink)

	// Install a remote source plugin that offers a 1080p WEB-DL, and a local
	// folders plugin that offers the local file on disk.
	env.installSourcePlugin(
		"community.example.remote",
		"Remote Indexer",
		"indexer",
		"",
		nil,
		func(ctx context.Context, req plugins.Request) (plugins.Response, error) {
			body, _ := json.Marshal(map[string]any{
				"streams": []map[string]any{
					{
						"name": "Remote 1080p",
						"url":  "https://cdn.example.com/blade.mkv",
						"ether": map[string]any{
							"id":          "remote-1",
							"releaseName": "Blade.Runner.2049.2017.1080p.WEB-DL.DDP5.1.H.264-REMOTE",
							"size":        int64(8_000_000_000),
						},
					},
				},
			})
			return plugins.Response{Status: 200, Body: body}, nil
		},
	)

	foldersRec := env.installSourcePlugin(
		"community.example.folders",
		"Local Folders",
		"direct",
		"read",
		nil,
		func(ctx context.Context, req plugins.Request) (plugins.Response, error) {
			body, _ := json.Marshal(map[string]any{
				"streams": []map[string]any{
					{
						"name": "Outside traversal",
						"ether": map[string]any{
							"id":          "bad-dots",
							"releaseName": "Blade.Runner.2049.2017.2160p.REMUX.mkv",
							"file":        "../outside/secret.mkv",
						},
					},
					{
						"name": "Outside symlink",
						"ether": map[string]any{
							"id":          "bad-symlink",
							"releaseName": "Blade.Runner.2049.2017.2160p.REMUX.mkv",
							"file":        escapeLink,
						},
					},
					{
						"name": "Local file",
						"ether": map[string]any{
							"id":          "local-1",
							"releaseName": "Blade Runner 2049 (2017).mkv",
							"file":        localMovie,
						},
					},
				},
			})
			return plugins.Response{Status: 200, Body: body}, nil
		},
	)
	if err := env.mgr.SetGrants(ctx, foldersRec.ID, plugins.Grants{Folders: []string{grantedDir}}); err != nil {
		t.Fatal(err)
	}

	title := &core.Title{
		Key:     core.MovieKey(335984),
		Type:    core.TypeMovie,
		Title:   "Blade Runner 2049",
		Year:    2017,
		Runtime: 164 * time.Minute,
		IDs:     core.IDs{IMDB: "tt1856101", TMDB: 335984},
	}

	res, err := env.res.Resolve(ctx, core.ResolveRequest{
		Title: title,
		Client: core.ClientCaps{
			MaxHeight:   2160,
			VideoCodecs: []string{"h264", "hevc"},
		},
	})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if res.Release.Indexer != "Local Folders" {
		t.Fatalf("expected folders plugin local file to play as top release, got indexer=%q name=%q", res.Release.Indexer, res.Release.Name)
	}

	src, err := env.media.Open(ctx, res.Release.ID, res.NZB)
	if err != nil {
		t.Fatalf("Open local file failed: %v", err)
	}
	defer src.Close() //nolint:errcheck

	lfs, ok := src.(core.LocalFileSource)
	if !ok || lfs.LocalFile() == nil {
		t.Fatalf("expected MediaSource to implement core.LocalFileSource, got %T", src)
	}
	session := &core.PlaySession{Key: title.Key, Source: src}
	if rev := core.SourceRevision(session); !strings.HasPrefix(rev, "src-") {
		t.Fatalf("expected valid SourceRevision from LocalFileSource, got %q", rev)
	}

	// Revoking the folders grant must immediately block opening or health-checking the payload.
	if err := env.mgr.SetGrants(ctx, foldersRec.ID, plugins.Grants{
		Folders: []string{grantedDir},
		Revoked: []string{"folders"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.media.Open(ctx, res.Release.ID, res.NZB); !errors.Is(err, plugins.ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied after revoking folders grant, got %v", err)
	}
}

func TestDynamicEnableDisableAndBudgets(t *testing.T) {
	env := newStreamTestEnv(t)
	ctx := context.Background()

	rec := env.installSourcePlugin(
		"community.example.limited",
		"Limited Source",
		"indexer",
		"",
		map[string]any{"searchPerHour": 1},
		func(ctx context.Context, req plugins.Request) (plugins.Response, error) {
			body, _ := json.Marshal(map[string]any{
				"streams": []map[string]any{
					{
						"name":     "Limited 1080p",
						"infoHash": "0123456789abcdef0123456789abcdef01234567",
						"ether": map[string]any{
							"id":          "lim-1",
							"releaseName": "Heat.1995.1080p.BluRay.x264-GRP",
							"size":        int64(10_000_000_000),
						},
					},
				},
			})
			return plugins.Response{Status: 200, Body: body}, nil
		},
	)

	sreq := core.SearchRequest{
		Type:  core.TypeMovie,
		Title: "Heat",
		Year:  1995,
		IDs:   core.IDs{IMDB: "tt0113277", TMDB: 949},
	}

	rels, err := env.sources.Search(ctx, sreq)
	if err != nil || len(rels) != 1 {
		t.Fatalf("first search failed: rels=%d err=%v", len(rels), err)
	}
	if rels[0].Protocol != core.ProtocolTorrent || rels[0].InfoHash != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("expected torrent release, got protocol=%q hash=%q", rels[0].Protocol, rels[0].InfoHash)
	}

	// Second search within the hour hits searchPerHour=1 limit.
	if _, err := env.sources.Search(ctx, sreq); !errors.Is(err, core.ErrBudgetExhausted) {
		t.Fatalf("expected ErrBudgetExhausted on second search, got %v", err)
	}

	// Disabling the plugin removes it from active stream sources immediately.
	if err := env.mgr.SetEnabled(ctx, rec.ID, false); err != nil {
		t.Fatal(err)
	}
	if env.sources.HasEnabledPlugins(ctx) {
		t.Fatal("expected no enabled stream_source plugins after SetEnabled(false)")
	}
}

func bytesRepeat(s string, count int) []byte {
	return []byte(strings.Repeat(s, count))
}
