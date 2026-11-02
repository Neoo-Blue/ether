package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/braedonsaunders/ether/internal/agent/tools"
)

// OwnerPrincipal is the console owner principal. The mount in
// internal/app/agent.go sits behind owner auth.
func OwnerPrincipal() tools.Principal {
	return tools.Principal{IsOwner: true, Surface: "console"}
}

// Handler returns the /api/agent subtree (prefix already stripped).
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /turn", s.handleTurn)
	mux.HandleFunc("GET /threads", s.handleThreads)
	mux.HandleFunc("GET /threads/{id}/messages", s.handleMessages)
	mux.HandleFunc("DELETE /threads/{id}", s.handleDeleteThread)
	mux.HandleFunc("POST /approvals/{id}", s.handleApproval)
	mux.HandleFunc("POST /secrets/{id}", s.handleSecret)
	mux.HandleFunc("POST /turns/{id}/cancel", s.handleCancel)
	// Singular spelling from SPEC §5.11, same handler.
	mux.HandleFunc("POST /turn/{id}/cancel", s.handleCancel)
	mux.HandleFunc("GET /status", s.handleStatus)
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"message": msg}})
}

func decodeBody(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}

type turnRequest struct {
	ThreadID string      `json:"threadId"`
	Prompt   string      `json:"prompt"`
	Context  TurnContext `json:"context"`
}

func (s *Service) handleTurn(w http.ResponseWriter, r *http.Request) {
	var req turnRequest
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" {
		writeErr(w, http.StatusBadRequest, "A prompt is required.")
		return
	}
	if len(req.Prompt) > 32<<10 {
		writeErr(w, http.StatusBadRequest, "Prompt is too long.")
		return
	}
	threadID := req.ThreadID
	// Resolve/create the thread up front so the header carries its id even
	// when the stream starts immediately.
	ctx := r.Context()
	if threadID == "" {
		th, err := s.store.CreateThread(ctx, threadTitle(req.Prompt))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "Could not start the conversation.")
			return
		}
		threadID = th.ID
	} else if _, ok, err := s.store.Thread(ctx, threadID); err != nil || !ok {
		writeErr(w, http.StatusNotFound, "Conversation not found.")
		return
	}
	streamHeaders(w)
	w.Header().Set("X-Ether-Thread-Id", threadID)
	w.WriteHeader(http.StatusOK)
	emit := func(c Chunk) error { return writeSSE(w, c) }
	if _, _, err := s.RunTurn(ctx, OwnerPrincipal(), threadID, req.Prompt, req.Context, emit); err != nil {
		_ = writeSSE(w, chunkError("The turn failed. Details are in the server log."))
		_ = writeSSE(w, chunkFinish("error"))
		s.log.Warn("agent turn http failed", "thread", threadID, "err", err)
	}
	writeSSEDone(w)
}

func (s *Service) handleThreads(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	threads, err := s.store.ListThreads(r.Context(), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Could not load conversations.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"threads": threads})
}

func (s *Service) handleMessages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok, err := s.store.Thread(r.Context(), id); err != nil || !ok {
		writeErr(w, http.StatusNotFound, "Conversation not found.")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	msgs, hasOlder, err := s.store.Messages(r.Context(), id, r.URL.Query().Get("before"), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Could not load messages.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"threadId": id, "messages": msgs, "hasOlder": hasOlder,
	})
}

func (s *Service) handleDeleteThread(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok, err := s.store.Thread(r.Context(), id); err != nil || !ok {
		writeErr(w, http.StatusNotFound, "Conversation not found.")
		return
	}
	if err := s.store.DeleteThread(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, "Could not delete the conversation.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (s *Service) handleApproval(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	if body.Decision != "approved" && body.Decision != "rejected" {
		writeErr(w, http.StatusBadRequest, "Decision must be approved or rejected.")
		return
	}
	appr, err := s.DecideApproval(r.Context(), OwnerPrincipal(), r.PathValue("id"), body.Decision, strings.TrimSpace(body.Note))
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeErr(w, http.StatusNotFound, "Approval not found.")
			return
		}
		if strings.Contains(err.Error(), "already") {
			writeErr(w, http.StatusConflict, "This approval has already been decided.")
			return
		}
		writeErr(w, http.StatusInternalServerError, "Could not save the decision.")
		s.log.Warn("agent approval failed", "id", r.PathValue("id"), "err", err)
		return
	}
	out := map[string]any{
		"approvalId": appr.ID, "status": appr.Status, "decisionNote": appr.DecisionNote,
	}
	if appr.FailureReason != "" {
		out["failureReason"] = appr.FailureReason
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Service) handleSecret(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Value string `json:"value"`
	}
	if err := decodeBody(r, &body); err != nil || body.Value == "" {
		writeErr(w, http.StatusBadRequest, "A value is required.")
		return
	}
	if err := s.SubmitSecret(r.Context(), r.PathValue("id"), body.Value); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeErr(w, http.StatusNotFound, "Secret request not found.")
			return
		}
		if strings.Contains(err.Error(), "expired") || strings.Contains(err.Error(), "already") {
			writeJSON(w, http.StatusOK, map[string]any{"status": "expired"})
			return
		}
		writeErr(w, http.StatusInternalServerError, "Could not store the secret.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "stored"})
}

func (s *Service) handleCancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cancelled := s.CancelTurn(id)
	writeJSON(w, http.StatusOK, map[string]any{"cancelled": cancelled})
}

func (s *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Status())
}
