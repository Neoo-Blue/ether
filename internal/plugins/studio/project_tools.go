package studio

import (
	"context"
	"encoding/json"

	"github.com/braedonsaunders/ether/internal/agent/tools"
)

// ProjectTools use the same draft service as human editing. Writes return
// normal agent proposals: no installation or viewer-domain writes occur.
func ProjectTools(s *Studio) []tools.Tool {
	if s == nil {
		return nil
	}
	read := tools.Tool{Name: "studio_project_read", Title: "Read Studio draft", Description: "Read a saved Studio project and its source files, or list projects when id is omitted.", ReadOnly: true, Audience: tools.AudienceOwner, Owner: "builtin", InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"additionalProperties":false}`)}
	read.Execute = func(ctx context.Context, _ tools.Principal, input map[string]any) (tools.Result, error) {
		id, err := optString(input, "id", 160)
		if err != nil {
			return tools.Result{}, err
		}
		if id == "" {
			return tools.Result{Data: map[string]any{"projects": s.ListProjects(ctx)}}, nil
		}
		p, err := s.GetProject(ctx, id)
		if err != nil {
			return tools.Result{}, tools.Userf("Draft not found")
		}
		return tools.Result{Data: map[string]any{"project": p}}, nil
	}
	create := tools.Tool{Name: "studio_project_create", Title: "Create Studio draft", Description: "Create a saved plugin draft from template page, list, channels, local_files, notification, ranking, tool or stremio_remix. Drafts are sandboxed and never installed, so this applies immediately. Read the saved source before editing it.", Audience: tools.AudienceOwner, Owner: "builtin", InputSchema: json.RawMessage(`{"type":"object","required":["template","name"],"properties":{"template":{"type":"string"},"name":{"type":"string"},"pluginId":{"type":"string"}},"additionalProperties":false}`)}
	create.Execute = func(ctx context.Context, _ tools.Principal, input map[string]any) (tools.Result, error) {
		template, err := reqString(input, "template", 64)
		if err != nil {
			return tools.Result{}, err
		}
		name, err := reqString(input, "name", 120)
		if err != nil {
			return tools.Result{}, err
		}
		id, err := optString(input, "pluginId", 120)
		if err != nil {
			return tools.Result{}, err
		}
		if _, ok := LookupTemplate(template); !ok {
			return tools.Result{}, tools.Userf("Unknown Studio template")
		}
		opts := CreateProjectOptions{Template: template, Name: name, PluginID: id}
		p, err := s.CreateProject(ctx, opts)
		if err != nil {
			return tools.Result{}, err
		}
		return tools.Result{Data: map[string]any{"project": p}}, nil
	}
	save := tools.Tool{Name: "studio_project_save", Title: "Save Studio source", Description: "Save source files to an existing Studio draft and return real compiler diagnostics. Requires the exact updatedAt from studio_project_read; a newer human edit makes a stale save fail, so read again and retry. Applies immediately; does not install the plugin. \"$t:key\" text resolves in manifest fields and ViewSpec literals only, never in view data values. Fix every diagnostic it returns before finishing.", Audience: tools.AudienceOwner, Owner: "builtin", InputSchema: json.RawMessage(`{"type":"object","required":["id","updatedAt","files"],"properties":{"id":{"type":"string"},"updatedAt":{"type":"string"},"files":{"type":"object","additionalProperties":{"type":"string"}}},"additionalProperties":false}`)}
	save.Execute = func(ctx context.Context, _ tools.Principal, input map[string]any) (tools.Result, error) {
		id, err := reqString(input, "id", 160)
		if err != nil {
			return tools.Result{}, err
		}
		version, err := reqString(input, "updatedAt", 64)
		if err != nil {
			return tools.Result{}, err
		}
		raw, err := json.Marshal(input["files"])
		if err != nil || len(raw) > 256*1024 {
			return tools.Result{}, tools.Userf("Draft update exceeds 256 KiB")
		}
		files := map[string]string{}
		if json.Unmarshal(raw, &files) != nil || len(files) == 0 || len(files) > 32 {
			return tools.Result{}, tools.Userf("Provide 1 to 32 source files")
		}
		p, comp, err := s.saveFilesVersion(ctx, id, files, version)
		if err != nil {
			return tools.Result{}, err
		}
		return tools.Result{Data: map[string]any{"project": p, "compile": comp}}, nil
	}
	return []tools.Tool{read, create, save}
}
