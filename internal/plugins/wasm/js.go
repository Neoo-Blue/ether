package wasm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/fastschema/qjs"
)

// faultPrefix marks a classified fault inside a JS exception message, the
// same trick appkit-sandbox uses to carry faults across the boundary.
const faultPrefix = "__ETHER_FAULT__"

func encodeFault(f *Fault) string {
	b, _ := json.Marshal(map[string]string{"code": f.Code, "message": f.Message})
	return faultPrefix + string(b)
}

func decodeFault(msg string) *Fault {
	at := strings.Index(msg, faultPrefix)
	if at < 0 {
		return nil
	}
	var f Fault
	// The payload may be followed by a JS stack trace; decode one value.
	dec := json.NewDecoder(strings.NewReader(msg[at+len(faultPrefix):]))
	var raw struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if dec.Decode(&raw) != nil || raw.Code == "" {
		return nil
	}
	f.Code, f.Message = raw.Code, raw.Message
	return &f
}

var qjsOnce sync.Once

// ensureQJS creates the first QuickJS runtime in the process with
// CloseOnContextDone enabled. fastschema/qjs caches its wazero runtime
// config package-globally from the first New call, and context-driven
// module close is the only reliable way to interrupt a tight JS loop
// (its MaxExecutionTime does not interrupt one), so the first runtime
// must be ours.
func ensureQJS() {
	qjsOnce.Do(func() {
		defer func() { _ = recover() }()
		if rt, err := qjs.New(qjs.Option{Context: context.Background(), CloseOnContextDone: true,
			Stdout: io.Discard, Stderr: io.Discard}); err == nil {
			defer func() { _ = recover() }()
			rt.Close()
		}
	})
}

// servicePrelude builds the ether global for request-ABI instances. Host
// calls are synchronous functions backed by __ether_host, charged by the
// Go side; a Fault comes back as a thrown Error whose message carries the
// fault prefix, and ether.abort throws an aborted fault.
const servicePrelude = `
"use strict";
globalThis.ether = (() => {
  const call = (name, args) => JSON.parse(__ether_host(name, JSON.stringify(args)));
  const ether = {
    log: (...a) => { try { __ether_host("log", JSON.stringify(a.map(String))); } catch (e) {} },
    abort: (msg) => { throw new Error("__ETHER_FAULT__" + JSON.stringify({ code: "aborted", message: String(msg == null ? "aborted" : msg) })); },
    kv: { get: (k) => call("kv.get", [k]), put: (k, v) => call("kv.put", [k, v]),
          delete: (k) => call("kv.delete", [k]), list: (p) => call("kv.list", [p == null ? "" : p]) },
    secret: { get: (n) => call("secret.get", [n]) },
    http: { fetch: (req) => call("http.fetch", [req]) },
    catalog: { search: (q) => call("catalog.search", [q]), get: (id) => call("catalog.get", [id]) },
    viewers: { history: (id, page) => call("viewers.history", [id, page == null ? 0 : page]),
               setWatchState: (v) => call("viewers.setWatchState", [v]) },
    collections: { upsert: (c) => call("collections.upsert", [c]) },
    folders: { list: (root, path) => call("folders.list", [root, path == null ? "" : path]),
               stat: (root, path) => call("folders.stat", [root, path]) },
    notify: { owner: (text) => call("notify.owner", [text]) },
  };
  return ether;
})();
globalThis.console = { log: (...a) => ether.log(...a), error: (...a) => ether.log(...a), warn: (...a) => ether.log(...a) };
`

