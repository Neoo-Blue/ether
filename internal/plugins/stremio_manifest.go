package plugins

import (
	"encoding/json"
	"strings"

	"github.com/braedonsaunders/ether/internal/stremio"
)

// AcceptsResource reports whether the add-on serves resource for typ and
// id (internal/stremio.Manifest.TakesID).
func (m Manifest) AcceptsResource(name, typ, id string) bool {
	return m.Stremio().TakesID(name, typ, id)
}

// ConfigurationRequired reports an add-on installed from its unconfigured
// link.
func (m Manifest) ConfigurationRequired() bool {
	return m.BehaviorHints.ConfigurationRequired
}

// StremioCatalogKey is the list-source key of a Stremio catalog.
func StremioCatalogKey(c StremioCat) string { return c.Key() }

// StremioChannelsKey is the channel-source key of a `tv` add-on.
const StremioChannelsKey = "channels"

// ServesTV reports whether a plain Stremio add-on is a Live TV source: it
// lists `tv` catalogs and answers `tv` streams.
func (m Manifest) ServesTV() bool {
	return m.HasResource(stremio.ResourceCatalog) && len(m.CatalogsOfType(stremio.TypeTV)) > 0 &&
		m.Serves(stremio.ResourceStream, stremio.TypeTV)
}

// EffectiveEther describes native adapters without modifying the foreign
// manifest stored on disk or inventing permissions for a third-party add-on.
func (m Manifest) EffectiveEther() *EtherBlock {
	e := m.derivedEther()
	if len(m.rolesOff) == 0 {
		return e
	}
	return withoutRoles(e, m.rolesOff)
}

// withoutRoles copies e without the switched-off capabilities and their
// contributions.
func withoutRoles(e *EtherBlock, off []Capability) *EtherBlock {
	cp := *e
	cp.Capabilities = nil
	isOff := map[Capability]bool{}
	for _, c := range off {
		isOff[c] = true
	}
	for _, c := range e.Capabilities {
		if !isOff[c] {
			cp.Capabilities = append(cp.Capabilities, c)
		}
	}
	c := e.Contributes
	if isOff[CapListSource] {
		c.ListSources = nil
	}
	if isOff[CapPluginDirectory] {
		c.Directories = nil
	}
	if isOff[CapMetadata] {
		c.Metadata = nil
	}
	if isOff[CapLiveTV] {
		c.Channels = nil
	}
	if isOff[CapStreamSource] {
		c.Source = nil
	}
	if isOff[CapSubtitles] {
		c.Subtitles = nil
	}
	cp.Contributes = c
	return &cp
}

// AddonRoles are the roles a plain Stremio add-on can play, derived from
// its manifest, whether or not the owner switched them off.
func (m Manifest) AddonRoles() []Capability {
	return m.derivedEther().Capabilities
}

func (m Manifest) derivedEther() *EtherBlock {
	if m.Ether != nil {
		return m.Ether
	}
	e := &EtherBlock{Kind: KindExtension, Summary: m.Description, Icon: m.Logo, Runtime: RuntimeDecl{Kind: "remote"}}
	if m.ConfigurationRequired() {
		return e
	}
	if m.HasResource("catalog") {
		e.Capabilities = append(e.Capabilities, CapListSource)
		for _, c := range m.Catalogs {
			if c.Type != "movie" && c.Type != "series" {
				continue
			}
			var fields []ListParamField
			for _, x := range c.Extras() {
				if x.Name == "skip" {
					continue
				}
				field := ListParamField{Key: x.Name, Label: stremioExtraLabel(x.Name), Required: x.IsRequired, Kind: "text"}
				for _, o := range x.Options {
					field.Options = append(field.Options, struct {
						Value string `json:"value"`
						Label string `json:"label"`
					}{o, o})
				}
				if len(field.Options) > 0 {
					field.Kind = "select"
				}
				fields = append(fields, field)
			}
			params, _ := json.Marshal(fields)
			label := c.Name
			if label == "" {
				label = c.ID
			}
			e.Contributes.ListSources = append(e.Contributes.ListSources, ListSourceMeta{Key: StremioCatalogKey(c), Label: label + " · " + c.Type, Catalog: c.Type + "/" + c.ID, Params: params, Refresh: "1h"})
		}
	}
	if m.HasResource("addon_catalog") && len(m.AddonCatalogs) > 0 {
		e.Capabilities = append(e.Capabilities, CapPluginDirectory)
		for _, c := range m.AddonCatalogs {
			label := c.Name
			if label == "" {
				label = c.ID
			}
			e.Contributes.Directories = append(e.Contributes.Directories, DirectoryMeta{Key: "directory-" + strings.TrimPrefix(StremioCatalogKey(c), "catalog-"), Label: label + " · " + c.Type})
		}
	}
	if m.HasResource("meta") {
		var types []string
		for _, typ := range []string{"movie", "show"} {
			protocolType := typ
			if typ == "show" {
				protocolType = "series"
			}
			if m.Serves(stremio.ResourceMeta, protocolType) {
				types = append(types, typ)
			}
		}
		if len(types) > 0 {
			e.Capabilities = append(e.Capabilities, CapMetadata)
			e.Contributes.Metadata = &MetadataMeta{Types: types}
		}
	}
	if m.ServesTV() {
		e.Capabilities = append(e.Capabilities, CapLiveTV)
		e.Contributes.Channels = &ChannelsMeta{Refresh: "6h"}
	}
	if m.Serves(stremio.ResourceStream, stremio.TypeMovie) || m.Serves(stremio.ResourceStream, stremio.TypeSeries) {
		e.Kind = KindSource
		e.Capabilities = append(e.Capabilities, CapStreamSource)
		e.Contributes.Source = &SourceMeta{Class: "indexer", Abilities: []string{"search"}}
	}
	if m.HasResource("subtitles") {
		e.Capabilities = append(e.Capabilities, CapSubtitles)
		e.Contributes.Subtitles = &SubtitlesMeta{Languages: json.RawMessage(`"any"`)}
	}
	return e
}

// Labels belong to the adapter descriptor; the imported manifest stays intact.
func stremioExtraLabel(name string) string {
	switch name {
	case "genre":
		return "Genre"
	case "search":
		return "Search"
	case "lastVideosIds":
		return "Recent video IDs"
	case "calendarVideosIds":
		return "Calendar video IDs"
	}
	return name
}
