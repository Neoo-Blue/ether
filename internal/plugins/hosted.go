package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/braedonsaunders/ether/internal/plugins/wasm"
)

// HotRuntime is implemented by hosted runtimes whose plugin exports the
// hot-path ABI (SPEC §5.2): ranking steps and rule conditions. The ranking
// adapters reach it through Manager.Hot.
type HotRuntime interface {
	Runtime
	// RankBatch returns one key per candidate (higher ranks first), neutral
	// (0) for candidates that exceeded the per-candidate budget, plus the
	// number of slow candidates for health.
	RankBatch(ctx context.Context, cands []CandidateFacts, config json.RawMessage) (keys []int64, slow int, err error)
	// CondMatch evaluates a rule condition; false on budget exhaustion.
	CondMatch(ctx context.Context, cand CandidateFacts, operator string, value json.RawMessage) (bool, error)
	HasRankKey() bool
	HasCondMatch() bool
}

// HostedRuntimeFactory opens hosted plugins (runtime.kind "hosted") on the
// wazero engine: JavaScript and TypeScript entries run on QuickJS, raw
// modules through the documented WASM ABI.
func HostedRuntimeFactory(eng *wasm.Engine) RuntimeFactory {
	return func(inst Installed) (Runtime, error) {
		if inst.Manifest.Ether == nil {
			return nil, fmt.Errorf("plugin %s: hosted plugins need an ether block", inst.ID)
		}
		decl := inst.Manifest.Ether.Runtime
		host := hostBridge{h: inst.Host}
		lim := wasm.Limits{}
		switch {
		case decl.Module != "":
			module, ok := inst.Files(decl.Module)
			if !ok {
				return nil, fmt.Errorf("plugin %s: module %q is missing", inst.ID, decl.Module)
			}
			p, err := eng.LoadWASM(inst.ID, inst.Manifest.Version, module, host, lim)
			if err != nil {
				return nil, err
			}
			return &hostedRuntime{p: p}, nil
		case decl.Entry != "":
			src, ok := inst.Files(decl.Entry)
			if !ok {
				return nil, fmt.Errorf("plugin %s: entry %q is missing", inst.ID, decl.Entry)
			}
			js := string(src)
			if ext := strings.ToLower(path.Ext(decl.Entry)); ext == ".ts" || ext == ".tsx" || ext == ".mts" {
				out, _, diags := wasm.Compile(map[string]string{decl.Entry: js}, decl.Entry)
				if err := diagnosticsError(diags); err != nil {
					return nil, fmt.Errorf("plugin %s: %w", inst.ID, err)
				}
				js = string(out)
			}
			p, err := eng.LoadJS(inst.ID, inst.Manifest.Version, js, host, lim)
			if err != nil {
				return nil, err
			}
			return &hostedRuntime{p: p}, nil
		default:
			return nil, fmt.Errorf("plugin %s: hosted runtime needs an entry or a module", inst.ID)
		}
	}
}

func diagnosticsError(diags []wasm.Diagnostic) error {
	var msgs []string
	for _, d := range diags {
		if d.Severity == "error" {
			msgs = append(msgs, d.Text)
		}
	}
	if len(msgs) == 0 {
		return nil
	}
	return errors.New("compile: " + strings.Join(msgs, "; "))
}

type hostedRuntime struct{ p *wasm.Plugin }

func (r *hostedRuntime) Call(ctx context.Context, req Request) (Response, error) {
	q := make(map[string][]string, len(req.Query))
	for k, v := range req.Query {
		q[k] = []string{v}
	}
	headers := req.Headers
	if req.Locale != "" {
		headers = cloneHeaders(headers)
		headers["X-Ether-Locale"] = req.Locale
	}
	resp, err := r.p.Call(ctx, wasm.Request{Method: req.Method, Path: req.Path, Query: q, Headers: headers, Body: req.Body})
	if err != nil {
		return Response{}, err
	}
	return Response{Status: resp.Status, Headers: resp.Headers, Body: resp.Body}, nil
}

func (r *hostedRuntime) Close() error       { return r.p.Close() }
func (r *hostedRuntime) HasRankKey() bool   { return r.p.HasRankKey() }
func (r *hostedRuntime) HasCondMatch() bool { return r.p.HasCondMatch() }

func (r *hostedRuntime) RankBatch(ctx context.Context, cands []CandidateFacts, config json.RawMessage) ([]int64, int, error) {
	in := make([]wasm.Candidate, len(cands))
	for i, c := range cands {
		facts, err := json.Marshal(c)
		if err != nil {
			return nil, 0, err
		}
		in[i] = wasm.Candidate{ReleaseID: c.ReleaseID, Facts: facts}
	}
	keys, out, err := r.p.RankBatch(ctx, in, config)
	return keys, out.SlowCandidates, err
}

func (r *hostedRuntime) CondMatch(ctx context.Context, cand CandidateFacts, operator string, value json.RawMessage) (bool, error) {
	facts, err := json.Marshal(cand)
	if err != nil {
		return false, err
	}
	ok, _, err := r.p.CondMatch(ctx, wasm.Candidate{ReleaseID: cand.ReleaseID, Facts: facts}, operator, value)
	return ok, err
}

