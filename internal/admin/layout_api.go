package admin

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
)

// collectionScheduler is the Collection Studio capability the Home
// designer uses for collection rows: a collection owns its row schedule,
// and the layout document carries it as collectionSchedules.
type collectionScheduler interface {
	CollectionSchedules(ctx context.Context) (map[string]*core.CollectionSchedule, error)
	ValidateSchedule(sc *core.CollectionSchedule) error
	SetSchedule(ctx context.Context, id string, sc *core.CollectionSchedule) error
}

func (h *Handler) scheduler() collectionScheduler {
	s, _ := h.deps.Collections.(collectionScheduler)
	return s
}

// writeLayout answers with the saved Home layout document (with every
// collection's schedule), the engine's per-section row defaults and the
// rows the document can target.
func (h *Handler) writeLayout(w http.ResponseWriter, r *http.Request, d core.HomeLayout) {
	avail, err := h.deps.Personalization.AvailableRows(r.Context())
	if err != nil {
		writeEngineError(w, err)
		return
	}
	d.CollectionSchedules = nil
	if s := h.scheduler(); s != nil {
		if d.CollectionSchedules, err = s.CollectionSchedules(r.Context()); err != nil {
			h.writeServiceError(w, err)
			return
		}
	}
	if avail == nil {
		avail = []core.RowDescriptor{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"layout":        d,
		"defaults":      map[string]any{"maxRows": h.deps.Personalization.LayoutDefaults()},
		"availableRows": avail,
	})
}
func (h *Handler) handleGetLayout(w http.ResponseWriter, r *http.Request) {
	d, err := h.deps.Personalization.HomeLayout(r.Context())
	if err != nil {
		writeEngineError(w, err)
		return
	}
	h.writeLayout(w, r, d)
}
func (h *Handler) handlePutLayout(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Layout *core.HomeLayout `json:"layout"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, 400, "bad_json", err.Error())
		return
	}
	if body.Layout == nil {
		writeValidationError(w, []FieldError{{Field: "layout", Message: "is required"}})
		return
	}
	// Edited collection schedules (null clears one) are saved to their
	// collections once the layout itself is valid and saved.
	schedules := body.Layout.CollectionSchedules
	body.Layout.CollectionSchedules = nil
	sched := h.scheduler()
	if len(schedules) > 0 {
		if sched == nil {
			writeValidationError(w, []FieldError{{Field: "layout.collectionSchedules", Message: "collections are unavailable"}})
			return
		}
		var fields []FieldError
		for _, id := range slices.Sorted(maps.Keys(schedules)) {
			var invalid interface{ InvalidFields() map[string]string }
			if err := sched.ValidateSchedule(schedules[id]); errors.As(err, &invalid) {
				for _, f := range slices.Sorted(maps.Keys(invalid.InvalidFields())) {
					fields = append(fields, FieldError{Field: "layout.collectionSchedules." + id + "." + f, Message: invalid.InvalidFields()[f]})
				}
			} else if err != nil {
				h.writeServiceError(w, err)
				return
			}
		}
		if len(fields) > 0 {
			writeValidationError(w, fields)
			return
		}
	}
	d, err := h.deps.Personalization.SetHomeLayout(r.Context(), *body.Layout)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	for _, id := range slices.Sorted(maps.Keys(schedules)) {
		// A collection deleted since the designer loaded has no schedule to keep.
		if err := sched.SetSchedule(r.Context(), id, schedules[id]); err != nil && !errors.Is(err, core.ErrNotFound) {
			h.writeServiceError(w, err)
			return
		}
	}
	if len(schedules) > 0 {
		h.invalidateOwnerRows()
	}
	h.writeLayout(w, r, d)
}

type layoutPreviewBody struct {
	ViewerID *int64           `json:"viewerId"`
	Layout   *core.HomeLayout `json:"layout"`
	Device   string           `json:"device"`
	At       string           `json:"at"`
	Section  string           `json:"section"`
	Count    int              `json:"count"`
}

func (h *Handler) decodeLayoutPreview(w http.ResponseWriter, r *http.Request) (layoutPreviewBody, core.HubContext, Personalization, bool) {
	var b layoutPreviewBody
	var hc core.HubContext
	e := h.deps.Personalization
	if err := decodeJSON(r, &b); err != nil {
		writeError(w, 400, "bad_json", err.Error())
		return b, hc, e, false
	}
	if b.Device == "" {
		b.Device = core.DeviceTV
	}
	switch b.Device {
	case core.DeviceTV, core.DeviceMobile, core.DeviceWeb, core.DeviceDesktop:
	default:
		writeValidationError(w, []FieldError{{Field: "device", Message: "must be tv, mobile, web or desktop"}})
		return b, hc, e, false
	}
	at := time.Now()
	if b.At != "" {
		var err error
		at, err = time.Parse(time.RFC3339, b.At)
		if err != nil {
			writeValidationError(w, []FieldError{{Field: "at", Message: "must be an RFC 3339 time"}})
			return b, hc, e, false
		}
	}
	if b.Count == 0 {
		b.Count = 12
	}
	if b.Count < 1 || b.Count > 100 {
		writeValidationError(w, []FieldError{{Field: "count", Message: "must be 1-100"}})
		return b, hc, e, false
	}
	if b.Layout == nil {
		d, err := e.HomeLayout(r.Context())
		if err != nil {
			writeEngineError(w, err)
			return b, hc, e, false
		}
		b.Layout = &d
	}
	hc = core.NewHubContext(at.Local(), "", "", "")
	hc.Device = b.Device
	return b, hc, e, true
}
func (h *Handler) handleLayoutPreview(w http.ResponseWriter, r *http.Request) {
	b, hc, e, ok := h.decodeLayoutPreview(w, r)
	if !ok {
		return
	}
	u := &core.User{IsOwner: true}
	var viewer any
	if b.ViewerID != nil {
		users, err := h.storedViewers(r.Context())
		if err != nil {
			writeEngineError(w, err)
			return
		}
		found := false
		for _, v := range users {
			if v.ID == *b.ViewerID {
				copy := v.User
				u = &copy
				found = true
				viewer = map[string]any{"id": v.ID, "title": toViewer(v, core.ViewerStats{}).Title}
				break
			}
		}
		if !found {
			writeError(w, 404, "not_found", "no such viewer")
			return
		}
	}
	p, err := e.PreviewLayout(r.Context(), u, hc, b.Section, b.Count, *b.Layout)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	h.writeHomePreview(w, p, viewer)
}
func overrideSettings(d core.HomeLayout, id int64) int {
	n := 0
	for _, v := range d.Viewers {
		if v.UserID == id {
			for _, p := range v.Pages {
				n += len(p.Rows) + len(p.DeviceMaxRows)
				if p.MaxRows > 0 {
					n++
				}
			}
		}
	}
	return n
}
func (h *Handler) handleLayoutImpact(w http.ResponseWriter, r *http.Request) {
	b, hc, e, ok := h.decodeLayoutPreview(w, r)
	if !ok {
		return
	}
	users, err := h.storedViewers(r.Context())
	if err != nil {
		writeEngineError(w, err)
		return
	}
	saved, err := e.HomeLayout(r.Context())
	if err != nil {
		writeEngineError(w, err)
		return
	}
	results := []map[string]any{}
	overrides, changed := 0, 0
	for _, u := range users {
		old, err := e.PreviewLayout(r.Context(), &u.User, hc, b.Section, b.Count, saved)
		if err != nil {
			writeEngineError(w, err)
			return
		}
		next, err := e.PreviewLayout(r.Context(), &u.User, hc, b.Section, b.Count, *b.Layout)
		if err != nil {
			writeEngineError(w, err)
			return
		}
		before, after := map[string]int{}, map[string]int{}
		for _, row := range old.Rows {
			if !row.Hidden {
				before[row.HubID] = len(before) + 1
			}
		}
		for _, row := range next.Rows {
			if !row.Hidden {
				after[row.HubID] = len(after) + 1
			}
		}
		added, removed := []string{}, []string{}
		moved := 0
		for _, row := range next.Rows {
			if row.Hidden {
				continue
			}
			if pos := before[row.HubID]; pos == 0 {
				added = append(added, row.Title)
			} else if pos != after[row.HubID] {
				moved++
			}
		}
		for _, row := range old.Rows {
			if !row.Hidden && after[row.HubID] == 0 {
				removed = append(removed, row.Title)
			}
		}
		n := overrideSettings(*b.Layout, u.ID)
		if n > 0 {
			overrides++
		}
		diff := len(added)+len(removed)+moved > 0
		if diff {
			changed++
		}
		results = append(results, map[string]any{"viewerId": u.ID, "title": toViewer(u, core.ViewerStats{}).Title, "overrides": n, "changed": diff, "added": added, "removed": removed, "moved": moved})
	}
	writeJSON(w, 200, map[string]any{"viewers": results, "summary": map[string]int{"viewers": len(users), "withOverrides": overrides, "changed": changed}})
}
