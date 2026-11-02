package collections

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/store"
)

// A collection owns the one schedule of its rows. The Collections editor
// edits it with the rest of the collection; the Home designer reads and
// writes it through CollectionSchedules and SetSchedule.

// CollectionSchedules returns every collection's schedule by public id;
// collections without one are absent.
func (s *Service) CollectionSchedules(ctx context.Context) (map[string]*core.CollectionSchedule, error) {
	rows, err := s.loadRows(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]*core.CollectionSchedule{}
	for _, r := range rows {
		if sc := r.spec.Targets.Schedule; !sc.IsZero() {
			out[r.publicID()] = sc
		}
	}
	return out, nil
}

// ValidateSchedule normalizes and checks a schedule without saving it.
func (s *Service) ValidateSchedule(sc *core.CollectionSchedule) error {
	return ValidateSchedule(NormalizeSchedule(cloneSchedule(sc)))
}

// SetSchedule replaces one collection's schedule (nil clears it), leaving
// the rest of the collection and its members as they are.
func (s *Service) SetSchedule(ctx context.Context, id string, sc *core.CollectionSchedule) error {
	row, err := s.rowByID(ctx, id)
	if err != nil {
		return err
	}
	sc = NormalizeSchedule(cloneSchedule(sc))
	if err := ValidateSchedule(sc); err != nil {
		return err
	}
	sp := row.spec
	sp.Targets.Schedule = sc
	js, err := specJSON(sp)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE collections_specs SET spec_json=?, updated_at=? WHERE id=?`, js, s.now().Unix(), row.id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return core.ErrNotFound
	}
	s.changed(row.id)
	return nil
}

func cloneSchedule(sc *core.CollectionSchedule) *core.CollectionSchedule {
	if sc == nil {
		return nil
	}
	c := *sc
	c.Days = slices.Clone(sc.Days)
	c.Devices = slices.Clone(sc.Devices)
	c.Dayparts = slices.Clone(sc.Dayparts)
	c.Months = slices.Clone(sc.Months)
	if sc.FromHour != nil {
		h := *sc.FromHour
		c.FromHour = &h
	}
	if sc.ToHour != nil {
		h := *sc.ToHour
		c.ToHour = &h
	}
	return &c
}

// HomeLayoutStore is the Home layout document the one-time schedule
// carry-over reads and rewrites. Implemented by internal/personalize.
type HomeLayoutStore interface {
	HomeLayout(context.Context) (core.HomeLayout, error)
	SetHomeLayout(context.Context, core.HomeLayout) (core.HomeLayout, error)
}

const layoutSchedulesMeta = "layout-schedules-v1"

// AdoptLayoutSchedules is the one-time carry-over from the time the Home
// layout kept its own schedule for collection rows: it folds each Everyone
// schedule of a collection row into the collection's schedule, preserving
// what viewers see (see foldLayoutSchedules), and removes it from the
// layout. It runs once per database.
func (s *Service) AdoptLayoutSchedules(ctx context.Context, layouts HomeLayoutStore) error {
	unlock, err := store.SessionLock(ctx, s.db, "collections:layout-schedules")
	if err != nil {
		return err
	}
	if unlock != nil {
		defer unlock()
	}
	if s.meta(ctx, layoutSchedulesMeta) != "" {
		return nil
	}
	d, err := layouts.HomeLayout(ctx)
	if err != nil {
		return err
	}
	rows, err := s.loadRows(ctx)
	if err != nil {
		return err
	}
	specs := make(map[string]core.CollectionSpec, len(rows))
	ids := make(map[string]int64, len(rows))
	for _, r := range rows {
		specs[r.publicID()] = r.spec
		ids[r.publicID()] = r.id
	}
	next, schedules, layoutChanged := foldLayoutSchedules(d, specs)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var touched []int64
	for id, sc := range schedules {
		sp := specs[id]
		sp.Targets.Schedule = sc
		js, err := specJSON(sp)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE collections_specs SET spec_json=?, updated_at=? WHERE id=?`, js, s.now().Unix(), ids[id]); err != nil {
			return err
		}
		touched = append(touched, ids[id])
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for _, id := range touched {
		s.changed(id)
	}
	if layoutChanged {
		if _, err := layouts.SetHomeLayout(ctx, next); err != nil {
			return fmt.Errorf("collection schedules: rewrite Home layout: %w", err)
		}
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO collections_meta(k,v) VALUES(?,'1') ON CONFLICT(k) DO UPDATE SET v=excluded.v`, layoutSchedulesMeta); err != nil {
		return err
	}
	if len(touched) > 0 || layoutChanged {
		s.log.Info("collections: moved Home layout schedules onto their collections", "collections", len(touched))
	}
	return nil
}

// collectionRowID returns the collection id an exact layout row names
// ("collection:<id>" or "collection.<id>"); patterns return "".
func collectionRowID(row string) string {
	for _, p := range []string{"collection:", "collection."} {
		if id, ok := strings.CutPrefix(row, p); ok && id != "" && !strings.ContainsAny(id, "*?[") {
			return id
		}
	}
	return ""
}

// rowSections is where a collection of a media type can appear.
func rowSections(mediaType string) []string {
	switch mediaType {
	case MediaMovie:
		return []string{"home", "movies"}
	case MediaShow:
		return []string{"home", "shows"}
	}
	return []string{"home", "movies", "shows"}
}

// foldLayoutSchedules computes the carry-over. Before, an exact Everyone
// (or viewer) policy for a collection row on a page replaced the
// collection's schedule on that page with the policy's When (none = any
// time); pages without one used the collection's schedule. The one
// schedule each collection keeps is the overlap of the schedules that
// governed it on every page it appeared on; a field with no overlap takes
// the Home page's value. Everyone policies lose their When. A viewer
// override that had no When kept that viewer on "any time"; it gets an
// every-day When so it still overrides the collection's schedule. It
// returns the new layout, the collections whose schedule changes, and
// whether the layout changed.
func foldLayoutSchedules(d core.HomeLayout, specs map[string]core.CollectionSpec) (core.HomeLayout, map[string]*core.CollectionSchedule, bool) {
	exact := func(pages []core.LayoutPage, section, id string) *core.RowPolicy {
		for i := range pages {
			if pages[i].Section != section {
				continue
			}
			for j := range pages[i].Rows {
				if collectionRowID(pages[i].Rows[j].Row) == id {
					return &pages[i].Rows[j]
				}
			}
		}
		return nil
	}
	ids := map[string]bool{}
	for _, p := range d.Global {
		for _, r := range p.Rows {
			if id := collectionRowID(r.Row); id != "" {
				ids[id] = true
			}
		}
	}
	changed := map[string]*core.CollectionSchedule{}
	final := map[string]*core.CollectionSchedule{}
	for id := range ids {
		sp, ok := specs[id]
		if !ok {
			continue
		}
		cur := sp.Targets.Schedule
		var surfaces []*core.CollectionSchedule
		governed := false
		for _, sec := range rowSections(sp.MediaType) {
			if p := exact(d.Global, sec, id); p != nil {
				if p.Status == "off" {
					continue
				}
				governed = true
				surfaces = append(surfaces, whenSchedule(p.When))
				continue
			}
			if sec == "home" && sp.Targets.HomeRow || sec != "home" && sp.Targets.SectionRow {
				surfaces = append(surfaces, cur)
			}
		}
		next := cur
		if governed {
			next = overlapSchedules(surfaces)
		}
		final[id] = next
		if !sameSchedule(cur, next) {
			changed[id] = next
		}
	}
	out := cloneHomeLayout(d)
	layoutChanged := false
	for i := range out.Global {
		for j := range out.Global[i].Rows {
			if r := &out.Global[i].Rows[j]; r.When != nil && collectionRowID(r.Row) != "" {
				r.When = nil
				layoutChanged = true
			}
		}
	}
	for i := range out.Viewers {
		for j := range out.Viewers[i].Pages {
			for k := range out.Viewers[i].Pages[j].Rows {
				r := &out.Viewers[i].Pages[j].Rows[k]
				id := collectionRowID(r.Row)
				if id == "" || r.When != nil || r.Status == "off" {
					continue
				}
				sc, ok := final[id]
				if !ok {
					sc = specs[id].Targets.Schedule
				}
				if !sc.IsZero() {
					r.When = &core.LayoutWhen{Days: []int{0, 1, 2, 3, 4, 5, 6}}
					layoutChanged = true
				}
			}
		}
	}
	return out, changed, layoutChanged
}

func cloneHomeLayout(d core.HomeLayout) core.HomeLayout {
	out := d
	clonePages := func(ps []core.LayoutPage) []core.LayoutPage {
		ps = slices.Clone(ps)
		for i := range ps {
			ps[i].Rows = slices.Clone(ps[i].Rows)
		}
		return ps
	}
	out.Global = clonePages(d.Global)
	out.Viewers = slices.Clone(d.Viewers)
	for i := range out.Viewers {
		out.Viewers[i].Pages = clonePages(out.Viewers[i].Pages)
	}
	return out
}

// whenSchedule converts a layout When to a collection schedule.
func whenSchedule(w *core.LayoutWhen) *core.CollectionSchedule {
	if w == nil {
		return nil
	}
	return NormalizeSchedule(&core.CollectionSchedule{
		Days: slices.Clone(w.Days), FromHour: w.FromHour, ToHour: w.ToHour, Devices: slices.Clone(w.Devices),
		Dayparts: slices.Clone(w.Dayparts), Months: slices.Clone(w.Months), StartDate: w.StartDate, EndDate: w.EndDate,
	})
}

func sameSchedule(a, b *core.CollectionSchedule) bool {
	if a.IsZero() || b.IsZero() {
		return a.IsZero() == b.IsZero()
	}
	eqInt := func(x, y *int) bool { return x == nil && y == nil || x != nil && y != nil && *x == *y }
	return slices.Equal(a.Days, b.Days) && eqInt(a.FromHour, b.FromHour) && eqInt(a.ToHour, b.ToHour) &&
		slices.Equal(a.Devices, b.Devices) && slices.Equal(a.Dayparts, b.Dayparts) && slices.Equal(a.Months, b.Months) &&
		a.StartDate == b.StartDate && a.EndDate == b.EndDate
}

// overlapSchedules intersects schedules (nil = any time). The first
// schedule is the preferred one: a field whose values do not overlap, or
// whose hours overlap in more than one window, takes its value from it.
func overlapSchedules(scs []*core.CollectionSchedule) *core.CollectionSchedule {
	if len(scs) == 0 {
		return nil
	}
	pref := scs[0]
	if pref == nil {
		pref = &core.CollectionSchedule{}
	}
	out := &core.CollectionSchedule{}
	out.Days = overlapSet(scs, pref.Days, func(s *core.CollectionSchedule) []int { return s.Days })
	out.Months = overlapSet(scs, pref.Months, func(s *core.CollectionSchedule) []int { return s.Months })
	out.Devices = overlapSet(scs, pref.Devices, func(s *core.CollectionSchedule) []string { return s.Devices })
	out.Dayparts = overlapSet(scs, pref.Dayparts, func(s *core.CollectionSchedule) []string { return s.Dayparts })
	// Hours.
	mask := uint32(1<<24 - 1)
	for _, s := range scs {
		if s != nil {
			mask &= hourMask(s)
		}
	}
	if from, to, ok := hourWindow(mask); ok {
		out.FromHour, out.ToHour = from, to
	} else {
		out.FromHour, out.ToHour = pref.FromHour, pref.ToHour
	}
	// Dates.
	start, end := "", ""
	for _, s := range scs {
		if s == nil {
			continue
		}
		if s.StartDate > start {
			start = s.StartDate
		}
		if s.EndDate != "" && (end == "" || s.EndDate < end) {
			end = s.EndDate
		}
	}
	if start != "" && end != "" && start > end {
		start, end = pref.StartDate, pref.EndDate
	}
	out.StartDate, out.EndDate = start, end
	return NormalizeSchedule(out)
}

// overlapSet intersects one list field across schedules (empty = any). An
// empty overlap of non-empty lists falls back to the preferred value.
func overlapSet[T comparable](scs []*core.CollectionSchedule, pref []T, field func(*core.CollectionSchedule) []T) []T {
	var acc []T
	set := false
	for _, s := range scs {
		if s == nil || len(field(s)) == 0 {
			continue
		}
		if !set {
			acc, set = slices.Clone(field(s)), true
			continue
		}
		acc = slices.DeleteFunc(acc, func(v T) bool { return !slices.Contains(field(s), v) })
	}
	if set && len(acc) == 0 {
		return slices.Clone(pref)
	}
	return acc
}

// hourMask is the set of local hours a schedule's hour window allows.
func hourMask(sc *core.CollectionSchedule) uint32 {
	if sc.FromHour == nil && sc.ToHour == nil {
		return 1<<24 - 1
	}
	probe := &core.CollectionSchedule{FromHour: sc.FromHour, ToHour: sc.ToHour}
	var m uint32
	for h := 0; h < 24; h++ {
		if probe.Allows("", hourAt(h)) {
			m |= 1 << h
		}
	}
	return m
}

// hourWindow turns an hour set back into one [from, to) window, wrapping
// past midnight; nil hours mean all day. ok is false for an empty set or
// more than one window.
func hourWindow(m uint32) (from, to *int, ok bool) {
	const all = 1<<24 - 1
	if m == all {
		return nil, nil, true
	}
	if m == 0 {
		return nil, nil, false
	}
	start, starts := -1, 0
	for h := 0; h < 24; h++ {
		prev := (h + 23) % 24
		if m&(1<<h) != 0 && m&(1<<prev) == 0 {
			start = h
			starts++
		}
	}
	if starts != 1 {
		return nil, nil, false
	}
	end := start
	for m&(1<<(end%24)) != 0 {
		end++
	}
	f, t := start, end%24
	if t == 0 {
		t = 24
	}
	return &f, &t, true
}

// hourAt is a fixed day at hour h, for hour-window arithmetic.
func hourAt(h int) time.Time { return time.Date(2024, time.January, 1, h, 0, 0, 0, time.UTC) }
