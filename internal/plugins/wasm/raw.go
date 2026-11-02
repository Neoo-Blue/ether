package wasm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// rawRuntime is one wazero runtime bucketed by its memory limit, with the
// "ether" host module instantiated and a compile cache per module hash.
type rawRuntime struct {
	rt    wazero.Runtime
	mu    sync.Mutex
	cache map[string]wazero.CompiledModule
}

type csKey struct{}

func withCallState(ctx context.Context, cs *callState) context.Context {
	return context.WithValue(ctx, csKey{}, cs)
}

func callStateOf(ctx context.Context) *callState {
	cs, _ := ctx.Value(csKey{}).(*callState)
	return cs
}

// abortPanic unwinds a guest that called ether.abort; the caller of the
// wasm function recovers it. Wazero does not recover host panics.
type abortPanic struct{ code uint32 }

func (e *Engine) rawRuntimeFor(memoryBytes uint64) (*rawRuntime, error) {
	pages := uint32(memoryBytes / 65536)
	if memoryBytes%65536 != 0 {
		pages++
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, &Error{Status: StatusError, Message: "engine is closed"}
	}
	if rt, ok := e.raw[pages]; ok {
		return rt, nil
	}
	ctx := context.Background()
	cfg := wazero.NewRuntimeConfig().
		WithCloseOnContextDone(true).
		WithMemoryLimitPages(pages).
		WithCompilationCache(wazero.NewCompilationCache())
	rt := wazero.NewRuntimeWithConfig(ctx, cfg)
	// Host module "ether": the only imports a raw plugin may have.
	_, err := rt.NewHostModuleBuilder("ether").
		NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(goLog), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, nil).Export("log").
		NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(goAbort), []api.ValueType{api.ValueTypeI32}, nil).Export("abort").
		NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(goHostCall),
		[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32},
		[]api.ValueType{api.ValueTypeI32}).Export("host_call").
		Instantiate(ctx)
	if err != nil {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("wasm: instantiate host module: %w", err)
	}
	rr := &rawRuntime{rt: rt, cache: map[string]wazero.CompiledModule{}}
	e.raw[pages] = rr
	return rr, nil
}

func readGuest(mod api.Module, ptr, n uint32) ([]byte, bool) {
	return mod.Memory().Read(ptr, n)
}

func goLog(ctx context.Context, mod api.Module, stack []uint64) {
	cs := callStateOf(ctx)
	if cs == nil {
		return
	}
	b, ok := readGuest(mod, uint32(stack[0]), uint32(stack[1]))
	if !ok || len(b) > 4096 {
		return
	}
	args, _ := json.Marshal([]string{string(b)})
	_, _ = cs.dispatch("log", args)
}

func goAbort(ctx context.Context, mod api.Module, stack []uint64) {
	panic(abortPanic{code: uint32(stack[0])})
}

// goHostCall returns result length, or -1 error, -2 forbidden, -3
// governance, or -(needed) when the guest buffer is too small.
func goHostCall(ctx context.Context, mod api.Module, stack []uint64) {
	ret := func(v int32) { stack[0] = api.EncodeI32(v) }
	cs := callStateOf(ctx)
	if cs == nil {
		ret(-1)
		return
	}
	name, ok1 := readGuest(mod, uint32(stack[0]), uint32(stack[1]))
	args, ok2 := readGuest(mod, uint32(stack[2]), uint32(stack[3]))
	if !ok1 || !ok2 {
		ret(-1)
		return
	}
	out, fault := cs.dispatch(string(name), json.RawMessage(args))
	if fault != nil {
		switch statusForFaultCode(fault.Code) {
		case StatusForbidden:
			ret(-2)
		case StatusGovernance:
			ret(-3)
		default:
			ret(-1)
		}
		return
	}
	if uint32(len(out)) > uint32(stack[5]) {
		ret(-int32(len(out)))
		return
	}
	mod.Memory().Write(uint32(stack[4]), out)
	ret(int32(len(out)))
}

// LoadWASM compiles (and caches) a raw module and returns the Plugin. A
// probe instance is built eagerly so load-time failures (bad imports,
// missing exports) surface here, not on first call.
func (e *Engine) LoadWASM(id, version string, module []byte, host Host, lim Limits) (*Plugin, error) {
	lim.normalize()
	rr, err := e.rawRuntimeFor(lim.MemoryBytes)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(module)
	key := hex.EncodeToString(sum[:])
	rr.mu.Lock()
	cm, ok := rr.cache[key]
	if !ok {
		cm, err = rr.rt.CompileModule(context.Background(), module)
		if err == nil {
			rr.cache[key] = cm
		}
	}
	rr.mu.Unlock()
	if err != nil {
		return nil, &Error{Status: StatusError, Message: "compile module: " + err.Error()}
	}
	if err := validateRawImports(cm); err != nil {
		return nil, err
	}
	p := &Plugin{id: id, version: version, eng: e, host: host, lim: lim, memo: newMemo(4096)}
	makeSvc := func() (instance, error) { return newRawInstance(rr, cm, false) }
	makeHot := func() (instance, error) { return newRawInstance(rr, cm, true) }
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
		return nil, &Error{Status: StatusError, Message: "module exports none of handle, rank_key, cond_match"}
	}
	return p, nil
}

func validateRawImports(cm wazero.CompiledModule) error {
	allowed := map[string]bool{"log": true, "abort": true, "host_call": true}
	for _, f := range cm.ImportedFunctions() {
		mod, name, _ := f.Import()
		if mod != "ether" || !allowed[name] {
			return &Error{Status: StatusForbidden,
				Message: fmt.Sprintf("forbidden import %s.%s: raw plugins may import only ether.log, ether.abort, ether.host_call", mod, name)}
		}
	}
	return nil
}

