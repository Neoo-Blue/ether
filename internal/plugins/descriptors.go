package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/braedonsaunders/ether/internal/agent"
	"github.com/braedonsaunders/ether/internal/agent/tools"
	"github.com/braedonsaunders/ether/internal/extend"
)

// textOf converts a manifest label ("$t:key" or plain text) to registry
// Text. Plain plugin text is carried as Resolved so the console shows it
// without a catalogue entry; $t: keys resolve through plugin strings.
func textOf(plugin, s string) extend.Text {
	if strings.HasPrefix(s, "$t:") {
		return extend.PluginText(plugin, strings.TrimPrefix(s, "$t:"))
	}
	return extend.Text{Resolved: s}
}

// DescriptorsFor builds the registry descriptors a manifest contributes.
// Exported for the kit and Studio previews.
func DescriptorsFor(man Manifest) []extend.Descriptor {
	if man.Ether == nil {
		return nil
	}
	id := man.ID
	c := man.Ether.Contributes
	var out []extend.Descriptor
	add := func(kind extend.Kind, key string, label extend.Text, meta any) {
		d := extend.Descriptor{Kind: kind, ID: extend.PluginID(id, key), Owner: id, Label: label, Group: man.Name}
		if meta != nil {
			d = d.WithMeta(meta)
		}
		out = append(out, d)
	}
	for _, p := range c.Pages {
		add(extend.KindPage, p.Key, textOf(id, p.Label), map[string]any{
			"route": p.Route, "pluginRoute": "/api/plugins/" + id + "/views/" + p.Key, "group": p.Group, "icon": p.Icon})
		add(extend.KindNavItem, "page-"+p.Key, textOf(id, p.Label), map[string]any{
			"route": "/plugins/" + id + "/" + p.Key, "pageKey": p.Key})
	}
	for _, s := range c.Slots {
		add(extend.KindSlot, s.Key, textOf(id, s.Label), map[string]any{"slot": s.Slot, "route": s.Route})
	}
	for _, cm := range c.Commands {
		add(extend.KindCommand, cm.Key, textOf(id, cm.Label), map[string]any{"action": cm.Action})
	}
	for _, rs := range c.RankingSteps {
		add(extend.KindRankingStep, rs.Key, textOf(id, rs.Label), map[string]any{
			"editor": rs.Editor, "defaultPosition": rs.DefaultPosition, "budget": rs.Budget, "hint": rs.Hint})
	}
	for _, rc := range c.RuleConditions {
		add(extend.KindRuleField, rc.Key, textOf(id, rc.Label), map[string]any{
			"operators": rc.Operators, "editor": rc.Editor})
	}
	for _, ls := range c.ListSources {
		meta := map[string]any{"catalog": ls.Catalog, "params": ls.Params, "maxItems": ls.MaxItems, "refresh": ls.Refresh, "hint": ls.Hint}
		add(extend.KindCollectionSource, ls.Key, textOf(id, ls.Label), meta)
		add(extend.KindRowSource, ls.Key, textOf(id, ls.Label), meta)
	}
	if man.HasCapability(CapLiveTV) && c.Channels != nil {
		add(extend.KindLiveTVSource, "channels", textOf(id, man.Name), map[string]any{"refresh": c.Channels.Refresh})
	}
	if man.HasCapability(CapNotify) && c.Notification != nil {
		add(extend.KindChannelKind, "notify", textOf(id, man.Name), map[string]any{
			"fields": notificationFields(id, c.Notification.Fields), "sendTest": c.Notification.SendTest, "selectable": true, "formats": c.Notification.Formats, "maxLength": c.Notification.MaxLength})
	}
	if man.HasCapability(CapSubtitles) && c.Subtitles != nil {
		add(extend.KindSubtitleProvider, "subtitles", textOf(id, man.Name), map[string]any{
			"languages": c.Subtitles.Languages, "formats": c.Subtitles.Formats})
	}
	if man.HasCapability(CapMarkers) && c.Markers != nil {
		add(extend.KindMarkerSource, "markers", textOf(id, man.Name), map[string]any{"kinds": c.Markers.Kinds})
	}
	if man.HasCapability(CapStreamSource) && c.Source != nil {
		add(extend.KindSourceKind, "stream", textOf(id, man.Name), map[string]any{
			"class": c.Source.Class, "abilities": c.Source.Abilities, "limits": c.Source.Limits})
	}
	if man.Ether.Kind == KindIntegration && c.Integration != nil {
		add(extend.KindIntegration, "integration", textOf(id, man.Name), map[string]any{
			"category": c.Integration.Category, "adds": c.Integration.Adds, "connect": c.Integration.Connect, "logo": c.Integration.Logo})
	}
	for _, tl := range c.Tools {
		add(extend.KindTool, tl.Name, textOf(id, tl.Title), map[string]any{
			"name": ToolName(id, tl.Name), "readOnly": tl.ReadOnly, "destructive": tl.Destructive, "openWorld": tl.OpenWorld})
	}
	return out
}

