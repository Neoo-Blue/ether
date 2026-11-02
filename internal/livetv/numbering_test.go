package livetv

import (
	"context"
	"testing"
	"time"
)

func TestNumberReservationsSurviveDisappearance(t *testing.T) {
	s := testService(t, Options{})
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	assign := func(cs []Channel, at time.Time) []Channel {
		t.Helper()
		tx, err := s.o.DB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err = assignImportedNumbers(ctx, tx, "s", cs, nil, nil, at); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		return cs
	}
	first := assign([]Channel{{ID: "a", Group: "News"}, {ID: "b", Group: "News"}, {ID: "c", Group: "Sports"}}, now)
	if first[0].Number != "1" || first[1].Number != "2" || first[2].Number != "1" {
		t.Fatalf("first: %+v", first)
	}
	second := assign([]Channel{{ID: "b", Group: "News"}, {ID: "d", Group: "News"}}, now.Add(24*time.Hour))
	if second[0].Number != "2" || second[1].Number != "3" {
		t.Fatalf("second: %+v", second)
	}
	returned := assign([]Channel{{ID: "a", Group: "News"}}, now.Add(2*24*time.Hour))
	if returned[0].Number != "1" {
		t.Fatalf("returned: %+v", returned)
	}
	expired := assign([]Channel{{ID: "e", Group: "News"}}, now.Add(10*24*time.Hour))
	if expired[0].Number != "1" {
		t.Fatalf("expired: %+v", expired)
	}
}
