package admin

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/braedonsaunders/ether/internal/prerolls"
)

// Prerolls is the owner pre-roll service (*prerolls.Service).
type Prerolls interface {
	List(ctx context.Context) ([]prerolls.Preroll, string, error)
	Get(ctx context.Context, id string) (prerolls.Preroll, error)
	SetSelection(ctx context.Context, mode string) error
	Create(ctx context.Context, in prerolls.Input) (prerolls.Preroll, error)
	Update(ctx context.Context, id string, in prerolls.Input) (prerolls.Preroll, error)
	Delete(ctx context.Context, id string) error
	Upload(ctx context.Context, id string, r io.Reader) (prerolls.Preroll, error)
	AttachPath(ctx context.Context, id, path string) (prerolls.Preroll, error)
	Plays(ctx context.Context, id string, limit int) ([]prerolls.PlayRow, error)
	Simulate(ctx context.Context, in prerolls.SimulateInput) (prerolls.Decision, error)
	ImportPMS(ctx context.Context, value string) ([]prerolls.Preroll, error)
	Files(ctx context.Context, id string) (video, thumb string, err error)
}

var _ Prerolls = (*prerolls.Service)(nil)

// prerollRoutes registers the pre-roll API. Every route needs the owner
// token; the console fetches the thumbnail and the range-capable preview
// with it (no cookies).
func (h *Handler) prerollRoutes(api func(string, http.HandlerFunc)) {
	if h.deps.Prerolls == nil {
		return
	}
	api("GET /api/admin/prerolls", h.handlePrerollList)
	api("PUT /api/admin/prerolls/selection", h.handlePrerollSelection)
	api("POST /api/admin/prerolls", h.handlePrerollCreate)
	api("POST /api/admin/prerolls/simulate", h.handlePrerollSimulate)
	api("POST /api/admin/prerolls/import/pms", h.handlePrerollImport)
	api("PUT /api/admin/prerolls/{id}", h.handlePrerollUpdate)
	api("DELETE /api/admin/prerolls/{id}", h.handlePrerollDelete)
	api("POST /api/admin/prerolls/{id}/video", h.handlePrerollVideo)
	api("GET /api/admin/prerolls/{id}/plays", h.handlePrerollPlays)
	api("GET /api/admin/prerolls/{id}/thumb", h.handlePrerollThumb)
	api("GET /api/admin/prerolls/{id}/preview", h.handlePrerollPreview)
}

// prerollVideoUpload reports whether path is a pre-roll video upload,
// which browsers send as multipart/form-data.
func prerollVideoUpload(path string) bool {
	rest, ok := strings.CutPrefix(path, "/api/admin/prerolls/")
	if !ok {
		return false
	}
	id, tail, ok := strings.Cut(rest, "/")
	return ok && id != "" && tail == "video"
}

func (h *Handler) prerollError(w http.ResponseWriter, err error) {
	var ve *prerolls.ValidationError
	switch {
	case errors.As(err, &ve):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{
			"code": "validation", "message": ve.Message, "fields": []FieldError{{Field: ve.Field, Message: ve.Message}},
		}})
	case errors.Is(err, prerolls.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "the video is larger than the pre-roll size limit")
	case errors.Is(err, prerolls.ErrStorageFull):
		writeError(w, http.StatusInsufficientStorage, "storage_full", "the pre-roll folder is full; delete a pre-roll first")
	case errors.Is(err, prerolls.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "no such pre-roll")
	default:
		h.log.Warn("pre-roll request", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "the pre-roll request failed")
	}
}

func (h *Handler) handlePrerollList(w http.ResponseWriter, r *http.Request) {
	list, selection, err := h.deps.Prerolls.List(r.Context())
	if err != nil {
		h.prerollError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"prerolls": list, "selection": selection})
}

func (h *Handler) handlePrerollSelection(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Selection string `json:"selection"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	if err := h.deps.Prerolls.SetSelection(r.Context(), body.Selection); err != nil {
		h.prerollError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"selection": body.Selection})
}

func (h *Handler) handlePrerollCreate(w http.ResponseWriter, r *http.Request) {
	var in prerolls.Input
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	p, err := h.deps.Prerolls.Create(r.Context(), in)
	if err != nil {
		h.prerollError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (h *Handler) handlePrerollUpdate(w http.ResponseWriter, r *http.Request) {
	var in prerolls.Input
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	p, err := h.deps.Prerolls.Update(r.Context(), r.PathValue("id"), in)
	if err != nil {
		h.prerollError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *Handler) handlePrerollDelete(w http.ResponseWriter, r *http.Request) {
	if err := h.deps.Prerolls.Delete(r.Context(), r.PathValue("id")); err != nil {
		h.prerollError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePrerollVideo takes a multipart "file" (streamed to disk, never
// buffered) or a JSON {"path": "..."} on the server.
func (h *Handler) handlePrerollVideo(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ct == "multipart/form-data" {
		mr, err := r.MultipartReader()
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_upload", "send the video as a multipart file field")
			return
		}
		for {
			part, err := mr.NextPart()
			if err != nil {
				writeError(w, http.StatusBadRequest, "bad_upload", "the upload has no file field")
				return
			}
			if part.FormName() != "file" {
				_ = part.Close()
				continue
			}
			p, err := h.deps.Prerolls.Upload(r.Context(), id, part)
			_ = part.Close()
			if err != nil {
				h.prerollError(w, err)
				return
			}
			writeJSON(w, http.StatusAccepted, p)
			return
		}
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	p, err := h.deps.Prerolls.AttachPath(r.Context(), id, body.Path)
	if err != nil {
		h.prerollError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, p)
}

func (h *Handler) handlePrerollPlays(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	plays, err := h.deps.Prerolls.Plays(r.Context(), r.PathValue("id"), limit)
	if err != nil {
		h.prerollError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plays": plays})
}

func (h *Handler) handlePrerollSimulate(w http.ResponseWriter, r *http.Request) {
	var in prerolls.SimulateInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	dec, err := h.deps.Prerolls.Simulate(r.Context(), in)
	if err != nil {
		h.prerollError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dec)
}

func (h *Handler) handlePrerollImport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Paths string `json:"paths"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	created, err := h.deps.Prerolls.ImportPMS(r.Context(), body.Paths)
	if err != nil && len(created) == 0 {
		h.prerollError(w, err)
		return
	}
	if err != nil {
		h.log.Warn("pre-roll import stopped early", "created", len(created), "err", err)
	}
	writeJSON(w, http.StatusCreated, map[string]any{"created": created})
}

func (h *Handler) servePrerollFile(w http.ResponseWriter, r *http.Request, thumb bool) {
	video, still, err := h.deps.Prerolls.Files(r.Context(), r.PathValue("id"))
	path, ctype := video, "video/mp4"
	if thumb {
		path, ctype = still, "image/jpeg"
	}
	if err != nil || path == "" {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Security-Policy", "sandbox")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

func (h *Handler) handlePrerollThumb(w http.ResponseWriter, r *http.Request) {
	h.servePrerollFile(w, r, true)
}

func (h *Handler) handlePrerollPreview(w http.ResponseWriter, r *http.Request) {
	h.servePrerollFile(w, r, false)
}
