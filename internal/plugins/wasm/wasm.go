package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Status classifies a finished call, following appkit-sandbox.
type Status string

const (
	StatusOK         Status = "ok"
	StatusError      Status = "error"
	StatusTimeout    Status = "timeout"
	StatusForbidden  Status = "forbidden"
	StatusAborted    Status = "aborted"
	StatusGovernance Status = "governance"
)

// Fault is a classified, non-ambient failure. A Host returns a *Fault to make
// the call end with that status; Code is one of the Status values other than
// ok (usually forbidden), or an application code the plugin can catch.
type Fault struct {
	Code    string
	Message string
}

func (f *Fault) Error() string { return f.Code + ": " + f.Message }

// Forbidden builds a forbidden Fault, e.g. for a revoked permission.
func Forbidden(message string) *Fault { return &Fault{Code: string(StatusForbidden), Message: message} }

// Error is the error type returned by Plugin calls. It carries the Outcome so
// WS7's health loop can read the status, trap flag and cost from one value.
type Error struct {
	Status  Status
	Message string
	Trap    bool // the instance trapped (panic in guest) or was killed by a deadline
}

func (e *Error) Error() string { return "wasm: " + string(e.Status) + ": " + e.Message }

// Outcome accounts for one finished call.
type Outcome struct {
	Status         Status
	Units          int64 // host-call units plus wall-time units
	Duration       time.Duration
	Trap           bool
	SlowCandidates int // hot path: candidates that went neutral on budget
	MemoHits       int // hot path: candidates served from the memo
}

// Request is the HTTP-shaped plugin request (SPEC 5.1.2 / 5.2).
type Request struct {
	Method  string              `json:"method"`
	Path    string              `json:"path"`
	Query   map[string][]string `json:"query,omitempty"`
	Headers map[string]string   `json:"headers,omitempty"`
	Body    json.RawMessage     `json:"body,omitempty"`
}

// Response is the plugin's answer. Body is the raw JSON the plugin produced
// as its body field (usually a JSON string), passed through untouched.
type Response struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// Host is the plugin-to-Ether service surface (SPEC 5.1.5). Name is a dotted
// service method: kv.get, kv.put, kv.delete, kv.list, secret.get,
// http.fetch, catalog.search, catalog.get, viewers.history,
// viewers.setWatchState, collections.upsert, folders.list, folders.stat,
// notify.owner, log. Args is a JSON array of positional arguments; the
// result is any JSON value. Return a *Fault to classify the failure
// (forbidden for a missing or revoked grant).
type Host interface {
	CallHost(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error)
}

// HostFunc adapts a function to Host.
type HostFunc func(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error)

func (f HostFunc) CallHost(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	return f(ctx, name, args)
}

// Default host-call unit costs, in the spirit of appkit-sandbox's priced
// host functions. Wall time adds 1 unit per WallUnitInterval.
var DefaultCosts = map[string]int64{
	"log": 1, "kv.get": 5, "kv.put": 10, "kv.delete": 5, "kv.list": 10,
	"secret.get": 5, "http.fetch": 100, "catalog.search": 50, "catalog.get": 20,
	"viewers.history": 50, "viewers.setWatchState": 50, "collections.upsert": 50,
	"folders.list": 20, "folders.stat": 20, "notify.owner": 20, "settings.get": 5,
}

const (
	// DefaultRankPerCandidate and DefaultRankBatch are the hot-path budgets
	// (SPEC §5.2). A manifest may lower them, never raise them.
	DefaultRankPerCandidate = 2 * time.Millisecond
	DefaultRankBatch        = 50 * time.Millisecond
	// WallUnitInterval is the wall time worth one unit.
	WallUnitInterval = 100 * time.Microsecond
	// BaseCallUnits is charged once per handle call.
	BaseCallUnits = 10
	// MaxMemoryBytes is the hard ceiling for a plugin instance.
	MaxMemoryBytes = 64 << 20
	// MaxResponseBytes caps a plugin response.
	MaxResponseBytes = 8 << 20
	// MaxHostResultBytes caps one host-call result returned to a guest.
	MaxHostResultBytes = 4 << 20
	maxLogsPerCall     = 100
)

