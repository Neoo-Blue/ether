package personalize

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
)

func TestHomeMemoRepresentativeMemory(t *testing.T) {
	clock := newClock(testNow)
	e := &Engine{opts: Options{Now: clock.Now}, memo: map[memoKey]*memoEntry{}}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < memoMaxEntries; i++ {
		rows := make([]core.HomeRow, 8)
		for j := range rows {
			rows[j].Items = make([]core.Title, 30)
			for k := range rows[j].Items {
				rows[j].Items[k].Title = fmt.Sprintf("title %d %d %d", i, j, k)
			}
		}
		e.memoPut(memoKey{user: int64(i)}, memoEntry{rows: rows})
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	var accounted int64
	for _, v := range e.memo {
		accounted += v.bytes
	}
	t.Logf("entries=%d accounted=%d heap delta=%d bytes", len(e.memo), accounted, int64(after.HeapAlloc)-int64(before.HeapAlloc))
	if accounted > memoMaxBytes {
		t.Fatalf("accounted bytes exceeded limit: %d", accounted)
	}
	runtime.KeepAlive(e)
}

func TestHomeMemoExpiresAndEvictsWithoutTruncatingRows(t *testing.T) {
	clock := newClock(testNow)
	e := &Engine{opts: Options{Now: clock.Now}, memo: map[memoKey]*memoEntry{}}
	rows := []core.HomeRow{{Items: make([]core.Title, 20)}}
	for i := 0; i < memoMaxEntries; i++ {
		e.memoPut(memoKey{user: int64(i)}, memoEntry{rows: rows})
		clock.Set(clock.Now().Add(time.Millisecond))
	}
	e.memoPut(memoKey{user: memoMaxEntries + 1000}, memoEntry{rows: rows})
	if len(e.memo) != memoMaxEntries {
		t.Fatalf("entries: %d", len(e.memo))
	}
	if _, ok := e.memoGet(memoKey{user: 0}); ok {
		t.Fatal("oldest entry retained")
	}
	got, ok := e.memoGet(memoKey{user: memoMaxEntries + 1000})
	if !ok || len(got.rows[0].Items) != 20 {
		t.Fatal("full shelf not retained")
	}
	large := []core.HomeRow{{Title: strings.Repeat("x", memoMaxBytes+1)}}
	e.memoPut(memoKey{user: 1001}, memoEntry{rows: large})
	if _, ok := e.memoGet(memoKey{user: 1001}); ok {
		t.Fatal("oversized memo retained")
	}
	clock.Set(clock.Now().Add(memoTTL))
	e.presentations = map[memoKey]presentation{{user: 1}: {at: clock.Now().Add(-presentationTTL)}}
	e.pruneMemos(clock.Now())
	if len(e.memo) != 0 {
		t.Fatalf("expired entries retained: %d", len(e.memo))
	}
	if len(e.presentations) != 0 {
		t.Fatal("expired presentation retained")
	}
}

func TestAssembledScreensAreMemoised(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()
	sciFiFan(h, 1)
	u := core.User{ID: 1}
	h.rebuild(u)
	hc := hubContext(testNow, core.DeviceTV)

	first, _ := h.e.HomeHubsFor(ctx, &u, hc, 12)
	gets := h.cat.callCount("get")
	again, _ := h.e.HomeHubsFor(ctx, &u, hc, 12)
	if h.cat.callCount("get") != gets {
		t.Fatal("a repeated Home load re-assembled instead of using the memo")
	}
	if len(first) != len(again) || first[2].ID != again[2].ID {
		t.Fatal("memoised Home differs")
	}
	// The memo hands out copies: a caller reordering its rows does not
	// change the next caller's.
	p1, _ := h.e.PreviewHome(ctx, &u, hc, "", 12)
	p1.Rows[0], p1.Rows[1] = p1.Rows[1], p1.Rows[0]
	p2, _ := h.e.PreviewHome(ctx, &u, hc, "", 12)
	if p2.Rows[0].HubID == p1.Rows[0].HubID {
		t.Fatal("memo shared its slice")
	}

	// Starting something new changes Continue Watching: re-assembled.
	h.start(1, movieKey(1, 3), 0.3, testNow.Add(time.Minute))
	h.e.HomeHubsFor(ctx, &u, hc, 12)
	if h.cat.callCount("get") == gets {
		t.Fatal("Continue Watching changed but the memo was reused")
	}
	// So does a layout change.
	gets = h.cat.callCount("get")
	if _, err := h.e.SetHomeLayout(ctx, core.HomeLayout{Global: []core.LayoutPage{{Section: "home", Rows: []core.RowPolicy{{Row: KindTop10, Status: "off"}}}}}); err != nil {
		t.Fatal(err)
	}
	hubs, _ := h.e.HomeHubsFor(ctx, &u, hc, 12)
	for _, hb := range hubs {
		if hb.ID == "home.top10" {
			t.Fatal("layout change ignored by the memo")
		}
	}
	if h.cat.callCount("get") == gets {
		t.Fatal("layout change did not re-assemble")
	}
}
