package extend

import "encoding/json"

// ParamOption is one choice of a select-shaped source parameter.
type ParamOption struct {
	Value string   `json:"value"`
	Label Text     `json:"label"`
	Media []string `json:"media,omitempty"`
}

// ParamField is one source-kind parameter. Kind uses the settings field
// kinds from docs/extend/SPEC.md §5.5 (text, url, select, number, bool,
// list, title, …), so the served-schema form (WS1f) can render it without
// translation. Name maps onto the persisted source object's JSON field.
type ParamField struct {
	Name     string        `json:"name"`
	Kind     string        `json:"kind"`
	Label    Text          `json:"label"`
	Hint     Text          `json:"hint,omitempty"`
	Required bool          `json:"required,omitempty"`
	Options  []ParamOption `json:"options,omitempty"`
	// Lookup names a catalogue lookup ("person", "keyword", "collection",
	// "company", "network", "watchProvider") for id-valued fields.
	Lookup string `json:"lookup,omitempty"`
	// Placeholder is display-only example text (a URL shape).
	Placeholder string `json:"placeholder,omitempty"`
}

// CollectionSourceMeta is Descriptor.Meta for KindCollectionSource.
type CollectionSourceMeta struct {
	Params []ParamField `json:"params,omitempty"`
	// Editor names the console widget family that edits this kind today:
	// none, tmdbChart, lookup, urlList, listSelect, discover, titles.
	// WS1f's served-schema renderer replaces this with Params alone.
	Editor string `json:"editor,omitempty"`
	// Defaults is the new-source template: the console creates a source as
	// {"kind": <id>, ...defaults}, then the owner fills required params.
	Defaults json.RawMessage `json:"defaults,omitempty"`
	// MediaTypes lists the collection media types this kind can feed.
	MediaTypes []string `json:"mediaTypes,omitempty"`
}

// RowSourceMeta is Descriptor.Meta for KindRowSource.
type RowSourceMeta struct {
	// HubID is the row pattern the Home Designer policy targets.
	HubID string `json:"hubId"`
	// RowKind is the personalization row kind, when the row is kind-backed.
	RowKind string `json:"rowKind,omitempty"`
	// Sections lists the pages this row can appear on (empty = every page).
	Sections []string `json:"sections,omitempty"`
	// Source is the row provenance (core.RowSource* for built-ins).
	Source string `json:"source,omitempty"`
	// CollectionSource links a list-backed row source to the collection
	// source kind that gathers its titles (WS3/WS9 list sources).
	CollectionSource string `json:"collectionSource,omitempty"`
}

// LiveTVSourceMeta is Descriptor.Meta for KindLiveTVSource.
type LiveTVSourceMeta struct {
	Params         []ParamField `json:"params,omitempty"`
	Editor         string       `json:"editor,omitempty"`
	YieldsChannels bool         `json:"yieldsChannels,omitempty"`
	YieldsGuide    bool         `json:"yieldsGuide,omitempty"`
	// Test reports whether the kind supports a connection test.
	Test bool `json:"test,omitempty"`
}
