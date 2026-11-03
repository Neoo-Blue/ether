package personalize

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/reqmemo"
	"github.com/braedonsaunders/ether/internal/store"
)

func TestTasteEvidenceReusedOnlyWithinViewerRequest(t *testing.T) {
	var calls atomic.Int32
	h := newHarness(t, func(o *Options) {
		o.Favorites = func(_ context.Context, u *core.User) ([]FavoriteSignal, error) {
			if u.ID == 987 {
				calls.Add(1)
			}
			return nil, nil
		}
	})
	u := core.User{ID: 987}
	ctx, stats := store.WithQueryStats(reqmemo.With(context.Background()))
	var firstQueries int64
	var first *Taste
	for n := range 3 {
		taste, _, err := h.e.buildTaste(ctx, u)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			first = taste
		} else if !reflect.DeepEqual(first, taste) {
			t.Fatal("memo changed taste")
		}
		if n == 0 {
			firstQueries = stats.Queries()
		}
		if firstQueries == 0 || stats.Queries() != firstQueries {
			t.Fatalf("repeated taste changed statement count: first=%d now=%d", firstQueries, stats.Queries())
		}
		if i := calls.Load(); i != 1 {
			t.Fatalf("unexpected repeated load: %d", i)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("same request loaded evidence %d times", got)
	}
	other, _, err := h.e.buildTaste(ctx, core.User{ID: 988})
	if err != nil {
		t.Fatal(err)
	}
	if other.UserID != 988 || first.UserID != 987 {
		t.Fatal("taste leaked between viewers")
	}
	uncached, _, err := h.e.buildTaste(reqmemo.With(context.Background()), u)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, uncached) {
		t.Fatal("memoized and fresh taste differ")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("new request did not reload evidence: %d", got)
	}
}
