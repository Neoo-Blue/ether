package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// StremioResource preserves the resource-local filters. An object without
// idPrefixes accepts every ID; it must not inherit the manifest's prefixes.
type StremioResource struct {
	Name       string   `json:"name"`
	Types      []string `json:"types,omitempty"`
	IDPrefixes []string `json:"idPrefixes,omitempty"`
}

type StremioExtra struct {
	Name         string   `json:"name"`
	Required     bool     `json:"isRequired,omitempty"`
	Options      []string `json:"options,omitempty"`
	OptionsLimit int      `json:"optionsLimit,omitempty"`
}

func (m Manifest) StremioResources() []StremioResource {
	var raw []json.RawMessage
	if json.Unmarshal(m.Resources, &raw) != nil {
		return nil
	}
	var out []StremioResource
	for _, value := range raw {
		var name string
		if json.Unmarshal(value, &name) == nil {
			out = append(out, StremioResource{name, m.Types, m.IDPrefixes})
			continue
		}
		var r StremioResource
		if json.Unmarshal(value, &r) == nil && r.Name != "" {
			out = append(out, r)
		}
	}
	return out
}
func (m Manifest) HasResource(name string) bool {
	for _, r := range m.StremioResources() {
		if r.Name == name {
			return true
		}
	}
	return false
}
func (m Manifest) AcceptsResource(name, typ, id string) bool {
	for _, r := range m.StremioResources() {
		if r.Name != name {
			continue
		}
		if name == "catalog" || name == "addon_catalog" {
			cats := m.Catalogs
			if name == "addon_catalog" {
				cats = m.AddonCatalogs
			}
			for _, c := range cats {
				if c.Type == typ && c.ID == id {
					return true
				}
			}
			continue
		}
		typeOK := len(r.Types) == 0
		for _, t := range r.Types {
			if t == typ {
				typeOK = true
			}
		}
		if !typeOK {
			continue
		}
		if len(r.IDPrefixes) == 0 {
			return true
		}
		for _, p := range r.IDPrefixes {
			if strings.HasPrefix(id, p) {
				return true
			}
		}
	}
	return false
}
func (m Manifest) ConfigurationRequired() bool {
	var hints struct {
		Required bool `json:"configurationRequired"`
	}
	_ = json.Unmarshal(m.BehaviorHints, &hints)
	return hints.Required
}
func StremioCatalogKey(c StremioCat) string {
	hash := sha256.Sum256([]byte(c.Type + "\x00" + c.ID))
	return "catalog-" + hex.EncodeToString(hash[:8])
}

// EffectiveEther describes native adapters without modifying the foreign
// manifest stored on disk or inventing permissions for a third-party add-on.
func (m Manifest) EffectiveEther() *EtherBlock {
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
			var extras []StremioExtra
			_ = json.Unmarshal(c.Extra, &extras)
			var fields []ListParamField
			for _, x := range extras {
				if x.Name == "skip" {
					continue
				}
				field := ListParamField{Key: x.Name, Label: stremioExtraLabel(x.Name), Required: x.Required, Kind: "text"}
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
			for _, r := range m.StremioResources() {
				if r.Name != "meta" {
					continue
				}
				accepts := len(r.Types) == 0
				for _, t := range r.Types {
					if t == protocolType {
						accepts = true
					}
				}
				if accepts {
					types = append(types, typ)
					break
				}
			}
		}
		if len(types) > 0 {
			e.Capabilities = append(e.Capabilities, CapMetadata)
			e.Contributes.Metadata = &MetadataMeta{Types: types}
		}
	}
	if m.HasResource("stream") {
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
