package resolver

import (
	"testing"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/extend"
)

// The registry-aware preview path accepts registered plugin steps and
// fields and rejects unregistered ones (WS1a item 4).
func TestServiceRegistryValidation(t *testing.T) {
	svc := New(nil, nil, nil, testCfg(), Options{})
	rules := []core.ReleaseRule{
		{ID: "r1", Name: "Prefer A", Action: "prefer", Conditions: []core.ReleaseCondition{{Field: "group", Operator: "equals", Value: "A"}}},
	}
	pluginStep := "plugin:example.dub/lang"
	ladder := append(core.DefaultRanking(rules), pluginStep)

	// No registry wired: structural acceptance, builtin compile.
	if err := svc.validateRanking(ladder, rules); err != nil {
		t.Fatalf("structural validation without registry: %v", err)
	}

	reg := extend.New()
	svc.SetExtendRegistry(reg)
	if err := svc.validateRanking(ladder, rules); err == nil {
		t.Fatal("unregistered plugin step must fail once a registry is wired")
	}

	step := extend.Descriptor{
		Kind: extend.KindRankingStep, ID: pluginStep, Owner: "example.dub",
		Label: extend.PluginText("example.dub", "step.lang"),
	}.WithMeta(extend.RankingStepMeta{Editor: extend.RankingEditorNone, CanMove: true, DefaultPosition: 1})
	field := extend.Descriptor{
		Kind: extend.KindRuleField, ID: "plugin:example.dub/dub_lang", Owner: "example.dub",
		Label: extend.PluginText("example.dub", "field.dub_lang"),
	}.WithMeta(extend.RuleFieldMeta{ValueClass: extend.RuleValueChoice, Operators: []string{"equals"}, DefaultOperator: "equals", SingleValue: true})
	if err := reg.Register(step, field); err != nil {
		t.Fatal(err)
	}
	if err := svc.validateRanking(ladder, rules); err != nil {
		t.Fatalf("registered plugin step should validate: %v", err)
	}

	dubRules := []core.ReleaseRule{
		{ID: "dub", Name: "Dub", Action: "prefer", Conditions: []core.ReleaseCondition{{Field: field.ID, Operator: "equals", Value: "fr"}}},
	}
	if _, err := svc.compileRules(dubRules); err != nil {
		t.Fatalf("registered plugin field should compile: %v", err)
	}
	badRules := []core.ReleaseRule{
		{ID: "dub", Name: "Dub", Action: "prefer", Conditions: []core.ReleaseCondition{{Field: field.ID, Operator: "contains", Value: "fr"}}},
	}
	if _, err := svc.compileRules(badRules); err == nil {
		t.Fatal("operator outside the registered field's declared set must fail")
	}
	// Single-value plugin fields take part in conflict detection.
	conflict := []core.ReleaseRule{
		{ID: "a", Name: "French dub", Action: "require", Conditions: []core.ReleaseCondition{{Field: field.ID, Operator: "equals", Value: "fr"}}},
		{ID: "b", Name: "German dub", Action: "require", Conditions: []core.ReleaseCondition{{Field: field.ID, Operator: "equals", Value: "de"}}},
	}
	if _, err := svc.compileRules(conflict); err != nil {
		t.Fatal(err)
	}
	specs := svc.fieldSpecs()
	if specs == nil || !specs[field.ID].External || !specs[field.ID].SingleValue {
		t.Fatalf("registry field spec not merged: %+v", specs[field.ID])
	}
	if _, ok := specs["group"]; !ok {
		t.Fatal("built-in fields must survive the registry merge")
	}
}
