package studio

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/braedonsaunders/ether/internal/agent/tools"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/plugins/wasm"
	"github.com/braedonsaunders/ether/internal/resolver"
	"github.com/braedonsaunders/ether/internal/store"
)

func TestStudioMCPToolsContractAndExecution(t *testing.T) {
	db := store.OpenTest(t)
	eng := wasm.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { _ = eng.Close() })

	reg := extend.New()
	if err := reg.Register(extend.Descriptor{
		Kind:  extend.KindRankingStep,
		ID:    "resolution",
		Owner: extend.Builtin,
		Label: extend.T("Resolution"),
	}); err != nil {
		t.Fatal(err)
	}

	cat := tools.NewCatalogue()
	mgr, err := plugins.New(plugins.Deps{
		DB:       db,
		Registry: reg,
		Tools:    cat,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.RegisterRuntime("hosted", plugins.HostedRuntimeFactory(eng))

	runner := NewRunner(Deps{
		DB:      db,
		Manager: mgr,
		Engine:  eng,
	})
	t.Cleanup(func() { _ = runner.Close() })

	deps := ToolDeps{
		Registry: reg,
		Manager:  mgr,
		Runner:   runner,
		Engine:   eng,
	}

	if err := RegisterTools(cat, deps); err != nil {
		t.Fatalf("RegisterTools failed: %v", err)
	}

	ownerPrincipal := tools.Principal{
		ViewerID: 1,
		IsOwner:  true,
		Surface:  "mcp",
		Scopes:   []string{"full"},
	}
	readOnlyPrincipal := tools.Principal{
		ViewerID: 1,
		IsOwner:  true,
		Surface:  "mcp",
		Scopes:   []string{"read"},
	}

	ctx := context.Background()

	// Verify visibility: full scope sees all 8 tools; read scope sees the 7 read-only tools and not plugin_install.
	fullVisible := cat.Visible(ctx, ownerPrincipal)
	if len(fullVisible) != 8 {
		t.Fatalf("full scope visible tools = %d, want 8", len(fullVisible))
	}
	readVisible := cat.Visible(ctx, readOnlyPrincipal)
	if len(readVisible) != 7 {
		t.Fatalf("read scope visible tools = %d, want 7", len(readVisible))
	}
	for _, rt := range readVisible {
		if rt.Name == "plugin_install" {
			t.Fatal("plugin_install must not be visible to a read-only MCP principal")
		}
	}

	// 1. registry_describe
	regTool, ok := cat.Lookup(ctx, ownerPrincipal, "registry_describe")
	if !ok {
		t.Fatal("registry_describe not found")
	}
	regRes, err := regTool.Execute(ctx, ownerPrincipal, map[string]any{"kind": "rankingStep"})
	if err != nil {
		t.Fatalf("registry_describe failed: %v", err)
	}
	if regRes.Data["count"] != 1 {
		t.Fatalf("registry_describe count = %v, want 1", regRes.Data["count"])
	}

	// 2. viewspec_validate and viewspec_render
	valTool, _ := cat.Lookup(ctx, ownerPrincipal, "viewspec_validate")
	validSpec := map[string]any{
		"specVersion": 1,
		"layout":      "list",
		"header": []any{
			map[string]any{
				"kind":  "page-header",
				"title": "Plugin overview",
			},
		},
		"body": []any{
			map[string]any{
				"kind":    "stat-tile",
				"iconKey": "check",
				"accent":  "teal",
				"label":   "Matched",
				"value":   "12",
			},
		},
	}
	valRes, err := valTool.Execute(ctx, ownerPrincipal, map[string]any{"spec": validSpec})
	if err != nil {
		t.Fatalf("viewspec_validate failed: %v", err)
	}
	if valRes.Data["ok"] != true {
		t.Fatalf("viewspec_validate ok = %v, errors = %v", valRes.Data["ok"], valRes.Data["errors"])
	}

	renderTool, _ := cat.Lookup(ctx, ownerPrincipal, "viewspec_render")
	renderRes, err := renderTool.Execute(ctx, ownerPrincipal, map[string]any{
		"spec": validSpec,
		"data": map[string]any{"count": 12},
	})
	if err != nil {
		t.Fatalf("viewspec_render failed: %v", err)
	}
	if renderRes.Data["ok"] != true || renderRes.Data["consoleUrl"] == "" {
		t.Fatalf("viewspec_render = %+v", renderRes.Data)
	}

	// 3. plugin_scaffold and plugin_check
	scafTool, _ := cat.Lookup(ctx, ownerPrincipal, "plugin_scaffold")
	scafRes, err := scafTool.Execute(ctx, ownerPrincipal, map[string]any{
		"id":           "org.example.scaffolded",
		"name":         "Scaffolded Ranker",
		"kind":         "extension",
		"capabilities": []any{"ranking_step"},
		"summary":      "Ranks releases with custom logic.",
	})
	if err != nil {
		t.Fatalf("plugin_scaffold failed: %v", err)
	}
	chkTool, _ := cat.Lookup(ctx, ownerPrincipal, "plugin_check")
	chkRes, err := chkTool.Execute(ctx, ownerPrincipal, map[string]any{
		"manifest": scafRes.Data["manifest"],
		"files":    scafRes.Data["files"],
	})
	if err != nil {
		t.Fatalf("plugin_check failed: %v", err)
	}
	if chkRes.Data["ok"] != true {
		t.Fatalf("plugin_check on scaffolded plugin failed: issues=%v", chkRes.Data["issues"])
	}

	// 4. plugin_replay
	insertHistoryFixture(t, db, 10, 301, "My Neighbor Totoro (1988)", "rel-a", []resolver.CandidateTrace{
		{ReleaseID: "rel-a", Name: "Totoro.1988.1080p.BluRay.x264-GRP", Score: 90, Height: 1080},
		{ReleaseID: "rel-b", Name: "Totoro.1988.DUB.1080p.BluRay.x264-GRP", Score: 85, Height: 1080},
	})
	replayTool, _ := cat.Lookup(ctx, ownerPrincipal, "plugin_replay")
	replayRes, err := replayTool.Execute(ctx, ownerPrincipal, map[string]any{
		"kind": "ranking",
		"devInstance": map[string]any{
			"manifest": scafRes.Data["manifest"],
			"files": map[string]any{
				"plugin.ts": `
function rank_key(candidate: { name: string }): number {
  return candidate.name.includes("DUB") ? 10 : 0;
}
(globalThis as any).rank_key = rank_key;
`,
			},
		},
	})
	if err != nil {
		t.Fatalf("plugin_replay failed: %v", err)
	}
	rankingData, _ := replayRes.Data["ranking"].(map[string]any)
	if rankingData == nil || rankingData["changedCount"] != float64(1) {
		t.Fatalf("plugin_replay ranking = %+v, want changedCount 1", rankingData)
	}

	// 5. plugin_install returns a Proposal without installing until Apply is called.
	installTool, _ := cat.Lookup(ctx, ownerPrincipal, "plugin_install")
	shadowManifest := map[string]any{
		"id":      "org.example.shadow-writer",
		"version": "1.0.0",
		"name":    "Shadow Writer",
		"ether": map[string]any{
			"protocol":     1,
			"kind":         "extension",
			"summary":      "Records watched completions in a shadow collection.",
			"capabilities": []string{"events"},
			"runtime":      map[string]any{"kind": "hosted", "entry": "plugin.js"},
			"permissions": map[string]any{
				"collections": "write",
			},
			"contributes": map[string]any{
				"events": map[string]any{"subscribe": []string{"playback.completed"}},
			},
		},
	}
	shadowJS := `
function handle(req) {
  ether.collections.upsert({ id: "watched-pile", title: "Watched pile" });
  return { status: 200, body: { ack: [] } };
}
globalThis.handle = handle;
`
	instRes, err := installTool.Execute(ctx, ownerPrincipal, map[string]any{
		"manifest": shadowManifest,
		"files":    map[string]any{"plugin.js": shadowJS},
		"shadow":   true,
	})
	if err != nil {
		t.Fatalf("plugin_install failed: %v", err)
	}
	if instRes.Proposal == nil {
		t.Fatal("plugin_install must return a Proposal requiring owner approval")
	}
	if _, err := mgr.Get(ctx, "org.example.shadow-writer"); err == nil {
		t.Fatal("plugin_install must not install the plugin before Proposal.Apply runs")
	}

	// Approve the proposal and verify installation succeeds in shadow mode.
	appliedRes, err := instRes.Proposal.Apply(ctx)
	if err != nil {
		t.Fatalf("Proposal.Apply failed: %v", err)
	}
	if appliedRes.Data["id"] != "org.example.shadow-writer" || appliedRes.Data["shadow"] != true {
		t.Fatalf("applied install result = %+v", appliedRes.Data)
	}

	// Trigger a call on the shadow-mode plugin so it writes a shadow journal entry,
	// then inspect it with plugin_shadow_report.
	if _, err := mgr.Call(ctx, "org.example.shadow-writer", plugins.Request{
		PluginID: "org.example.shadow-writer",
		Method:   "POST",
		Path:     "/ether/event",
		Body:     json.RawMessage(`{"events":[]}`),
	}); err != nil {
		t.Fatal(err)
	}

	shadowTool, _ := cat.Lookup(ctx, ownerPrincipal, "plugin_shadow_report")
	shadowRep, err := shadowTool.Execute(ctx, ownerPrincipal, map[string]any{
		"plugin": "org.example.shadow-writer",
	})
	if err != nil {
		t.Fatalf("plugin_shadow_report failed: %v", err)
	}
	if shadowRep.Data["count"] != 1 {
		t.Fatalf("shadow report count = %v, want 1", shadowRep.Data["count"])
	}

	// Verify copy rules across all tool titles and descriptions.
	for _, tl := range fullVisible {
		for _, text := range []string{tl.Title, tl.Description} {
			if strings.ContainsAny(text, "—–!") {
				t.Fatalf("tool %s copy violates punctuation rules: %q", tl.Name, text)
			}
			lower := " " + strings.ToLower(text) + " "
			for _, banned := range []string{" simply ", " just ", " easily "} {
				if strings.Contains(lower, banned) {
					t.Fatalf("tool %s copy uses banned word %q: %q", tl.Name, banned, text)
				}
			}
		}
	}
}
