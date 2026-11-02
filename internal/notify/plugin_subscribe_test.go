package notify

import (
	"context"
	"testing"

	"github.com/braedonsaunders/ether/internal/store"
)

func TestPluginEventSubscription(t *testing.T) {
	s := New(store.OpenTest(t), Options{})
	var events []Event
	stop := s.Subscribe(func(e Event) { events = append(events, e) })
	if err := s.Emit(context.Background(), Event{Kind: "user.joined", Message: "viewer"}); err != nil {
		t.Fatal(err)
	}
	if err := s.TitleAvailableOnce(context.Background(), "title:1", "Movie"); err != nil {
		t.Fatal(err)
	}
	if err := s.TitleAvailableOnce(context.Background(), "title:1", "Movie"); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Kind != "user.joined" || events[1].Kind != "title.available" || events[0].At.IsZero() {
		t.Fatalf("events: %+v", events)
	}
	stop()
	if err := s.Emit(context.Background(), Event{Kind: "user.joined"}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatal("received event after unsubscribe")
	}
}