// Limits bound one plugin. Zero fields take the defaults in normalize.
type Limits struct {
	MemoryBytes uint64        // per instance; default 32 MiB, max 64 MiB
	Deadline    time.Duration // per handle call; default 30s
	UnitBudget  int64         // per handle call; default 10000
	PoolSize    int           // instances per pool; default 2
	// Hot-path budgets. A manifest may lower these, never raise them:
	// normalize clamps to the defaults below.
	RankPerCandidate time.Duration // default (and max) 2ms
	RankBatch        time.Duration // default (and max) 50ms
	Costs            map[string]int64
	// unclamped lets this package's tests raise the hot-path budgets so
	// correctness checks do not depend on machine speed. Other packages
	// cannot set it, so production budgets are always clamped.
	unclamped bool
}

func (l *Limits) normalize() {
	if l.MemoryBytes == 0 || l.MemoryBytes > MaxMemoryBytes {
		if l.MemoryBytes == 0 {
			l.MemoryBytes = 32 << 20
		} else {
			l.MemoryBytes = MaxMemoryBytes
		}
	}
	if l.Deadline <= 0 {
		l.Deadline = 30 * time.Second
	}
	if l.UnitBudget <= 0 {
		l.UnitBudget = 10000
	}
	if l.PoolSize <= 0 {
		l.PoolSize = 2
	}
	if l.RankPerCandidate <= 0 || (l.RankPerCandidate > DefaultRankPerCandidate && !l.unclamped) {
		l.RankPerCandidate = DefaultRankPerCandidate
	}
	if l.RankBatch <= 0 || (l.RankBatch > DefaultRankBatch && !l.unclamped) {
		l.RankBatch = DefaultRankBatch
	}
	if l.Costs == nil {
		l.Costs = DefaultCosts
	}
}

func (l *Limits) costOf(name string) int64 {
	if c, ok := l.Costs[name]; ok {
		return c
	}
	return 25 // unpriced service: charge a middle rate, never free
}

// Stats are the health counters WS7's backoff loop consumes.
type Stats struct {
	Calls          int64
	Errors         int64
	Timeouts       int64
	Traps          int64
	Forbidden      int64
	Aborted        int64
	Governance     int64
	SlowCandidates int64
	Units          int64
}

type counters struct {
	calls, errors, timeouts, traps, forbidden, aborted, governance, slow, units atomic.Int64
}

func (c *counters) record(o Outcome) {
	c.calls.Add(1)
	c.units.Add(o.Units)
	c.slow.Add(int64(o.SlowCandidates))
	if o.Trap {
		c.traps.Add(1)
	}
	switch o.Status {
	case StatusError:
		c.errors.Add(1)
	case StatusTimeout:
		c.timeouts.Add(1)
	case StatusForbidden:
		c.forbidden.Add(1)
	case StatusAborted:
		c.aborted.Add(1)
	case StatusGovernance:
		c.governance.Add(1)
	}
}

func (c *counters) snapshot() Stats {
	return Stats{Calls: c.calls.Load(), Errors: c.errors.Load(), Timeouts: c.timeouts.Load(),
		Traps: c.traps.Load(), Forbidden: c.forbidden.Load(), Aborted: c.aborted.Load(),
		Governance: c.governance.Load(), SlowCandidates: c.slow.Load(), Units: c.units.Load()}
}

// callState is the per-call governance state shared with host functions. One
// instance serves one call at a time, so no locking is needed beyond the
// exceeded flag (read after the call from the same goroutine).
type callState struct {
	ctx       context.Context
	host      Host
	lim       *Limits
	start     time.Time
	last      time.Time
	units     int64
	logs      int
	hot       bool
	exceeded  bool
	aborted   bool
	abortCode uint32
	logger    *slog.Logger
	pluginID  string
}

func (cs *callState) chargeWall() {
	now := time.Now()
	cs.units += int64(now.Sub(cs.last) / WallUnitInterval)
	cs.last = now
}

// charge accounts one host call. It returns a governance Fault when the
// budget is spent; the flag is sticky so a plugin that catches the error
// still ends the call as governance.
func (cs *callState) charge(name string) *Fault {
	cs.chargeWall()
	cs.units += cs.lim.costOf(name)
	if cs.units > cs.lim.UnitBudget {
		cs.exceeded = true
		return &Fault{Code: string(StatusGovernance),
			Message: fmt.Sprintf("governance budget exceeded (%d/%d units)", cs.units, cs.lim.UnitBudget)}
	}
	return nil
}

func (cs *callState) finishUnits() int64 {
	cs.chargeWall()
	if cs.units > cs.lim.UnitBudget {
		cs.exceeded = true
	}
	return cs.units
}

