package app

import (
	"context"
	"sync"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/plugins/adapters"
)

var (
	streamSourcesMu  sync.RWMutex
	appStreamSources = map[*App]*adapters.StreamSources{}
)

func (a *App) initStreamSources(inner core.Indexer) *adapters.StreamSources {
	ss := adapters.NewStreamSources(inner)
	streamSourcesMu.Lock()
	appStreamSources[a] = ss
	streamSourcesMu.Unlock()
	a.closers = append(a.closers, func() {
		streamSourcesMu.Lock()
		delete(appStreamSources, a)
		streamSourcesMu.Unlock()
	})
	return ss
}

func (a *App) streamSources() *adapters.StreamSources {
	streamSourcesMu.RLock()
	ss := appStreamSources[a]
	streamSourcesMu.RUnlock()
	return ss
}

func (a *App) bindStreamSources(mgr *plugins.Manager) {
	ss := a.streamSources()
	if ss == nil {
		return
	}
	ss.Bind(mgr, a.debrid)
	if a.extendRT != nil && a.extendRT.registry != nil {
		unsub := a.extendRT.registry.Subscribe(func() {
			a.invalidatePluginSourceCaches()
		})
		a.closers = append(a.closers, unsub)
	}
	a.invalidatePluginSourceCaches()
}

func (a *App) invalidatePluginSourceCaches() {
	if a.resolver == nil || a.cfg == nil {
		return
	}
	cfg := a.cfg.Get()
	bump := cfg
	bump.Debrid.CachedOnly = !cfg.Debrid.CachedOnly
	a.resolver.UpdateConfig(bump)
	a.resolver.UpdateConfig(cfg)
}

func (a *App) hasEnabledPluginSources() bool {
	ss := a.streamSources()
	if ss == nil {
		return false
	}
	return ss.HasEnabledPlugins(context.Background())
}

// registerSourceDescriptors registers built-in stream source kinds for the Sources tabs (WS9b).
func (a *App) registerSourceDescriptors(r *extend.Registry) error {
	builtins := []extend.Descriptor{
		{
			Kind:  extend.KindSourceKind,
			ID:    "debrid",
			Owner: extend.Builtin,
			Label: extend.T("Debrid service"),
			Hint:  extend.T("Streams cached torrents over HTTPS from an owner debrid account."),
			Order: 10,
		},
		{
			Kind:  extend.KindSourceKind,
			ID:    "usenet",
			Owner: extend.Builtin,
			Label: extend.T("Usenet provider"),
			Hint:  extend.T("Fetches articles directly from primary and backup NNTP providers."),
			Order: 20,
		},
		{
			Kind:  extend.KindSourceKind,
			ID:    "indexer",
			Owner: extend.Builtin,
			Label: extend.T("Release indexer"),
			Hint:  extend.T("Searches Newznab, Torznab, built-in sites and Stremio add-ons for releases."),
			Order: 30,
		},
		{
			Kind:  extend.KindSourceKind,
			ID:    "direct",
			Owner: extend.Builtin,
			Label: extend.T("Direct stream source"),
			Hint:  extend.T("Plays direct HTTP streams or granted local folders."),
			Order: 40,
		},
	}
	for _, d := range builtins {
		d = d.WithMeta(map[string]any{"class": d.ID})
		if err := r.Register(d); err != nil {
			return err
		}
	}
	return nil
}
