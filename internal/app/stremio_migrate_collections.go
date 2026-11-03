package app

import (
	"context"
	"strconv"
	"strings"

	"github.com/braedonsaunders/ether/internal/collections"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/stremio"
)

// migrateLegacyStremioCollections turns every collection source entered as
// a Stremio catalog link into the list source of the installed add-on
// (installed now, or reused) for the same catalog, with the same filters
// and item limit. The collection keeps its id, members and refresh.
func (a *App) migrateLegacyStremioCollections(ctx context.Context) error {
	if a.collections == nil {
		return nil
	}
	return a.collections.MigrateLegacyStremioSources(ctx, func(ctx context.Context, src core.CollectionSource) (core.CollectionSource, error) {
		rec, err := a.installAddon(ctx, plugins.AddonInstall{
			Link: src.URL, Roles: []plugins.Capability{plugins.CapListSource},
			Offline: []string{stremio.ResourceCatalog}, Exclusive: true,
		})
		if err != nil {
			return core.CollectionSource{}, err
		}
		return legacyCatalogSource(rec, src), nil
	})
}

// legacyCatalogSource is the add-on list source that replaces a legacy
// Stremio catalog source of one media type.
func legacyCatalogSource(rec plugins.Record, src core.CollectionSource) core.CollectionSource {
	cat := stremio.Catalog{Type: stremio.TypeMovie, ID: strings.TrimSpace(src.List)}
	if src.MediaType == collections.MediaShow {
		cat.Type = stremio.TypeSeries
	}
	// The retired source sent only these extras. When the manifest is known,
	// keep the ones the catalog declares, so the source validates.
	decl, declared := rec.Manifest.Stremio().CatalogNamed(cat.Type, cat.ID)
	var params map[string]string
	for _, name := range []string{"genre", "search"} {
		v := strings.TrimSpace(src.Params[name])
		if v == "" {
			continue
		}
		if _, ok := decl.ExtraNamed(name); declared && !ok {
			continue
		}
		if params == nil {
			params = map[string]string{}
		}
		params[name] = v
	}
	maxItems := src.MaxItems
	if maxItems <= 0 {
		maxItems, _ = strconv.Atoi(strings.TrimSpace(src.Params["maxItems"]))
	}
	maxItems = min(max(maxItems, 0), stremio.MaxCatalogItems)
	return core.CollectionSource{Kind: extend.PluginID(rec.ID, plugins.StremioCatalogKey(cat)), Params: params, MaxItems: maxItems}
}
