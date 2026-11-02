package core

import (
	"testing"
	"time"
)

func TestCollectionScheduleAllows(t *testing.T) {
	fri21 := time.Date(2026, 10, 16, 21, 30, 0, 0, time.UTC) // Friday, October, evening
	h := func(n int) *int { return &n }
	cases := []struct {
		sc     *CollectionSchedule
		device string
		at     time.Time
		want   bool
	}{
		{nil, "tv", fri21, true},
		{&CollectionSchedule{Days: []int{5}}, "", fri21, true},
		{&CollectionSchedule{Days: []int{0, 6}}, "", fri21, false},
		{&CollectionSchedule{FromHour: h(17), ToHour: h(22)}, "", fri21, true},
		{&CollectionSchedule{FromHour: h(17), ToHour: h(21)}, "", fri21, false},
		{&CollectionSchedule{FromHour: h(22), ToHour: h(2)}, "", fri21, false},
		{&CollectionSchedule{FromHour: h(22), ToHour: h(2)}, "", fri21.Add(3 * time.Hour), true},
		{&CollectionSchedule{FromHour: h(9), ToHour: h(9)}, "", fri21, true},
		{&CollectionSchedule{FromHour: h(18), ToHour: h(24)}, "", fri21, true},
		{&CollectionSchedule{Devices: []string{"tv"}}, "mobile", fri21, false},
		{&CollectionSchedule{Devices: []string{"tv"}}, "", fri21, true},
		{&CollectionSchedule{Dayparts: []string{DaypartEvening}}, "", fri21, true},
		{&CollectionSchedule{Dayparts: []string{DaypartMorning, DaypartLate}}, "", fri21, false},
		{&CollectionSchedule{Months: []int{10, 12}}, "", fri21, true},
		{&CollectionSchedule{Months: []int{12}}, "", fri21, false},
		{&CollectionSchedule{StartDate: "2026-10-16", EndDate: "2026-10-16"}, "", fri21, true},
		{&CollectionSchedule{StartDate: "2026-10-17"}, "", fri21, false},
		{&CollectionSchedule{EndDate: "2026-10-15"}, "", fri21, false},
	}
	for i, c := range cases {
		if got := c.sc.Allows(c.device, c.at); got != c.want {
			t.Errorf("case %d: got %v want %v", i, got, c.want)
		}
	}
	if !(*CollectionSchedule)(nil).IsZero() || !(&CollectionSchedule{}).IsZero() || (&CollectionSchedule{Months: []int{1}}).IsZero() {
		t.Fatal("IsZero")
	}
}
