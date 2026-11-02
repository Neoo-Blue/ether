package plugins

import (
	"context"
	"testing"
)

func TestPublishLogsOnlySubscribedEvents(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	count := func(table string) int {
		t.Helper()
		var n int
		if err := env.m.store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	env.m.Publish(ctx, Event{Name: EventPlaybackStarted})
	if count("plugins_event_log") != 0 || count("plugins_outbox") != 0 {
		t.Fatal("event without subscribers wrote to storage")
	}
	rec := env.installHello(t, "1.0.0")
	env.m.Publish(ctx, Event{Name: EventPlaybackStarted})
	if count("plugins_event_log") != 1 || count("plugins_outbox") != 1 {
		t.Fatal("subscribed event was not logged and queued")
	}
	if err := env.m.SetEnabled(ctx, rec.ID, false); err != nil {
		t.Fatal(err)
	}
	env.m.Publish(ctx, Event{Name: EventPlaybackStarted})
	if count("plugins_event_log") != 1 || count("plugins_outbox") != 1 {
		t.Fatal("disabled subscriber caused a write")
	}
}