// hotPrelude locks a hot-path instance down: no host services except log
// and abort, Date fixed at the epoch, Math.random a seeded generator that
// __ether_rank_batch resets per batch. The monotonic clock is captured in
// a closure for budget checks, then removed from the global scope.
const hotPrelude = `
"use strict";
globalThis.ether = (() => {
  const now = globalThis.__ether_now_ms;
  globalThis.__ether_now_ms = undefined;
  let state = 0x2545F491;
  const nextRandom = () => { state = (state * 1103515245 + 12345) & 0x7fffffff; return state / 0x80000000; };
  class FixedDate extends Date { constructor(...a) { super(...(a.length ? a : [0])); } static now() { return 0; } }
  globalThis.Date = FixedDate;
  Math.random = nextRandom;
  globalThis.__ether_rank_batch = (candsJson, cfgJson, perMs) => {
    state = 0x2545F491;
    const cands = JSON.parse(candsJson);
    const cfg = JSON.parse(cfgJson);
    const out = new Array(cands.length);
    for (let i = 0; i < cands.length; i++) {
      const t0 = now();
      const v = rank_key(JSON.parse(cands[i]), cfg);
      out[i] = (now() - t0 > perMs) ? null : (typeof v === "bigint" ? Number(v) : v);
    }
    return JSON.stringify(out);
  };
  return {
    log: (...a) => { try { __ether_host("log", JSON.stringify(a.map(String))); } catch (e) {} },
    abort: (msg) => { throw new Error("__ETHER_FAULT__" + JSON.stringify({ code: "aborted", message: String(msg == null ? "aborted" : msg) })); },
  };
})();
globalThis.console = { log: (...a) => ether.log(...a), error: (...a) => ether.log(...a), warn: (...a) => ether.log(...a) };
`

// exportProbe reports entry points and normalizes esbuild's IIFE exports
// (global name __plugin) onto globalThis so both authoring styles work.
const exportProbe = `
(() => {
  const pick = (n) => (typeof globalThis[n] === "function" ? globalThis[n]
    : (globalThis.__plugin && typeof globalThis.__plugin[n] === "function" ? globalThis.__plugin[n] : undefined));
  for (const n of ["handle", "rank_key", "cond_match"]) { const f = pick(n); if (f) globalThis[n] = f; }
  return JSON.stringify({ handle: typeof globalThis.handle === "function",
    rank_key: typeof globalThis.rank_key === "function",
    cond_match: typeof globalThis.cond_match === "function" });
})()
`

type jsInstance struct {
	rt           *qjs.Runtime
	qc           *qjs.Context
	instanceCaps caps
	hot          bool
	cs           *callState // set for the duration of one call
}

