package viewspec

var tones = []string{"default", "negative", "positive", "warning", "muted", "strong"}

func (v *validator) block(raw any, path string) {
	o, ok := asObj(v, raw, path, "block")
	if !ok {
		return
	}
	kindRaw, present := o["kind"]
	if !present {
		v.err(join(path, "kind"), "is required")
		return
	}
	kind, ok := kindRaw.(string)
	if !ok {
		v.err(join(path, "kind"), "block kind must be a string")
		return
	}
	switch kind {
	case "page-header":
		v.blockStrict(o, path, []string{"description", "back", "actions", "actionsClassName"}, []string{"kind", "title"})
		v.optFieldRef(o, "when", path)
		v.value(o["title"], join(path, "title"), "title")
		v.optValue(o, "description", path)
		if back, present := o["back"]; present {
			if bo, ok := asObj(v, back, join(path, "back"), "back"); ok {
				v.strict(bo, join(path, "back"), nil, []string{"href", "label"})
				v.value(bo["href"], join(join(path, "back"), "href"), "href")
				v.value(bo["label"], join(join(path, "back"), "label"), "label")
			}
		}
		v.widgetRefs(o, path)
		v.optString(o, "actionsClassName", path, 300)
	case "filter-bar":
		v.filterBar(o, path)
	case "summary-line":
		v.blockStrict(o, path, nil, []string{"kind", "label", "value"})
		v.optFieldRef(o, "when", path)
		v.value(o["label"], join(path, "label"), "label")
		v.cell(o["value"], join(path, "value"))
	case "paper":
		v.blockStrict(o, path, []string{"company", "periodPhrase", "note", "wide"}, []string{"kind", "title", "blocks"})
		v.optFieldRef(o, "when", path)
		v.optValue(o, "company", path)
		v.value(o["title"], join(path, "title"), "title")
		v.optValue(o, "periodPhrase", path)
		v.optValue(o, "note", path)
		if raw, present := o["wide"]; present {
			v.boolValue(raw, join(path, "wide"), "wide")
		}
		v.childBlocks(o, path, true)
	case "table":
		v.table(o, path)
	case "pagination":
		v.blockStrict(o, path, []string{"bare", "pageParamKey"}, []string{"kind", "basePath", "total", "page", "perPage"})
		v.optFieldRef(o, "when", path)
		v.value(o["basePath"], join(path, "basePath"), "basePath")
		v.fieldRef(o["total"], join(path, "total"))
		v.fieldRef(o["page"], join(path, "page"))
		v.fieldRef(o["perPage"], join(path, "perPage"))
		v.optBool(o, "bare", path)
		v.optString(o, "pageParamKey", path, 60)
	case "text":
		// The TypeScript text schema also allows "size"; the published 0.2.0
		// schema omits it, so stay closed against it.
		v.blockStrict(o, path, []string{"tone", "className"}, []string{"kind", "content"})
		v.value(o["content"], join(path, "content"), "content")
		if raw, present := o["tone"]; present {
			v.tone(raw, join(path, "tone"), tones...)
		}
		v.optString(o, "className", path, 300)
		v.optFieldRef(o, "when", path)
	case "widget":
		v.blockStrict(o, path, []string{"props"}, []string{"kind", "widget"})
		v.widgetName(o["widget"], join(path, "widget"))
		v.optProps(o, "props", path)
		v.optFieldRef(o, "when", path)
		v.widgetProps(o, path, false)
	case "grid":
		v.blockStrict(o, path, []string{"className", "as"}, []string{"kind", "blocks"})
		v.optFieldRef(o, "when", path)
		v.optString(o, "className", path, 300)
		if raw, present := o["as"]; present {
			v.enum(raw, join(path, "as"), "as", "div", "section")
		}
		v.childBlocks(o, path, true)
	case "heading":
		v.blockStrict(o, path, []string{"className"}, []string{"kind", "level", "content"})
		v.optFieldRef(o, "when", path)
		if raw, present := o["level"]; present {
			if n, ok := num(raw); !ok {
				v.err(join(path, "level"), "level must be 2 or 3")
			} else if i, err := n.Int64(); err != nil || (i != 2 && i != 3) {
				v.err(join(path, "level"), "level must be 2 or 3")
			}
		}
		v.value(o["content"], join(path, "content"), "content")
		v.optString(o, "className", path, 300)
	case "panel":
		v.blockStrict(o, path, []string{"iconKey", "hint", "className", "bodyClassName"}, []string{"kind", "title", "blocks"})
		v.optFieldRef(o, "when", path)
		v.value(o["title"], join(path, "title"), "title")
		v.optValue(o, "iconKey", path)
		v.optValue(o, "hint", path)
		v.optString(o, "className", path, 300)
		v.optString(o, "bodyClassName", path, 300)
		v.childBlocks(o, path, true)
	case "stat-tile":
		v.blockStrict(o, path, []string{"sub", "tone"}, []string{"kind", "iconKey", "accent", "label", "value"})
		v.value(o["iconKey"], join(path, "iconKey"), "iconKey")
		v.value(o["accent"], join(path, "accent"), "accent")
		v.value(o["label"], join(path, "label"), "label")
		v.value(o["value"], join(path, "value"), "value")
		v.optValue(o, "sub", path)
		if raw, present := o["tone"]; present {
			v.tone(raw, join(path, "tone"), "default", "positive", "warning", "negative")
		}
		v.optFieldRef(o, "when", path)
	case "repeat":
		v.blockStrict(o, path, []string{"className", "itemClassName", "unwrapped", "empty"}, []string{"kind", "items", "itemKey", "blocks"})
		v.optFieldRef(o, "when", path)
		v.fieldRef(o["items"], join(path, "items"))
		v.fieldRef(o["itemKey"], join(path, "itemKey"))
		v.optString(o, "className", path, 300)
		v.optString(o, "itemClassName", path, 300)
		v.optBool(o, "unwrapped", path)
		if empty, present := o["empty"]; present {
			if eo, ok := asObj(v, empty, join(path, "empty"), "empty"); ok {
				v.strict(eo, join(path, "empty"), []string{"className"}, []string{"text"})
				v.value(eo["text"], join(join(path, "empty"), "text"), "text")
				v.optString(eo, "className", join(path, "empty"), 300)
			}
		}
		v.childBlocks(o, path, true)
	case "frame":
		v.blockStrict(o, path, []string{"props"}, []string{"kind", "frame", "blocks"})
		v.optFieldRef(o, "when", path)
		v.frameName(o["frame"], join(path, "frame"))
		v.optProps(o, "props", path)
		v.frameProps(o, path)
		v.childBlocks(o, path, true)
	default:
		v.err(join(path, "kind"), "unknown block kind %q", kind)
	}
	// "when" is shared by most blocks through BlockCommon; widget and
	// stat-tile carry their own. It is validated per case above where the
	// TypeScript schema allows it.
	if _, hasWhen := o["when"]; hasWhen {
		switch kind {
		case "page-header", "filter-bar", "summary-line", "paper", "table",
			"pagination", "text", "widget", "grid", "heading", "panel",
			"stat-tile", "repeat", "frame":
			// Already handled (or harmless to re-check the shape once).
		}
	}
}

