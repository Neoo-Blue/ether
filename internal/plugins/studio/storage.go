package studio

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/braedonsaunders/ether/internal/store"
)

func init() {
	store.Register("plugin_studio", 1, `CREATE TABLE IF NOT EXISTS plugin_studio_projects (id TEXT PRIMARY KEY, project_json TEXT NOT NULL);`)
}

// OpenStorage restores authoring drafts only. It never installs a plugin or
// runs restored code. Compile diagnostics are recomputed by normal previews.
func (s *Studio) OpenStorage(ctx context.Context, db *sql.DB) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := db.QueryContext(ctx, "SELECT project_json FROM plugin_studio_projects")
	if err != nil {
		return err
	}
	defer rows.Close()
	restored := map[string]*Project{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return err
		}
		var p Project
		if err = json.Unmarshal([]byte(raw), &p); err != nil {
			return fmt.Errorf("restore Studio draft: %w", err)
		}
		if p.ID == "" || p.Files == nil {
			return fmt.Errorf("restore Studio draft: missing identity or files")
		}
		p.kv = map[string][]byte{}
		p.collections = map[string]json.RawMessage{}
		p.watchState = map[string]bool{}
		restored[p.ID] = &p
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(s.projects) > 0 {
		return fmt.Errorf("Studio storage must open before creating projects")
	}
	s.db = db
	s.projects = restored
	return nil
}
func (s *Studio) persistLocked(ctx context.Context, p *Project) error {
	if s.db == nil {
		return nil
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO plugin_studio_projects(id,project_json) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET project_json=excluded.project_json`, p.ID, string(raw))
	return err
}
