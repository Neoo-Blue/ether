package agent

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
)

// validateInput checks a model-supplied tool input against the tool's JSON
// Schema (the subset Ether tools use: objects, properties, required,
// additionalProperties, primitive types, enum, arrays/items, string and
// number bounds). It fails closed on unknown schema keywords is not
// required; unknown keywords are ignored, known ones enforced.
func validateInput(schema json.RawMessage, input any) error {
	var s map[string]any
	if len(schema) == 0 || json.Unmarshal(schema, &s) != nil {
		return fmt.Errorf("tool has no valid input schema")
	}
	return validateValue(s, input, "input")
}

func validateValue(schema map[string]any, v any, path string) error {
	if enum, ok := schema["enum"].([]any); ok {
		for _, e := range enum {
			if reflect.DeepEqual(e, v) {
				return nil // enum match is sufficient here
			}
		}
		return fmt.Errorf("%s must be one of the allowed values", path)
	}
	switch typ, _ := schema["type"].(string); typ {
	case "object", "":
		obj, ok := v.(map[string]any)
		if !ok {
			if typ == "object" {
				return fmt.Errorf("%s must be an object", path)
			}
			return nil
		}
		props, _ := schema["properties"].(map[string]any)
		if req, ok := schema["required"].([]any); ok {
			for _, r := range req {
				name, _ := r.(string)
				if _, present := obj[name]; !present {
					return fmt.Errorf("%s.%s is required", path, name)
				}
			}
		}
		additional, hasAdditional := schema["additionalProperties"]
		for key, val := range obj {
			propSchema, known := props[key]
			if !known {
				if hasAdditional {
					if b, isBool := additional.(bool); isBool && !b {
						return fmt.Errorf("%s.%s is not an allowed field", path, key)
					}
					if sub, isMap := additional.(map[string]any); isMap {
						if err := validateValue(sub, val, path+"."+key); err != nil {
							return err
						}
					}
				}
				continue
			}
			if sub, isMap := propSchema.(map[string]any); isMap {
				if err := validateValue(sub, val, path+"."+key); err != nil {
					return err
				}
			}
		}
	case "string":
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("%s must be a string", path)
		}
		if ml, ok := num(schema["maxLength"]); ok && float64(len(s)) > ml {
			return fmt.Errorf("%s is too long", path)
		}
		if ml, ok := num(schema["minLength"]); ok && float64(len(s)) < ml {
			return fmt.Errorf("%s is too short", path)
		}
	case "number":
		n, ok := v.(float64)
		if !ok {
			return fmt.Errorf("%s must be a number", path)
		}
		return checkRange(schema, n, path)
	case "integer":
		n, ok := v.(float64)
		if !ok || math.Trunc(n) != n {
			return fmt.Errorf("%s must be an integer", path)
		}
		return checkRange(schema, n, path)
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", path)
		}
	case "array":
		arr, ok := v.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", path)
		}
		if mx, ok := num(schema["maxItems"]); ok && float64(len(arr)) > mx {
			return fmt.Errorf("%s has too many items", path)
		}
		if sub, isMap := schema["items"].(map[string]any); isMap {
			for i, item := range arr {
				if err := validateValue(sub, item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
	case "null":
		if v != nil {
			return fmt.Errorf("%s must be null", path)
		}
	}
	return nil
}

func num(v any) (float64, bool) {
	n, ok := v.(float64)
	return n, ok
}

func checkRange(schema map[string]any, n float64, path string) error {
	if mn, ok := num(schema["minimum"]); ok && n < mn {
		return fmt.Errorf("%s is below its minimum", path)
	}
	if mx, ok := num(schema["maximum"]); ok && n > mx {
		return fmt.Errorf("%s is above its maximum", path)
	}
	return nil
}
