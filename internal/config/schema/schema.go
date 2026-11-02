// Package schema is the settings schema contract shared by Ether's own
// settings pages and, later, plugin settings (docs/extend/SPEC.md §5.5).
//
// The server serves schemas as settingsSchema descriptors; the console's
// SchemaForm renders them and never keeps its own copy. Labels and help are
// extend.Text: built-ins declare them with extend.T so the i18n extractor
// carries them into the console catalogue, and plugins will carry $t keys.
package schema

import (
	"encoding/json"

	"github.com/braedonsaunders/ether/internal/extend"
)

// Kind names one field editor. The console mirrors this list in
// web/src/pages/settings/SchemaForm.tsx.
type Kind string

const (
	KindGroup    Kind = "group"
	KindText     Kind = "text"
	KindSecret   Kind = "secret"
	KindDuration Kind = "duration"
	KindReadonly Kind = "readonly"
	KindNumber   Kind = "number"
	KindBool     Kind = "bool"
	KindSelect   Kind = "select"
	KindList     Kind = "list"
	KindBytes    Kind = "bytes"
	// KindJSON is built-ins only; plugin schemas may not use it.
	KindJSON        Kind = "json"
	KindHidden      Kind = "hidden"
	KindMultiSelect Kind = "multiSelect"
	KindOrderedList Kind = "orderedList"
	// KindLanguage is an ISO 639 language (optionally with a region, en-US).
	KindLanguage   Kind = "language"
	KindViewer     Kind = "viewer"
	KindCollection Kind = "collection"
	// KindFolder is a folder path on the server. Plugin schemas may use it
	// only with the folders permission.
	KindFolder Kind = "folder"
	KindURL    Kind = "url"
	// KindTitle is one catalogue title, picked with TitleSearch.
	KindTitle Kind = "title"
)

var knownKinds = map[Kind]bool{
	KindGroup: true, KindText: true, KindSecret: true, KindDuration: true,
	KindReadonly: true, KindNumber: true, KindBool: true, KindSelect: true,
	KindList: true, KindBytes: true, KindJSON: true, KindHidden: true,
	KindMultiSelect: true, KindOrderedList: true, KindLanguage: true,
	KindViewer: true, KindCollection: true, KindFolder: true, KindURL: true,
	KindTitle: true,
}

// Known reports whether k is a field kind both sides render.
func Known(k Kind) bool { return knownKinds[k] }

// Option is one choice for select, multiSelect and orderedList fields.
type Option struct {
	Value string      `json:"value"`
	Label extend.Text `json:"label"`
}

// Field is one setting. Only the fields a kind uses are set.
type Field struct {
	Key   string      `json:"key"`
	Label extend.Text `json:"label"`
	Help  extend.Text `json:"help,omitempty"`
	Kind  Kind        `json:"kind"`

	Placeholder string   `json:"placeholder,omitempty"`
	Wide        bool     `json:"wide,omitempty"`
	Min         *float64 `json:"min,omitempty"`
	Max         *float64 `json:"max,omitempty"`
	Step        *float64 `json:"step,omitempty"`
	// Unit is shown beside number and bytes inputs ("GiB", "p", "chunks").
	Unit extend.Text `json:"unit,omitempty"`
	// Numeric stores select, list, multiSelect and orderedList values as
	// numbers instead of strings.
	Numeric  bool `json:"numeric,omitempty"`
	Required bool `json:"required,omitempty"`

	Options []Option `json:"options,omitempty"`
	// OptionsFrom names a runtime option source (a plugin's
	// GET /ether/options/{field}) used when Options is empty.
	OptionsFrom string  `json:"optionsFrom,omitempty"`
	Fields      []Field `json:"fields,omitempty"` // KindGroup only
}

// Group is one section of a schema. More holds the advanced fields the
// console shows after "More options".
type Group struct {
	ID          string      `json:"id,omitempty"`
	Title       extend.Text `json:"title,omitempty"`
	Description extend.Text `json:"description,omitempty"`
	Fields      []Field     `json:"fields"`
	More        []Field     `json:"more,omitempty"`
}

// Schema is a full settings form.
type Schema struct {
	Groups []Group `json:"groups"`
}

// Meta wraps the schema as a settingsSchema descriptor's Meta.
func (s Schema) Meta(configKey string) extend.SettingsSchemaMeta {
	raw, err := json.Marshal(s)
	if err != nil { // Schema holds no unmarshalable values.
		panic("schema: marshal: " + err.Error())
	}
	return extend.SettingsSchemaMeta{ConfigKey: configKey, Schema: raw}
}

// Builtin is one built-in settings form served as a descriptor.
type Builtin struct {
	ID string `json:"id"`
	// ConfigKey is the config.json section the form edits, when it edits one.
	ConfigKey string      `json:"configKey,omitempty"`
	Label     extend.Text `json:"label"`
	Schema    Schema      `json:"schema"`
}

// F takes the address of a number bound for a field literal.
func F(v float64) *float64 { return &v }

// MarshalJSON drops empty Text values (encoding/json keeps zero structs
// despite omitempty), so served schemas carry no "help": {} noise.
func (f Field) MarshalJSON() ([]byte, error) {
	type fieldAlias Field
	return marshalWithoutEmptyText(fieldAlias(f), map[string]extend.Text{
		"help": f.Help, "unit": f.Unit,
	})
}

// MarshalJSON drops an empty title and description.
func (g Group) MarshalJSON() ([]byte, error) {
	type groupAlias Group
	return marshalWithoutEmptyText(groupAlias(g), map[string]extend.Text{
		"title": g.Title, "description": g.Description,
	})
}

func marshalWithoutEmptyText(v any, texts map[string]extend.Text) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	for key, text := range texts {
		if text.Empty() {
			delete(obj, key)
		}
	}
	return json.Marshal(obj)
}
