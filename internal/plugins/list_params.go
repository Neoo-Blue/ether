package plugins

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/braedonsaunders/ether/internal/extend"
)

type ListParamField struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Label       string `json:"label"`
	Help        string `json:"help"`
	Hint        string `json:"hint"`
	Required    bool   `json:"required"`
	Placeholder string `json:"placeholder"`
	Default     any    `json:"default"`
	Options     []struct {
		Value string `json:"value"`
		Label string `json:"label"`
	} `json:"options"`
}

func ParseListParamFields(raw json.RawMessage) []ListParamField {
	if len(raw) == 0 {
		return nil
	}
	var arr []ListParamField
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}
	var obj struct {
		Fields []ListParamField `json:"fields"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj.Fields
	}
	return nil
}

func ParamFieldsFromList(pluginID string, fields []ListParamField) ([]extend.ParamField, string) {
	params := make([]extend.ParamField, 0, len(fields))
	defaults := map[string]any{}
	for _, f := range fields {
		key := firstNonEmptyParam(f.Key, f.Name)
		if key == "" {
			continue
		}
		kind := f.Kind
		if kind == "" {
			kind = "text"
		}
		pf := extend.ParamField{
			Name:        key,
			Kind:        kind,
			Label:       textOf(pluginID, firstNonEmptyParam(f.Label, key)),
			Hint:        textOf(pluginID, firstNonEmptyParam(f.Hint, f.Help)),
			Required:    f.Required,
			Placeholder: f.Placeholder,
		}
		for _, opt := range f.Options {
			pf.Options = append(pf.Options, extend.ParamOption{
				Value: opt.Value,
				Label: textOf(pluginID, firstNonEmptyParam(opt.Label, opt.Value)),
			})
		}
		params = append(params, pf)
		if f.Default != nil {
			defaults[key] = fmt.Sprint(f.Default)
		}
	}
	raw, err := json.Marshal(defaults)
	if err != nil || len(defaults) == 0 {
		return params, `{}`
	}
	return params, string(raw)
}

func firstNonEmptyParam(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
