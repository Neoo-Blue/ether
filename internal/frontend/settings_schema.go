package frontend

import (
	"fmt"
	"math"
	"sort"
)

// SettingsSchema describes a frontend's owner-console settings so the Apps
// page renders them without per-protocol code (WS1d). It carries field keys
// and kinds only; the console owns the translated copy for each known field
// key, section key and note key, exactly as it owns page components for
// descriptor IDs.
type SettingsSchema struct {
	Sections []SettingsSection `json:"sections"`
	// AccountConnect means devices connect through the viewer account page
	// (an addon address, a files address or a playlist), not a server
	// address on this page.
	AccountConnect bool `json:"accountConnect,omitempty"`
	// SignInHelp shows the "how people sign in" section (Quick Connect and
	// app passwords).
	SignInHelp bool `json:"signInHelp,omitempty"`
}

// SettingsSection is one grouped block of fields. Key names the console
// copy: "find" (how apps find this server), "tv" (what the TV shows),
// "home" (personalized home rows) or "connect".
type SettingsSection struct {
	Key    string          `json:"key"`
	Fields []SettingsField `json:"fields"`
}

// SettingsField is one setting. Kind is one of: serverName (read-only name
// from General settings), port, bool, number, viewer (the DLNA browse-as
// picker), rowKinds (personalized row kind switches) or note (static text
// named by Key, for example "addon", "files" or "playlist").
type SettingsField struct {
	Key     string   `json:"key"`
	Kind    string   `json:"kind"`
	More    bool     `json:"more,omitempty"`
	Min     *int     `json:"min,omitempty"`
	Max     *int     `json:"max,omitempty"`
	Options []string `json:"options,omitempty"`
}

func intp(n int) *int { return &n }

// homeRowKinds are the personalized row kinds Jellyfin and Emby apps can
// show, in display order.
var homeRowKinds = []string{
	"because-you-watched", "trending", "top-10", "fans-also-loved",
	"new-releases", "hidden-gems", "more-from-person", "popular-on-server",
}

// MediaBrowserSchema is the settings schema the Jellyfin and Emby frontends
// share; quickConnect adds Jellyfin's Quick Connect switch.
func MediaBrowserSchema(quickConnect bool) SettingsSchema {
	find := []SettingsField{
		{Key: "serverName", Kind: "serverName"},
		{Key: "port", Kind: "port", Min: intp(1), Max: intp(65535)},
		{Key: "discovery", Kind: "bool", More: true},
	}
	if quickConnect {
		find = append(find, SettingsField{Key: "quickConnect", Kind: "bool", More: true})
	}
	find = append(find, SettingsField{Key: "publicViewerPicker", Kind: "bool", More: true})
	return SettingsSchema{
		SignInHelp: true,
		Sections: []SettingsSection{
			{Key: "home", Fields: []SettingsField{
				{Key: "homeRows", Kind: "bool"},
				{Key: "homeRowCount", Kind: "number", More: true, Min: intp(0), Max: intp(12)},
				{Key: "homeRowKinds", Kind: "rowKinds", More: true, Options: append([]string(nil), homeRowKinds...)},
			}},
			{Key: "find", Fields: find},
		},
	}
}

// NamedServerSchema is a server apps find by name and port, with discovery
// (Silo).
func NamedServerSchema() SettingsSchema {
	return SettingsSchema{
		SignInHelp: true,
		Sections: []SettingsSection{{Key: "find", Fields: []SettingsField{
			{Key: "serverName", Kind: "serverName"},
			{Key: "port", Kind: "port", Min: intp(1), Max: intp(65535)},
			{Key: "discovery", Kind: "bool", More: true},
		}}},
	}
}

// DLNASchema browses the library as one chosen viewer.
func DLNASchema() SettingsSchema {
	return SettingsSchema{Sections: []SettingsSection{{Key: "tv", Fields: []SettingsField{
		{Key: "serverName", Kind: "serverName"},
		{Key: "viewerId", Kind: "viewer"},
		{Key: "discovery", Kind: "bool", More: true},
	}}}}
}

// AccountSchema is a surface with no port of its own; viewers get its
// address from their account page. note names the console copy.
func AccountSchema(note string) SettingsSchema {
	return SettingsSchema{
		AccountConnect: true,
		Sections:       []SettingsSection{{Key: "connect", Fields: []SettingsField{{Key: note, Kind: "note"}}}},
	}
}

// PortSchema is a frontend whose only protocol setting is its port (Plex,
// whose console settings live on their own page).
func PortSchema() SettingsSchema {
	return SettingsSchema{Sections: []SettingsSection{{Key: "find", Fields: []SettingsField{
		{Key: "port", Kind: "port", Min: intp(1), Max: intp(65535)},
	}}}}
}

// schemaOf returns the descriptor's schema when it carries one of this
// package's schema values.
func schemaOf(d Descriptor) (SettingsSchema, bool) {
	s, ok := d.SettingsSchema.(SettingsSchema)
	return s, ok
}

// validateAgainstSchema checks keys and value shapes against the schema.
// Read-only kinds (serverName, note) are not settings and never stored.
func (s SettingsSchema) validate(settings Settings) error {
	fields := map[string]SettingsField{}
	for _, sec := range s.Sections {
		for _, f := range sec.Fields {
			if f.Kind == "serverName" || f.Kind == "note" {
				continue
			}
			fields[f.Key] = f
		}
	}
	keys := make([]string, 0, len(settings))
	for k := range settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		f, ok := fields[k]
		if !ok {
			return fmt.Errorf("unknown setting %s", k)
		}
		if err := f.validateValue(settings[k]); err != nil {
			return err
		}
	}
	return nil
}

func (f SettingsField) validateValue(v any) error {
	switch f.Kind {
	case "bool":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("invalid %s", f.Key)
		}
	case "port", "number", "viewer":
		n, ok := v.(float64)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) {
			return fmt.Errorf("invalid %s", f.Key)
		}
		if f.Key == "viewerId" && (n <= 0 || n > 9007199254740991) {
			return fmt.Errorf("invalid %s", f.Key)
		}
		if f.Min != nil && n < float64(*f.Min) {
			return fmt.Errorf("invalid %s", f.Key)
		}
		if f.Max != nil && n > float64(*f.Max) {
			return fmt.Errorf("invalid %s", f.Key)
		}
	case "rowKinds":
		list, ok := v.([]any)
		if !ok {
			return fmt.Errorf("invalid %s", f.Key)
		}
		allowed := map[string]bool{}
		for _, o := range f.Options {
			allowed[o] = true
		}
		for _, item := range list {
			kind, ok := item.(string)
			if !ok || (len(allowed) > 0 && !allowed[kind]) {
				return fmt.Errorf("invalid %s", f.Key)
			}
		}
	}
	return nil
}
