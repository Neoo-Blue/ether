package collections

import (
	"reflect"
	"slices"
	"testing"

	"github.com/braedonsaunders/ether/internal/core"
)

func hr(n int) *int { return &n }

func targeted(media string, home, section bool, sc *core.CollectionSchedule) core.CollectionSpec {
	sp := core.CollectionSpec{Name: "x", MediaType: media}
	sp.Targets.HomeRow, sp.Targets.SectionRow, sp.Targets.Schedule = home, section, sc
	return sp
}

func page(section string, rows ...core.RowPolicy) core.LayoutPage {
	return core.LayoutPage{Section: section, Rows: rows}
}

func TestFoldLayoutSchedules(t *testing.T) {
	everyDay := &core.LayoutWhen{Days: []int{0, 1, 2, 3, 4, 5, 6}}
	cases := []struct {
		name     string
		spec     core.CollectionSpec
		global   []core.LayoutPage
		viewer   []core.LayoutPage
		want     *core.CollectionSchedule
		changed  bool
		viewerWn *core.LayoutWhen
	}{
		{
			name: "both enforced on different pages: the overlap",
			spec: targeted(MediaMovie, true, true, &core.CollectionSchedule{FromHour: hr(17), ToHour: hr(22)}),
			global: []core.LayoutPage{page("home", core.RowPolicy{Row: "collection:7", Status: "eligible",
				When: &core.LayoutWhen{Days: []int{0, 6}}})},
			want:    &core.CollectionSchedule{Days: []int{0, 6}, FromHour: hr(17), ToHour: hr(22)},
			changed: true,
		},
		{
			name:   "Home policy without When replaced the only schedule: any time",
			spec:   targeted(MediaMovie, true, false, &core.CollectionSchedule{Days: []int{0, 6}}),
			global: []core.LayoutPage{page("home", core.RowPolicy{Row: "collection:7", Status: "always", Slot: 2})},
			want:   nil, changed: true,
		},
		{
			name: "Home policy is the only page: its When",
			spec: targeted(MediaShow, false, false, nil),
			global: []core.LayoutPage{page("home", core.RowPolicy{Row: "collection.7", Status: "eligible",
				When: &core.LayoutWhen{Months: []int{12}, Dayparts: []string{"evening"}, StartDate: "2026-12-01", EndDate: "2026-12-31", Devices: []string{"tv"}}})},
			want:    &core.CollectionSchedule{Months: []int{12}, Dayparts: []string{"evening"}, StartDate: "2026-12-01", EndDate: "2026-12-31", Devices: []string{"tv"}},
			changed: true,
		},
		{
			name: "days that never overlap keep Home's",
			spec: targeted(MediaMovie, true, true, &core.CollectionSchedule{Days: []int{1, 2}}),
			global: []core.LayoutPage{page("home", core.RowPolicy{Row: "collection:7", Status: "eligible",
				When: &core.LayoutWhen{Days: []int{0, 6}}})},
			want:    &core.CollectionSchedule{Days: []int{0, 6}},
			changed: true,
		},
		{
			name: "hours overlapping in two windows keep Home's",
			spec: targeted(MediaMovie, true, true, &core.CollectionSchedule{FromHour: hr(4), ToHour: hr(23)}),
			global: []core.LayoutPage{page("home", core.RowPolicy{Row: "collection:7", Status: "eligible",
				When: &core.LayoutWhen{FromHour: hr(22), ToHour: hr(6)}})},
			want:    &core.CollectionSchedule{FromHour: hr(22), ToHour: hr(6)},
			changed: true,
		},
		{
			name: "wrapping hours overlap into one window",
			spec: targeted(MediaMovie, true, true, &core.CollectionSchedule{FromHour: hr(20), ToHour: hr(2)}),
			global: []core.LayoutPage{page("home", core.RowPolicy{Row: "collection:7", Status: "eligible",
				When: &core.LayoutWhen{FromHour: hr(22), ToHour: hr(24)}})},
			want:    &core.CollectionSchedule{FromHour: hr(22), ToHour: hr(24)},
			changed: true,
		},
		{
			name: "months and dates narrow",
			spec: targeted(MediaMixed, true, true, &core.CollectionSchedule{Months: []int{11, 12}, StartDate: "2026-11-01", EndDate: "2026-12-31"}),
			global: []core.LayoutPage{page("shows", core.RowPolicy{Row: "collection:7", Status: "eligible",
				When: &core.LayoutWhen{Months: []int{12}, StartDate: "2026-12-01", EndDate: "2027-01-31"}})},
			want:    &core.CollectionSchedule{Months: []int{12}, StartDate: "2026-12-01", EndDate: "2026-12-31"},
			changed: true,
		},
		{
			name: "a page where the row is off does not count",
			spec: targeted(MediaMovie, true, true, &core.CollectionSchedule{Days: []int{5}}),
			global: []core.LayoutPage{page("home", core.RowPolicy{Row: "collection:7", Status: "off",
				When: &core.LayoutWhen{Days: []int{0}}})},
			want: &core.CollectionSchedule{Days: []int{5}},
		},
		{
			name: "a policy on a page the collection cannot appear on does not count",
			spec: targeted(MediaMovie, true, false, &core.CollectionSchedule{Days: []int{5}}),
			global: []core.LayoutPage{page("shows", core.RowPolicy{Row: "collection:7", Status: "eligible",
				When: &core.LayoutWhen{Days: []int{0}}})},
			want: &core.CollectionSchedule{Days: []int{5}},
		},
		{
			name:     "a viewer override without a When stays any time",
			spec:     targeted(MediaMovie, true, false, &core.CollectionSchedule{Days: []int{5}}),
			viewer:   []core.LayoutPage{page("home", core.RowPolicy{Row: "collection:7", Status: "eligible", Slot: 1})},
			want:     &core.CollectionSchedule{Days: []int{5}},
			viewerWn: everyDay,
		},
		{
			name:     "a viewer override with a When keeps it",
			spec:     targeted(MediaMovie, true, false, &core.CollectionSchedule{Days: []int{5}}),
			viewer:   []core.LayoutPage{page("home", core.RowPolicy{Row: "collection:7", Status: "eligible", When: &core.LayoutWhen{Days: []int{3}}})},
			want:     &core.CollectionSchedule{Days: []int{5}},
			viewerWn: &core.LayoutWhen{Days: []int{3}},
		},
		{
			name:   "a viewer override of an unscheduled collection needs nothing",
			spec:   targeted(MediaMovie, true, false, nil),
			viewer: []core.LayoutPage{page("home", core.RowPolicy{Row: "collection:7", Status: "eligible", Slot: 1})},
			want:   nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := core.HomeLayout{Global: c.global}
			if c.viewer != nil {
				d.Viewers = []core.ViewerLayout{{UserID: 2, Pages: c.viewer}}
			}
			before := cloneHomeLayout(d)
			out, changed, _ := foldLayoutSchedules(d, map[string]core.CollectionSpec{"7": c.spec})
			if !reflect.DeepEqual(d, before) {
				t.Fatal("input layout mutated")
			}
			got, ok := changed["7"]
			if ok != c.changed {
				t.Fatalf("changed = %v, want %v (%+v)", ok, c.changed, got)
			}
			if !ok {
				got = c.spec.Targets.Schedule
			}
			if !sameSchedule(got, c.want) {
				t.Fatalf("schedule = %+v, want %+v", got, c.want)
			}
			for _, p := range out.Global {
				for _, r := range p.Rows {
					if r.When != nil {
						t.Fatalf("Everyone policy kept its When: %+v", r)
					}
				}
			}
			if c.viewer != nil {
				w := out.Viewers[0].Pages[0].Rows[0].When
				if !reflect.DeepEqual(w, c.viewerWn) {
					t.Fatalf("viewer When = %+v, want %+v", w, c.viewerWn)
				}
			}
		})
	}
}