var filterControls = []string{
	"search", "period", "dateRange", "asOf", "customer", "breakout",
	"compare", "basis", "dimensions", "subsidiary", "showZero", "scale",
	"sections",
}

func (v *validator) filterBar(o obj, path string) {
	v.blockStrict(o, path,
		[]string{"searchPlaceholder", "leading", "actions", "dimensions",
			"subsidiaries", "customers", "dateRange", "primaryFilter",
			"periodPresets", "extraPeriods", "extraPeriodsLabel", "defaultPeriod"},
		[]string{"kind", "controls"})
	v.optFieldRef(o, "when", path)
	if controls, present := o["controls"]; present {
		if co, ok := asObj(v, controls, join(path, "controls"), "controls"); ok {
			v.strict(co, join(path, "controls"), append([]string(nil), filterControls...), nil)
			for _, name := range filterControls {
				if raw, present := co[name]; present {
					v.boolean(raw, join(join(path, "controls"), name), name)
					if v.plugin && name != "search" {
						if on, isBool := raw.(bool); isBool && on {
							v.err(join(join(path, "controls"), name),
								"filter-bar control %q is not available in plugin documents", name)
						}
					}
				}
			}
		}
	}
	v.optValue(o, "searchPlaceholder", path)
	if leading, present := o["leading"]; present {
		if lo, ok := asObj(v, leading, join(path, "leading"), "leading"); ok {
			v.strict(lo, join(path, "leading"), []string{"divider"}, []string{"kind", "links"})
			if k, present := lo["kind"]; present {
				v.enum(k, join(join(path, "leading"), "kind"), "kind", "toggle-links")
			}
			if links, present := lo["links"]; present {
				if arr, ok := v.array(links, join(join(path, "leading"), "links"), "links", 0, 8); ok {
					for i, raw := range arr {
						lp := index(join(join(path, "leading"), "links"), i)
						if link, ok := asObj(v, raw, lp, "link"); ok {
							v.strict(link, lp, nil, []string{"href", "label", "activeWhen"})
							v.value(link["href"], join(lp, "href"), "href")
							v.value(link["label"], join(lp, "label"), "label")
							v.fieldRef(link["activeWhen"], join(lp, "activeWhen"))
						}
					}
				}
			}
			v.optBool(lo, "divider", join(path, "leading"))
		}
	}
	v.widgetRefs(o, path)
	// Loader-bound accounting inputs. Plugin documents may not bind them:
	// Ether's filter bar only searches.
	accounting := []string{"dimensions", "subsidiaries", "customers", "dateRange",
		"primaryFilter", "periodPresets", "extraPeriods"}
	for _, key := range accounting {
		if _, present := o[key]; present {
			if v.plugin {
				v.err(join(path, key), "%s is not available in plugin documents", key)
			} else {
				v.fieldRef(o[key], join(path, key))
			}
		}
	}
	if v.plugin {
		if _, present := o["extraPeriodsLabel"]; present {
			v.err(join(path, "extraPeriodsLabel"), "extraPeriodsLabel is not available in plugin documents")
		}
		if _, present := o["defaultPeriod"]; present {
			v.err(join(path, "defaultPeriod"), "defaultPeriod is not available in plugin documents")
		}
	} else {
		v.optValue(o, "extraPeriodsLabel", path)
		v.optValue(o, "defaultPeriod", path)
	}
}

