package studio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/plugins/kit"
	"github.com/braedonsaunders/ether/internal/plugins/wasm"
	"github.com/braedonsaunders/ether/internal/viewspec"
)

// ErrProjectNotFound is returned when a Studio project ID does not exist.
var ErrProjectNotFound = errors.New("studio: project not found")

// Deps are the explicit dependencies for the Studio service.
type Deps struct {
	Manager *plugins.Manager
	Engine  *wasm.Engine
	// Publish sends a live SSE event (such as "studio.reload") to the console.
	Publish func(ctx context.Context, typ string, data any) error
	Logger  *slog.Logger
	Now     func() time.Time
}

// Project is one in-progress plugin project in Studio.
type Project struct {
	ID          string            `json:"id"`
	Template    string            `json:"template"`
	Name        string            `json:"name"`
	PluginID    string            `json:"pluginId"`
	Slot        string            `json:"slot,omitempty"`
	StremioURL  string            `json:"stremioUrl,omitempty"`
	Files       map[string]string `json:"files"`
	Shadow      bool              `json:"shadow"`
	Settings    map[string]any    `json:"settings,omitempty"`
	Diagnostics []wasm.Diagnostic `json:"diagnostics"`
	DefaultPage string            `json:"defaultPage,omitempty"`
	DefaultPath string            `json:"defaultRoute,omitempty"`
	CreatedAt   time.Time         `json:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
	kv          map[string][]byte
	calls       []plugins.CallRecord
	logs        []string
	journal     []plugins.JournalEntry
	collections map[string]json.RawMessage
	watchState  map[string]bool
	nextCallID  int64
	nextJournID int64
}

// ProjectSummary is one item in the Studio project list.
type ProjectSummary struct {
	ID        string    `json:"id"`
	Template  string    `json:"template"`
	Name      string    `json:"name"`
	PluginID  string    `json:"pluginId"`
	Slot      string    `json:"slot,omitempty"`
	Shadow    bool      `json:"shadow"`
	Errors    int       `json:"errors"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// CompileResult holds the output and diagnostics from compiling a project.
type CompileResult struct {
	OK            bool              `json:"ok"`
	Diagnostics   []wasm.Diagnostic `json:"diagnostics"`
	JSSize        int               `json:"jsSize,omitempty"`
	SourcemapSize int               `json:"sourcemapSize,omitempty"`
	compiledJS    []byte
	sourcemap     []byte
}

// CheckItem is one conformance check outcome from the kit / Studio check runner.
type CheckItem struct {
	ID       string `json:"id"`
	Category string `json:"category"` // "manifest" | "copy" | "schema" | "compile" | "runtime" | "viewspec"
	Label    string `json:"label"`
	Status   string `json:"status"` // "pass" | "fail" | "warn"
	Message  string `json:"message,omitempty"`
	Fix      string `json:"fix,omitempty"`
}

// CheckReport is the complete result of checking a Studio project.
type CheckReport struct {
	OK          bool              `json:"ok"`
	Checks      []CheckItem       `json:"checks"`
	Diagnostics []wasm.Diagnostic `json:"diagnostics"`
}

// RunRequest asks Studio to invoke a route or hot-path function on a project's
// sandboxed dev instance.
type RunRequest struct {
	Method    string                  `json:"method,omitempty"`
	Path      string                  `json:"path,omitempty"`
	Query     map[string]string       `json:"query,omitempty"`
	Headers   map[string]string       `json:"headers,omitempty"`
	Body      json.RawMessage         `json:"body,omitempty"`
	Locale    string                  `json:"locale,omitempty"`
	HotPath   string                  `json:"hotPath,omitempty"` // "" | "rank" | "cond"
	Candidate *plugins.CandidateFacts `json:"candidate,omitempty"`
	Config    json.RawMessage         `json:"config,omitempty"`
	Operator  string                  `json:"operator,omitempty"`
	Value     json.RawMessage         `json:"value,omitempty"`
}

// RunResponse is the result of invoking a project's sandboxed dev instance.
type RunResponse struct {
	Status  int                    `json:"status"`
	Headers map[string]string      `json:"headers,omitempty"`
	Body    json.RawMessage        `json:"body,omitempty"`
	MS      int64                  `json:"ms"`
	Units   int64                  `json:"units"`
	Logs    []string               `json:"logs"`
	Call    plugins.CallRecord     `json:"call"`
	Hints   []ValidationHint       `json:"hints"`
	Journal []plugins.JournalEntry `json:"journal"`
}

// PreviewResponse is the result of previewing a project's UI page/slot or capability route.
type PreviewResponse struct {
	Kind   string           `json:"kind"` // "view" | "route"
	Route  string           `json:"route"`
	Page   string           `json:"page,omitempty"`
	Title  string           `json:"title,omitempty"`
	Spec   json.RawMessage  `json:"spec,omitempty"`
	Data   json.RawMessage  `json:"data,omitempty"`
	Body   json.RawMessage  `json:"body,omitempty"`
	Valid  bool             `json:"valid"`
	Errors []string         `json:"errors,omitempty"`
	Hints  []ValidationHint `json:"hints,omitempty"`
	MS     int64            `json:"ms"`
}

// Studio manages in-console plugin development projects and dev instances.
type Studio struct {
	mgr       *plugins.Manager
	eng       *wasm.Engine
	ownEngine bool
	publish   func(ctx context.Context, typ string, data any) error
	log       *slog.Logger
	now       func() time.Time

	mu       sync.Mutex
	projects map[string]*Project
	seq      int
}

// New creates a new Studio service.
func New(deps Deps) *Studio {
	log := deps.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	now := deps.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	eng := deps.Engine
	ownEngine := false
	if eng == nil {
		eng = wasm.NewEngine(log)
		ownEngine = true
	}
	return &Studio{
		mgr:       deps.Manager,
		eng:       eng,
		ownEngine: ownEngine,
		publish:   deps.Publish,
		log:       log,
		now:       now,
		projects:  map[string]*Project{},
	}
}

// Close releases the WASM engine if Studio created its own.
func (s *Studio) Close() error {
	if s.ownEngine && s.eng != nil {
		return s.eng.Close()
	}
	return nil
}

// CreateProject scaffolds a new Studio project from one of the 8 templates.
func (s *Studio) CreateProject(_ context.Context, opts CreateProjectOptions) (*Project, error) {
	tplID := strings.TrimSpace(opts.Template)
	if tplID == "" {
		tplID = "channels"
	}
	tpl, ok := LookupTemplate(tplID)
	if !ok {
		return nil, fmt.Errorf("studio: unknown template %q", tplID)
	}
	pluginID, name, files := applyProjectOptions(tpl, opts)
	comp := s.CompileFiles(files, "")

	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	projID := pluginID
	if _, exists := s.projects[projID]; exists {
		projID = fmt.Sprintf("%s-%d", pluginID, s.seq)
	}
	now := s.now()
	p := &Project{
		ID:          projID,
		Template:    tpl.ID,
		Name:        name,
		PluginID:    pluginID,
		Slot:        strings.TrimSpace(opts.Slot),
		StremioURL:  strings.TrimSpace(opts.StremioURL),
		Files:       files,
		Shadow:      true,
		Settings:    map[string]any{},
		Diagnostics: comp.Diagnostics,
		DefaultPage: tpl.DefaultPage,
		DefaultPath: tpl.DefaultRoute,
		CreatedAt:   now,
		UpdatedAt:   now,
		kv:          map[string][]byte{},
		calls:       []plugins.CallRecord{},
		logs:        []string{},
		journal:     []plugins.JournalEntry{},
		collections: map[string]json.RawMessage{},
		watchState:  map[string]bool{},
	}
	s.projects[p.ID] = p
	return cloneProject(p), nil
}

// ListProjects returns summaries of all Studio projects, newest first.
func (s *Studio) ListProjects(_ context.Context) []ProjectSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ProjectSummary, 0, len(s.projects))
	for _, p := range s.projects {
		errs := 0
		for _, d := range p.Diagnostics {
			if d.Severity == "error" {
				errs++
			}
		}
		out = append(out, ProjectSummary{
			ID:        p.ID,
			Template:  p.Template,
			Name:      p.Name,
			PluginID:  p.PluginID,
			Slot:      p.Slot,
			Shadow:    p.Shadow,
			Errors:    errs,
			UpdatedAt: p.UpdatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

func (s *Studio) lookupLocked(id string) (*Project, bool) {
	id = strings.TrimPrefix(id, "studio:")
	if p, ok := s.projects[id]; ok {
		return p, true
	}
	for _, p := range s.projects {
		if p.PluginID == id {
			return p, true
		}
	}
	return nil, false
}

// GetProject returns a copy of a Studio project by project ID or plugin ID.
func (s *Studio) GetProject(_ context.Context, id string) (*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.lookupLocked(id)
	if !ok {
		return nil, ErrProjectNotFound
	}
	return cloneProject(p), nil
}

// SaveFiles updates a project's files, recompiles it, and broadcasts a
// studio.reload event over the console stream (SPEC WS11a).
func (s *Studio) SaveFiles(
	ctx context.Context,
	id string,
	files map[string]string,
	shadow *bool,
	settings map[string]any,
) (*Project, CompileResult, error) {
	s.mu.Lock()
	p, ok := s.lookupLocked(id)
	if !ok {
		s.mu.Unlock()
		return nil, CompileResult{}, ErrProjectNotFound
	}
	merged := make(map[string]string, len(p.Files)+len(files))
	for k, v := range p.Files {
		merged[k] = v
	}
	for k, v := range files {
		if v == "" && len(files) == 1 {
			merged[k] = ""
		} else {
			merged[k] = v
		}
	}
	p.Files = merged
	if manRaw, hasMan := merged["manifest.json"]; hasMan {
		if man, err := plugins.ParseManifest([]byte(manRaw)); err == nil {
			if man.ID != "" {
				p.PluginID = man.ID
			}
			if man.Name != "" {
				p.Name = man.Name
			}
		}
	}
	if shadow != nil {
		p.Shadow = *shadow
	}
	if settings != nil {
		p.Settings = settings
	}
	comp := s.CompileFiles(p.Files, "")
	p.Diagnostics = comp.Diagnostics
	p.UpdatedAt = s.now()
	cloned := cloneProject(p)
	s.mu.Unlock()

	if s.publish != nil {
		_ = s.publish(ctx, "studio.reload", map[string]any{
			"projectId": cloned.ID,
			"pluginId":  cloned.PluginID,
			"ok":        comp.OK,
			"at":        cloned.UpdatedAt.Unix(),
		})
	}
	return cloned, comp, nil
}

// DeleteProject removes a project from Studio.
func (s *Studio) DeleteProject(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.lookupLocked(id)
	if !ok {
		return ErrProjectNotFound
	}
	delete(s.projects, p.ID)
	return nil
}

// CompileFiles validates JSON files (manifest, settings schema, i18n strings)
// and compiles the TypeScript/JavaScript entry with wasm.Compile.
func (s *Studio) CompileFiles(files map[string]string, entryOverride string) CompileResult {
	diags := []wasm.Diagnostic{}
	manContent, hasMan := files["manifest.json"]
	var man plugins.Manifest
	if !hasMan {
		diags = append(diags, wasm.Diagnostic{
			File:     "manifest.json",
			Line:     1,
			Column:   1,
			Text:     "manifest.json is missing from the project",
			Severity: "error",
		})
		return CompileResult{OK: false, Diagnostics: diags}
	}

	var rawCheck any
	if err := json.Unmarshal([]byte(manContent), &rawCheck); err != nil {
		diags = append(diags, jsonSyntaxDiagnostic("manifest.json", manContent, err))
		return CompileResult{OK: false, Diagnostics: diags}
	}
	var err error
	man, err = plugins.ParseManifest([]byte(manContent))
	if err != nil {
		diags = append(diags, jsonSyntaxDiagnostic("manifest.json", manContent, err))
		return CompileResult{OK: false, Diagnostics: diags}
	}
	if verr := man.Validate(); verr != nil {
		var ve *plugins.ValidationError
		if errors.As(verr, &ve) {
			for _, fe := range ve.Fields {
				line, col := locateJSONField(manContent, fe.Field)
				diags = append(diags, wasm.Diagnostic{
					File:     "manifest.json",
					Line:     line,
					Column:   col,
					Text:     fe.Field + ": " + fe.Message,
					Severity: "error",
				})
			}
		} else {
			diags = append(diags, wasm.Diagnostic{
				File:     "manifest.json",
				Line:     1,
				Column:   1,
				Text:     verr.Error(),
				Severity: "error",
			})
		}
	}

	// Validate settings schema file if referenced.
	if man.Ether != nil && len(man.Ether.Settings) > 0 {
		ref := settingsRefOf(man.Ether.Settings)
		if ref != "" {
			schemaText, ok := files[ref]
			if !ok {
				line, col := locateJSONField(manContent, "ether.settings")
				diags = append(diags, wasm.Diagnostic{
					File:     "manifest.json",
					Line:     line,
					Column:   col,
					Text:     fmt.Sprintf("settings file %q is missing", ref),
					Severity: "error",
				})
			} else {
				var sdoc any
				if jerr := json.Unmarshal([]byte(schemaText), &sdoc); jerr != nil {
					diags = append(diags, jsonSyntaxDiagnostic(ref, schemaText, jerr))
				} else if serr := plugins.LintSettingsSchema([]byte(schemaText)); serr != nil {
					var ve *plugins.ValidationError
					if errors.As(serr, &ve) {
						for _, fe := range ve.Fields {
							line, col := locateJSONField(schemaText, fe.Field)
							diags = append(diags, wasm.Diagnostic{
								File:     ref,
								Line:     line,
								Column:   col,
								Text:     fe.Field + ": " + fe.Message,
								Severity: "error",
							})
						}
					}
				}
			}
		}
	}

	// Validate i18n strings files and $t: references.
	enStrings := map[string]string{}
	if man.Ether != nil {
		for loc, raw := range man.Ether.Strings {
			var strPath string
			if json.Unmarshal(raw, &strPath) == nil && strPath != "" {
				strContent, ok := files[strPath]
				if !ok {
					line, col := locateJSONField(manContent, "ether.strings."+loc)
					diags = append(diags, wasm.Diagnostic{
						File:     "manifest.json",
						Line:     line,
						Column:   col,
						Text:     fmt.Sprintf("strings file %q is missing", strPath),
						Severity: "error",
					})
					continue
				}
				var table map[string]string
				if jerr := json.Unmarshal([]byte(strContent), &table); jerr != nil {
					diags = append(diags, jsonSyntaxDiagnostic(strPath, strContent, jerr))
					continue
				}
				if loc == "en" {
					enStrings = table
				}
				for k, val := range table {
					if msg := plugins.LintCopy(val, 120); msg != "" {
						line, col := locateJSONField(strContent, k)
						diags = append(diags, wasm.Diagnostic{
							File:     strPath,
							Line:     line,
							Column:   col,
							Text:     fmt.Sprintf("%s: %s", k, msg),
							Severity: "warning",
						})
					}
				}
			}
		}
	}
	_ = enStrings

	entry := strings.TrimSpace(entryOverride)
	if entry == "" && man.Ether != nil {
		entry = man.Ether.Runtime.Entry
	}
	var js, sm []byte
	if entry != "" {
		var tsDiags []wasm.Diagnostic
		js, sm, tsDiags = wasm.Compile(files, entry)
		for _, td := range tsDiags {
			td.File = strings.TrimPrefix(td.File, "ether-vfs:")
			diags = append(diags, td)
		}
	}

	hasErr := false
	for _, d := range diags {
		if d.Severity == "error" {
			hasErr = true
			break
		}
	}
	return CompileResult{
		OK:            !hasErr,
		Diagnostics:   diags,
		JSSize:        len(js),
		SourcemapSize: len(sm),
		compiledJS:    js,
		sourcemap:     sm,
	}
}

// CheckProject runs the conformance and copy kit against the Studio project.
func (s *Studio) CheckProject(ctx context.Context, id string) (CheckReport, error) {
	p, err := s.GetProject(ctx, id)
	if err != nil {
		return CheckReport{}, err
	}
	return s.CheckFiles(ctx, p.ID, p.Files), nil
}

// CheckFiles runs the conformance and copy checks on a file set.
func (s *Studio) CheckFiles(ctx context.Context, projectID string, files map[string]string) CheckReport {
	_ = projectID
	data := make(map[string][]byte, len(files))
	for name, contents := range files {
		data[name] = []byte(contents)
	}
	pkg := &kit.Package{ManifestRaw: data["manifest.json"], Files: data}
	rep := kit.Check(ctx, pkg)
	checks := make([]CheckItem, 0, len(rep.Errors)+len(rep.Warnings)+1)
	for _, issue := range rep.Errors {
		checks = append(checks, CheckItem{ID: issue.Rule, Category: issue.Rule, Label: issue.Field, Status: "fail", Message: issue.Message})
	}
	for _, issue := range rep.Warnings {
		checks = append(checks, CheckItem{ID: issue.Rule, Category: issue.Rule, Label: issue.Field, Status: "warn", Message: issue.Message})
	}
	if rep.OK {
		checks = append(checks, CheckItem{ID: "conformance", Category: "conformance", Label: "Conformance", Status: "pass", Message: "Plugin passed conformance checks."})
	}
	return CheckReport{OK: rep.OK, Checks: checks, Diagnostics: []wasm.Diagnostic{}}
}

// RunProject executes a request or hot-path call against a project's sandboxed dev instance.
func (s *Studio) RunProject(ctx context.Context, id string, req RunRequest) (RunResponse, error) {
	s.mu.Lock()
	p, ok := s.lookupLocked(id)
	if !ok {
		s.mu.Unlock()
		return RunResponse{}, ErrProjectNotFound
	}
	files := cloneMap(p.Files)
	pluginID := p.PluginID
	projID := p.ID
	defaultPath := p.DefaultPath
	s.mu.Unlock()

	comp := s.CompileFiles(files, "")
	if !comp.OK || len(comp.compiledJS) == 0 {
		msg := "project has compile errors"
		for _, d := range comp.Diagnostics {
			if d.Severity == "error" {
				msg = d.Text
				break
			}
		}
		return RunResponse{}, fmt.Errorf("studio: %s", msg)
	}

	host := &devHost{s: s, projectID: projID}
	plug, err := s.eng.LoadJS(pluginID, "0.1.0-dev", string(comp.compiledJS), host, wasm.Limits{})
	if err != nil {
		return RunResponse{}, err
	}
	defer plug.Close()

	start := s.now()
	if req.HotPath == "rank" || (req.HotPath == "" && !plug.HasHandle() && plug.HasRankKey()) {
		cand := plugins.CandidateFacts{
			ReleaseID:  "cand-1",
			Name:       "Movie.2024.2160p.UHD.BluRay.MULTI.FR.EN",
			Resolution: "2160p",
			Languages:  []string{"fr", "en"},
			Cached:     true,
			Verified:   true,
		}
		if req.Candidate != nil {
			cand = *req.Candidate
		}
		facts, _ := json.Marshal(cand)
		cfg := req.Config
		if len(cfg) == 0 {
			cfg = json.RawMessage(`{"language":"fr"}`)
		}
		keys, out, rerr := plug.RankBatch(ctx, []wasm.Candidate{{ReleaseID: cand.ReleaseID, Facts: facts}}, cfg)
		ms := s.now().Sub(start).Milliseconds()
		var bodyBytes json.RawMessage
		status := http.StatusOK
		errStr := ""
		if rerr != nil {
			status = http.StatusInternalServerError
			errStr = rerr.Error()
			bodyBytes, _ = json.Marshal(map[string]any{"error": errStr})
		} else {
			score := int64(0)
			if len(keys) > 0 {
				score = keys[0]
			}
			bodyBytes, _ = json.Marshal(map[string]any{
				"releaseId": cand.ReleaseID,
				"candidate": cand.Name,
				"score":     score,
				"slow":      out.SlowCandidates,
			})
		}
		return s.recordDevCall(projID, "POST", "hot:rank_key", status, ms, out.Units, facts, bodyBytes, errStr), nil
	}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	routePath := strings.TrimSpace(req.Path)
	if routePath == "" {
		routePath = defaultPath
	}
	if routePath == "" {
		routePath = "/ether/health"
	}
	q := make(map[string][]string, len(req.Query))
	for k, v := range req.Query {
		q[k] = []string{v}
	}
	headers := map[string]string{
		"X-Ether-Deadline": start.Add(plugins.DeadlineFor(routePath)).UTC().Format(time.RFC3339),
	}
	for k, v := range req.Headers {
		headers[k] = v
	}
	if req.Locale != "" {
		headers["X-Ether-Locale"] = req.Locale
	}

	resp, outcome, callErr := plug.CallDetailed(ctx, wasm.Request{
		Method:  method,
		Path:    routePath,
		Query:   q,
		Headers: headers,
		Body:    req.Body,
	})
	ms := s.now().Sub(start).Milliseconds()
	body := unwrapBody(resp.Body)
	status := resp.Status
	errStr := ""
	if callErr != nil {
		errStr = callErr.Error()
		if status == 0 {
			status = http.StatusInternalServerError
		}
	} else if status == 0 {
		status = http.StatusOK
	}
	return s.recordDevCall(projID, method, routePath, status, ms, outcome.Units, req.Body, body, errStr), nil
}

func (s *Studio) recordDevCall(
	projID, method, route string,
	status int,
	ms, units int64,
	reqBody, respBody json.RawMessage,
	errStr string,
) RunResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.lookupLocked(projID)
	if !ok {
		return RunResponse{Status: status, Body: respBody, MS: ms, Units: units}
	}
	p.nextCallID++
	call := plugins.CallRecord{
		ID:       p.nextCallID,
		Plugin:   p.PluginID,
		Route:    route,
		Method:   method,
		Status:   status,
		MS:       ms,
		Error:    errStr,
		Request:  RedactJSON(reqBody),
		Response: RedactJSON(respBody),
		Created:  s.now(),
	}
	p.calls = append([]plugins.CallRecord{call}, p.calls...)
	if len(p.calls) > plugins.CallsCap {
		p.calls = p.calls[:plugins.CallsCap]
	}
	hints := InspectCall(call)
	logs := append([]string(nil), p.logs...)
	journal := append([]plugins.JournalEntry(nil), p.journal...)
	return RunResponse{
		Status:  status,
		Body:    respBody,
		MS:      ms,
		Units:   units,
		Logs:    logs,
		Call:    call,
		Hints:   hints,
		Journal: journal,
	}
}

// PreviewProject fetches and validates a page/slot ViewSpec document or capability
// route output from the project's sandboxed dev instance.
func (s *Studio) PreviewProject(
	ctx context.Context,
	id, pageKey, routeOverride string,
	params map[string]string,
) (PreviewResponse, error) {
	p, err := s.GetProject(ctx, id)
	if err != nil {
		return PreviewResponse{}, err
	}
	man, _ := plugins.ParseManifest([]byte(p.Files["manifest.json"]))
	route := strings.TrimSpace(routeOverride)
	page := strings.TrimSpace(pageKey)

	if route == "" && man.Ether != nil {
		if page != "" {
			for _, pg := range man.Ether.Contributes.Pages {
				if pg.Key == page {
					route = pg.Route
					break
				}
			}
			if route == "" {
				for _, sl := range man.Ether.Contributes.Slots {
					if sl.Key == page || sl.Slot == page || strings.ReplaceAll(sl.Slot, ".", "-") == page {
						route = sl.Route
						break
					}
				}
			}
		}
		if route == "" && len(man.Ether.Contributes.Pages) > 0 {
			page = man.Ether.Contributes.Pages[0].Key
			route = man.Ether.Contributes.Pages[0].Route
		}
		if route == "" && len(man.Ether.Contributes.Slots) > 0 {
			page = man.Ether.Contributes.Slots[0].Key
			route = man.Ether.Contributes.Slots[0].Route
		}
	}
	if route == "" {
		route = p.DefaultPath
	}
	if route == "" {
		route = "/ether/health"
	}

	runResp, err := s.RunProject(ctx, p.ID, RunRequest{
		Method: http.MethodGet,
		Path:   route,
		Query:  params,
		Locale: params["locale"],
	})
	if err != nil {
		return PreviewResponse{}, err
	}

	var viewDoc struct {
		Title string          `json:"title,omitempty"`
		Spec  json.RawMessage `json:"spec"`
		Data  json.RawMessage `json:"data"`
	}
	if json.Unmarshal(runResp.Body, &viewDoc) == nil && len(viewDoc.Spec) > 0 {
		resolvedSpec := resolveStringsInJSON(viewDoc.Spec, p.Files, params["locale"])
		vres := viewspec.ValidatePlugin(resolvedSpec)
		data := viewDoc.Data
		if len(data) == 0 {
			data = json.RawMessage("{}")
		}
		return PreviewResponse{
			Kind:   "view",
			Route:  route,
			Page:   page,
			Title:  viewDoc.Title,
			Spec:   resolvedSpec,
			Data:   data,
			Body:   runResp.Body,
			Valid:  vres.OK,
			Errors: vres.Errors,
			Hints:  runResp.Hints,
			MS:     runResp.MS,
		}, nil
	}

	return PreviewResponse{
		Kind:  "route",
		Route: route,
		Body:  runResp.Body,
		Valid: runResp.Status >= 200 && runResp.Status < 300,
		Hints: runResp.Hints,
		MS:    runResp.MS,
	}, nil
}

// PreviewInstallProject prepares a Studio project for installation through the
// plugin host's permission sheet.
func (s *Studio) PreviewInstallProject(ctx context.Context, id string) (*plugins.Preview, error) {
	if s.mgr == nil {
		return nil, fmt.Errorf("studio: plugin manager is not configured")
	}
	src, err := s.buildInstallSource(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.mgr.PreviewInstall(ctx, src)
}

// InstallProject installs a Studio project into the live plugin host after the
// owner confirms the permission sheet.
func (s *Studio) InstallProject(ctx context.Context, id string, opts plugins.InstallOptions) (plugins.Record, error) {
	if s.mgr == nil {
		return plugins.Record{}, fmt.Errorf("studio: plugin manager is not configured")
	}
	preview, err := s.PreviewInstallProject(ctx, id)
	if err != nil {
		return plugins.Record{}, err
	}
	return s.mgr.Install(ctx, preview, opts)
}

func (s *Studio) buildInstallSource(ctx context.Context, id string) (plugins.Source, error) {
	p, err := s.GetProject(ctx, id)
	if err != nil {
		return plugins.Source{}, err
	}
	comp := s.CompileFiles(p.Files, "")
	if !comp.OK {
		return plugins.Source{}, fmt.Errorf("studio: fix compile and manifest errors before installing")
	}
	byteFiles := make(map[string][]byte, len(p.Files)+2)
	for k, v := range p.Files {
		byteFiles[k] = []byte(v)
	}
	man, err := plugins.ParseManifest([]byte(p.Files["manifest.json"]))
	if err != nil {
		return plugins.Source{}, err
	}
	if man.Ether != nil && man.Ether.Runtime.Entry != "" && len(comp.compiledJS) > 0 {
		byteFiles[man.Ether.Runtime.Entry] = comp.compiledJS
		if len(comp.sourcemap) > 0 {
			byteFiles[man.Ether.Runtime.Entry+".map"] = comp.sourcemap
		}
	}
	return plugins.Source{
		Kind:     "studio",
		Manifest: json.RawMessage(p.Files["manifest.json"]),
		Files:    byteFiles,
	}, nil
}

// SetShadow toggles shadow mode on a Studio project's dev instance.
func (s *Studio) SetShadow(_ context.Context, id string, shadow bool) (*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.lookupLocked(id)
	if !ok {
		return nil, ErrProjectNotFound
	}
	p.Shadow = shadow
	p.UpdatedAt = s.now()
	return cloneProject(p), nil
}

// Journal returns the dev instance's recorded writes (shadow and live).
func (s *Studio) Journal(_ context.Context, id string, shadowOnly bool) ([]plugins.JournalEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.lookupLocked(id)
	if !ok {
		return nil, ErrProjectNotFound
	}
	out := make([]plugins.JournalEntry, 0, len(p.journal))
	for _, e := range p.journal {
		if shadowOnly && !e.Shadow {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// Undo reverts all non-undone entries in the dev instance's journal, newest first.
func (s *Studio) Undo(_ context.Context, id string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.lookupLocked(id)
	if !ok {
		return 0, ErrProjectNotFound
	}
	undone := 0
	for i := range p.journal {
		if p.journal[i].Undone {
			continue
		}
		e := &p.journal[i]
		if !e.Shadow {
			switch e.Op {
			case "collections.upsert":
				var inv struct {
					ID       string          `json:"id"`
					Previous json.RawMessage `json:"previous"`
				}
				if json.Unmarshal(e.Inverse, &inv) == nil && inv.ID != "" {
					if len(inv.Previous) > 0 {
						p.collections[inv.ID] = inv.Previous
					} else {
						delete(p.collections, inv.ID)
					}
				}
			case "viewers.setWatchState":
				var inv struct {
					Key     string `json:"key"`
					Watched bool   `json:"watched"`
				}
				if json.Unmarshal(e.Inverse, &inv) == nil && inv.Key != "" {
					p.watchState[inv.Key] = inv.Watched
				}
			}
		}
		e.Undone = true
		undone++
	}
	return undone, nil
}

// Calls returns the dev instance's recorded calls and plain-language validation hints.
func (s *Studio) Calls(_ context.Context, id string) ([]plugins.CallRecord, []ValidationHint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.lookupLocked(id)
	if !ok {
		return nil, nil, ErrProjectNotFound
	}
	calls := append([]plugins.CallRecord(nil), p.calls...)
	return calls, InspectCalls(calls), nil
}

// devHost implements wasm.Host for a Studio sandboxed dev instance.
type devHost struct {
	s         *Studio
	projectID string
}

func (h *devHost) CallHost(ctx context.Context, name string, raw json.RawMessage) (json.RawMessage, error) {
	var args []json.RawMessage
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, &wasm.Fault{Code: "bad_arguments", Message: "host arguments must be an array"}
		}
	}
	str := func(i int) string {
		var v string
		if i < len(args) {
			_ = json.Unmarshal(args[i], &v)
		}
		return v
	}
	num := func(i int) int64 {
		var n float64
		if i < len(args) {
			_ = json.Unmarshal(args[i], &n)
		}
		return int64(n)
	}

	h.s.mu.Lock()
	p, ok := h.s.lookupLocked(h.projectID)
	if !ok {
		h.s.mu.Unlock()
		return nil, wasm.Forbidden("studio project not found")
	}

	recordJournal := func(op, detail string, inverse any) {
		p.nextJournID++
		invBytes, _ := json.Marshal(inverse)
		entry := plugins.JournalEntry{
			ID:      p.nextJournID,
			Plugin:  p.PluginID,
			Op:      op,
			Detail:  detail,
			Inverse: invBytes,
			Shadow:  p.Shadow,
			Created: h.s.now(),
		}
		p.journal = append([]plugins.JournalEntry{entry}, p.journal...)
	}

	switch name {
	case "log":
		level, msg := str(0), str(1)
		if len(args) == 1 {
			level, msg = "info", str(0)
		}
		line := level + ": " + msg
		if len(args) > 2 {
			line += " " + RedactJSON(args[2])
		}
		p.logs = append(p.logs, line)
		if len(p.logs) > plugins.LogsCap {
			p.logs = p.logs[len(p.logs)-plugins.LogsCap:]
		}
		h.s.mu.Unlock()
		return json.RawMessage("null"), nil

	case "kv.get":
		v, exists := p.kv[str(0)]
		h.s.mu.Unlock()
		if !exists {
			return json.RawMessage("null"), nil
		}
		out, _ := json.Marshal(string(v))
		return out, nil

	case "kv.put":
		key := str(0)
		val := str(1)
		if val == "" && len(args) > 1 {
			val = string(args[1])
		}
		p.kv[key] = []byte(val)
		h.s.mu.Unlock()
		return json.RawMessage("null"), nil

	case "kv.delete":
		delete(p.kv, str(0))
		h.s.mu.Unlock()
		return json.RawMessage("null"), nil

	case "kv.list":
		prefix := str(0)
		keys := []string{}
		for k := range p.kv {
			if strings.HasPrefix(k, prefix) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		h.s.mu.Unlock()
		out, _ := json.Marshal(keys)
		return out, nil

	case "collections.upsert":
		var colDoc map[string]any
		var rawDoc json.RawMessage
		if len(args) > 0 {
			rawDoc = args[0]
			_ = json.Unmarshal(args[0], &colDoc)
		}
		colID, _ := colDoc["id"].(string)
		colName, _ := colDoc["name"].(string)
		if colName == "" {
			colName = colID
		}
		prev := p.collections[colID]
		if !p.Shadow && colID != "" {
			p.collections[colID] = rawDoc
		}
		recordJournal("collections.upsert", colName, map[string]any{
			"id":       colID,
			"previous": prev,
			"doc":      rawDoc,
		})
		h.s.mu.Unlock()
		return json.RawMessage("null"), nil

	case "viewers.setWatchState":
		viewerID := num(0)
		titleID := str(1)
		var watched bool
		if len(args) > 2 {
			_ = json.Unmarshal(args[2], &watched)
		}
		key := fmt.Sprintf("%d:%s", viewerID, titleID)
		prev := p.watchState[key]
		if !p.Shadow {
			p.watchState[key] = watched
		}
		recordJournal("viewers.setWatchState", titleID, map[string]any{
			"key":      key,
			"viewerId": viewerID,
			"titleId":  titleID,
			"watched":  prev,
		})
		h.s.mu.Unlock()
		return json.RawMessage("null"), nil

	case "notify.owner":
		text := str(0)
		recordJournal("notify.owner", text, map[string]any{"text": text})
		p.logs = append(p.logs, "notice: "+text)
		h.s.mu.Unlock()
		return json.RawMessage("null"), nil

	case "catalog.search":
		h.s.mu.Unlock()
		return json.RawMessage(`{"titles":[{"id":1,"title":"The Matrix","type":"movie","year":1999}]}`), nil

	case "catalog.get":
		h.s.mu.Unlock()
		return json.RawMessage(`{"id":1,"title":"The Matrix","type":"movie","year":1999}`), nil

	case "folders.list":
		h.s.mu.Unlock()
		return json.RawMessage(`[{"name":"Movie (2024)","path":"movies/Movie (2024)","dir":true}]`), nil

	case "folders.stat":
		p := str(1)
		h.s.mu.Unlock()
		out, _ := json.Marshal(plugins.FolderEntry{Name: path.Base(p), Path: p, Dir: false, Size: 1024})
		return out, nil

	default:
		h.s.mu.Unlock()
		return json.RawMessage("null"), nil
	}
}

func unwrapBody(raw json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return json.RawMessage("{}")
	}
	if strings.HasPrefix(trimmed, `"`) {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			sTrim := strings.TrimSpace(s)
			if strings.HasPrefix(sTrim, "{") || strings.HasPrefix(sTrim, "[") {
				return json.RawMessage(sTrim)
			}
		}
	}
	return raw
}

func resolveStringsInJSON(raw json.RawMessage, files map[string]string, locale string) json.RawMessage {
	table := map[string]string{}
	if enRaw, ok := files["i18n/en.json"]; ok {
		_ = json.Unmarshal([]byte(enRaw), &table)
	}
	if locale != "" && locale != "en" {
		if locRaw, ok := files["i18n/"+locale+".json"]; ok {
			var locTable map[string]string
			if json.Unmarshal([]byte(locRaw), &locTable) == nil {
				for k, v := range locTable {
					table[k] = v
				}
			}
		}
	}
	if len(table) == 0 {
		return raw
	}
	var node any
	if json.Unmarshal(raw, &node) != nil {
		return raw
	}
	var walk func(any) any
	walk = func(v any) any {
		switch t := v.(type) {
		case string:
			if strings.HasPrefix(t, "$t:") {
				key := strings.TrimPrefix(t, "$t:")
				if resolved, ok := table[key]; ok {
					return resolved
				}
			}
			return t
		case map[string]any:
			for k, child := range t {
				t[k] = walk(child)
			}
			return t
		case []any:
			for i, child := range t {
				t[i] = walk(child)
			}
			return t
		default:
			return v
		}
	}
	out, err := json.Marshal(walk(node))
	if err != nil {
		return raw
	}
	return out
}

func settingsRefOf(raw json.RawMessage) string {
	var doc map[string]any
	if json.Unmarshal(raw, &doc) == nil {
		if ref, ok := doc["$ref"].(string); ok {
			return ref
		}
	}
	return ""
}

func cloneMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneProject(p *Project) *Project {
	cp := *p
	cp.Files = cloneMap(p.Files)
	cp.Diagnostics = append([]wasm.Diagnostic(nil), p.Diagnostics...)
	cp.calls = append([]plugins.CallRecord(nil), p.calls...)
	cp.logs = append([]string(nil), p.logs...)
	cp.journal = append([]plugins.JournalEntry(nil), p.journal...)
	return &cp
}
