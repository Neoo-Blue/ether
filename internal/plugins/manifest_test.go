package plugins

import (
	"strings"
	"testing"
)

func baseManifest() Manifest {
	return Manifest{
		ID: "community.example.hello", Version: "1.0.0", Name: "Hello",
		Description: "Greets the household.",
		Ether: &EtherBlock{
			Protocol: 1, Kind: KindExtension, Summary: "Greets the household",
			Runtime:     RuntimeDecl{Kind: "hosted", Entry: "dist/plugin.js"},
			Permissions: Permissions{},
		},
	}
}

func hasField(err error, field string) bool {
	ve, ok := err.(*ValidationError)
	if !ok {
		return false
	}
	for _, f := range ve.Fields {
		if f.Field == field || strings.HasPrefix(f.Field, field) {
			return true
		}
	}
	return false
}

func TestManifestValidate(t *testing.T) {
	if err := baseManifest().Validate(); err != nil {
		t.Fatalf("base manifest: %v", err)
	}
	cases := []struct {
		name  string
		mut   func(*Manifest)
		field string
	}{
		{"id upper", func(m *Manifest) { m.ID = "Community.Example" }, "id"},
		{"id no dot", func(m *Manifest) { m.ID = "hello" }, "id"},
		{"version", func(m *Manifest) { m.Version = "1.0" }, "version"},
		{"name empty", func(m *Manifest) { m.Name = "" }, "name"},
		{"protocol", func(m *Manifest) { m.Ether.Protocol = 2 }, "ether.protocol"},
		{"kind", func(m *Manifest) { m.Ether.Kind = "widget" }, "ether.kind"},
		{"summary long", func(m *Manifest) { m.Ether.Summary = strings.Repeat("Word ", 20) }, "ether.summary"},
		{"summary em dash", func(m *Manifest) { m.Ether.Summary = "Greets people — warmly" }, "ether.summary"},
		{"summary bang", func(m *Manifest) { m.Ether.Summary = "Greets people!" }, "ether.summary"},
		{"summary banned word", func(m *Manifest) { m.Ether.Summary = "Simply greets people" }, "ether.summary"},
		{"summary lower case", func(m *Manifest) { m.Ether.Summary = "greets people" }, "ether.summary"},
		{"runtime both files", func(m *Manifest) { m.Ether.Runtime.Module = "p.wasm" }, "ether.runtime"},
		{"runtime remote entry", func(m *Manifest) { m.Ether.Runtime.Kind = "remote" }, "ether.runtime"},
		{"cluster", func(m *Manifest) { m.Ether.Cluster = "somewhere" }, "ether.cluster"},
		{"health fast", func(m *Manifest) { m.Ether.Health.Interval = "5s" }, "ether.health.interval"},
		{"schedule fast", func(m *Manifest) {
			m.Ether.Schedule = []ScheduleEntry{{Every: "1m", Call: "/ether/job/x"}}
		}, "ether.schedule[0].every"},
		{"schedule call", func(m *Manifest) {
			m.Ether.Schedule = []ScheduleEntry{{Every: "1h", Call: "job/x"}}
		}, "ether.schedule[0].call"},
		{"network url", func(m *Manifest) { m.Ether.Permissions.Network = []string{"https://x.tv"} }, "ether.permissions.network[0]"},
		{"network wildcard", func(m *Manifest) { m.Ether.Permissions.Network = []string{"*.x.tv"} }, ""},
		{"catalog level", func(m *Manifest) { m.Ether.Permissions.Catalog = "write" }, "ether.permissions.catalog"},
		{"watch state all", func(m *Manifest) { m.Ether.Permissions.Viewers.WriteWatchState = "all" }, "ether.permissions.viewers.write_watch_state"},
		{"reserved cap", func(m *Manifest) { m.Ether.Capabilities = []Capability{"metadata"} }, "ether.capabilities[0]"},
		{"unknown cap", func(m *Manifest) { m.Ether.Capabilities = []Capability{"telepathy"} }, "ether.capabilities[0]"},
		{"cap kind mismatch", func(m *Manifest) {
			m.Ether.Capabilities = []Capability{CapStreamSource}
			m.Ether.Contributes.Source = &SourceMeta{Class: "direct"}
		}, "ether.capabilities[0]"},
		{"stream needs source meta", func(m *Manifest) {
			m.Ether.Kind = KindSource
			m.Ether.Capabilities = []Capability{CapStreamSource}
		}, "ether.contributes.source"},
		{"stream bad class", func(m *Manifest) {
			m.Ether.Kind = KindSource
			m.Ether.Capabilities = []Capability{CapStreamSource}
			m.Ether.Contributes.Source = &SourceMeta{Class: "smoke-signals"}
		}, "ether.contributes.source.class"},
		{"livetv needs test", func(m *Manifest) {
			m.Ether.Kind = KindLiveTVSource
			m.Ether.Capabilities = []Capability{CapLiveTV}
			m.Ether.Contributes.Channels = &ChannelsMeta{}
		}, "ether.contributes.channels"},
		{"notify needs sendTest", func(m *Manifest) {
			m.Ether.Kind = KindNotificationChannel
			m.Ether.Capabilities = []Capability{CapNotify}
			m.Ether.Contributes.Notification = &NotificationMeta{}
		}, "ether.contributes.notification"},
		{"events unknown", func(m *Manifest) {
			m.Ether.Capabilities = []Capability{CapEvents}
			m.Ether.Contributes.Events = &EventsMeta{Subscribe: []string{"moon.phase"}}
		}, "ether.contributes.events.subscribe"},
		{"ranking remote", func(m *Manifest) {
			m.Ether.Runtime = RuntimeDecl{Kind: "remote"}
			m.Ether.Capabilities = []Capability{CapRankingStep}
			m.Ether.Contributes.RankingSteps = []RankingStepMeta{{Key: "dub", Label: "Dub language"}}
		}, "ether.capabilities"},
		{"list source max items", func(m *Manifest) {
			m.Ether.Capabilities = []Capability{CapListSource}
			m.Ether.Contributes.ListSources = []ListSourceMeta{{Key: "top", Label: "Top picks", Catalog: "top", MaxItems: 3000}}
		}, "ether.contributes.listSources[0].maxItems"},
		{"list source refresh", func(m *Manifest) {
			m.Ether.Capabilities = []Capability{CapListSource}
			m.Ether.Contributes.ListSources = []ListSourceMeta{{Key: "top", Label: "Top picks", Catalog: "top", Refresh: "5m"}}
		}, "ether.contributes.listSources[0].refresh"},
		{"tool read and destructive", func(m *Manifest) {
			m.Ether.Capabilities = []Capability{CapTools}
			m.Ether.Contributes.Tools = []ToolMeta{{Name: "do_thing", Title: "Do thing", Description: "Does it", InputSchema: []byte(`{"type":"object"}`), ReadOnly: true, Destructive: true}}
		}, "ether.contributes.tools[0]"},
		{"tool name", func(m *Manifest) {
			m.Ether.Capabilities = []Capability{CapTools}
			m.Ether.Contributes.Tools = []ToolMeta{{Name: "DoThing", Title: "Do thing", Description: "Does it", InputSchema: []byte(`{"type":"object"}`)}}
		}, "ether.contributes.tools[0].name"},
		{"label title case", func(m *Manifest) {
			m.Ether.Contributes.Pages = []PageMeta{{Key: "home", Label: "My Great Page", Route: "/ether/view/home"}}
		}, "ether.contributes.pages[0].label"},
		{"slot unknown", func(m *Manifest) {
			m.Ether.Contributes.Slots = []SlotMeta{{Slot: "fridge.door", Key: "x", Label: "Fridge", Route: "/ether/view/x"}}
		}, "ether.contributes.slots[0].slot"},
		{"t key strings", func(m *Manifest) { m.Ether.Summary = "$t:summary" }, "ether.strings"},
		{"settings json kind", func(m *Manifest) {
			m.Ether.Settings = []byte(`{"fields":[{"key":"raw","kind":"json","label":"Raw"}]}`)
		}, "ether.settings"},
		{"settings label long", func(m *Manifest) {
			m.Ether.Settings = []byte(`{"fields":[{"key":"a","kind":"text","label":"` + strings.Repeat("Long ", 12) + `"}]}`)
		}, "ether.settings"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := baseManifest()
			// deep-copy the ether block so mutations do not leak
			e := *m.Ether
			m.Ether = &e
			tc.mut(&m)
			err := m.Validate()
			if tc.field == "" {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error on %s, got none", tc.field)
			}
			if !hasField(err, tc.field) {
				t.Fatalf("want field %s in %v (%v)", tc.field, err, err.(*ValidationError).Fields)
			}
		})
	}
}

