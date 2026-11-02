package app

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/braedonsaunders/ether/internal/admin"
	"github.com/braedonsaunders/ether/internal/clusterbus"
	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/playback"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/plugins/catalogue"
	"github.com/braedonsaunders/ether/internal/plugins/studio"
	"github.com/braedonsaunders/ether/internal/plugins/wasm"
	"github.com/braedonsaunders/ether/internal/state"
)

// pluginRuntime is the plugin host and its adapters.
type pluginRuntime struct {
	mgr    *plugins.Manager
	cat    *catalogue.Service
	studio *studio.Studio
}

func (a *App) buildPlugins(ctx context.Context, cfg config.Config) (*pluginRuntime, error) {
	mgr, err := plugins.New(plugins.Deps{
		DB: a.db, Sealer: a.cfg,
		Registry: a.extendRT.registry, Tools: a.extendRT.tools,
		Logger:   a.log.With("component", "plugins"),
		IsLeader: func() bool { return a.cluster == nil || a.cluster.IsLeader() },
		Bus:      clusterbus.FromCluster(a.cluster),
		Backends: plugins.Backends{
			CatalogSearch: func(ctx context.Context, query string, limit, offset int) (json.RawMessage, error) {
				if offset > 0 {
					limit += offset
				}
				titles, err := a.catalog.Search(ctx, query, []core.MediaType{core.TypeMovie, core.TypeShow}, limit)
				if err != nil {
					return nil, err
				}
				if offset > 0 {
					if offset >= len(titles) {
						titles = nil
					} else {
						titles = titles[offset:]
					}
				}
				type titleSummary struct {
					ID    int64  `json:"id"`
					Title string `json:"title"`
					Type  string `json:"type"`
					Year  int    `json:"year,omitempty"`
				}
				out := make([]titleSummary, 0, len(titles))
				for _, t := range titles {
					out = append(out, titleSummary{ID: int64(t.Key), Title: t.Title, Type: string(t.Type), Year: t.Year})
				}
				return json.Marshal(map[string]any{"titles": out})
			},
			Notice: func(text string) {
				a.log.Warn("plugin notice", "text", text)
				if a.streamRT != nil {
					_ = a.streamRT.Publish(context.Background(), admin.StreamNotice, map[string]any{"text": text})
				}
			},
		},
	})
	if err != nil {
		return nil, err
	}
	cat, err := catalogue.New(catalogue.Options{
		DB:        a.db,
		Manager:   mgr,
		IndexURL:  cfg.Plugins.IndexURL,
		PublicKey: cfg.Plugins.PublicKey,
		Log:       a.log.With("component", "plugins-catalogue"),
	})
	if err != nil {
		return nil, err
	}
	mgr.SetCatalogue(cat)

	// Hosted plugins run sandboxed in-process (SPEC §5.2).
	engine := wasm.NewEngine(a.log.With("component", "plugins-wasm"))
	mgr.RegisterRuntime("hosted", plugins.HostedRuntimeFactory(engine))
	a.closers = append(a.closers, func() { _ = engine.Close() })
	if err := mgr.Start(ctx); err != nil {
		return nil, err
	}
	a.bindStreamSources(mgr)
	unsubscribe := mgr.SubscribeInvalidations()

	studioSvc := studio.New(studio.Deps{
		Manager: mgr,
		Engine:  engine,
		Publish: func(pubCtx context.Context, event string, data any) error {
			if a.streamRT != nil {
				a.streamRT.Publish(pubCtx, event, data)
			}
			return nil
		},
		Logger: a.log.With("component", "plugins-studio"),
	})

	// Event sources (SPEC §5.7): playback and watch state. Normalized
	// notify kinds (title.available, user.joined) reach plugins through
	// Manager.Publish; the notify service has no subscribe hook yet.
	publish := func(ev plugins.Event) {
		pctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		mgr.Publish(pctx, ev)
	}
	stopPlayback := func() {}
	if a.playback != nil {
		stopPlayback = a.playback.Subscribe(func(ev playback.Event) {
			name := plugins.NormalizePlayback(ev.Kind, "")
			if name == "" {
				return
			}
			payload, _ := json.Marshal(map[string]any{"kind": ev.Kind})
			var viewer int64
			if ev.Session != nil {
				viewer = ev.Session.UserID
				payload, _ = json.Marshal(map[string]any{
					"kind": ev.Kind, "ratingKey": int64(ev.Session.Key),
				})
			}
			publish(plugins.Event{Name: name, At: ev.At, ViewerID: viewer, Payload: payload})
		})
	}
	stopState := func() {}
	if a.state != nil {
		stopState = a.state.Subscribe(func(ev state.Event) {
			var name string
			switch ev.Kind {
			case state.EventCompleted, state.EventScrobble:
				name = plugins.EventPlaybackCompleted
			case state.EventProgress:
				name = plugins.NormalizePlayback("update", ev.State)
			}
			if name == "" {
				return
			}
			payload, _ := json.Marshal(map[string]any{"ratingKey": int64(ev.Key), "completed": ev.Completed})
			publish(plugins.Event{Name: name, At: ev.At, ViewerID: ev.UserID, Payload: payload})
		})
	}

	runCtx, cancelRun := context.WithCancel(context.Background())
	go mgr.Run(runCtx)
	go cat.Run(runCtx, catalogue.DefaultRefreshInterval)
	a.closers = append(a.closers, func() {
		cancelRun()
		stopPlayback()
		stopState()
		unsubscribe()
	})
	return &pluginRuntime{mgr: mgr, cat: cat, studio: studioSvc}, nil
}

// pluginHandlers mounts the owner plugin API under /api/plugins (WS7, WS11).
func (a *App) pluginHandlers(m map[string]http.Handler) {
	if a.pluginsRT != nil && a.pluginsRT.mgr != nil {
		h := a.pluginsRT.mgr.Handler()
		if a.pluginsRT.studio != nil {
			h = a.pluginsRT.studio.WrapPluginHandler(h)
		}
		m["plugins"] = h
	}
}

// mountPlugins mounts the plugin tunnel and plugin host API (WS8b).
func (a *App) mountPlugins(mux *http.ServeMux) {}

// pluginTextResolver resolves plugin strings for the registry (WS7).
func (a *App) pluginTextResolver() extend.TextResolver {
	if a.pluginsRT == nil || a.pluginsRT.mgr == nil {
		return nil
	}
	return a.pluginsRT.mgr.TextResolver()
}
