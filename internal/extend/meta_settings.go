package extend

import "encoding/json"

// SettingsSchemaMeta is the Meta of a KindSettingsSchema descriptor. Schema
// is an encoded internal/config/schema.Schema; it stays raw here so this
// package does not import the schema package (which imports this one for
// Text). The TypeScript mirror is SettingsSchemaMeta in
// web/src/pages/settings/SchemaForm.tsx.
type SettingsSchemaMeta struct {
	// ConfigKey is the config.json section a built-in form edits, when it
	// edits one. Plugin settings leave it empty.
	ConfigKey string          `json:"configKey,omitempty"`
	Schema    json.RawMessage `json:"schema"`
}