func (v *validator) widgetRefs(o obj, path string) {
	raw, present := o["actions"]
	if !present {
		return
	}
	arr, ok := v.array(raw, join(path, "actions"), "actions", 0, 12)
	if !ok {
		return
	}
	for i, item := range arr {
		ip := index(join(path, "actions"), i)
		wo, ok := asObj(v, item, ip, "widget reference")
		if !ok {
			continue
		}
		v.blockStrict(wo, ip, []string{"props"}, []string{"widget"})
		v.widgetName(wo["widget"], join(ip, "widget"))
		v.optProps(wo, "props", ip)
		v.optFieldRef(wo, "when", ip)
		v.widgetProps(wo, ip, false)
	}
}

func (v *validator) widgetName(raw any, path string) {
	name, ok := v.str(raw, path, "widget", 64)
	if !ok {
		return
	}
	if v.plugin {
		if !IsEtherWidget(name) {
			v.err(path, "unknown Ether widget %q", name)
		}
		return
	}
	if name == "" || !widgetSlug.MatchString(name) {
		v.err(path, "widget must be a slug")
	}
}

func (v *validator) frameName(raw any, path string) {
	name, ok := v.str(raw, path, "frame", 64)
	if !ok {
		return
	}
	if v.plugin {
		if !IsEtherFrame(name) {
			v.err(path, "unknown Ether frame %q", name)
		}
		return
	}
	if name == "" || !widgetSlug.MatchString(name) {
		v.err(path, "frame must be a slug")
	}
}

func jsonKind(value any) string {
	switch value.(type) {
	case string:
		return "string"
	case bool:
		return "bool"
	case []any:
		return "array"
	case obj:
		return "object"
	default:
		return "number"
	}
}

// checkProps validates literal props against a shape in plugin mode. Field
// references pass: their resolved type is the loader's business and the
// renderer checks it again after resolution.
func (v *validator) checkProps(shape propShape, props obj, path string) {
	allowed := append(append([]string(nil), shape.required...), shape.optional...)
	seen := map[string]bool{}
	for name, value := range props {
		seen[name] = true
		if !inSet(allowed, name) {
			v.err(join(path, name), "unknown prop")
			continue
		}
		if isFieldRef(value) {
			v.fieldRef(value, join(path, name))
			continue
		}
		if kinds, known := shape.literals[name]; known {
			if !inSet(kinds, jsonKind(value)) {
				v.err(join(path, name), "prop %q must be %v", name, kinds)
			}
		}
	}
	for _, name := range shape.required {
		if !seen[name] {
			v.err(join(path, name), "prop is required")
		}
	}
	for _, group := range shape.oneOf {
		anySeen := false
		for _, name := range group {
			if seen[name] {
				anySeen = true
			}
		}
		if !anySeen {
			v.err(path, "one of %v is required", group)
		}
	}
}

func (v *validator) widgetProps(o obj, path string, _ bool) {
	if !v.plugin {
		return
	}
	name, _ := o["widget"].(string)
	shape, known := widgetShapes[name]
	if !known {
		return
	}
	props, _ := o["props"].(obj)
	if props == nil {
		props = obj{}
	}
	v.checkProps(shape, props, join(path, "props"))
}

func (v *validator) frameProps(o obj, path string) {
	if !v.plugin {
		return
	}
	name, _ := o["frame"].(string)
	shape, known := frameShapes[name]
	if !known {
		return
	}
	props, _ := o["props"].(obj)
	if props == nil {
		props = obj{}
	}
	v.checkProps(shape, props, join(path, "props"))
}

