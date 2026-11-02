package plugins

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/braedonsaunders/ether/internal/store"
)

func init() {
	store.Register("plugins_proposals", 1, `CREATE TABLE plugins_proposals(id TEXT PRIMARY KEY, plugin TEXT NOT NULL, plugin_id TEXT NOT NULL, status TEXT NOT NULL, created INTEGER NOT NULL);`)
}

// saveActionProposal replaces the plugin's opaque proposal ID with a host ID.
func (m *Manager) saveActionProposal(ctx context.Context, plugin string, body []byte) ([]byte, error) {
	if len(body) > 1<<20 {
		return nil, errors.New("plugin action result too large")
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(body, &result); err != nil || result == nil {
		return nil, errors.New("invalid plugin action result")
	}
	if raw, ok := result["proposal"]; ok && string(raw) != "null" {
		var p map[string]json.RawMessage
		if json.Unmarshal(raw, &p) != nil {
			return nil, errors.New("invalid plugin proposal")
		}
		var pluginID, title string
		_ = json.Unmarshal(p["id"], &pluginID)
		_ = json.Unmarshal(p["title"], &title)
		if pluginID == "" || len(pluginID) > 256 || title == "" {
			return nil, errors.New("plugin proposal requires an id and title")
		}
		id := newEventID() + newEventID()
		if _, err := m.store.db.ExecContext(ctx, `INSERT INTO plugins_proposals(id,plugin,plugin_id,status,created) VALUES(?,?,?,'pending',?)`, id, plugin, pluginID, m.now().Unix()); err != nil {
			return nil, err
		}
		p["id"], _ = json.Marshal(id)
		result["proposal"], _ = json.Marshal(p)
	}
	return json.Marshal(result)
}

func (m *Manager) handleProposalDecision(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&in); err != nil || (in.Decision != "approved" && in.Decision != "rejected") || len(in.Note) > 4096 {
		writeErr(w, 400, "invalid_decision", "Invalid proposal decision.", nil)
		return
	}
	plugin, id := r.PathValue("id"), r.PathValue("pid")
	var pluginID string
	err := m.store.db.QueryRowContext(r.Context(), `UPDATE plugins_proposals SET status=? WHERE id=? AND plugin=? AND status='pending' RETURNING plugin_id`, in.Decision, id, plugin).Scan(&pluginID)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, 404, "not_found", "Proposal not found or already decided.", nil)
		return
	}
	if err != nil {
		m.fail(w, err)
		return
	}
	body, _ := json.Marshal(in)
	resp, err := m.Call(r.Context(), plugin, Request{Method: "POST", Path: "/ether/proposal/" + url.PathEscape(pluginID), Body: body})
	if err != nil {
		m.fail(w, err)
		return
	}
	if resp.Status < 200 || resp.Status >= 300 {
		writeErr(w, 502, "plugin_error", fmt.Sprintf("Plugin proposal decision failed (%d).", resp.Status), nil)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if len(resp.Body) == 0 {
		_, _ = w.Write([]byte(`{}`))
		return
	}
	_, _ = w.Write(resp.Body)
}
