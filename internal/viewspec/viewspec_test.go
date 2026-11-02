package viewspec

import (
	"strings"
	"testing"
)

// Mirrors schema.test.ts in @braedonsaunders/appkit-viewspec: every
// advertised kind validates, the unions stay closed, and poisoned paths are
// refused. Plugin-mode cases add the Ether rules.

func specWith(block string) []byte {
	return []byte(`{"specVersion":1,"layout":"list","header":[],"body":[` + block + `]}`)
}

var blockSamples = map[string]string{
	"page-header":  `{"kind":"page-header","title":"x"}`,
	"filter-bar":   `{"kind":"filter-bar","controls":{"period":true}}`,
	"summary-line": `{"kind":"summary-line","label":"x","value":{"kind":"text","field":{"$":"a"}}}`,
	"paper":        `{"kind":"paper","title":"x","blocks":[]}`,
	"table":        `{"kind":"table","rows":{"$":"rows"},"rowKey":{"$":"id"},"columns":[{"header":"x","cell":{"kind":"text","field":{"$":"a"}}}]}`,
	"pagination":   `{"kind":"pagination","basePath":"/x","total":{"$":"t"},"page":{"$":"p"},"perPage":{"$":"n"}}`,
	"text":         `{"kind":"text","content":"x"}`,
	"widget":       `{"kind":"widget","widget":"some-widget"}`,
	"grid":         `{"kind":"grid","blocks":[]}`,
	"heading":      `{"kind":"heading","level":2,"content":"x"}`,
	"panel":        `{"kind":"panel","title":"x","blocks":[]}`,
	"stat-tile":    `{"kind":"stat-tile","iconKey":"x","accent":"teal","label":"x","value":"x"}`,
	"repeat":       `{"kind":"repeat","items":{"$":"rows"},"itemKey":{"$":"id"},"blocks":[]}`,
	"frame":        `{"kind":"frame","frame":"card","blocks":[]}`,
}

var cellSamples = map[string]string{
	"text":        `{"kind":"text","field":{"$":"a"}}`,
	"money":       `{"kind":"money","field":{"$":"a"}}`,
	"number":      `{"kind":"number","field":{"$":"a"}}`,
	"date":        `{"kind":"date","field":{"$":"a"}}`,
	"badge":       `{"kind":"badge","field":{"$":"a"}}`,
	"link":        `{"kind":"link","field":{"$":"a"},"href":{"$":"h"}}`,
	"record-link": `{"kind":"record-link","field":{"$":"a"},"recordType":"party","id":{"$":"i"}}`,
	"drill":       `{"kind":"drill","target":{"$":"d"},"inner":{"kind":"text","field":{"$":"a"}}}`,
	"txn":         `{"kind":"txn","target":{"$":"d"},"inner":{"kind":"text","field":{"$":"a"}}}`,
	"widget":      `{"kind":"widget","widget":"some-cell"}`,
}

func TestEveryAdvertisedBlockKindValidates(t *testing.T) {
	for kind, sample := range blockSamples {
		if got := Validate(specWith(sample)); !got.OK {
			t.Errorf("block kind %q rejected: %v", kind, got.Errors)
		}
	}
}

func TestEveryAdvertisedCellKindValidates(t *testing.T) {
	for kind, cell := range cellSamples {
		block := `{"kind":"table","rows":{"$":"rows"},"rowKey":{"$":"id"},"columns":[{"header":"x","cell":` + cell + `}]}`
		if got := Validate(specWith(block)); !got.OK {
			t.Errorf("cell kind %q rejected: %v", kind, got.Errors)
		}
	}
}

