package plugins

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/braedonsaunders/ether/internal/stremio"
)

// AddonInstall is one request to install (or reuse) the add-on at Link and
// switch Roles on. Name is a fallback display name when the manifest cannot
// be read; Offline lists the resources a legacy entry proves the add-on
// serves, used to record it while its host is unreachable.
type AddonInstall struct {
	Link    string
	Name    string
	Roles   []Capability
	Offline []string
	// Exclusive turns every other role off on a newly created record
	// (migrations keep exactly what the legacy entry did).
	Exclusive bool
}

// InstallAddon installs the add-on at link, or finds it when that link is
// already installed, and switches the requested roles on. A private host
// is trusted as LAN: the owner pasted it.
func (m *Manager) InstallAddon(ctx context.Context, in AddonInstall) (Record, error) {
	mgr := m
	id, base, err := StremioInstallID(in.Link)
	if err != nil {
		return Record{}, err
	}
	existing, getErr := mgr.Get(ctx, id)
	created := getErr != nil
	if created {
		src := Source{Kind: "url", URL: base, LAN: privateHost(base)}
		preview, err := mgr.PreviewInstall(ctx, src)
		if err != nil && len(in.Offline) > 0 {
			raw, merr := offlineManifest(base, in.Name, in.Offline)
			if merr != nil {
				return Record{}, merr
			}
			src.Manifest = raw
			preview, err = mgr.PreviewInstall(ctx, src)
		}
		if err != nil {
			return Record{}, err
		}
		if _, err := mgr.Install(ctx, preview, InstallOptions{Grants: Grants{LAN: src.LAN}}); err != nil {
			return Record{}, err
		}
		if existing, err = mgr.Get(ctx, id); err != nil {
			return Record{}, err
		}
	}
	want := map[Capability]bool{}
	for _, r := range in.Roles {
		want[r] = true
	}
	off := map[Capability]bool{}
	for _, r := range existing.RolesOff {
		off[r] = true
	}
	if created && in.Exclusive {
		// Every role but the requested ones stays off, including roles
		// the add-on only declares later (its manifest is re-read hourly):
		// a migrated entry keeps doing exactly what it did.
		for _, r := range AddonCapabilities {
			off[r] = true
		}
	}
	for r := range want {
		delete(off, r)
	}
	if !created && len(want) == 0 {
		return existing, nil
	}
	list := make([]Capability, 0, len(off))
	for r := range off {
		list = append(list, r)
	}
	if err := mgr.store.setRolesOff(ctx, id, list); err != nil {
		return Record{}, err
	}
	saved, err := mgr.store.record(ctx, id)
	if err != nil {
		return Record{}, err
	}
	mgr.closeRuntime(id)
	mgr.syncDescriptors(ctx, saved)
	mgr.broadcast(ctx)
	return saved, nil
}

// AddonCapabilities are the roles a plain Stremio add-on can play.
var AddonCapabilities = []Capability{CapStreamSource, CapListSource, CapLiveTV, CapSubtitles, CapMetadata, CapPluginDirectory}

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
	var catalogs []stremio.Catalog
	for _, r := range resources {
		if r == "tv" {
			// A `tv` add-on is one with tv catalogs and tv streams; the
			// placeholder catalog is replaced by the real ones on refresh.
			res = append(res, stremio.Resource{Name: stremio.ResourceStream, Short: true}, stremio.Resource{Name: stremio.ResourceCatalog, Short: true})
			catalogs = append(catalogs, stremio.Catalog{Type: stremio.TypeTV, ID: "channels", Name: name})
			continue
		}
		res = append(res, stremio.Resource{Name: r, Short: true})
	}
	return json.Marshal(stremio.Manifest{ID: "legacy." + strings.ReplaceAll(u.Hostname(), ".", "-"), Version: "0.0.0", Name: name, Types: types, IDPrefixes: []string{"tt"}, Resources: res, Catalogs: catalogs})
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
