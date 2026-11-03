package viewspec

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// validator walks a decoded document collecting every problem, mirroring the
// strict-object zod schemas in the TypeScript package: unknown properties
// are rejections, and every kind has a fixed field set.
type validator struct {
	plugin bool
	errors []string
}

func (v *validator) err(path, format string, args ...any) {
	where := path
	if where == "" {
		where = "(root)"
	}
	v.errors = append(v.errors, where+": "+fmt.Sprintf(format, args...))
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func index(path string, i int) string { return fmt.Sprintf("%s.%d", path, i) }

type obj = map[string]any

func asObj(v *validator, value any, path, what string) (obj, bool) {
	o, ok := value.(obj)
	if !ok {
		v.err(path, "%s must be an object", what)
	}
	return o, ok
}

// strict rejects keys outside the allowed set and reports missing required
// keys. It mirrors z.strictObject.
func (v *validator) strict(o obj, path string, allowed, required []string) {
	set := make(map[string]bool, len(allowed))
	for _, k := range allowed {
		set[k] = true
	}
	for _, k := range required {
		set[k] = true
	}
	keys := make([]string, 0, len(o))
	for k := range o {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !set[k] {
			v.err(join(path, k), "unknown property")
		}
	}
	for _, k := range required {
		if _, present := o[k]; !present {
			v.err(join(path, k), "is required")
		}
	}
}

// blockStrict is strict for blocks and widget references, which may all
// carry the shared "when" presence flag.
func (v *validator) blockStrict(o obj, path string, allowed, required []string) {
	v.strict(o, path, append(append([]string(nil), allowed...), "when"), required)
}

func (v *validator) str(value any, path, what string, maxLen int) (string, bool) {
	s, ok := value.(string)
	if !ok {
		v.err(path, "%s must be a string", what)
		return "", false
	}
	if maxLen > 0 && len(s) > maxLen {
		v.err(path, "%s is too long (max %d)", what, maxLen)
		return s, false
	}
	return s, true
}

func (v *validator) boolean(value any, path, what string) {
	if _, ok := value.(bool); !ok {
		v.err(path, "%s must be a boolean", what)
	}
}

func num(value any) (json.Number, bool) {
	n, ok := value.(json.Number)
	return n, ok
}

func (v *validator) intInRange(value any, path, what string, min, max int64) {
	n, ok := num(value)
	if !ok {
		v.err(path, "%s must be a number", what)
		return
	}
	i, err := n.Int64()
	if err != nil {
		v.err(path, "%s must be an integer", what)
		return
	}
	if i < min || i > max {
		v.err(path, "%s must be between %d and %d", what, min, max)
	}
}

func (v *validator) enum(value any, path, what string, options ...string) {
	s, ok := value.(string)
	if !ok {
		v.err(path, "%s must be one of %s", what, strings.Join(options, ", "))
		return
	}
	for _, o := range options {
		if s == o {
			return
		}
	}
	v.err(path, "%s must be one of %s", what, strings.Join(options, ", "))
}

// fieldRef validates a {$: "path"} reference, including the path guards.
func (v *validator) fieldRef(value any, path string) {
	o, ok := asObj(v, value, path, "field reference")
	if !ok {
		return
	}
	v.strict(o, path, nil, []string{"$"})
	raw, present := o["$"]
	if !present {
		return
	}
	s, ok := v.str(raw, join(path, "$"), "field path", 200)
	if ok && !validFieldPath(s) {
		if s == "" {
			v.err(join(path, "$"), "field path must not be empty")
		} else {
			v.err(join(path, "$"), "field path may not name __proto__, constructor, or prototype")
		}
	}
}

func isFieldRef(value any) bool {
	o, ok := value.(obj)
	if !ok {
		return false
	}
	_, has := o["$"]
	return has && len(o) == 1
}

// value validates a literal-or-reference string.
func (v *validator) value(value any, path, what string) {
	if _, ok := value.(string); ok {
		return
	}
	if isFieldRef(value) {
		v.fieldRef(value, path)
		return
	}
	v.err(path, "%s must be a string or a field reference", what)
}

func (v *validator) boolValue(value any, path, what string) {
	if _, ok := value.(bool); ok {
		return
	}
	if isFieldRef(value) {
		v.fieldRef(value, path)
		return
	}
	v.err(path, "%s must be a boolean or a field reference", what)
}

func (v *validator) tone(value any, path string, options ...string) {
	if isFieldRef(value) {
		v.fieldRef(value, path)
		return
	}
	v.enum(value, path, "tone", options...)
}

func (v *validator) optFieldRef(o obj, key, path string) {
	if raw, present := o[key]; present {
		v.fieldRef(raw, join(path, key))
	}
}

func (v *validator) optValue(o obj, key, path string) {
	if raw, present := o[key]; present {
		v.value(raw, join(path, key), key)
	}
}

func (v *validator) optString(o obj, key, path string, maxLen int) {
	if raw, present := o[key]; present {
		v.str(raw, join(path, key), key, maxLen)
	}
}

func (v *validator) optBool(o obj, key, path string) {
	if raw, present := o[key]; present {
		v.boolean(raw, join(path, key), key)
	}
}

func (v *validator) array(value any, path, what string, min, max int) ([]any, bool) {
	arr, ok := value.([]any)
	if !ok {
		v.err(path, "%s must be an array", what)
		return nil, false
	}
	if len(arr) < min {
		v.err(path, "%s must have at least %d item(s)", what, min)
	}
	if max > 0 && len(arr) > max {
		v.err(path, "%s must have at most %d item(s)", what, max)
	}
	return arr, true
}

func (v *validator) optProps(o obj, key, path string) {
	if raw, present := o[key]; present {
		if _, ok := raw.(obj); !ok {
			v.err(join(path, key), "props must be an object")
		}
	}
}

// rejectClassFields walks the whole document and refuses every class-name
// field, wherever it hides: blocks, cells, columns, span rows or props.
func (v *validator) rejectClassFields(value any, path string) {
	switch node := value.(type) {
	case obj:
		keys := make([]string, 0, len(node))
		for k := range node {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if classFields[k] {
				v.err(join(path, k), "class-name fields are not allowed in plugin documents")
			}
			v.rejectClassFields(node[k], join(path, k))
		}
	case []any:
		for i, item := range node {
			v.rejectClassFields(item, index(path, i))
		}
	}
}

func (v *validator) page(root obj, path string) {
	v.strict(root, path,
		[]string{"route", "bodyClassName"},
		[]string{"specVersion", "layout", "header", "body"})
	if raw, present := root["route"]; present {
		if s, ok := v.str(raw, join(path, "route"), "route", 120); ok {
			if s == "" || !routeRe.MatchString(s) {
				v.err(join(path, "route"), "route must be an absolute route pattern")
			}
		}
	}
	if raw, present := root["specVersion"]; present {
		if n, ok := num(raw); !ok {
			v.err(join(path, "specVersion"), "specVersion must be %d", SpecVersion)
		} else if i, err := n.Int64(); err != nil || i != SpecVersion {
			v.err(join(path, "specVersion"), "specVersion must be %d", SpecVersion)
		}
	}
	if raw, present := root["layout"]; present {
		v.enum(raw, join(path, "layout"), "layout", "list", "detail", "bare")
	}
	v.optString(root, "bodyClassName", path, 300)
	if raw, present := root["header"]; present {
		if arr, ok := v.array(raw, join(path, "header"), "header", 0, 20); ok {
			v.blocks(arr, join(path, "header"))
		}
	}
	if raw, present := root["body"]; present {
		if arr, ok := v.array(raw, join(path, "body"), "body", 0, 40); ok {
			v.blocks(arr, join(path, "body"))
		}
	}
}

func (v *validator) blocks(arr []any, path string) {
	for i, raw := range arr {
		v.block(raw, index(path, i))
	}
}

func (v *validator) childBlocks(o obj, path string, required bool) {
	raw, present := o["blocks"]
	if !present {
		if required {
			v.err(join(path, "blocks"), "is required")
		}
		return
	}
	if arr, ok := v.array(raw, join(path, "blocks"), "blocks", 0, 40); ok {
		v.blocks(arr, join(path, "blocks"))
	}
}