// ToolName namespaces a plugin tool for the catalogue: dots and dashes
// become underscores ("<plugin>__<name>").
func ToolName(plugin, name string) string {
	p := strings.NewReplacer(".", "_", "-", "_").Replace(plugin)
	return p + "__" + name
}

// syncDescriptors replaces a plugin's registry descriptors and tools to
// match its record: none while disabled.
func (m *Manager) syncDescriptors(ctx context.Context, r Record) {
	m.indexEventSubscriptions(r)
	m.syncNotificationKind(r)
	if m.deps.Registry != nil {
		var ds []extend.Descriptor
		if r.Enabled {
			ds = DescriptorsFor(r.Manifest)
		}
		if err := m.deps.Registry.ReplaceOwner(r.ID, ds); err != nil {
			m.log.Warn("plugins: registry sync", "plugin", r.ID, "err", err)
		}
	}
	if m.deps.Tools != nil {
		var ts []tools.Tool
		if r.Enabled && r.Manifest.Ether != nil {
			for _, tl := range r.Manifest.Ether.Contributes.Tools {
				ts = append(ts, m.pluginTool(r, tl))
			}
		}
		if err := m.deps.Tools.ReplaceOwner(r.ID, ts); err != nil {
			m.log.Warn("plugins: tool sync", "plugin", r.ID, "err", err)
		}
	}
}

// pluginTool adapts one contributed tool to the catalogue. Read-only tools
// execute through /ether/tool/{name}; writes return a Proposal whose
// Apply runs the call after owner approval (SPEC §5.10).
func (m *Manager) pluginTool(r Record, tl ToolMeta) tools.Tool {
	pluginID, name := r.ID, tl.Name
	title := tl.Title
	if strings.HasPrefix(title, "$t:") {
		title = name
	}
	run := func(ctx context.Context, input map[string]any) (tools.Result, error) {
		body, err := json.Marshal(input)
		if err != nil {
			return tools.Result{}, err
		}
		resp, err := m.Call(ctx, pluginID, Request{Method: "POST", Path: "/ether/tool/" + name, Body: body})
		if err != nil {
			return tools.Result{}, tools.Userf("Plugin %s tool %s failed.", pluginID, name)
		}
		if resp.Status >= 300 {
			return tools.Result{}, tools.Userf("Plugin %s tool %s failed with status %d.", pluginID, name, resp.Status)
		}
		if len(resp.Body) > 64*1024 {
			return tools.Result{}, tools.Userf("Plugin tool output is too large.")
		}
		var data map[string]any
		if len(resp.Body) > 0 {
			if err := json.Unmarshal(resp.Body, &data); err != nil {
				return tools.Result{}, tools.Userf("Plugin tool returned invalid output.")
			}
		}
		redactValue(data)
		return tools.Result{Data: data}, nil
	}
	return tools.Tool{
		Name: ToolName(pluginID, name), Title: title, Description: tl.Description,
		InputSchema: tl.InputSchema, ReadOnly: tl.ReadOnly, Destructive: tl.Destructive,
		OpenWorld: tl.OpenWorld, Audience: tools.AudienceOwner, Owner: pluginID,
		Execute: func(ctx context.Context, _ tools.Principal, input map[string]any) (tools.Result, error) {
			encoded, err := json.Marshal(input)
			if err != nil || len(encoded) > 64*1024 {
				return tools.Result{}, tools.Userf("Tool input is invalid or too large.")
			}
			var snapshot map[string]any
			if err := json.Unmarshal(encoded, &snapshot); err != nil {
				return tools.Result{}, tools.Userf("Tool input is invalid.")
			}
			if err := agent.ValidateToolInput(tl.InputSchema, snapshot); err != nil {
				return tools.Result{}, tools.Userf("Invalid tool input: %s", err)
			}
			if tl.ReadOnly {
				return run(ctx, snapshot)
			}
			return tools.Result{Proposal: &tools.Proposal{
				Category:    fmt.Sprintf("Run %s from %s", title, r.Name),
				Description: tl.Description,
				Input:       snapshot,
				Apply: func(ctx context.Context) (tools.Result, error) {
					var approved map[string]any
					if err := json.Unmarshal(encoded, &approved); err != nil {
						return tools.Result{}, err
					}
					return run(ctx, approved)
				},
			}}, nil
		},
	}
}

