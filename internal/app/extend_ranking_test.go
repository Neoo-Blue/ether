package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/releasepolicy"
)

// rankingFixturePath is the JSON the console mock serves and the web
// vitests read. Regenerate with ETHER_UPDATE_RANKING_FIXTURE=1.
func rankingFixturePath(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "web", "src", "mock", "extend", "descriptors", "ranking.json")
}

func servedRankingDescriptors(t *testing.T) []extend.Descriptor {
	t.Helper()
	r := extend.New()
	if err := r.Register(builtinRankingDescriptors()...); err != nil {
		t.Fatalf("register built-in ranking descriptors: %v", err)
	}
	return r.List(extend.KindRankingStep, extend.KindRuleField)
}

func asJSON(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// The mock descriptors must equal what Go registers: drift in either
// direction fails here (WS1a item 6).
func TestBuiltinRankingDescriptorsMatchMockFixture(t *testing.T) {
	served := servedRankingDescriptors(t)
	path := rankingFixturePath(t)
	if os.Getenv("ETHER_UPDATE_RANKING_FIXTURE") == "1" {
		raw, err := json.MarshalIndent(served, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture (regenerate with ETHER_UPDATE_RANKING_FIXTURE=1): %v", err)
	}
	var fixture []extend.Descriptor
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if got, want := asJSON(t, fixture), asJSON(t, served); !reflect.DeepEqual(got, want) {
		gotRaw, _ := json.MarshalIndent(got, "", "  ")
		wantRaw, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("mock ranking fixture drifted from Go descriptors\nfixture:\n%s\ngo:\n%s", gotRaw, wantRaw)
	}
}

// Every built-in step and field is registered, in today's order, and the
// two required steps stay required and immovable.
func TestBuiltinRankingDescriptorCoverage(t *testing.T) {
	served := servedRankingDescriptors(t)
	var steps, fields []extend.Descriptor
	for _, d := range served {
		switch d.Kind {
		case extend.KindRankingStep:
			steps = append(steps, d)
		case extend.KindRuleField:
			fields = append(fields, d)
		}
	}
	if len(steps) != len(core.DefaultRankingSteps) {
		t.Fatalf("ranking steps = %d, want %d", len(steps), len(core.DefaultRankingSteps))
	}
	for i, id := range core.DefaultRankingSteps {
		d := steps[i]
		if d.ID != id || d.Order != i || d.Owner != extend.Builtin {
			t.Fatalf("step %d = %+v, want builtin %q at order %d", i, d, id, i)
		}
		meta, err := extend.MetaOf[extend.RankingStepMeta](d)
		if err != nil {
			t.Fatalf("step %q meta: %v", id, err)
		}
		if meta.DefaultPosition != i {
			t.Fatalf("step %q defaultPosition = %d, want %d", id, meta.DefaultPosition, i)
		}
		required := id == core.RankConnection || id == core.RankVerified
		if meta.Required != required {
			t.Fatalf("step %q required = %v, want %v", id, meta.Required, required)
		}
		if meta.CanMove == required {
			t.Fatalf("step %q canMove = %v, want %v", id, meta.CanMove, !required)
		}
	}
	specs := releasepolicy.BuiltinFields()
	if len(fields) != len(specs) {
		t.Fatalf("rule fields = %d, want %d", len(fields), len(specs))
	}
	for i, spec := range specs {
		d := fields[i]
		if d.ID != spec.ID || d.Order != i {
			t.Fatalf("field %d = %q order %d, want %q at %d", i, d.ID, d.Order, spec.ID, i)
		}
		meta, err := extend.MetaOf[extend.RuleFieldMeta](d)
		if err != nil {
			t.Fatalf("field %q meta: %v", spec.ID, err)
		}
		if meta.ValueClass != spec.ValueClass || meta.SingleValue != spec.SingleValue ||
			meta.DefaultOperator != spec.DefaultOperator || !reflect.DeepEqual(meta.Operators, spec.Operators) {
			t.Fatalf("field %q meta %+v disagrees with spec %+v", spec.ID, meta, spec)
		}
	}
}