func newJSInstance(source string, lim *Limits, hot bool) (in *jsInstance, err error) {
	defer func() {
		if r := recover(); r != nil {
			in, err = nil, &Error{Status: StatusError, Message: fmt.Sprintf("create JS instance: %v", r), Trap: true}
		}
	}()
	rt, err2 := qjs.New(qjs.Option{Context: context.Background(), CloseOnContextDone: true,
		MemoryLimit: int(lim.MemoryBytes), MaxStackSize: 1 << 20,
		Stdout: io.Discard, Stderr: io.Discard})
	if err2 != nil {
		return nil, &Error{Status: StatusError, Message: "create JS runtime: " + err2.Error(), Trap: true}
	}
	in = &jsInstance{rt: rt, qc: rt.Context(), hot: hot}
	qc := in.qc
	qc.SetFunc("__ether_host", func(this *qjs.This) (*qjs.Value, error) {
		args := this.Args()
		if in.cs == nil || len(args) != 2 {
			return nil, errors.New(encodeFault(Forbidden("host call outside a plugin call")))
		}
		out, fault := in.cs.dispatch(args[0].String(), json.RawMessage(args[1].String()))
		if fault != nil {
			return nil, errors.New(encodeFault(fault))
		}
		return qc.NewString(string(out)), nil
	})
	qc.SetFunc("__ether_now_ms", func(this *qjs.This) (*qjs.Value, error) {
		return qc.NewFloat64(float64(time.Now().UnixNano()) / 1e6), nil
	})
	prelude := servicePrelude
	if hot {
		prelude = hotPrelude
	}
	if _, err := qc.Eval("ether-prelude.js", qjs.Code(prelude)); err != nil {
		in.closeInstance()
		return nil, &Error{Status: StatusError, Message: "prelude: " + err.Error(), Trap: true}
	}
	if _, err := qc.Eval("plugin.js", qjs.Code(source)); err != nil {
		in.closeInstance()
		return nil, &Error{Status: StatusError, Message: "load plugin: " + firstLine(err.Error()), Trap: false}
	}
	v, err := qc.Eval("ether-probe.js", qjs.Code(exportProbe))
	if err != nil {
		in.closeInstance()
		return nil, &Error{Status: StatusError, Message: "probe exports: " + err.Error(), Trap: true}
	}
	var found struct {
		Handle    bool `json:"handle"`
		RankKey   bool `json:"rank_key"`
		CondMatch bool `json:"cond_match"`
	}
	if err := json.Unmarshal([]byte(v.String()), &found); err != nil {
		in.closeInstance()
		return nil, &Error{Status: StatusError, Message: "probe exports: " + err.Error(), Trap: true}
	}
	in.instanceCaps = caps{Handle: found.Handle, RankKey: found.RankKey, CondMatch: found.CondMatch}
	return in, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func (in *jsInstance) caps() caps { return in.instanceCaps }

func (in *jsInstance) closeInstance() {
	defer func() { _ = recover() }()
	in.rt.Close()
}

// jsCall runs fn with the call context installed, converting qjs panics
// (it panics when a deadline closes the module mid-call) into *Error.
func (in *jsInstance) jsCall(ctx context.Context, cs *callState, fn func() (*qjs.Value, error)) (val *qjs.Value, callErr *Error) {
	in.cs = cs
	in.qc.Context = ctx
	defer func() {
		in.cs = nil
		in.qc.Context = context.Background()
		if r := recover(); r != nil {
			val = nil
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				callErr = &Error{Status: StatusTimeout, Message: "call exceeded its deadline", Trap: true}
				return
			}
			callErr = &Error{Status: StatusError, Message: fmt.Sprintf("%v", r), Trap: true}
		}
	}()
	v, err := fn()
	if err != nil {
		return nil, classifyJSError(ctx, err)
	}
	if v != nil && v.IsPromise() {
		v, err = v.Await()
		if err != nil {
			return nil, classifyJSError(ctx, err)
		}
	}
	return v, nil
}

func classifyJSError(ctx context.Context, err error) *Error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &Error{Status: StatusTimeout, Message: "call exceeded its deadline", Trap: true}
	}
	msg := err.Error()
	if f := decodeFault(msg); f != nil {
		st := statusForFaultCode(f.Code)
		return &Error{Status: st, Message: f.Message, Trap: st == StatusAborted}
	}
	if strings.Contains(msg, "module closed") || strings.Contains(msg, "interrupted") {
		return &Error{Status: StatusTimeout, Message: "call exceeded its deadline", Trap: true}
	}
	return &Error{Status: StatusError, Message: firstLine(msg)}
}

func (in *jsInstance) globalFn(name string) *qjs.Value {
	return in.qc.Global().GetPropertyStr(name)
}

func (in *jsInstance) handle(ctx context.Context, cs *callState, reqJSON []byte) ([]byte, *Error) {
	v, err := in.jsCall(ctx, cs, func() (*qjs.Value, error) {
		g := in.qc.Global()
		return in.qc.Invoke(in.globalFn("__ether_dispatch_handle"), g, in.qc.NewString(string(reqJSON)))
	})
	if err != nil {
		// Fall back to calling handle directly with a parsed object; the
		// dispatch wrapper is installed by installDispatch at load. If it is
		// missing this error is the real one.
		return nil, err
	}
	return []byte(v.String()), nil
}

func (in *jsInstance) rankOne(ctx context.Context, cs *callState, cand, cfg []byte) (int64, *Error) {
	scores, err := in.rankBatch(ctx, cs, [][]byte{cand}, cfg, time.Duration(1<<62))
	if err != nil {
		return 0, err
	}
	if scores[0] == nil {
		return 0, &Error{Status: StatusTimeout, Message: "candidate exceeded its budget", Trap: false}
	}
	return *scores[0], nil
}

