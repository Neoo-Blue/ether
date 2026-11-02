package viewspec

func (v *validator) cell(raw any, path string) {
	o, ok := asObj(v, raw, path, "cell")
	if !ok {
		return
	}
	kind, _ := o["kind"].(string)
	switch kind {
	case "text", "money", "number", "date", "badge", "link", "record-link":
		v.leafCell(o, path, kind)
	case "drill", "txn":
		v.strict(o, path, nil, []string{"kind", "target", "inner"})
		v.fieldRef(o["target"], join(path, "target"))
		// Inner must be a leaf: drills cannot nest.
		if inner, ok := asObj(v, o["inner"], join(path, "inner"), "inner cell"); ok {
			innerKind, _ := inner["kind"].(string)
			switch innerKind {
			case "text", "money", "number", "date", "badge", "link", "record-link":
				v.leafCell(inner, join(path, "inner"), innerKind)
			default:
				v.err(join(path, "inner"), "inner cell must be a leaf cell")
			}
		}
	case "widget":
		v.strict(o, path, []string{"props"}, []string{"kind", "widget"})
		v.widgetName(o["widget"], join(path, "widget"))
		v.optProps(o, "props", path)
		v.widgetProps(o, path, false)
	default:
		if _, present := o["kind"]; !present {
			v.err(join(path, "kind"), "is required")
		} else {
			v.err(join(path, "kind"), "unknown cell kind %q", kind)
		}
	}
}

func (v *validator) leafCell(o obj, path, kind string) {
	switch kind {
	case "text":
		v.strict(o, path, []string{"className", "fallback", "tone", "numeric", "fallbackClassName", "prefix", "suffix"}, []string{"kind", "field"})
		v.fieldRef(o["field"], join(path, "field"))
		v.optString(o, "className", path, 200)
		v.optValue(o, "fallback", path)
		if raw, present := o["tone"]; present {
			v.tone(raw, join(path, "tone"), tones...)
		}
		v.optBool(o, "numeric", path)
		v.optString(o, "fallbackClassName", path, 200)
		for _, key := range []string{"prefix", "suffix"} {
			if raw, present := o[key]; present {
				if po, ok := asObj(v, raw, join(path, key), key); ok {
					v.strict(po, join(path, key), []string{"className"}, []string{"field"})
					v.fieldRef(po["field"], join(join(path, key), "field"))
					v.optString(po, "className", join(path, key), 200)
				}
			}
		}
	case "money", "number":
		v.strict(o, path, []string{"tone"}, []string{"kind", "field"})
		v.fieldRef(o["field"], join(path, "field"))
		if raw, present := o["tone"]; present {
			v.tone(raw, join(path, "tone"), tones...)
		}
	case "date":
		v.strict(o, path, nil, []string{"kind", "field"})
		v.fieldRef(o["field"], join(path, "field"))
	case "badge":
		v.strict(o, path, []string{"variant"}, []string{"kind", "field"})
		v.fieldRef(o["field"], join(path, "field"))
		if raw, present := o["variant"]; present {
			if isFieldRef(raw) {
				v.fieldRef(raw, join(path, "variant"))
			} else {
				v.enum(raw, join(path, "variant"), "variant",
					"default", "secondary", "outline", "destructive", "warning", "success")
			}
		}
	case "link":
		v.strict(o, path, []string{"className"}, []string{"kind", "field", "href"})
		v.fieldRef(o["field"], join(path, "field"))
		v.fieldRef(o["href"], join(path, "href"))
		v.optString(o, "className", path, 200)
	case "record-link":
		v.strict(o, path, nil, []string{"kind", "field", "recordType", "id"})
		v.fieldRef(o["field"], join(path, "field"))
		v.value(o["recordType"], join(path, "recordType"), "recordType")
		v.fieldRef(o["id"], join(path, "id"))
	}
}
