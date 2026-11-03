package segcache

import (
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
)

func TestReleaseReasonRetention(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	c := openT(t, t.TempDir(), 1024)
	c.now = func() time.Time { return now }
	mustPut(t, c, "r", 0, []byte("x"))

	got := c.Releases()[0].Reason
	if got.Retention.Kind != "lru" || len(got.Causes) != 1 || got.Causes[0].Code != core.CacheCauseUnrecorded {
		t.Fatalf("default = %+v", got)
	}

	if err := c.Pin("r", "owner"); err != nil {
		t.Fatal(err)
	}
	got = c.Releases()[0].Reason
	if got.Retention.Kind != "pinned" || len(got.Retention.Pins) != 1 || got.Retention.Pins[0] != "owner" {
		t.Fatalf("pinned = %+v", got.Retention)
	}

	if err := c.Unpin("r", "owner"); err != nil {
		t.Fatal(err)
	}
	c.SetPolicy(Policy{Retention: 48 * time.Hour})
	got = c.Releases()[0].Reason
	if got.Retention.Kind != "expiry" || got.Retention.Days != 2 || !got.Retention.Until.After(now) {
		t.Fatalf("expiry = %+v", got.Retention)
	}

	c.SetPolicy(Policy{Mode: "adaptive", ReplayBoost: 24 * time.Hour})
	c.RecordPlay("r", "R", 100)
	c.RecordPlay("r", "R", 100)
	got = c.Releases()[0].Reason
	if got.Retention.Kind != "replay" || got.Retention.Hours != 24 {
		t.Fatalf("replay = %+v", got.Retention)
	}
}

func TestReleaseReasonAddLiveCause(t *testing.T) {
	c := openT(t, t.TempDir(), 1024)
	mustPut(t, c, "r", 0, []byte("x"))
	r := c.Releases()[0]
	if r.Reason.Causes[0].Code != core.CacheCauseUnrecorded {
		t.Fatalf("legacy causes = %+v", r.Reason.Causes)
	}
	at := time.Unix(2000, 0)
	r.Reason.AddCause(core.CacheCause{Code: core.CacheCausePlaying, FirstSeen: at})
	r.Reason.AddCause(core.CacheCause{Code: core.CacheCausePlaying, FirstSeen: at.Add(time.Hour)})
	if len(r.Reason.Causes) != 1 || r.Reason.Causes[0].Code != core.CacheCausePlaying || !r.Reason.Causes[0].FirstSeen.Equal(at) {
		t.Fatalf("live causes = %+v", r.Reason.Causes)
	}
}

func TestReleaseReasonOrdering(t *testing.T) {
	c := openT(t, t.TempDir(), 1024)
	mustPut(t, c, "r", 0, []byte("x"))
	c.RecordProvenance(core.CacheProvenance{ReleaseID: "r", Cause: core.CacheCausePaused})
	c.RecordProvenance(core.CacheProvenance{ReleaseID: "r", Cause: core.CacheCausePlaying})
	got := c.Releases()[0].Reason
	if len(got.Causes) != 2 || got.Causes[0].Code != core.CacheCausePlaying || got.Causes[1].Code != core.CacheCausePaused {
		t.Fatalf("cause order = %+v", got.Causes)
	}
}
