package etherplugin

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

var allowedSettingKinds = map[string]bool{
	"group":       true,
	"text":        true,
	"secret":      true,
	"duration":    true,
	"readonly":    true,
	"number":      true,
	"bool":        true,
	"select":      true,
	"list":        true,
	"bytes":       true,
	"hidden":      true,
	"multiSelect": true,
	"orderedList": true,
	"language":    true,
	"viewer":      true,
	"collection":  true,
	"folder":      true,
	"url":         true,
	"title":       true,
}

// FieldProblem describes one schema validation or copy-rule problem.
type FieldProblem struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// CollectSettingFields returns all leaf SettingFields from a SettingsSchema.
func CollectSettingFields(schema *SettingsSchema) []SettingField {
	if schema == nil {
		return nil
	}
	var out []SettingField
	var walk func(fields []SettingField)
	walk = func(fields []SettingField) {
		for _, f := range fields {
			if f.Key != "" && f.Kind != "" && f.Kind != "group" {
				out = append(out, f)
			}
			if len(f.Fields) > 0 {
				walk(f.Fields)
			}
		}
	}
	walk(schema.Fields)
	for _, g := range schema.Groups {
		walk(g.Fields)
	}
	return out
}

// LintSettingsSchema checks a SettingsSchema for forbidden kinds and copy-rule violations.
func LintSettingsSchema(schema *SettingsSchema) []FieldProblem {
	if schema == nil {
		return nil
	}
	var problems []FieldProblem
	var walk func(fields []SettingField, prefix string)
	walk = func(fields []SettingField, prefix string) {
		for idx, f := range fields {
			path := fmt.Sprintf("%s[%d]", prefix, idx)
			if f.Kind == "json" {
				problems = append(problems, FieldProblem{
					Field:   path + ".kind",
					Message: "the json field kind is not allowed in plugin settings",
				})
			} else if f.Kind != "" && !allowedSettingKinds[f.Kind] {
				problems = append(problems, FieldProblem{
					Field:   path + ".kind",
					Message: fmt.Sprintf("unknown field kind %q", f.Kind),
				})
			}
			if f.Label != "" && !strings.HasPrefix(f.Label, "$t:") {
				if msg := LintCopy(f.Label, 40); msg != "" {
					problems = append(problems, FieldProblem{Field: path + ".label", Message: msg})
				}
			}
			if f.Hint != "" && !strings.HasPrefix(f.Hint, "$t:") {
				if msg := LintCopy(f.Hint, 120); msg != "" {
					problems = append(problems, FieldProblem{Field: path + ".hint", Message: msg})
				}
				trimmed := strings.TrimRight(f.Hint, ".")
				if strings.Contains(trimmed, ".") {
					problems = append(problems, FieldProblem{Field: path + ".hint", Message: "must be one sentence"})
				}
			}
			if len(f.Fields) > 0 {
				walk(f.Fields, path+".fields")
			}
		}
	}
	if len(schema.Fields) > 0 {
		walk(schema.Fields, "ether.settings.fields")
	}
	for idx, g := range schema.Groups {
		gPath := fmt.Sprintf("ether.settings.groups[%d]", idx)
		if g.Label != "" && !strings.HasPrefix(g.Label, "$t:") {
			if msg := LintCopy(g.Label, 40); msg != "" {
				problems = append(problems, FieldProblem{Field: gPath + ".label", Message: msg})
			}
		}
		if g.Hint != "" && !strings.HasPrefix(g.Hint, "$t:") {
			if msg := LintCopy(g.Hint, 120); msg != "" {
				problems = append(problems, FieldProblem{Field: gPath + ".hint", Message: msg})
			}
		}
		walk(g.Fields, gPath+".fields")
	}
	return problems
}

// DecodeSettings applies schema defaults and validates incoming settings,
// returning the decoded settings map and any JSON-pointer keyed errors.
func DecodeSettings(
	schema *SettingsSchema,
	incoming map[string]any,
	defaults map[string]any,
) (map[string]any, map[string]string) {
	fields := CollectSettingFields(schema)
	result := map[string]any{}
	for k, v := range defaults {
		result[k] = v
	}
	for _, f := range fields {
		if f.Default != nil {
			if _, exists := result[f.Key]; !exists {
				result[f.Key] = f.Default
			}
		}
	}
	for k, v := range incoming {
		result[k] = v
	}

	errors := map[string]string{}
	for _, f := range fields {
		val, exists := result[f.Key]
		ptr := "/" + f.Key
		if f.Required && (!exists || val == nil || val == "") {
			errors[ptr] = "is required"
			continue
		}
		if !exists || val == nil || val == "" {
			continue
		}
		switch f.Kind {
		case "number", "bytes", "viewer":
			num, ok := toFloat64(val)
			if !ok || math.IsNaN(num) || math.IsInf(num, 0) {
				errors[ptr] = "must be a number"
				continue
			}
			if f.Min != nil && num < *f.Min {
				errors[ptr] = fmt.Sprintf("must be at least %v", *f.Min)
			}
			if f.Max != nil && num > *f.Max {
				errors[ptr] = fmt.Sprintf("must be at most %v", *f.Max)
			}
			result[f.Key] = num
		case "bool":
			if _, ok := val.(bool); !ok {
				errors[ptr] = "must be true or false"
			}
		case "list", "multiSelect", "orderedList":
			switch val.(type) {
			case []any, []string:
			default:
				errors[ptr] = "must be a list"
			}
		case "url":
			s, ok := val.(string)
			if !ok || (!strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://")) {
				errors[ptr] = "must be an http or https URL"
			}
		}
	}
	return result, errors
}

func toFloat64(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case int32:
		return float64(t), true
	case string:
		n, err := strconv.ParseFloat(t, 64)
		return n, err == nil
	default:
		return 0, false
	}
}