func TestClosedUnions(t *testing.T) {
	cases := []struct {
		name string
		doc  []byte
	}{
		{"unknown block", specWith(`{"kind":"script","src":"evil.js"}`)},
		{"stray property", specWith(`{"kind":"heading","level":2,"content":"x","onClick":"x"}`)},
		{"wrong version", []byte(`{"specVersion":99,"layout":"list","header":[],"body":[]}`)},
		{"unknown cell", specWith(`{"kind":"table","rows":{"$":"r"},"rowKey":{"$":"id"},"columns":[{"header":"x","cell":{"kind":"html","field":{"$":"a"}}}]}`)},
		{"nested drill", specWith(`{"kind":"table","rows":{"$":"r"},"rowKey":{"$":"id"},"columns":[{"header":"x","cell":{"kind":"drill","target":{"$":"d"},"inner":{"kind":"drill","target":{"$":"d"},"inner":{"kind":"text","field":{"$":"a"}}}}}]}`)},
		{"invalid json", []byte(`{"specVersion":`)},
		{"not an object", []byte(`[1,2]`)},
		{"bad layout", []byte(`{"specVersion":1,"layout":"wide","header":[],"body":[]}`)},
		{"bad route", []byte(`{"specVersion":1,"layout":"list","route":"items/42","header":[],"body":[]}`)},
		{"too many columns", specWith(`{"kind":"table","rows":{"$":"r"},"rowKey":{"$":"id"},"columns":[` + strings.Repeat(`{"header":"x","cell":{"kind":"text","field":{"$":"a"}}},`, 60) + `{"header":"x","cell":{"kind":"text","field":{"$":"a"}}}]}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Validate(tc.doc); got.OK {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestDepthCap(t *testing.T) {
	block := `{"kind":"grid","blocks":[]}`
	for i := 0; i < 12; i++ {
		block = `{"kind":"grid","blocks":[` + block + `]}`
	}
	got := Validate(specWith(block))
	if got.OK {
		t.Fatal("over-deep spec must be rejected")
	}
	if !strings.Contains(strings.Join(got.Errors, ";"), "maximum depth") {
		t.Fatalf("expected depth error, got %v", got.Errors)
	}
	// At the cap is fine.
	block = `{"kind":"grid","blocks":[]}`
	for i := 0; i < 4; i++ {
		block = `{"kind":"grid","blocks":[` + block + `]}`
	}
	if got := Validate(specWith(block)); !got.OK {
		t.Fatalf("spec within depth rejected: %v", got.Errors)
	}
}

func TestPrototypePathsRefused(t *testing.T) {
	for _, path := range []string{"__proto__", "a.__proto__.b", "constructor", "x.prototype", ""} {
		doc := specWith(`{"kind":"text","content":{"$":"` + path + `"}}`)
		if got := Validate(doc); got.OK {
			t.Errorf("path %q must be refused", path)
		}
	}
	fine := specWith(`{"kind":"text","content":{"$":"invoice.customer.name"}}`)
	if got := Validate(fine); !got.OK {
		t.Fatalf("ordinary path rejected: %v", got.Errors)
	}
	rooted := specWith(`{"kind":"text","content":{"$":"$root.title"}}`)
	if got := Validate(rooted); !got.OK {
		t.Fatalf("$root path rejected: %v", got.Errors)
	}
}

func TestPluginRejectsClassFields(t *testing.T) {
	cases := []string{
		`{"kind":"text","content":"x","className":"text-red"}`,
		`{"kind":"panel","title":"x","bodyClassName":"p-0","blocks":[]}`,
		`{"kind":"repeat","items":{"$":"r"},"itemKey":{"$":"id"},"itemClassName":"x","blocks":[]}`,
		`{"kind":"page-header","title":"x","actionsClassName":"flex"}`,
		`{"kind":"table","rows":{"$":"r"},"rowKey":{"$":"id"},"columns":[{"header":"x","headerClassName":"w-40","cell":{"kind":"text","field":{"$":"a"}}}]}`,
		`{"kind":"table","rows":{"$":"r"},"rowKey":{"$":"id"},"columns":[{"header":"x","cell":{"kind":"text","field":{"$":"a"},"className":"mono"}}]}`,
		`{"kind":"table","rows":{"$":"r"},"rowKey":{"$":"id"},"columns":[{"header":"x","cell":{"kind":"text","field":{"$":"a"},"fallbackClassName":"dim"}}]}`,
		`{"kind":"grid","className":"grid-cols-3","blocks":[]}`,
	}
	for _, block := range cases {
		doc := specWith(block)
		if got := ValidatePlugin(doc); got.OK {
			t.Errorf("plugin doc with class field accepted: %s", block)
		}
		if got := Validate(doc); !got.OK {
			t.Errorf("built-in doc with class field rejected (%s): %v", block, got.Errors)
		}
	}
	// Page-level bodyClassName is also a class field.
	doc := []byte(`{"specVersion":1,"layout":"list","bodyClassName":"flex","header":[],"body":[]}`)
	if got := ValidatePlugin(doc); got.OK {
		t.Error("plugin doc with page bodyClassName accepted")
	}
}

func TestPluginWidgetAndFrameAllowlist(t *testing.T) {
	good := []string{
		`{"kind":"widget","widget":"ether.badge","props":{"label":"New"}}`,
		`{"kind":"widget","widget":"ether.posterGrid","props":{"items":{"$":"titles"}}}`,
		`{"kind":"frame","frame":"ether.section","props":{"title":"x"},"blocks":[]}`,
		`{"kind":"frame","frame":"ether.tabs","props":{"tabs":[{"key":"a","label":"A"}]},"blocks":[]}`,
	}
	for _, block := range good {
		if got := ValidatePlugin(specWith(block)); !got.OK {
			t.Errorf("Ether widget rejected (%s): %v", block, got.Errors)
		}
	}
	bad := []string{
		`{"kind":"widget","widget":"some-widget"}`,
		`{"kind":"widget","widget":"ether.unknown","props":{}}`,
		`{"kind":"frame","frame":"card","blocks":[]}`,
		`{"kind":"widget","widget":"ether.badge","props":{"bogus":1}}`,
		`{"kind":"widget","widget":"ether.badge","props":{}}`,
		`{"kind":"widget","widget":"ether.titleCard","props":{"reason":"x"}}`,
		`{"kind":"widget","widget":"ether.actionButton","props":{"label":"Go"}}`,
		`{"kind":"widget","widget":"ether.posterGrid","props":{"items":"not-an-array"}}`,
	}
	for _, block := range bad {
		if got := ValidatePlugin(specWith(block)); got.OK {
			t.Errorf("invalid widget use accepted: %s", block)
		}
	}
}

func TestPluginFilterBarAndTableRules(t *testing.T) {
	if got := ValidatePlugin(specWith(`{"kind":"filter-bar","controls":{"search":true}}`)); !got.OK {
		t.Fatalf("search-only filter bar rejected: %v", got.Errors)
	}
	for _, block := range []string{
		`{"kind":"filter-bar","controls":{"period":true}}`,
		`{"kind":"filter-bar","controls":{"search":true},"customers":{"$":"c"}}`,
		`{"kind":"filter-bar","controls":{"search":true},"defaultPeriod":"month"}`,
		`{"kind":"table","variant":"report","rows":{"$":"r"},"rowKey":{"$":"id"},"columns":[{"header":"x","cell":{"kind":"text","field":{"$":"a"}}}]}`,
	} {
		if got := ValidatePlugin(specWith(block)); got.OK {
			t.Errorf("plugin rule not enforced: %s", block)
		}
	}
	// A switched-off accounting control is harmless and allowed.
	if got := ValidatePlugin(specWith(`{"kind":"filter-bar","controls":{"period":false}}`)); !got.OK {
		t.Fatalf("disabled control rejected: %v", got.Errors)
	}
}

func TestWhenFlagAllowedOnBlocks(t *testing.T) {
	for _, block := range []string{
		`{"kind":"text","content":"x","when":{"$":"show"}}`,
		`{"kind":"heading","level":3,"content":"x","when":{"$":"show"}}`,
		`{"kind":"widget","widget":"ether.badge","props":{"label":"x"},"when":{"$":"show"}}`,
		`{"kind":"page-header","title":"x","actions":[{"widget":"ether.badge","props":{"label":"x"},"when":{"$":"show"}}]}`,
	} {
		if got := ValidatePlugin(specWith(block)); !got.OK {
			t.Errorf("when flag rejected (%s): %v", block, got.Errors)
		}
	}
	// Cells carry no when flag.
	bad := `{"kind":"table","rows":{"$":"r"},"rowKey":{"$":"id"},"columns":[{"header":"x","cell":{"kind":"text","field":{"$":"a"},"when":{"$":"s"}}}]}`
	if got := ValidatePlugin(specWith(bad)); got.OK {
		t.Error("cell with when flag accepted")
	}
}

func TestResolvePath(t *testing.T) {
	scope := map[string]any{"a": map[string]any{"b": "x"}}
	got, err := ResolvePath(scope, "a.b")
	if err != nil || got != "x" {
		t.Fatalf("ResolvePath(a.b) = %v, %v", got, err)
	}
	got, err = ResolvePath(scope, "a.missing.deep")
	if err != nil || got != nil {
		t.Fatalf("missing path = %v, %v", got, err)
	}
	for _, path := range []string{"__proto__", "a.constructor"} {
		if _, err := ResolvePath(scope, path); err == nil {
			t.Errorf("ResolvePath(%q) must fail", path)
		}
	}
	// Plugin validation rejects the same paths before they can be stored.
	if got := ValidatePlugin(specWith(`{"kind":"text","content":{"$":"a.__proto__"}}`)); got.OK {
		t.Error("plugin doc with prototype path accepted")
	}
}