func TestManifestStremioOnly(t *testing.T) {
	m := Manifest{ID: "org.example.addon", Version: "0.1.0", Name: "Plain"}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	if !m.IsStremioOnly() {
		t.Fatal("want stremio only")
	}
}

func TestPermissionSheetAndDiff(t *testing.T) {
	m := baseManifest()
	m.Ether.Permissions = Permissions{
		Network: []string{"api.x.tv"}, Catalog: "read", Folders: "read",
		Viewers: ViewerPerms{WriteWatchState: "granted_viewers"}, Secrets: []string{"token"},
	}
	m.Ether.Capabilities = []Capability{CapEvents}
	m.Ether.Contributes.Events = &EventsMeta{Subscribe: []string{EventPlaybackStarted}}
	lines, nv, nf := PermissionSheet(m)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"Connects to api.x.tv", "Reads your catalogue", "Marks titles watched", "Reads folders you choose", "Stores an access token", "Gets play and new-release events"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing line %q in %q", want, joined)
		}
	}
	if !nv || !nf {
		t.Fatal("want viewer and folder picks")
	}
	cur := baseManifest()
	if added := PermissionDiff(cur, m); len(added) == 0 {
		t.Fatal("want permission increase")
	}
	if added := PermissionDiff(m, cur); len(added) != 0 {
		t.Fatalf("removal must not count, got %v", added)
	}
}

func TestLintCopy(t *testing.T) {
	if msg := LintCopy("Wrapped year in viewing", 40); msg != "" {
		t.Fatal(msg)
	}
	if msg := LintCopy("Just do it", 40); msg == "" {
		t.Fatal("want banned word failure")
	}
}

func TestActionSlotContract(t *testing.T) {
	m := baseManifest()
	m.Ether.Contributes.Slots = []SlotMeta{{Slot: "actionMenu.title", Key: "mark", Label: "Mark", Action: "mark"}}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	ds := DescriptorsFor(m)
	if len(ds) != 1 || !strings.Contains(string(ds[0].Meta), `"action":"mark"`) || !strings.Contains(string(ds[0].Meta), `"key":"mark"`) {
		t.Fatalf("descriptor: %+v", ds)
	}
	m.Ether.Contributes.Slots[0].Action = ""
	if err := m.Validate(); !hasField(err, "ether.contributes.slots[0].action") {
		t.Fatalf("expected action error, got %v", err)
	}
	m.Ether.Contributes.Slots[0] = SlotMeta{Slot: "title.panel", Key: "panel", Label: "Panel"}
	if err := m.Validate(); !hasField(err, "ether.contributes.slots[0].route") {
		t.Fatalf("expected route error, got %v", err)
	}
}
