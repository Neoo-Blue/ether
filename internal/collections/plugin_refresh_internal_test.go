package collections

import (
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
)

type timedPluginKind struct{ fakeKind }

func (timedPluginKind) MinRefresh() time.Duration { return 6 * time.Hour }

func TestPluginMinimumRefreshCadence(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s := &Service{kinds: NewKindRegistry(), now: func() time.Time { return now }}
	if err := s.RegisterSourceKind(timedPluginKind{}); err != nil {
		t.Fatal(err)
	}
	row := &specRow{enabled: true, lastBuilt: now.Add(-2 * time.Hour), buildHash: "built", spec: core.CollectionSpec{Refresh: RefreshLive, Sources: []core.CollectionSource{{Kind: "plugin:test/list"}}}}
	if s.due(row) {
		t.Fatal("live collection refreshed before the plugin interval")
	}
	row.lastBuilt = now.Add(-7 * time.Hour)
	if !s.due(row) {
		t.Fatal("live collection did not refresh after the plugin interval")
	}
	row.spec.Refresh = RefreshHourly
	row.lastBuilt = now.Add(-2 * time.Hour)
	if s.due(row) {
		t.Fatal("hourly collection ignored the plugin interval")
	}
	row.spec.Refresh = RefreshDaily
	row.lastBuilt = now.Add(-7 * time.Hour)
	if s.due(row) {
		t.Fatal("plugin interval overrode the owner's slower daily interval")
	}
}