type rawInstance struct {
	mod               api.Module
	allocFn, handleFn api.Function
	rankFn, condFn    api.Function
	instanceCaps      caps
	hot               bool
}

func newRawInstance(rr *rawRuntime, cm wazero.CompiledModule, hot bool) (*rawInstance, error) {
	mod, err := rr.rt.InstantiateModule(context.Background(), cm,
		wazero.NewModuleConfig().WithName(""))
	if err != nil {
		return nil, &Error{Status: StatusError, Message: "instantiate module: " + err.Error(), Trap: true}
	}
	in := &rawInstance{mod: mod, hot: hot,
		allocFn: mod.ExportedFunction("alloc"), handleFn: mod.ExportedFunction("handle"),
		rankFn: mod.ExportedFunction("rank_key"), condFn: mod.ExportedFunction("cond_match")}
	if in.allocFn == nil || mod.Memory() == nil {
		_ = mod.Close(context.Background())
		return nil, &Error{Status: StatusError, Message: "module must export memory and alloc"}
	}
	in.instanceCaps = caps{Handle: in.handleFn != nil, RankKey: in.rankFn != nil, CondMatch: in.condFn != nil}
	return in, nil
}

func (in *rawInstance) caps() caps { return in.instanceCaps }

func (in *rawInstance) closeInstance() {
	defer func() { _ = recover() }()
	_ = in.mod.Close(context.Background())
}

// rawCall converts wazero errors and host panics into classified *Error.
func (in *rawInstance) rawCall(ctx context.Context, fn api.Function, args ...uint64) (res []uint64, callErr *Error) {
	defer func() {
		if r := recover(); r != nil {
			if ap, ok := r.(abortPanic); ok {
				callErr = &Error{Status: StatusAborted, Message: fmt.Sprintf("plugin aborted with code %d", ap.code), Trap: true}
				return
			}
			callErr = &Error{Status: StatusError, Message: fmt.Sprintf("host panic: %v", r), Trap: true}
		}
	}()
	res, err := fn.Call(ctx, args...)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			return nil, &Error{Status: StatusTimeout, Message: "call exceeded its deadline", Trap: true}
		}
		return nil, &Error{Status: StatusError, Message: err.Error(), Trap: true}
	}
	return res, nil
}

func (in *rawInstance) writeInput(ctx context.Context, b []byte) (uint32, *Error) {
	res, err := in.rawCall(ctx, in.allocFn, uint64(len(b)))
	if err != nil {
		return 0, err
	}
	ptr := uint32(res[0])
	if !in.mod.Memory().Write(ptr, b) {
		return 0, &Error{Status: StatusError, Message: "guest alloc returned an out-of-bounds pointer", Trap: true}
	}
	return ptr, nil
}

func (in *rawInstance) handle(ctx context.Context, cs *callState, reqJSON []byte) ([]byte, *Error) {
	if in.handleFn == nil {
		return nil, &Error{Status: StatusError, Message: "module does not export handle"}
	}
	cctx := withCallState(ctx, cs)
	ptr, err := in.writeInput(cctx, reqJSON)
	if err != nil {
		return nil, err
	}
	res, err := in.rawCall(cctx, in.handleFn, uint64(ptr), uint64(len(reqJSON)))
	if err != nil {
		return nil, err
	}
	outPtr, outLen := uint32(res[0]>>32), uint32(res[0])
	if outLen > MaxResponseBytes {
		return nil, &Error{Status: StatusError, Message: "plugin response exceeds size cap", Trap: true}
	}
	b, ok := in.mod.Memory().Read(outPtr, outLen)
	if !ok {
		return nil, &Error{Status: StatusError, Message: "plugin returned an out-of-bounds response pointer", Trap: true}
	}
	return append([]byte(nil), b...), nil
}

func (in *rawInstance) rankOne(ctx context.Context, cs *callState, cand, cfg []byte) (int64, *Error) {
	if in.rankFn == nil {
		return 0, &Error{Status: StatusError, Message: "module does not export rank_key"}
	}
	cctx := withCallState(ctx, cs)
	cp, err := in.writeInput(cctx, cand)
	if err != nil {
		return 0, err
	}
	fp, err := in.writeInput(cctx, cfg)
	if err != nil {
		return 0, err
	}
	res, err := in.rawCall(cctx, in.rankFn, uint64(cp), uint64(len(cand)), uint64(fp), uint64(len(cfg)))
	if err != nil {
		return 0, err
	}
	return int64(res[0]), nil
}

func (in *rawInstance) condOne(ctx context.Context, cs *callState, cand []byte, op string, val []byte) (bool, *Error) {
	if in.condFn == nil {
		return false, &Error{Status: StatusError, Message: "module does not export cond_match"}
	}
	cctx := withCallState(ctx, cs)
	cp, err := in.writeInput(cctx, cand)
	if err != nil {
		return false, err
	}
	opB := []byte(op)
	opP, err := in.writeInput(cctx, opB)
	if err != nil {
		return false, err
	}
	vp, err := in.writeInput(cctx, val)
	if err != nil {
		return false, err
	}
	res, err := in.rawCall(cctx, in.condFn, uint64(cp), uint64(len(cand)), uint64(opP), uint64(len(opB)), uint64(vp), uint64(len(val)))
	if err != nil {
		return false, err
	}
	return res[0] != 0, nil
}