func (in *jsInstance) rankBatch(ctx context.Context, cs *callState, cands [][]byte, cfg []byte, per time.Duration) ([]*int64, *Error) {
	strs := make([]string, len(cands))
	for i, c := range cands {
		strs[i] = string(c)
	}
	candsJSON, _ := json.Marshal(strs)
	perMs := float64(per) / float64(time.Millisecond)
	v, err := in.jsCall(ctx, cs, func() (*qjs.Value, error) {
		g := in.qc.Global()
		return in.qc.Invoke(in.globalFn("__ether_rank_batch"), g,
			in.qc.NewString(string(candsJSON)), in.qc.NewString(string(cfg)), in.qc.NewFloat64(perMs))
	})
	if err != nil {
		return nil, err
	}
	var out []*int64
	if uerr := json.Unmarshal([]byte(v.String()), &out); uerr != nil {
		return nil, &Error{Status: StatusError, Message: "rank_key returned a non-numeric result: " + uerr.Error()}
	}
	if len(out) != len(cands) {
		return nil, &Error{Status: StatusError, Message: "rank batch returned the wrong number of scores"}
	}
	return out, nil
}

func (in *jsInstance) condOne(ctx context.Context, cs *callState, cand []byte, op string, val []byte) (bool, *Error) {
	v, err := in.jsCall(ctx, cs, func() (*qjs.Value, error) {
		g := in.qc.Global()
		c := in.qc.ParseJSON(string(cand))
		value := in.qc.ParseJSON(string(val))
		return in.qc.Invoke(in.globalFn("cond_match"), g, c, in.qc.NewString(op), value)
	})
	if err != nil {
		return false, err
	}
	return v.String() == "true", nil
}

// LoadJS loads a JavaScript plugin (TypeScript authors go through Compile
// first). A probe instance is built eagerly so syntax errors and a missing
// handle surface at load.
func (e *Engine) LoadJS(id, version, source string, host Host, lim Limits) (*Plugin, error) {
	lim.normalize()
	p := &Plugin{id: id, version: version, eng: e, host: host, lim: lim, memo: newMemo(4096), source: source}
	makeSvc := func() (instance, error) {
		in, err := newJSInstance(source, &p.lim, false)
		if err != nil {
			return nil, err
		}
		if err := in.installDispatch(); err != nil {
			in.closeInstance()
			return nil, err
		}
		return in, nil
	}
	makeHot := func() (instance, error) { return newJSInstance(source, &p.lim, true) }
	probe, err := makeSvc()
	if err != nil {
		return nil, err
	}
	p.caps = probe.caps()
	p.svc = newPool(lim.PoolSize, makeSvc)
	p.hot = newPool(lim.PoolSize, makeHot)
	p.svc.put(probe, true)
	if !p.caps.Handle && !p.caps.RankKey && !p.caps.CondMatch {
		p.Close()
		return nil, &Error{Status: StatusError, Message: "plugin defines none of handle, rank_key, cond_match"}
	}
	return p, nil
}

// installDispatch wraps handle so Go passes one JSON string and gets one
// JSON string back; the plugin sees plain objects either side.
func (in *jsInstance) installDispatch() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &Error{Status: StatusError, Message: fmt.Sprintf("install dispatch: %v", r), Trap: true}
		}
	}()
	_, err2 := in.qc.Eval("ether-dispatch.js", qjs.Code(`
globalThis.__ether_dispatch_handle = (reqJson) => {
  const res = handle(JSON.parse(reqJson));
  if (res && typeof res.then === "function") return res.then((r) => JSON.stringify(r == null ? {} : r));
  return JSON.stringify(res == null ? {} : res);
};`))
	if err2 != nil {
		return &Error{Status: StatusError, Message: "install dispatch: " + err2.Error(), Trap: true}
	}
	return nil
}
