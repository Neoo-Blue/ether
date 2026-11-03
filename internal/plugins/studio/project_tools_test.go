package studio

import (
	"context"
	"github.com/braedonsaunders/ether/internal/agent/tools"
	"testing"
	"time"
)

func TestProjectToolsRequireApprovalAndRejectStaleWrites(t *testing.T) {
	s, _, _ := newTestStudio(t)
	ctx := context.Background()
	ts := ProjectTools(s)
	for _, tool := range ts {
		if err := tool.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	principal := tools.Principal{IsOwner: true, Surface: "console"}
	proposal, err := ts[1].Execute(ctx, principal, map[string]any{"template": "page", "name": "A real draft"})
	if err != nil || proposal.Proposal == nil {
		t.Fatalf("create proposal: %v", err)
	}
	if len(s.ListProjects(ctx)) != 0 {
		t.Fatal("draft created before approval")
	}
	result, err := proposal.Proposal.Apply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = proposal.Proposal.Apply(ctx); err != nil || len(s.ListProjects(ctx)) != 1 {
		t.Fatal("repeated approval duplicated draft")
	}
	p := result.Data["project"].(*Project)
	update, err := ts[2].Execute(ctx, principal, map[string]any{"id": p.ID, "updatedAt": p.UpdatedAt.Format(time.RFC3339Nano), "files": map[string]any{"src/plugin.ts": "export function handle() { return {}; }"}})
	if err != nil || update.Proposal == nil {
		t.Fatalf("save proposal %v", err)
	}
	if _, _, err = s.SaveFiles(ctx, p.ID, map[string]string{"notes.txt": "human edit"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = update.Proposal.Apply(ctx); err == nil {
		t.Fatal("stale model edit overwrote human edit")
	}
	current, _ := s.GetProject(ctx, p.ID)
	if current.Files["notes.txt"] != "human edit" || current.Files["src/plugin.ts"] != p.Files["src/plugin.ts"] {
		t.Fatal("rejected edit changed source")
	}
}
