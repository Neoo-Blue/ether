package studio

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/agent/tools"
)

// Drafts are sandboxed and never installed, so the agent edits them
// directly; a stale model save must still never overwrite a human edit.
func TestProjectToolsApplyDraftsAndRejectStaleWrites(t *testing.T) {
	s, _, _ := newTestStudio(t)
	ctx := context.Background()
	ts := ProjectTools(s)
	for _, tool := range ts {
		if err := tool.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	principal := tools.Principal{IsOwner: true, Surface: "console"}
	created, err := ts[1].Execute(ctx, principal, map[string]any{"template": "page", "name": "A real draft"})
	if err != nil || created.Proposal != nil || len(s.ListProjects(ctx)) != 1 {
		t.Fatalf("create: %v proposal=%v", err, created.Proposal)
	}
	p, _ := s.GetProject(ctx, created.Data["project"].(map[string]any)["id"].(string))
	stale := p.UpdatedAt.Format(time.RFC3339Nano)
	saved, err := ts[2].Execute(ctx, principal, map[string]any{"id": p.ID, "updatedAt": stale, "files": map[string]any{"src/plugin.ts": "export function handle() { return {}; }"}})
	if err != nil || saved.Proposal != nil {
		t.Fatalf("save: %v", err)
	}
	current, _ := s.GetProject(ctx, p.ID)
	if current.Files["src/plugin.ts"] != "export function handle() { return {}; }" {
		t.Fatal("save did not apply")
	}
	if _, _, err = s.SaveFiles(ctx, p.ID, map[string]string{"notes.txt": "human edit"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	after, _ := s.GetProject(ctx, p.ID)
	if _, err = ts[2].Execute(ctx, principal, map[string]any{"id": p.ID, "updatedAt": current.UpdatedAt.Format(time.RFC3339Nano), "files": map[string]any{"src/plugin.ts": "overwrite"}}); err == nil {
		t.Fatal("stale model edit overwrote human edit")
	}
	final, _ := s.GetProject(ctx, p.ID)
	if final.Files["notes.txt"] != "human edit" || final.Files["src/plugin.ts"] != after.Files["src/plugin.ts"] {
		t.Fatal("rejected edit changed source")
	}
}

// A draft that compiles but renders a literal "${...}" is reported back to
// the model, so it fixes what the owner would see.
func TestProjectSaveReportsRenderedProblems(t *testing.T) {
	s, _, _ := newTestStudio(t)
	ctx := context.Background()
	ts := ProjectTools(s)
	principal := tools.Principal{IsOwner: true, Surface: "console"}
	created, err := ts[1].Execute(ctx, principal, map[string]any{"template": "page", "name": "Preview check"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.GetProject(ctx, created.Data["project"].(map[string]any)["id"].(string))
	src := strings.Replace(p.Files["src/plugin.ts"], `label: "$t:featured_picks"`, `label: "${count} picks"`, 1)
	if src == p.Files["src/plugin.ts"] {
		t.Fatal("template title not found")
	}
	saved, err := ts[2].Execute(ctx, principal, map[string]any{"id": p.ID, "updatedAt": p.UpdatedAt.Format(time.RFC3339Nano), "files": map[string]any{"src/plugin.ts": src}})
	if err != nil {
		t.Fatal(err)
	}
	prev, _ := saved.Data["preview"].(map[string]any)
	if prev == nil || prev["ok"] != false || prev["problems"] == nil {
		t.Fatalf("rendered problem not reported: %v", saved.Data["preview"])
	}
}

// The agent learns why a draft that previews fine cannot be installed yet,
// instead of the owner finding out at the Install button.
func TestProjectSaveReportsInstallCheck(t *testing.T) {
	s, _, _ := newTestStudio(t)
	ctx := context.Background()
	ts := ProjectTools(s)
	principal := tools.Principal{IsOwner: true, Surface: "console"}
	created, err := ts[1].Execute(ctx, principal, map[string]any{"template": "page", "name": "Install feedback"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.GetProject(ctx, created.Data["project"].(map[string]any)["id"].(string))
	src := strings.Replace(p.Files["src/plugin.ts"], `label: "$t:featured_picks"`, `label: "Featured picks"`, 1)
	saved, err := ts[2].Execute(ctx, principal, map[string]any{"id": p.ID, "updatedAt": p.UpdatedAt.Format(time.RFC3339Nano), "files": map[string]any{"src/plugin.ts": src}})
	if err != nil {
		t.Fatal(err)
	}
	if msg, _ := saved.Data["installCheck"].(string); !strings.Contains(msg, "$t:key") {
		t.Fatalf("install check not reported: %v", saved.Data["installCheck"])
	}
}