func TestFoldLayoutSchedulesLeavesPatternsAndOtherRows(t *testing.T) {
	glob := core.RowPolicy{Row: "collection:*", Status: "eligible", When: &core.LayoutWhen{Days: []int{1}}}
	other := core.RowPolicy{Row: "short-on-time", Status: "eligible", When: &core.LayoutWhen{Days: []int{2}}}
	gone := core.RowPolicy{Row: "collection:99", Status: "eligible", When: &core.LayoutWhen{Days: []int{3}}}
	d := core.HomeLayout{Global: []core.LayoutPage{page("home", glob, other, gone)}}
	out, changed, layoutChanged := foldLayoutSchedules(d, map[string]core.CollectionSpec{})
	if len(changed) != 0 || !layoutChanged {
		t.Fatalf("changed %v layoutChanged %v", changed, layoutChanged)
	}
	rows := out.Global[0].Rows
	if rows[0].When == nil || rows[1].When == nil || rows[2].When != nil {
		t.Fatalf("rows: %+v", rows)
	}
	if _, _, again := foldLayoutSchedules(out, map[string]core.CollectionSpec{}); again {
		t.Fatal("second fold changed the layout")
	}
}

func TestHourWindow(t *testing.T) {
	sc := func(f, to int) *core.CollectionSchedule { return &core.CollectionSchedule{FromHour: &f, ToHour: &to} }
	for _, c := range []struct {
		in       *core.CollectionSchedule
		from, to int
	}{{sc(17, 22), 17, 22}, {sc(22, 2), 22, 2}, {sc(18, 24), 18, 24}, {sc(0, 6), 0, 6}} {
		f, to, ok := hourWindow(hourMask(c.in))
		if !ok || *f != c.from || *to != c.to {
			t.Fatalf("%v-%v: got %v %v %v", *c.in.FromHour, *c.in.ToHour, f, to, ok)
		}
	}
	if f, to, ok := hourWindow(1<<24 - 1); !ok || f != nil || to != nil {
		t.Fatal("all day")
	}
	if _, _, ok := hourWindow(0); ok {
		t.Fatal("empty")
	}
	if _, _, ok := hourWindow(hourMask(sc(1, 3)) | hourMask(sc(5, 7))); ok {
		t.Fatal("two windows")
	}
	if got := overlapSchedules([]*core.CollectionSchedule{nil, nil}); got != nil {
		t.Fatalf("nil overlap: %+v", got)
	}
	if got := overlapSet([]*core.CollectionSchedule{{Days: []int{1, 2, 3}}, nil, {Days: []int{3, 2}}}, nil, func(s *core.CollectionSchedule) []int { return s.Days }); !slices.Equal(got, []int{2, 3}) {
		t.Fatalf("overlapSet: %v", got)
	}
}