func cloneHeaders(h map[string]string) map[string]string {
	out := make(map[string]string, len(h)+1)
	for k, v := range h {
		out[k] = v
	}
	return out
}

// Hot returns the hosted runtime of an enabled plugin when it exports the
// hot-path ABI. Callers on the play path must pass a context with the
// resolve's deadline.
func (m *Manager) Hot(ctx context.Context, pluginID string) (HotRuntime, bool) {
	r, err := m.Get(ctx, pluginID)
	if err != nil || !r.Enabled {
		return nil, false
	}
	rt, err := m.runtimeFor(ctx, r)
	if err != nil {
		return nil, false
	}
	hot, ok := rt.(HotRuntime)
	return hot, ok
}

// hostBridge maps the hosted runtime's dotted host calls (positional JSON
// argument arrays) onto the Host interface. Grant checks, journalling and
// shadow mode all live behind Host.
type hostBridge struct{ h Host }

func (b hostBridge) CallHost(ctx context.Context, name string, raw json.RawMessage) (json.RawMessage, error) {
	if b.h == nil {
		return nil, wasm.Forbidden("no host services are configured for this plugin")
	}
	var args []json.RawMessage
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, &wasm.Fault{Code: "bad_arguments", Message: "host arguments must be an array"}
		}
	}
	str := func(i int) string {
		var s string
		if i < len(args) {
			_ = json.Unmarshal(args[i], &s)
		}
		return s
	}
	num := func(i int) int64 {
		var n float64
		if i < len(args) {
			_ = json.Unmarshal(args[i], &n)
		}
		return int64(n)
	}
	result := func(v any, err error) (json.RawMessage, error) {
		if err != nil {
			return nil, mapHostError(err)
		}
		if v == nil {
			return json.RawMessage("null"), nil
		}
		if r, ok := v.(json.RawMessage); ok {
			return r, nil
		}
		out, err := json.Marshal(v)
		return out, err
	}
	switch name {
	case "kv.get":
		v, ok, err := b.h.KVGet(ctx, str(0))
		if err != nil || !ok {
			return result(nil, err)
		}
		return result(string(v), nil)
	case "kv.put":
		var value string
		if len(args) > 1 {
			if err := json.Unmarshal(args[1], &value); err != nil {
				value = string(args[1]) // non-string values are stored as JSON text
			}
		}
		return result(nil, b.h.KVPut(ctx, str(0), []byte(value)))
	case "kv.delete":
		return result(nil, b.h.KVDelete(ctx, str(0)))
	case "kv.list":
		keys, err := b.h.KVList(ctx, str(0))
		return result(keys, err)
	case "secret.get":
		v, err := b.h.SecretGet(ctx, str(0))
		return result(v, err)
	case "http.fetch":
		var req struct {
			Method  string            `json:"method"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
			Body    string            `json:"body"`
		}
		if len(args) > 0 {
			if err := json.Unmarshal(args[0], &req); err != nil {
				return nil, &wasm.Fault{Code: "bad_arguments", Message: "http.fetch takes {method, url, headers, body}"}
			}
		}
		if req.Method == "" {
			req.Method = "GET"
		}
		resp, err := b.h.HTTPFetch(ctx, FetchRequest{Method: req.Method, URL: req.URL, Headers: req.Headers, Body: []byte(req.Body)})
		if err != nil {
			return nil, mapHostError(err)
		}
		return result(map[string]any{"status": resp.Status, "headers": resp.Headers, "body": string(resp.Body)}, nil)
	case "catalog.search":
		v, err := b.h.CatalogSearch(ctx, str(0), int(num(1)), int(num(2)))
		return result(v, err)
	case "catalog.get":
		v, err := b.h.CatalogGet(ctx, str(0))
		return result(v, err)
	case "viewers.history":
		v, err := b.h.ViewerHistory(ctx, num(0), int(num(1)), int(num(2)))
		return result(v, err)
	case "viewers.setWatchState":
		var watched bool
		if len(args) > 2 {
			_ = json.Unmarshal(args[2], &watched)
		}
		return result(nil, b.h.SetWatchState(ctx, num(0), str(1), watched))
	case "collections.upsert":
		var col json.RawMessage
		if len(args) > 0 {
			col = args[0]
		}
		return result(nil, b.h.CollectionsUpsert(ctx, col))
	case "folders.list":
		v, err := b.h.FoldersList(ctx, str(0), str(1))
		return result(v, err)
	case "folders.stat":
		v, err := b.h.FoldersStat(ctx, str(0), str(1))
		return result(v, err)
	case "notify.owner":
		return result(nil, b.h.NotifyOwner(ctx, str(0)))
	case "log":
		level, msg := str(0), str(1)
		if len(args) == 1 {
			level, msg = "info", str(0)
		}
		var attrs map[string]any
		if len(args) > 2 {
			_ = json.Unmarshal(args[2], &attrs)
		}
		return result(nil, b.h.Log(ctx, level, msg, attrs))
	default:
		return nil, wasm.Forbidden("unknown host service " + name)
	}
}

func mapHostError(err error) error {
	switch {
	case errors.Is(err, ErrPermissionDenied):
		return wasm.Forbidden(err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return &wasm.Fault{Code: string(wasm.StatusTimeout), Message: "host call timed out"}
	default:
		return &wasm.Fault{Code: "host_error", Message: err.Error()}
	}
}
