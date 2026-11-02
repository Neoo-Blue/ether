package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/braedonsaunders/ether/internal/admin"
	"github.com/braedonsaunders/ether/internal/clusterbus"
	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/identity"
	"github.com/braedonsaunders/ether/internal/notify"
	"github.com/braedonsaunders/ether/internal/playback"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/plugins/adapters"
	"github.com/braedonsaunders/ether/internal/plugins/catalogue"
	"github.com/braedonsaunders/ether/internal/plugins/kit"
	"github.com/braedonsaunders/ether/internal/plugins/remote"
	"github.com/braedonsaunders/ether/internal/plugins/studio"
	"github.com/braedonsaunders/ether/internal/plugins/wasm"
	"github.com/braedonsaunders/ether/internal/state"
)

// pluginRuntime is the plugin host and its adapters.
type pluginRuntime struct {
	mgr        *plugins.Manager
	cat        *catalogue.Service
	studio     *studio.Studio
	tunnel     *remote.TunnelServer
	pairing    *remote.Pairing
	pluginHost http.Handler
}

func (a *App) buildPlugins(ctx context.Context, cfg config.Config) (*pluginRuntime, error) {
	mgr, err := plugins.New(plugins.Deps{
		DB: a.db, Sealer: a.cfg,
		CheckPackage: kit.CheckInstall,
		Registry:     a.extendRT.registry, Tools: a.extendRT.tools, Notifications: a.notifications,
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
	if err := cat.EnforceCachedRevocations(ctx); err != nil {
		_ = engine.Close()
		return nil, err
	}
	a.closers = append(a.closers, func() { _ = engine.Close() })
	grants, err := identity.NewGrants(a.db, a.users, 30*24*time.Hour, nil)
	if err != nil {
		a.log.Warn("plugin credentials unavailable", "err", err)
	}
	auth := remote.CredentialResolverFunc(func(ctx context.Context, credential string) (string, error) {
		if grants == nil {
			return "", remote.ErrUnavailable
		}
		_, device, _, err := grants.ResolveGrantDevice(ctx, "plugin", credential, "")
		if err != nil || device.ID == "" {
			return "", remote.ErrUnauthorized
		}
		rec, err := mgr.Get(ctx, device.ID)
		if err != nil || !rec.Enabled || rec.Manifest.Ether == nil || rec.Manifest.Ether.Runtime.Kind != "remote" {
			return "", remote.ErrUnauthorized
		}
		return device.ID, nil
	})
	log := a.log.With("component", "plugins-remote")
	tunnel := remote.NewTunnelServer(remote.TunnelOptions{Auth: auth, Logger: log})
	factory := &remote.Factory{Tunnel: tunnel, TrustedLAN: remote.GrantsLAN(mgr.GrantsOf), Logger: log}
	mgr.RegisterRuntime("remote", factory.RuntimeFactory())
	host := remote.NewHostHandler(remote.HostOptions{Host: remote.HostAdapter{HostFor: mgr.HostFor, SettingsWithSecrets: mgr.PluginSettings}, Auth: auth, Logger: log})
	pairing := remote.NewPairing(a.db, grants, func(ctx context.Context) (int64, error) {
		owner, err := a.users.Owner(ctx)
		if err != nil {
			return 0, err
		}
		if owner == nil || !owner.IsOwner {
			return 0, core.ErrUnauthorized
		}
		return owner.ID, nil
	}, func(ctx context.Context, id string) bool {
		rec, err := mgr.Get(ctx, id)
		return err == nil && rec.Enabled && rec.Manifest.Ether != nil && rec.Manifest.Ether.Runtime.Kind == "remote"
	}, tunnel)
	pairing.Exists = func(ctx context.Context, id string) bool { _, err := mgr.Get(ctx, id); return err == nil }
	pairing.TunnelURL = func(r *http.Request) string {
		scheme, host := "ws", portalSanitizeHost(r.Host)
		if r.TLS != nil {
			scheme = "wss"
		}
		if portalProxyTrusted(r, identity.ProxyPrefixes(a.cfg.TrustedProxyCIDRs())) {
			if forwarded := portalForwardedHost(r); forwarded != "" {
				host = forwarded
			}
			if proto := portalForwardedProto(r); proto == "https" {
				scheme = "wss"
			} else if proto == "http" {
				scheme = "ws"
			}
		}
		if host == "" {
			return ""
		}
		return scheme + "://" + host + remote.TunnelPath
	}
	a.closers = append(a.closers, func() { _ = tunnel.Close() })
	adapters.Attach(mgr, a.collections, a.personalize, a.livetv)
	if err := mgr.Start(ctx); err != nil {
		return nil, err
	}
	a.bindStreamSources(mgr)
	if a.resolver != nil {
		a.resolver.SetRankingHooks(adapters.Ranking{Manager: mgr, Registry: a.extendRT.registry})
	}
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

	// Event sources (SPEC §5.7): playback, watch state and notifications.
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
	stopNotify := func() {}
	if a.notifications != nil {
		stopNotify = a.notifications.Subscribe(func(ev notify.Event) {
			if ev.Kind != "title.available" && ev.Kind != "user.joined" {
				return
			}
			payload := json.RawMessage(`{}`)
			if ev.Kind == "title.available" {
				payload, _ = json.Marshal(map[string]any{"title": ev.Title, "message": ev.Message})
			}
			publish(plugins.Event{Name: plugins.NormalizeNotify(ev.Kind), At: ev.At, Payload: payload})
		})
	}

	runCtx, cancelRun := context.WithCancel(context.Background())
	go mgr.Run(runCtx)
	go cat.Run(runCtx, catalogue.DefaultRefreshInterval)
	a.closers = append(a.closers, func() {
		cancelRun()
		stopPlayback()
		stopState()
		stopNotify()
		unsubscribe()
	})
	return &pluginRuntime{mgr: mgr, cat: cat, studio: studioSvc, tunnel: tunnel, pairing: pairing, pluginHost: host}, nil
}

// pluginHandlers mounts the owner plugin API under /api/plugins (WS7, WS11).
func (a *App) pluginHandlers(m map[string]http.Handler) {
	if a.pluginsRT != nil && a.pluginsRT.mgr != nil {
		h := a.pluginsRT.mgr.Handler()
		if a.pluginsRT.studio != nil {
			h = a.pluginsRT.studio.WrapPluginHandler(h)
		}
		if a.pluginsRT.pairing != nil {
			wrapped := a.pluginsRT.pairing.WrapOwner(h)
			h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
				if len(parts) >= 2 && (parts[1] == "pair" || parts[1] == "pairings") {
					access, ok := admin.RequestAccess(r.Context())
					if !ok || !access.Owner {
						http.Error(w, "owner authentication required", http.StatusUnauthorized)
						return
					}
				}
				wrapped.ServeHTTP(w, r)
			})
		}
		m["plugins"] = h
	}
}

// mountPlugins mounts the plugin tunnel and plugin host API (WS8b).
func (a *App) mountPlugins(mux *http.ServeMux) {
	if a.pluginsRT == nil {
		return
	}
	if a.pluginsRT.tunnel != nil {
		mux.Handle("GET "+remote.TunnelPath, a.pluginsRT.tunnel.Handler())
	}
	if a.pluginsRT.pluginHost != nil {
		mux.Handle(remote.HostPath, a.pluginsRT.pluginHost)
	}
	if a.pluginsRT.pairing != nil {
		mux.Handle(remote.PairExchangePath, lanOnly(http.HandlerFunc(a.pluginsRT.pairing.Exchange), a.cfg.TrustedProxyCIDRs))
	}
}

// pluginTextResolver resolves plugin strings for the registry (WS7).
func (a *App) pluginTextResolver() extend.TextResolver {
	if a.pluginsRT == nil || a.pluginsRT.mgr == nil {
		return nil
	}
	return a.pluginsRT.mgr.TextResolver()
}
