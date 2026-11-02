package collections_test

import (
	"context"
	"errors"
	"testing"

	"github.com/braedonsaunders/ether/internal/core"
)

type fakeLayouts struct {
	d    core.HomeLayout
	sets int
	fail error // returned once by SetHomeLayout
}

func (f *fakeLayouts) HomeLayout(context.Context) (core.HomeLayout, error) { return f.d, nil }
func (f *fakeLayouts) SetHomeLayout(_ context.Context, d core.HomeLayout) (core.HomeLayout, error) {
	if err := f.fail; err != nil {
		f.fail = nil
		return core.HomeLayout{}, err
	}
	f.d = d
	f.sets++
	return d, nil
}

func TestAdoptLayoutSchedulesRunsOnce(t *testing.T) {
	e := newEnv(t)
	from, to := 17, 22
	sp := spec("Evenings", "movie", catalogSource(), rl("genre", "is", "Thriller"))
	sp.Targets.HomeRow, sp.Targets.SectionRow = true, true
	sp.Targets.Schedule = &core.CollectionSchedule{FromHour: &from, ToHour: &to}
	made, err := e.svc.CreateSpec(e.ctx, sp)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := e.svc.CreateSpec(e.ctx, spec("Plain", "movie", catalogSource(), rl("genre", "is", "Comedy")))
	if err != nil {
		t.Fatal(err)
	}
	layouts := &fakeLayouts{d: core.HomeLayout{Global: []core.LayoutPage{{Section: "home", Rows: []core.RowPolicy{
		{Row: "collection:" + made.ID, Status: "eligible", When: &core.LayoutWhen{Days: []int{0, 6}}},
		{Row: "short-on-time", Status: "eligible", When: &core.LayoutWhen{Days: []int{1}}},
	}}}}}
	if err := e.svc.AdoptLayoutSchedules(e.ctx, layouts); err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.GetSpec(e.ctx, made.ID)
	if err != nil {
		t.Fatal(err)
	}
	sc := got.Targets.Schedule
	if sc == nil || len(sc.Days) != 2 || sc.Days[0] != 0 || sc.Days[1] != 6 || *sc.FromHour != 17 || *sc.ToHour != 22 {
		t.Fatalf("collection schedule: %+v", sc)
	}
	rows := layouts.d.Global[0].Rows
	if layouts.sets != 1 || rows[0].When != nil || rows[1].When == nil {
		t.Fatalf("layout: %d %+v", layouts.sets, rows)
	}
	if p, _ := e.svc.GetSpec(e.ctx, plain.ID); p.Targets.Schedule != nil {
		t.Fatalf("untouched collection got a schedule: %+v", p.Targets.Schedule)
	}
	// Once only: an Everyone policy saved later is the owner's, not old data.
	layouts.d.Global[0].Rows[0] = core.RowPolicy{Row: "collection:" + made.ID, Status: "always", Slot: 1}
	if err := e.svc.AdoptLayoutSchedules(e.ctx, layouts); err != nil {
		t.Fatal(err)
	}
	if again, _ := e.svc.GetSpec(e.ctx, made.ID); again.Targets.Schedule == nil || layouts.sets != 1 {
		t.Fatalf("carry-over ran twice: %+v %d", again.Targets.Schedule, layouts.sets)
	}
}

func TestSetSchedule(t *testing.T) {
	e := newEnv(t)
	made, err := e.svc.CreateSpec(e.ctx, spec("Winter", "movie", catalogSource(), rl("genre", "is", "Drama")))
	if err != nil {
		t.Fatal(err)
	}
	midnight, from := 0, 20
	if err := e.svc.SetSchedule(e.ctx, made.ID, &core.CollectionSchedule{Months: []int{12, 1}, Dayparts: []string{"evening"}, FromHour: &from, ToHour: &midnight}); err != nil {
		t.Fatal(err)
	}
	all, err := e.svc.CollectionSchedules(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	sc := all[made.ID]
	if sc == nil || len(sc.Months) != 2 || sc.Dayparts[0] != "evening" || *sc.ToHour != 24 {
		t.Fatalf("saved schedule: %+v", sc)
	}
	if got, _ := e.svc.GetSpec(e.ctx, made.ID); got.Name != "Winter" || got.Targets.Schedule == nil {
		t.Fatalf("spec after schedule: %+v", got)
	}
	var invalid interface{ InvalidFields() map[string]string }
	err = e.svc.SetSchedule(e.ctx, made.ID, &core.CollectionSchedule{Months: []int{13}, StartDate: "2026-12-31", EndDate: "2026-12-01"})
	if !errors.As(err, &invalid) || invalid.InvalidFields()["months"] == "" || invalid.InvalidFields()["endDate"] == "" {
		t.Fatalf("invalid schedule: %v", err)
	}
	if err := e.svc.ValidateSchedule(&core.CollectionSchedule{Dayparts: []string{"noon"}}); !errors.As(err, &invalid) {
		t.Fatalf("validate: %v", err)
	}
	if err := e.svc.SetSchedule(e.ctx, made.ID, &core.CollectionSchedule{}); err != nil {
		t.Fatal(err)
	}
	if all, _ := e.svc.CollectionSchedules(e.ctx); all[made.ID] != nil {
		t.Fatalf("cleared schedule: %+v", all[made.ID])
	}
	if err := e.svc.SetSchedule(e.ctx, "123456", nil); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("missing collection: %v", err)
	}
}

// A carry-over that stops after moving the schedules (here the layout
// rewrite fails) finishes on the next start without folding again: a
// second fold of a rewritten layout would wipe the moved schedule.
func TestAdoptLayoutSchedulesResumesAfterPartialRun(t *testing.T) {
	e := newEnv(t)
	sp := spec("Weekends", "movie", catalogSource(), rl("genre", "is", "Thriller"))
	sp.Targets.HomeRow = true
	made, err := e.svc.CreateSpec(e.ctx, sp)
	if err != nil {
		t.Fatal(err)
	}
	layouts := &fakeLayouts{fail: errors.New("injected layout write failure"), d: core.HomeLayout{Global: []core.LayoutPage{{Section: "home", Rows: []core.RowPolicy{
		{Row: "collection:" + made.ID, Status: "eligible", When: &core.LayoutWhen{Days: []int{0, 6}}},
	}}}}}
	if err := e.svc.AdoptLayoutSchedules(e.ctx, layouts); err == nil {
		t.Fatal("layout failure not reported")
	}
	// The layout write lands but the run stops before its done marker:
	// simulate by rewriting the layout as the first run would have.
	layouts.d.Global[0].Rows[0].When = nil
	if err := e.svc.AdoptLayoutSchedules(e.ctx, layouts); err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.GetSpec(e.ctx, made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sc := got.Targets.Schedule; sc == nil || len(sc.Days) != 2 || sc.Days[0] != 0 || sc.Days[1] != 6 {
		t.Fatalf("rerun wiped the moved schedule: %+v", sc)
	}
	if layouts.d.Global[0].Rows[0].When != nil {
		t.Fatalf("layout keeps the Everyone When: %+v", layouts.d.Global[0].Rows[0])
	}
}
