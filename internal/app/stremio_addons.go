package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/braedonsaunders/ether/internal/animemap"
	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/plugins/adapters"
	"github.com/braedonsaunders/ether/internal/stremio"
)

// Stremio add-ons have one home: an installed add-on record in the plugin
// manager (internal/plugins), whatever page the owner added it from. Its
// roles (streams, catalogs, Live TV channels, subtitles, metadata) come
// from its manifest and can be switched off one by one. Streams appear in
// the indexer list as an IndexerKindAddon row, so they are searched,
// cached, budgeted and ordered like every other indexer.

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

// syncAddonIndexers keeps one indexer row per add-on that can serve
// streams (new rows go last) and drops rows whose add-on is gone. Rows
// keep the owner's order, priority and limits. Cluster leader only.
func (a *App) syncAddonIndexers(ctx context.Context) error {
	mgr := a.pluginManager()
	if mgr == nil || (a.cluster != nil && !a.cluster.IsLeader()) {
		return nil
	}
	recs, err := mgr.List(ctx)
	if err != nil {
		return err
	}
	streams := map[string]plugins.Record{}
	for _, r := range recs {
		for _, c := range r.Manifest.AddonRoles() {
			if c == plugins.CapStreamSource {
				streams[r.ID] = r
			}
		}
		if r.Manifest.Ether != nil && r.Manifest.HasCapability(plugins.CapStreamSource) {
			streams[r.ID] = r
		}
	}
	cfg := a.cfg.Get()
	changed := false
	have := map[string]bool{}
	rows := cfg.Indexers[:0:0]
	for _, ix := range cfg.Indexers {
		if ix.IsAddon() {
			if _, ok := streams[ix.Plugin]; !ok {
				changed = true
				continue
			}
			have[ix.Plugin] = true
		}
		rows = append(rows, ix)
	}
	for _, r := range recs {
		if _, ok := streams[r.ID]; !ok || have[r.ID] {
			continue
		}
		rows = append(rows, config.Indexer{ID: "addon-" + strings.TrimPrefix(r.ID, "addon."), Name: r.Name, Kind: config.IndexerKindAddon, Plugin: r.ID, Enabled: true, Protocol: "torrent"})
		changed = true
	}
	if !changed {
		return nil
	}
	return a.cfg.Update(func(c *config.Config) error {
		c.Indexers = rows
		return nil
	})
}

// migrateLegacyStremio moves every add-on entered before add-ons had one
// home (indexer links, subtitle provider links, collection catalog links,
// Live TV add-on sources) into installed add-ons. It is idempotent: the
// add-on id depends only on its link, and migrated entries are rewritten
// in place. Cluster leader only.
func (a *App) migrateLegacyStremio(ctx context.Context) {
	if a.pluginManager() == nil || (a.cluster != nil && !a.cluster.IsLeader()) {
		return
	}
	for name, step := range map[string]func(context.Context) error{
		"indexers":    a.migrateLegacyStremioIndexers,
		"subtitles":   a.migrateLegacyStremioSubtitles,
		"collections": a.migrateLegacyStremioCollections,
		"livetv":      a.migrateLegacyStremioLiveTV,
	} {
		if err := step(ctx); err != nil {
			a.log.Warn("stremio add-on migration incomplete; it retries on the next start", "area", name, "err", err)
		}
	}
	if err := a.syncAddonIndexers(ctx); err != nil {
		a.log.Warn("add-on indexer rows not synced", "err", err)
	}
}

// migrateLegacyStremioIndexers turns kind "stremio" indexer rows into
// add-on rows. The row keeps its id, name, priority and limits, so release
// ids, pins and blocks stay the same.
func (a *App) migrateLegacyStremioIndexers(ctx context.Context) error {
	cfg := a.cfg.Get()
	type move struct {
		at  int
		rec plugins.Record
	}
	var moves []move
	var firstErr error
	for i, ix := range cfg.Indexers {
		if ix.Kind != config.IndexerKindStremio {
			continue
		}
		roles := []plugins.Capability{plugins.CapStreamSource}
		rec, err := a.installAddon(ctx, plugins.AddonInstall{Link: ix.URL, Name: ix.Name, Roles: roles, Offline: []string{stremio.ResourceStream}, Exclusive: true})
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("indexer %q: %w", ix.Name, err)
			}
			continue
		}
		if !ix.Enabled {
			if rec, err = a.pluginManager().SetRoles(ctx, rec.ID, append(rec.RolesOff, plugins.CapStreamSource)); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		moves = append(moves, move{i, rec})
	}
	if len(moves) == 0 {
		return firstErr
	}
	err := a.cfg.Update(func(c *config.Config) error {
		for _, mv := range moves {
			if mv.at >= len(c.Indexers) || c.Indexers[mv.at].Kind != config.IndexerKindStremio {
				continue
			}
			ix := &c.Indexers[mv.at]
			ix.Kind, ix.Plugin, ix.URL, ix.Protocol, ix.Enabled = config.IndexerKindAddon, mv.rec.ID, "", "torrent", true
		}
		return nil
	})
	if err != nil {
		return err
	}
	return firstErr
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

// testAddonStreams checks that an add-on row can be searched: the add-on
// answers its manifest and serves movie or series streams by ids Ether has.
func (a *App) testAddonStreams(ctx context.Context, plugin string) error {
	mgr := a.pluginManager()
	if mgr == nil {
		return errors.New("add-ons are not available yet")
	}
	rec, err := mgr.Get(ctx, plugin)
	if err != nil {
		return errors.New("this add-on is no longer installed")
	}
	if !rec.Enabled {
		return errors.New("this add-on is turned off")
	}
	body, err := mgr.Fetcher(rec.ID).Fetch(ctx, "/manifest.json", stremio.MaxManifestBytes)
	if err != nil {
		return err
	}
	man, err := stremio.DecodeManifest(body)
	if err != nil {
		return err
	}
	return man.CheckStreams()
}

// lazyAnimeIDs loads the community anime id mapping the first time an
// add-on that looks anime up by Kitsu, MAL, AniList or AniDB id needs it.
type lazyAnimeIDs struct {
	once sync.Once
	svc  *animemap.Service
}

func (l *lazyAnimeIDs) Lookup(ctx context.Context, req core.SearchRequest, db string) (int64, int, bool, error) {
	l.once.Do(func() { l.svc = animemap.New(animemap.Options{}) })
	return l.svc.Lookup(ctx, req, db)
}
