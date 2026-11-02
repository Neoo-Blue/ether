package livetv

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
)

func TestHealthSweepHidesAndRestores(t *testing.T) {
	now := time.Unix(100, 0)
	prober := &fixtureLiveProber{err: errors.New("offline")}
	s := testService(t, Options{Now: func() time.Time { return now }, Transcoder: prober})
	c := Channel{ID: "ch", Name: "Test", Number: "1", Kind: "iptv", Enabled: true, StreamURL: "https://example.invalid/live"}
	if _, err := s.SaveChannel(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	s.sweepChannels(context.Background())
	cs, err := s.Channels(context.Background())
	if err != nil || len(cs) != 1 || cs[0].Health != "unavailable" {
		t.Fatalf("channels=%+v err=%v", cs, err)
	}
	if s.allowed(context.Background(), &core.User{ID: 123}, cs[0], nil) {
		t.Fatal("unavailable channel visible to viewer")
	}
	if !s.allowed(context.Background(), &core.User{ID: 1, IsOwner: true}, cs[0], nil) {
		t.Fatal("owner cannot see unavailable channel")
	}
	prober.err = nil
	s.sweepChannels(context.Background())
	cs, _ = s.Channels(context.Background())
	if cs[0].Health != "available" || !s.allowed(context.Background(), &core.User{ID: 123}, cs[0], nil) {
		t.Fatalf("not restored: %+v", cs[0])
	}
}
