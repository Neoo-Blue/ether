package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// PlexWatchStatus is the viewer-facing connection. It never includes a token.
type PlexWatchStatus struct {
	Connected      bool   `json:"connected"`
	PlexID         int64  `json:"plexId,omitempty"`
	Username       string `json:"username,omitempty"`
	Direction      int    `json:"direction"`
	ImportInterval int    `json:"importInterval"`
	LastImport     int64  `json:"lastImport"`
	LastSend       int64  `json:"lastSend"`
	NextImport     int64  `json:"nextImport"`
	Problem        string `json:"problem"`
	ProblemCode    int    `json:"problemCode"`
	Eligibility    string `json:"eligibility"`
	Revoked        bool   `json:"revoked"`
	Pending        int    `json:"pending"`
	Held           int    `json:"held"`
	HeldLegacy     int    `json:"heldLegacy"`
	Dead           int    `json:"dead"`
	InFlight       bool   `json:"inFlight"`
	Skipped        int    `json:"skipped"`
	Restricted     bool   `json:"restricted"`
	PINCode        string `json:"pinCode,omitempty"`
	PINURL         string `json:"pinUrl,omitempty"`
	PINExpires     string `json:"pinExpires,omitempty"`
	AccountMatch   bool   `json:"accountMatch"`
}

// Sentinels returned by the app adapter so this package does not import
// the watch client.
var (
	ErrPlexAccountMismatch = errors.New("plex account does not belong to this viewer")
	ErrPlexNotEligible     = errors.New("account has not allowed watch-state sync")
	ErrPlexWatchRevoked    = errors.New("plex rejected this viewer's token")
)

// PlexWatch is the per-viewer account watch-state integration. Handlers
// always pass the stored viewer's Plex account id, never the owner's.
type PlexWatch interface {
	Status(ctx context.Context, userID, plexID int64) (PlexWatchStatus, error)
	BeginPIN(ctx context.Context, userID, plexID int64) (PlexWatchStatus, error)
	FinishPIN(ctx context.Context, userID, plexID int64) (PlexWatchStatus, error)
	SetDirection(ctx context.Context, userID, plexID int64, direction, interval int) (PlexWatchStatus, error)
	Disconnect(ctx context.Context, userID, plexID int64) (PlexWatchStatus, error)
	Retry(ctx context.Context, userID, plexID int64) (PlexWatchStatus, error)
	Resolve(ctx context.Context, userID, plexID int64) (PlexWatchStatus, error)
	ExportCurrent(ctx context.Context, userID, plexID int64) (PlexWatchStatus, error)
	ImportCurrent(ctx context.Context, userID, plexID int64) (PlexWatchStatus, error)
	Unmatched(ctx context.Context, userID, plexID int64, after string) ([]string, error)
}

func (h *Handler) plexWatchViewer(w http.ResponseWriter, r *http.Request) (*ViewerRecord, bool) {
	if h.deps.PlexWatch == nil {
		writeError(w, http.StatusNotFound, "not_found", "plex account watch state is not available")
		return nil, false
	}
	v, ok := h.viewerByID(w, r)
	if !ok {
		return nil, false
	}
	if v.PlexID <= 0 {
		writeError(w, http.StatusBadRequest, "no_plex_id", "this viewer has no Plex account id")
		return nil, false
	}
	return v, true
}

func (h *Handler) writePlexWatch(w http.ResponseWriter, st PlexWatchStatus, err error) {
	if err == nil {
		writeJSON(w, http.StatusOK, st)
		return
	}
	switch {
	case errors.Is(err, ErrPlexAccountMismatch):
		writeError(w, http.StatusConflict, "account_mismatch", "the Plex account that approved the code is not this viewer")
	case errors.Is(err, ErrPlexNotEligible):
		writeError(w, http.StatusForbidden, "not_eligible", "Plex has not allowed watch-state sync for this account")
	case errors.Is(err, ErrPlexWatchRevoked):
		writeError(w, http.StatusUnauthorized, "revoked", "Plex rejected this viewer's token")
	default:
		writeError(w, http.StatusBadRequest, "plex_watch", "plex watch state could not be updated")
	}
}

