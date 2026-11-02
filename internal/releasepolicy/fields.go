package releasepolicy

import "strings"

// FieldSpec describes one rule field for validation and for the extend
// descriptor registry (WS1a). The console renders its condition picker
// from descriptors built on these specs, so this table is the one Go
// source for which fields exist and how their values validate.
type FieldSpec struct {
	ID string
	// ValueClass: text, choice, language, number or none
	// (extend.RuleValue* values, kept as strings to avoid a cycle).
	ValueClass      string
	Operators       []string
	DefaultOperator string
	SingleValue     bool
	// External marks a plugin-contributed field: it compiles when its
	// descriptor is registered and evaluates Unknown until the plugin
	// hot path (WS9h) supplies matches.
	External bool
}

// FieldLookup resolves a condition field id to its spec. The resolver
// builds one from the descriptor registry; nil means the built-in table.
type FieldLookup func(id string) (FieldSpec, bool)

var builtinFields = []FieldSpec{
	{ID: "language", ValueClass: "language", Operators: []string{"equals"}, DefaultOperator: "equals"},
	{ID: "subtitles", ValueClass: "language", Operators: []string{"equals"}, DefaultOperator: "equals"},
	{ID: "audio_matches_original", ValueClass: "none", Operators: []string{"equals"}, DefaultOperator: "equals"},
	{ID: "name", ValueClass: "text", Operators: []string{"equals", "contains", "regex"}, DefaultOperator: "contains"},
	{ID: "group", ValueClass: "text", Operators: []string{"equals", "contains", "regex"}, DefaultOperator: "equals", SingleValue: true},
	{ID: "source", ValueClass: "choice", Operators: []string{"equals", "contains", "regex"}, DefaultOperator: "equals", SingleValue: true},
	{ID: "video", ValueClass: "choice", Operators: []string{"equals", "contains", "regex"}, DefaultOperator: "equals", SingleValue: true},
	{ID: "audio", ValueClass: "choice", Operators: []string{"equals", "contains", "regex"}, DefaultOperator: "equals", SingleValue: true},
	{ID: "height", ValueClass: "number", Operators: []string{"range"}, DefaultOperator: "range"},
	{ID: "size_gib", ValueClass: "number", Operators: []string{"range"}, DefaultOperator: "range"},
	{ID: "hdr", ValueClass: "choice", Operators: []string{"equals", "contains", "regex"}, DefaultOperator: "equals"},
	{ID: "edition", ValueClass: "text", Operators: []string{"equals", "contains", "regex"}, DefaultOperator: "equals", SingleValue: true},
	{ID: "indexer", ValueClass: "text", Operators: []string{"equals", "contains", "regex"}, DefaultOperator: "equals", SingleValue: true},
	{ID: "protocol", ValueClass: "choice", Operators: []string{"equals", "contains", "regex"}, DefaultOperator: "equals", SingleValue: true},
}

// BuiltinFields returns the built-in rule fields in console picker order.
func BuiltinFields() []FieldSpec {
	out := make([]FieldSpec, len(builtinFields))
	copy(out, builtinFields)
	return out
}

// LookupBuiltinField is the default FieldLookup.
func LookupBuiltinField(id string) (FieldSpec, bool) {
	for _, f := range builtinFields {
		if f.ID == id {
			return f, true
		}
	}
	return FieldSpec{}, false
}

// IsExternalFieldID reports whether id is plugin-namespaced
// ("plugin:<id>/<key>"), the shape every plugin rule field takes.
func IsExternalFieldID(id string) bool {
	rest, ok := strings.CutPrefix(id, "plugin:")
	return ok && strings.Contains(rest, "/")
}

func lookupOrBuiltin(lookup FieldLookup) FieldLookup {
	if lookup != nil {
		return lookup
	}
	return LookupBuiltinField
}