// dispatch runs one host service for a guest, applying governance and the
// hot-path lockdown. It is the single funnel for JS ether.* and raw
// ether.host_call.
func (cs *callState) dispatch(name string, args json.RawMessage) (json.RawMessage, *Fault) {
	if cs.hot && name != "log" {
		return nil, Forbidden("host services are not available on the hot path: " + name)
	}
	if name == "log" {
		cs.logs++
		if cs.logs > maxLogsPerCall {
			return json.RawMessage("null"), nil // rate-limited: drop quietly
		}
		if f := cs.charge(name); f != nil {
			return nil, f
		}
		if cs.host != nil {
			_, _ = cs.host.CallHost(cs.ctx, name, args)
		} else {
			cs.logger.Debug("plugin log", "plugin", cs.pluginID, "args", string(args))
		}
		return json.RawMessage("null"), nil
	}
	if f := cs.charge(name); f != nil {
		return nil, f
	}
	if cs.host == nil {
		return nil, Forbidden("no host services are configured for this plugin")
	}
	out, err := cs.host.CallHost(cs.ctx, name, args)
	if err != nil {
		if f, ok := err.(*Fault); ok {
			return nil, f
		}
		return nil, &Fault{Code: "host_error", Message: err.Error()}
	}
	if len(out) > MaxHostResultBytes {
		return nil, &Fault{Code: "host_error", Message: "host result exceeds size cap"}
	}
	if len(out) == 0 {
		out = json.RawMessage("null")
	}
	return out, nil
}

// caps records which ABI entry points a plugin defines.
type caps struct {
	Handle    bool
	RankKey   bool
	CondMatch bool
}

// instance is one live guest (a QuickJS runtime or a raw module instance).
// Instances are not safe for concurrent use; the pool lends one at a time.
// Any call that ends with Trap set, or with a timeout, poisons the instance.
type instance interface {
	handle(ctx context.Context, cs *callState, reqJSON []byte) ([]byte, *Error)
	rankOne(ctx context.Context, cs *callState, cand, cfg []byte) (int64, *Error)
	condOne(ctx context.Context, cs *callState, cand []byte, op string, val []byte) (bool, *Error)
	caps() caps
	closeInstance()
}

// batchRanker is implemented by instances (JavaScript) that can score a
// whole batch in one boundary crossing. Nil scores mark slow candidates.
type batchRanker interface {
	rankBatch(ctx context.Context, cs *callState, cands [][]byte, cfg []byte, per time.Duration) ([]*int64, *Error)
}

func statusForFaultCode(code string) Status {
	switch code {
	case string(StatusForbidden):
		return StatusForbidden
	case string(StatusAborted):
		return StatusAborted
	case string(StatusGovernance):
		return StatusGovernance
	case string(StatusTimeout):
		return StatusTimeout
	default:
		return StatusError
	}
}

// pool lends instances, building on demand up to size, discarding poisoned
// ones so crash policy (backoff, disable) can count fresh failures.
type pool struct {
	makeFn func(context.Context) (instance, error)
	idle   chan instance
	slots  chan struct{}
	done   atomic.Bool
}

func newPool(size int, makeFn func(context.Context) (instance, error)) *pool {
	return &pool{makeFn: makeFn, idle: make(chan instance, size), slots: make(chan struct{}, size)}
}

func (p *pool) get(ctx context.Context) (instance, error) {
	if p.done.Load() {
		return nil, &Error{Status: StatusError, Message: "plugin is closed"}
	}
	select {
	case in := <-p.idle:
		return in, nil
	default:
	}
	select {
	case in := <-p.idle:
		return in, nil
	case p.slots <- struct{}{}:
		in, err := p.makeFn(ctx)
		if err != nil {
			<-p.slots
			return nil, err
		}
		return in, nil
	case <-ctx.Done():
		return nil, &Error{Status: StatusTimeout, Message: "timed out waiting for a plugin instance"}
	}
}

func (p *pool) put(in instance, healthy bool) {
	if !healthy || p.done.Load() {
		in.closeInstance()
		<-p.slots
		return
	}
	select {
	case p.idle <- in:
	default:
		in.closeInstance()
		<-p.slots
	}
}

func (p *pool) close() {
	p.done.Store(true)
	for {
		select {
		case in := <-p.idle:
			in.closeInstance()
			<-p.slots
		default:
			return
		}
	}
}

// Engine compiles and caches guest code and builds Plugins. It owns the
// wazero runtimes used by raw modules; JavaScript instances manage their
// own (QuickJS) modules. Safe for concurrent use.
type Engine struct {
	logger *slog.Logger
	mu     sync.Mutex
	raw    map[uint32]*rawRuntime // by memory-limit pages
	closed bool
}