func (h *Handler) handlePlexWatchStatus(w http.ResponseWriter, r *http.Request) {
	v, ok := h.plexWatchViewer(w, r)
	if !ok {
		return
	}
	st, err := h.deps.PlexWatch.Status(r.Context(), v.ID, v.PlexID)
	h.writePlexWatch(w, st, err)
}

func (h *Handler) handlePlexWatchPIN(w http.ResponseWriter, r *http.Request) {
	v, ok := h.plexWatchViewer(w, r)
	if !ok {
		return
	}
	st, err := h.deps.PlexWatch.BeginPIN(r.Context(), v.ID, v.PlexID)
	h.writePlexWatch(w, st, err)
}

func (h *Handler) handlePlexWatchPINFinish(w http.ResponseWriter, r *http.Request) {
	v, ok := h.plexWatchViewer(w, r)
	if !ok {
		return
	}
	st, err := h.deps.PlexWatch.FinishPIN(r.Context(), v.ID, v.PlexID)
	h.writePlexWatch(w, st, err)
}

func (h *Handler) handlePlexWatchEnabled(w http.ResponseWriter, r *http.Request) {
	v, ok := h.plexWatchViewer(w, r)
	if !ok {
		return
	}
	var body struct {
		Direction int `json:"direction"`
		Interval  int `json:"interval"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", "expected a JSON object with enabled")
		return
	}
	st, err := h.deps.PlexWatch.SetDirection(r.Context(), v.ID, v.PlexID, body.Direction, body.Interval)
	h.writePlexWatch(w, st, err)
}

func (h *Handler) handlePlexWatchDisconnect(w http.ResponseWriter, r *http.Request) {
	v, ok := h.plexWatchViewer(w, r)
	if !ok {
		return
	}
	st, err := h.deps.PlexWatch.Disconnect(r.Context(), v.ID, v.PlexID)
	h.writePlexWatch(w, st, err)
}

func (h *Handler) handlePlexWatchRetry(w http.ResponseWriter, r *http.Request) {
	v, ok := h.plexWatchViewer(w, r)
	if !ok {
		return
	}
	st, err := h.deps.PlexWatch.Retry(r.Context(), v.ID, v.PlexID)
	h.writePlexWatch(w, st, err)
}

func (h *Handler) handlePlexWatchResolve(w http.ResponseWriter, r *http.Request) {
	v, ok := h.plexWatchViewer(w, r)
	if !ok {
		return
	}
	st, err := h.deps.PlexWatch.Resolve(r.Context(), v.ID, v.PlexID)
	h.writePlexWatch(w, st, err)
}

func (h *Handler) handlePlexWatchExport(w http.ResponseWriter, r *http.Request) {
	v, ok := h.plexWatchViewer(w, r)
	if !ok {
		return
	}
	st, err := h.deps.PlexWatch.ExportCurrent(r.Context(), v.ID, v.PlexID)
	h.writePlexWatch(w, st, err)
}

func (h *Handler) handlePlexWatchImport(w http.ResponseWriter, r *http.Request) {
	v, ok := h.plexWatchViewer(w, r)
	if !ok {
		return
	}
	st, err := h.deps.PlexWatch.ImportCurrent(r.Context(), v.ID, v.PlexID)
	h.writePlexWatch(w, st, err)
}

func (h *Handler) handlePlexWatchUnmatched(w http.ResponseWriter, r *http.Request) {
	v, ok := h.plexWatchViewer(w, r)
	if !ok {
		return
	}
	after := r.URL.Query().Get("after")
	if len(after) > 256 {
		writeError(w, http.StatusBadRequest, "bad_cursor", "invalid cursor")
		return
	}
	items, err := h.deps.PlexWatch.Unmatched(r.Context(), v.ID, v.PlexID, after)
	if err != nil {
		h.writePlexWatch(w, PlexWatchStatus{}, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}