func (v *validator) table(o obj, path string) {
	v.blockStrict(o, path,
		[]string{"variant", "sorting", "leading", "trailing", "empty", "emptyRow"},
		[]string{"kind", "rows", "rowKey", "columns"})
	v.optFieldRef(o, "when", path)
	if raw, present := o["variant"]; present {
		if v.plugin {
			v.enum(raw, join(path, "variant"), "variant", "app")
		} else {
			v.enum(raw, join(path, "variant"), "variant", "report", "app")
		}
	}
	if sorting, present := o["sorting"]; present {
		if so, ok := asObj(v, sorting, join(path, "sorting"), "sorting"); ok {
			sp := join(path, "sorting")
			v.strict(so, sp, []string{"sortParamKey", "dirParamKey", "pageParamKey"}, []string{"basePath", "sort", "dir"})
			v.value(so["basePath"], join(sp, "basePath"), "basePath")
			v.fieldRef(so["sort"], join(sp, "sort"))
			v.fieldRef(so["dir"], join(sp, "dir"))
			v.optString(so, "sortParamKey", sp, 60)
			v.optString(so, "dirParamKey", sp, 60)
			v.optString(so, "pageParamKey", sp, 60)
		}
	}
	for _, key := range []string{"leading", "trailing"} {
		if raw, present := o[key]; present {
			if arr, ok := v.array(raw, join(path, key), key, 0, 10); ok {
				for i, item := range arr {
					v.spanRow(item, index(join(path, key), i))
				}
			}
		}
	}
	v.fieldRef(o["rows"], join(path, "rows"))
	v.fieldRef(o["rowKey"], join(path, "rowKey"))
	if cols, present := o["columns"]; present {
		if arr, ok := v.array(cols, join(path, "columns"), "columns", 1, 60); ok {
			for i, item := range arr {
				cp := index(join(path, "columns"), i)
				co, ok := asObj(v, item, cp, "column")
				if !ok {
					continue
				}
				v.strict(co, cp, []string{"align", "sort", "className", "headerClassName", "srOnlyHeader"}, []string{"header", "cell"})
				v.value(co["header"], join(cp, "header"), "header")
				if raw, present := co["align"]; present {
					v.enum(raw, join(cp, "align"), "align", "left", "right", "center")
				}
				v.cell(co["cell"], join(cp, "cell"))
				v.optString(co, "sort", cp, 60)
				v.optString(co, "className", cp, 200)
				v.optString(co, "headerClassName", cp, 200)
				v.optBool(co, "srOnlyHeader", cp)
			}
		}
	}
	if empty, present := o["empty"]; present {
		if eo, ok := asObj(v, empty, join(path, "empty"), "empty"); ok {
			v.strict(eo, join(path, "empty"), []string{"description"}, []string{"title"})
			v.value(eo["title"], join(join(path, "empty"), "title"), "title")
			v.optValue(eo, "description", join(path, "empty"))
		}
	}
	if emptyRow, present := o["emptyRow"]; present {
		if eo, ok := asObj(v, emptyRow, join(path, "emptyRow"), "emptyRow"); ok {
			ep := join(path, "emptyRow")
			v.strict(eo, ep, []string{"className"}, []string{"text", "colSpan"})
			v.value(eo["text"], join(ep, "text"), "text")
			v.intInRange(eo["colSpan"], join(ep, "colSpan"), "colSpan", 1, 60)
			v.optString(eo, "className", ep, 200)
		}
	}
}

func (v *validator) spanRow(raw any, path string) {
	o, ok := asObj(v, raw, path, "span row")
	if !ok {
		return
	}
	v.strict(o, path, []string{"labelClassName", "className"}, []string{"label", "labelColSpan", "cells"})
	v.value(o["label"], join(path, "label"), "label")
	v.intInRange(o["labelColSpan"], join(path, "labelColSpan"), "labelColSpan", 1, 60)
	v.optString(o, "labelClassName", path, 200)
	v.optString(o, "className", path, 200)
	if cells, present := o["cells"]; present {
		if arr, ok := v.array(cells, join(path, "cells"), "cells", 0, 20); ok {
			for i, item := range arr {
				cp := index(join(path, "cells"), i)
				co, ok := asObj(v, item, cp, "span cell")
				if !ok {
					continue
				}
				v.strict(co, cp, []string{"align", "className"}, []string{"cell"})
				v.cell(co["cell"], join(cp, "cell"))
				if raw, present := co["align"]; present {
					v.enum(raw, join(cp, "align"), "align", "left", "right", "center")
				}
				v.optString(co, "className", cp, 200)
			}
		}
	}
}
