package personalize

import (
	"testing"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/extend"
)

func TestRowKindRegistryMatchesStaticKinds(t *testing.T) {
	reg := NewRowKindRegistry()
	ds := reg.RowDescriptors(nil)
	if len(ds) != len(kinds) {
		t.Fatalf("row descriptors = %d, want %d", len(ds), len(kinds))
	}
	for i, k := range kinds {
		if ds[i].Kind != k.kind || ds[i].Title != k.title || ds[i].Group != k.group {
			t.Fatalf("descriptor %d = %+v, want kind %s", i, ds[i], k.kind)
		}
	}
	// Dynamic extras follow, sorted by title.
	extra := []core.RowDescriptor{{HubID: "collection:2", Title: "Zulu", Kind: "collection:2"}, {HubID: "collection:1", Title: "Alpha", Kind: "collection:1"}}
	out := reg.RowDescriptors(append([]core.RowDescriptor(nil), extra...))
	if out[len(out)-2].Title != "Alpha" || out[len(out)-1].Title != "Zulu" {
		t.Fatalf("extras not sorted: %v", out[len(out)-2:])
	}
}

func TestRowSourceDescriptors(t *testing.T) {
	ds := BuiltinRowDescriptors()
	if len(ds) != len(kinds) {
		t.Fatalf("descriptors = %d, want %d", len(ds), len(kinds))
	}
	for _, d := range ds {
		if err := d.Validate(); err != nil {
			t.Errorf("%s: %v", d.ID, err)
		}
		if d.Kind != extend.KindRowSource {
			t.Errorf("%s kind = %s", d.ID, d.Kind)
		}
		meta, err := extend.MetaOf[extend.RowSourceMeta](d)
		if err != nil || meta.HubID == "" || meta.RowKind != d.ID {
			t.Errorf("%s meta = %+v err %v", d.ID, meta, err)
		}
	}
}

type fakeRowKind struct{}

func (fakeRowKind) Kind() string { return "plugin:test/row" }
func (fakeRowKind) Descriptor() core.RowDescriptor {
	return core.RowDescriptor{HubID: "plugin:test/row", Title: "Fake row", Kind: "plugin:test/row", Group: "picks"}
}

func TestRuntimeRowKindRegistration(t *testing.T) {
	reg := NewRowKindRegistry()
	if err := reg.Register(fakeRowKind{}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(fakeRowKind{}); err == nil {
		t.Fatal("duplicate row kind accepted")
	}
	if _, ok := reg.Lookup("plugin:test/row"); !ok {
		t.Fatal("runtime row kind missing")
	}
	if got := len(reg.RowDescriptors(nil)); got != len(kinds)+1 {
		t.Fatalf("row descriptors = %d, want %d", got, len(kinds)+1)
	}
}