// PermissionSheet returns the plain-language lines the owner sees at
// install (SPEC §5.4). needsViewers and needsFolders say the sheet needs
// viewer or folder picks.
func PermissionSheet(man Manifest) (lines []string, needsViewers, needsFolders bool) {
	if man.Ether == nil {
		return []string{"Connects to the add-on address you gave"}, false, false
	}
	p := man.Ether.Permissions
	for _, h := range p.Network {
		lines = append(lines, "Connects to "+h)
	}
	if normLevel(p.Catalog) == "read" {
		lines = append(lines, "Reads your catalogue")
	}
	if normLevel(p.Viewers.Identity) != "none" {
		lines = append(lines, "Sees who is watching (viewers you choose)")
		needsViewers = needsViewers || normLevel(p.Viewers.Identity) == "granted_viewers"
	}
	if normLevel(p.Viewers.WatchHistory) != "none" {
		lines = append(lines, "Reads watch history")
		needsViewers = needsViewers || normLevel(p.Viewers.WatchHistory) == "granted_viewers"
	}
	if normLevel(p.Viewers.WriteWatchState) != "none" {
		lines = append(lines, "Marks titles watched")
		needsViewers = true
	}
	if normLevel(p.Collections) == "write" {
		lines = append(lines, "Creates and updates collections")
	}
	if normLevel(p.Folders) == "read" {
		lines = append(lines, "Reads folders you choose")
		needsFolders = true
	}
	for range p.Secrets {
		lines = append(lines, "Stores an access token")
	}
	if man.HasCapability(CapEvents) {
		lines = append(lines, "Gets play and new-release events")
	}
	if len(lines) == 0 {
		lines = []string{"Needs no access to your server or the network"}
	}
	return lines, needsViewers, needsFolders
}

// PermissionDiff lists the permissions `next` asks for that `cur` does
// not have. A non-empty result pauses auto-update and asks the owner.
func PermissionDiff(cur, next Manifest) []string {
	if cur.Ether == nil || next.Ether == nil {
		if next.Ether != nil {
			lines, _, _ := PermissionSheet(next)
			return lines
		}
		return nil
	}
	a, b := cur.Ether.Permissions, next.Ether.Permissions
	var added []string
	have := map[string]bool{}
	for _, h := range a.Network {
		have[h] = true
	}
	for _, h := range b.Network {
		if !have[h] {
			added = append(added, "Connects to "+h)
		}
	}
	level := map[string]int{"none": 0, "granted_viewers": 1, "read": 1, "write": 1, "all": 2}
	raised := func(x, y string) bool { return level[normLevel(y)] > level[normLevel(x)] }
	if raised(a.Catalog, b.Catalog) {
		added = append(added, "Reads your catalogue")
	}
	if raised(a.Viewers.Identity, b.Viewers.Identity) {
		added = append(added, "Sees who is watching (viewers you choose)")
	}
	if raised(a.Viewers.WatchHistory, b.Viewers.WatchHistory) {
		added = append(added, "Reads watch history")
	}
	if raised(a.Viewers.WriteWatchState, b.Viewers.WriteWatchState) {
		added = append(added, "Marks titles watched")
	}
	if raised(a.Collections, b.Collections) {
		added = append(added, "Creates and updates collections")
	}
	if raised(a.Folders, b.Folders) {
		added = append(added, "Reads folders you choose")
	}
	haveSecret := map[string]bool{}
	for _, s := range a.Secrets {
		haveSecret[s] = true
	}
	for _, s := range b.Secrets {
		if !haveSecret[s] {
			added = append(added, "Stores an access token")
		}
	}
	if !cur.HasCapability(CapEvents) && next.HasCapability(CapEvents) {
		added = append(added, "Gets play and new-release events")
	}
	return added
}
