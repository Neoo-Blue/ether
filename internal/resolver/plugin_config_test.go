package resolver

import (
	"encoding/json"
	"testing"
)

func TestPluginStepConfigValidation(t *testing.T) {
	tests := []struct {
		editor, config string
		valid          bool
	}{
		{`{"kind":"toggle"}`, `{"value":true}`, true}, {`{"kind":"toggle"}`, `{"value":null}`, false},
		{`{"kind":"choice","options":[{"value":"en"},{"value":"fr"}]}`, `{"value":"fr"}`, true},
		{`{"kind":"choice","options":["en"]}`, `{"value":"fr"}`, false},
		{`{"kind":"ordered_list","options":["en","fr"]}`, `{"value":["fr","en"]}`, true},
		{`{"kind":"ordered_list","options":["en"]}`, `{"value":["en","en"]}`, false},
		{`{"kind":"range"}`, `{"min":2,"max":3}`, true}, {`{"kind":"range"}`, `{"min":3,"max":2}`, false},
		{`null`, `{}`, true}, {`null`, `{"value":true}`, false},
	}
	for _, tc := range tests {
		meta := json.RawMessage(`{"editor":` + tc.editor + `}`)
		err := ValidateStepConfig(meta, json.RawMessage(tc.config))
		if (err == nil) != tc.valid {
			t.Errorf("editor %s config %s: %v", tc.editor, tc.config, err)
		}
	}
}