func NewEngine(logger *slog.Logger) *Engine {
	if logger == nil {
		logger = slog.Default()
	}
	ensureQJS()
	return &Engine{logger: logger, raw: map[uint32]*rawRuntime{}}
}

func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	for _, rt := range e.raw {
		_ = rt.rt.Close(context.Background())
	}
	e.raw = map[uint32]*rawRuntime{}
	return nil
}

// Plugin is one loaded hosted plugin. Call serves the request ABI;
// RankBatch and CondMatch serve the hot path. Safe for concurrent use.
type Plugin struct {
	id, version string
	eng         *Engine
	host        Host
	lim         Limits
	caps        caps
	svc         *pool
	hot         *pool
	stats       counters
	memo        *memo
	source      string // JS source (rebuild counts / cold start), raw: ""
	closed      atomic.Bool
}

// ID and Version identify the plugin for memo keys and logs.
func (p *Plugin) ID() string      { return p.id }
func (p *Plugin) Version() string { return p.version }

// HasHandle, HasRankKey and HasCondMatch report the exported entry points.
func (p *Plugin) HasHandle() bool    { return p.caps.Handle }
func (p *Plugin) HasRankKey() bool   { return p.caps.RankKey }
func (p *Plugin) HasCondMatch() bool { return p.caps.CondMatch }

func (p *Plugin) Stats() Stats { return p.stats.snapshot() }

func (p *Plugin) Close() error {
	if p.closed.Swap(true) {
		return nil
	}
	if p.svc != nil {
		p.svc.close()
	}
	if p.hot != nil {
		p.hot.close()
	}
	return nil
}

// Call runs the request ABI. The returned error, when non-nil, is a *Error
// classified for the host's health loop.
func (p *Plugin) Call(ctx context.Context, req Request) (Response, error) {
	resp, _, err := p.CallDetailed(ctx, req)
	return resp, err
}

// CallDetailed is Call plus the Outcome (units, duration, trap flag).
func (p *Plugin) CallDetailed(ctx context.Context, req Request) (Response, Outcome, error) {
	start := time.Now()
	out := Outcome{Status: StatusOK}
	defer func() {
		out.Duration = time.Since(start)
		p.stats.record(out)
	}()
	if p.closed.Load() {
		out.Status = StatusError
		return Response{}, out, &Error{Status: StatusError, Message: "plugin is closed"}
	}
	if !p.caps.Handle {
		out.Status = StatusError
		return Response{}, out, &Error{Status: StatusError, Message: "plugin does not export handle"}
	}
	reqJSON, err := json.Marshal(req)
	if err != nil {
		out.Status = StatusError
		return Response{}, out, &Error{Status: StatusError, Message: err.Error()}
	}
	callCtx, cancel := context.WithTimeout(ctx, p.lim.Deadline)
	defer cancel()
	cs := &callState{ctx: callCtx, host: p.host, lim: &p.lim, start: start, last: start,
		units: BaseCallUnits, logger: p.eng.logger, pluginID: p.id}
	in, err := p.svc.get(callCtx)
	if err != nil {
		out.Status = StatusTimeout
		if e, ok := err.(*Error); ok {
			out.Status = e.Status
		}
		return Response{}, out, err
	}
	respJSON, callErr := in.handle(callCtx, cs, reqJSON)
	out.Units = cs.finishUnits()
	healthy := callErr == nil || !callErr.Trap
	if callErr != nil && callErr.Status == StatusTimeout {
		healthy = false
	}
	p.svc.put(in, healthy)
	if callErr != nil {
		out.Status, out.Trap = callErr.Status, callErr.Trap
		return Response{}, out, callErr
	}
	if cs.exceeded {
		out.Status = StatusGovernance
		return Response{}, out, &Error{Status: StatusGovernance, Message: "governance budget exceeded"}
	}
	if len(respJSON) > MaxResponseBytes {
		out.Status = StatusError
		return Response{}, out, &Error{Status: StatusError, Message: "plugin response exceeds size cap"}
	}
	var resp Response
	if err := json.Unmarshal(respJSON, &resp); err != nil {
		out.Status = StatusError
		return Response{}, out, &Error{Status: StatusError, Message: "plugin returned invalid response JSON: " + err.Error()}
	}
	if resp.Status == 0 {
		resp.Status = 200
	}
	return resp, out, nil
}
