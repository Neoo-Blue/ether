package releasepolicy

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/braedonsaunders/ether/internal/core"
)

// registryLookup mirrors what the resolver builds from ruleField
// descriptors: the built-in table plus one registered plugin field.
func registryLookup(lookup map[string]FieldSpec) FieldLookup {
	return func(id string) (FieldSpec, bool) {
		f, ok := lookup[id]
		return f, ok
	}
}

func mergedSpecs(extra ...FieldSpec) map[string]FieldSpec {
	out := map[string]FieldSpec{}
	for _, f := range BuiltinFields() {
		out[f.ID] = f
	}
	for _, f := range extra {
		out[f.ID] = f
	}
	return out
}

// Golden parity (WS1a item 7): compiling the same rules through the
// built-in table and through a registry-shaped lookup must accept and
// reject the same policies and evaluate them identically.
func TestCompileWithMatchesCompile(t *testing.T) {
	rules := []core.ReleaseRule{
		languageRule(),
		{ID: "grp", Name: "Prefer FLUX", Action: "prefer", Conditions: []core.ReleaseCondition{{Field: "group", Operator: "equals", Value: "FLUX"}}},
		{ID: "big", Name: "Big files", Action: "require", Match: "any", Conditions: []core.ReleaseCondition{{Field: "height", Operator: "range", Min: 1080, Max: 2160}, {Field: "size_gib", Operator: "range", Min: 4}}},
		{ID: "orig", Name: "Original audio", Action: "prefer", Conditions: []core.ReleaseCondition{{Field: "audio_matches_original", Operator: "equals"}}},
		{ID: "named", Name: "Named", Action: "avoid", Conditions: []core.ReleaseCondition{{Field: "name", Operator: "regex", Value: "cam|ts"}}},
	}
	rels := []core.Release{
		{Name: "Film.2024.2160p.WEB-DL-FLUX", Size: 20 << 30, Info: core.ReleaseInfo{Height: 2160, Group: "FLUX", Languages: []string{"english"}}},
		{Name: "Film.2024.1080p.CAM-GRP", Size: 2 << 30, Info: core.ReleaseInfo{Height: 1080, Group: "GRP", Languages: []string{"norwegian"}}},
		{Name: "Film.2024.720p.WEB-DL-GRP", Size: 1 << 30, Info: core.ReleaseInfo{Height: 720}},
	}
	base, err := Compile(rules)
	if err != nil {
		t.Fatal(err)
	}
	viaRegistry, err := CompileWith(rules, registryLookup(mergedSpecs()))
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range rels {
		a, _ := json.Marshal(base.Evaluate(rel, Context{OriginalLanguage: "en"}))
		b, _ := json.Marshal(viaRegistry.Evaluate(rel, Context{OriginalLanguage: "en"}))
		if string(a) != string(b) {
			t.Fatalf("evaluate %q differs:\ncompile:      %s\ncompile-with: %s", rel.Name, a, b)
		}
	}

	invalid := [][]core.ReleaseRule{
		{{ID: "x", Name: "X", Action: "prefer", Conditions: []core.ReleaseCondition{{Field: "bogus", Operator: "equals", Value: "v"}}}},
		{{ID: "x", Name: "X", Action: "prefer", Conditions: []core.ReleaseCondition{{Field: "language", Operator: "contains", Value: "en"}}}},
		{{ID: "x", Name: "X", Action: "prefer", Conditions: []core.ReleaseCondition{{Field: "height", Operator: "equals", Value: "1080"}}}},
		{{ID: "x", Name: "X", Action: "prefer", Conditions: []core.ReleaseCondition{{Field: "audio_matches_original", Operator: "equals", Value: "en"}}}},
		{{ID: "x", Name: "X", Action: "prefer", Conditions: []core.ReleaseCondition{{Field: "name", Operator: "regex", Value: "("}}}},
	}
	for _, bad := range invalid {
		_, errA := Compile(bad)
		_, errB := CompileWith(bad, registryLookup(mergedSpecs()))
		if (errA == nil) != (errB == nil) || (errA != nil && errA.Error() != errB.Error()) {
			t.Fatalf("invalid policy errors differ: compile=%v compile-with=%v", errA, errB)
		}
	}
}

// A registered plugin field compiles under its declared shape and stays
// neutral; an unregistered plugin field compiles structurally under
// Compile and is rejected only by a registry lookup that lacks it when
// the lookup is authoritative... CompileWith falls back to the structural
// shape for namespaced ids the lookup does not know, matching Compile.
func TestExternalFieldsCompileAndStayNeutral(t *testing.T) {
	dub := FieldSpec{ID: "plugin:example.dub/lang", ValueClass: "choice", Operators: []string{"equals"}, DefaultOperator: "equals", External: true}
	rules := []core.ReleaseRule{
		{ID: "dub", Name: "Dub", Action: "prefer", Conditions: []core.ReleaseCondition{{Field: dub.ID, Operator: "equals", Value: "fr"}}},
	}
	p, err := CompileWith(rules, registryLookup(mergedSpecs(dub)))
	if err != nil {
		t.Fatalf("registered plugin field should compile: %v", err)
	}
	got := p.Evaluate(core.Release{Name: "Film.FRENCH.1080p"}, Context{})
	if got.Rejected || len(got.Preferences) != 1 || got.Preferences[0] != 0 || got.Explanations[0].State != Unknown {
		t.Fatalf("plugin condition must stay neutral, got %+v", got)
	}
	// A registered field still rejects operators it does not declare.
	bad := []core.ReleaseRule{
		{ID: "dub", Name: "Dub", Action: "prefer", Conditions: []core.ReleaseCondition{{Field: dub.ID, Operator: "regex", Value: "fr"}}},
	}
	if _, err := CompileWith(bad, registryLookup(mergedSpecs(dub))); err == nil {
		t.Fatal("undeclared operator on a registered plugin field must fail")
	}
	// Without any registry, the namespaced field compiles structurally so
	// a saved profile keeps working if the plugin goes away.
	if _, err := Compile(rules); err != nil {
		t.Fatalf("plugin field should compile structurally: %v", err)
	}
	// Namespacing is not a free pass for malformed conditions.
	shapeless := []core.ReleaseRule{
		{ID: "dub", Name: "Dub", Action: "prefer", Conditions: []core.ReleaseCondition{{Field: dub.ID, Operator: "equals", Value: ""}}},
	}
	if _, err := Compile(shapeless); err == nil {
		t.Fatal("valueless plugin condition must fail")
	}
}

func TestBuiltinFieldsCoverCompileSurface(t *testing.T) {
	if len(BuiltinFields()) != 14 {
		t.Fatalf("builtin fields = %d, want 14", len(BuiltinFields()))
	}
	for _, f := range BuiltinFields() {
		if f.External {
			t.Fatalf("built-in field %q marked external", f.ID)
		}
		if !strings.Contains("text choice language number none", f.ValueClass) {
			t.Fatalf("field %q has unknown value class %q", f.ID, f.ValueClass)
		}
	}
}
