package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/braedonsaunders/ether/internal/animemap"
	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/plugins"
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

// addonInstall is one request to install (or reuse) the add-on at Link and
// switch Roles on. Name is a fallback display name when the manifest cannot
// be read; Offline lists the resources a legacy entry proves the add-on
// serves, used to record it while its host is unreachable.
type addonInstall struct {
	Link    string
	Name    string
	Roles   []plugins.Capability
	Offline []string
	// Exclusive turns every other role off on a newly created record
	// (migrations keep exactly what the legacy entry did).
	Exclusive bool
}

// installAddon installs the add-on at link, or finds it when that link is
// already installed, and switches the requested roles on. A private host
// is trusted as LAN: the owner pasted it.
func (a *App) installAddon(ctx context.Context, in addonInstall) (plugins.Record, error) {
	mgr := a.pluginManager()
	if mgr == nil {
		return plugins.Record{}, errors.New("add-ons are not available yet")
	}
	id, base, err := plugins.StremioInstallID(in.Link)
	if err != nil {
		return plugins.Record{}, err
	}
	existing, getErr := mgr.Get(ctx, id)
	created := getErr != nil
	if created {
		src := plugins.Source{Kind: "url", URL: base, LAN: privateHost(base)}
		preview, err := mgr.PreviewInstall(ctx, src)
		if err != nil && len(in.Offline) > 0 {
			raw, merr := offlineManifest(base, in.Name, in.Offline)
			if merr != nil {
				return plugins.Record{}, merr
			}
			src.Manifest = raw
			preview, err = mgr.PreviewInstall(ctx, src)
		}
		if err != nil {
			return plugins.Record{}, err
		}
		if _, err := mgr.Install(ctx, preview, plugins.InstallOptions{Grants: plugins.Grants{LAN: src.LAN}}); err != nil {
			return plugins.Record{}, err
		}
		if existing, err = mgr.Get(ctx, id); err != nil {
			return plugins.Record{}, err
		}
	}
	want := map[plugins.Capability]bool{}
	for _, r := range in.Roles {
		want[r] = true
	}
	off := map[plugins.Capability]bool{}
	for _, r := range existing.RolesOff {
		off[r] = true
	}
	if created && in.Exclusive {
		for _, r := range existing.Manifest.AddonRoles() {
			off[r] = true
		}
	}
	for r := range want {
		delete(off, r)
	}
	offered := map[plugins.Capability]bool{}
	for _, r := range existing.Manifest.AddonRoles() {
		offered[r] = true
	}
	var list []plugins.Capability
	for r := range off {
		if offered[r] {
			list = append(list, r)
		}
	}
	if len(list) != len(existing.RolesOff) || created {
		return mgr.SetRoles(ctx, id, list)
	}
	return existing, nil
}

// offlineManifest is a minimal manifest for a legacy entry whose add-on is
// unreachable while it is migrated: it serves what the entry proves.
func offlineManifest(base, name string, resources []string) (json.RawMessage, error) {
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = u.Hostname()
	}
	types := []string{stremio.TypeMovie, stremio.TypeSeries}
	for _, r := range resources {
		if r == "tv" {
			types = []string{stremio.TypeTV}
		}
	}
	res := stremio.Resources{}
	for _, r := range resources {
		if r == "tv" {
			res = append(res, stremio.Resource{Name: stremio.ResourceStream, Short: true}, stremio.Resource{Name: stremio.ResourceCatalog, Short: true})
			continue
		}
		res = append(res, stremio.Resource{Name: r, Short: true})
	}
	return json.Marshal(stremio.Manifest{ID: "legacy." + strings.ReplaceAll(u.Hostname(), ".", "-"), Version: "0.0.0", Name: name, Types: types, IDPrefixes: []string{"tt"}, Resources: res})
}

// privateHost reports a link whose host is a loopback, private or
// link-local address, or resolves only to such addresses.
func privateHost(link string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if strings.HasSuffix(host, ".local") || host == "localhost" {
		return true
	}
	ips := []net.IP{net.ParseIP(host)}
	if ips[0] == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(addrs) == 0 {
			return false
		}
		ips = ips[:0]
		for _, a := range addrs {
			ips = append(ips, a.IP)
		}
	}
	for _, ip := range ips {
		if !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()) {
			return false
		}
	}
	return true
}

// addonIndexers expands the indexer list for the running search manager:
// an add-on row searches when its add-on is installed, enabled and its
// stream role is on; it carries the add-on's link in memory only (search
// cache identity and secret scrubbing), never in the saved config.
func (a *App) addonIndexers(ctx context.Context, rows []config.Indexer) []config.Indexer {
	out := make([]config.Indexer, len(rows))
	copy(out, rows)
	mgr := a.pluginManager()
	for i := range out {
		if !out[i].IsAddon() {
			continue
		}
		out[i].Enabled = false
		if mgr == nil || out[i].Plugin == "" {
			continue
		}
		rec, err := mgr.Get(ctx, out[i].Plugin)
		if err != nil {
			continue
		}
		out[i].URL = rec.SourceURL
		out[i].Protocol = "torrent"
		out[i].Enabled = rec.Enabled && rec.Manifest.HasCapability(plugins.CapStreamSource)
	}
	return out
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
		rec, err := a.installAddon(ctx, addonInstall{Link: ix.URL, Name: ix.Name, Roles: roles, Offline: []string{stremio.ResourceStream}, Exclusive: true})
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
