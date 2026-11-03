package app

import (
	"context"
	"errors"

	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/plugins/adapters"
)

// A Stremio stream add-on the owner enters on the Indexers page (Torrentio)
// is Ether's own Stremio indexer (internal/stremio Backend), configured
// there like any indexer. Every other add-on role (catalogs, Live TV
// channels, subtitles, title details) lives in an installed add-on record
// in the plugin manager, whatever page it was added from; an installed
// plugin's streams join the search manager as a run-time indexer row.
// Both paths speak the protocol through internal/stremio.

func (a *App) pluginManager() *plugins.Manager {
	if a.pluginsRT == nil {
		return nil
	}
	return a.pluginsRT.mgr
}

// installAddon installs (or reuses) an add-on; see plugins.InstallAddon.
func (a *App) installAddon(ctx context.Context, in plugins.AddonInstall) (plugins.Record, error) {
	mgr := a.pluginManager()
	if mgr == nil {
		return plugins.Record{}, errors.New("add-ons are not available yet")
	}
	return mgr.InstallAddon(ctx, in)
}

// addonIndexers expands the indexer list for the running search manager
// (adapters.ExpandAddonIndexers).
func (a *App) addonIndexers(ctx context.Context, rows []config.Indexer) []config.Indexer {
	return adapters.ExpandAddonIndexers(ctx, a.pluginManager(), rows)
}

// migrateLegacyStremio moves the add-on roles entered by link outside the
// Indexers page (subtitle provider links, collection catalog links, Live TV
// add-on sources) into installed add-ons. It is idempotent: the
// add-on id depends only on its link, and migrated entries are rewritten
// in place. Cluster leader only.
func (a *App) migrateLegacyStremio(ctx context.Context) {
	if a.pluginManager() == nil || (a.cluster != nil && !a.cluster.IsLeader()) {
		return
	}
	for name, step := range map[string]func(context.Context) error{
		"subtitles":   a.migrateLegacyStremioSubtitles,
		"collections": a.migrateLegacyStremioCollections,
		"livetv":      a.migrateLegacyStremioLiveTV,
	} {
		if err := step(ctx); err != nil {
			a.log.Warn("stremio add-on migration incomplete; it retries on the next start", "area", name, "err", err)
		}
	}
}

// withAddonIndexers is c with its add-on indexer rows expanded for the
// running search manager (addonIndexers).
func (a *App) withAddonIndexers(c config.Config) config.Config {
	c.Indexers = a.addonIndexers(context.Background(), c.Indexers)
	return c
}

// reapplyAddonIndexers pushes add-on changes (installed, removed, enabled,
// a role switched, a new link) into the running search manager.
func (a *App) reapplyAddonIndexers() {
	if a.indexers == nil || a.cfg == nil {
		return
	}
	a.indexers.ApplyConfig(a.withAddonIndexers(a.cfg.Get()))
}
