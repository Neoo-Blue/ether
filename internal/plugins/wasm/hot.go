package wasm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

// Candidate is one release on the hot path. Facts is the CandidateFacts
// JSON (SPEC 5.8); ReleaseID keys the memo and must be stable per release.
type Candidate struct {
	ReleaseID string
	Facts     json.RawMessage
}

// memo caches hot-path results by (plugin, version, config hash, release).
// Bounded with FIFO eviction; values are int64 ranks or bool conditions
// encoded as int64.
type memo struct {
	mu    sync.Mutex
	cap   int
	items map[string]int64
	order []string
}

func newMemo(cap int) *memo { return &memo{cap: cap, items: map[string]int64{}} }

func (m *memo) get(k string) (int64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.items[k]
	return v, ok
}

func (m *memo) put(k string, v int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.items[k]; ok {
		m.items[k] = v
		return
	}
	for len(m.items) >= m.cap && len(m.order) > 0 {
		old := m.order[0]
		m.order = m.order[1:]
		delete(m.items, old)
	}
	m.items[k] = v
	m.order = append(m.order, k)
}

func hashKey(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// warmHot builds one hot instance at load so the first batch does not
// spend its 50ms budget on instance creation.
func (p *Plugin) warmHot() {
	if !p.caps.RankKey && !p.caps.CondMatch {
		return
	}
	if in, err := p.hot.get(context.Background()); err == nil {
		p.hot.put(in, true)
	}
}

func (p *Plugin) hotCallState(ctx context.Context, start time.Time) *callState {
	return &callState{ctx: ctx, host: p.host, lim: &p.lim, start: start, last: start,
		hot: true, logger: p.eng.logger, pluginID: p.id}
}

// RankBatch scores candidates with the plugin's rank_key. Scores align
// with cands; a candidate that exceeds its budget, or arrives after the
// batch budget is spent, scores the neutral 0 and counts as slow. Errors
// are returned only for instance-level failures (trap, whole-batch
// timeout); per-candidate slowness never fails the resolve.
func (p *Plugin) RankBatch(ctx context.Context, cands []Candidate, config json.RawMessage) ([]int64, Outcome, error) {
	start := time.Now()
	out := Outcome{Status: StatusOK}
	defer func() {
		out.Duration = time.Since(start)
		p.stats.record(out)
	}()
	scores := make([]int64, len(cands))
	if !p.caps.RankKey {
		out.Status = StatusError
		return scores, out, &Error{Status: StatusError, Message: "plugin does not export rank_key"}
	}
	if len(config) == 0 {
		config = json.RawMessage("{}")
	}
	cfgHash := hashKey(string(config))
	pending := make([]int, 0, len(cands))
	for i, c := range cands {
		if v, ok := p.memo.get(hashKey(p.id, p.version, "rank", cfgHash, c.ReleaseID)); ok {
			scores[i] = v
			out.MemoHits++
		} else {
			pending = append(pending, i)
		}
	}
	if len(pending) == 0 {
		return scores, out, nil
	}
	batchCtx, cancel := context.WithTimeout(ctx, p.lim.RankBatch)
	defer cancel()
	cs := p.hotCallState(batchCtx, start)
	in, err := p.hot.get(batchCtx)
	if err != nil {
		out.SlowCandidates = len(pending)
		out.Status = StatusTimeout
		return scores, out, nil // neutral for the rest; health sees the counters
	}
	healthy := true
	if br, ok := in.(batchRanker); ok {
		facts := make([][]byte, len(pending))
		for j, i := range pending {
			facts[j] = cands[i].Facts
		}
		got, callErr := br.rankBatch(batchCtx, cs, facts, config, p.lim.RankPerCandidate)
		if callErr != nil {
			healthy = !callErr.Trap && callErr.Status != StatusTimeout
			out.Status, out.Trap = callErr.Status, callErr.Trap
			out.SlowCandidates = len(pending)
			p.hot.put(in, healthy)
			out.Units = cs.finishUnits()
			return scores, out, callErr
		}
		for j, i := range pending {
			if got[j] == nil {
				out.SlowCandidates++
				continue // neutral 0, not memoized: timing, not a value
			}
			scores[i] = *got[j]
			p.memo.put(hashKey(p.id, p.version, "rank", cfgHash, cands[i].ReleaseID), *got[j])
		}
	} else {
		for _, i := range pending {
			remain := time.Until(start.Add(p.lim.RankBatch))
			if remain <= 0 {
				out.SlowCandidates++
				continue
			}
			per := p.lim.RankPerCandidate
			if remain < per {
				per = remain
			}
			candCtx, ccancel := context.WithTimeout(batchCtx, per)
			v, callErr := in.rankOne(candCtx, cs, cands[i].Facts, config)
			ccancel()
			if callErr != nil {
				out.SlowCandidates++
				if callErr.Trap || callErr.Status == StatusTimeout {
					healthy = false
					out.Status, out.Trap = callErr.Status, callErr.Trap
					// Instance is dead or killed; remaining candidates
					// cannot run on it. They stay neutral.
					for _, k := range pending {
						if k > i {
							out.SlowCandidates++
						}
					}
					break
				}
				continue
			}
			scores[i] = v
			p.memo.put(hashKey(p.id, p.version, "rank", cfgHash, cands[i].ReleaseID), v)
		}
	}
	p.hot.put(in, healthy)
	out.Units = cs.finishUnits()
	return scores, out, nil
}

// CondMatch evaluates one rule condition. Budget exhaustion and errors
// read as false with the reason in the Outcome, matching RankBatch's
// neutral-on-trouble rule.
func (p *Plugin) CondMatch(ctx context.Context, cand Candidate, operator string, value json.RawMessage) (bool, Outcome, error) {
	start := time.Now()
	out := Outcome{Status: StatusOK}
	defer func() {
		out.Duration = time.Since(start)
		p.stats.record(out)
	}()
	if !p.caps.CondMatch {
		out.Status = StatusError
		return false, out, &Error{Status: StatusError, Message: "plugin does not export cond_match"}
	}
	if len(value) == 0 {
		value = json.RawMessage("null")
	}
	key := hashKey(p.id, p.version, "cond", hashKey(operator, string(value)), cand.ReleaseID)
	if v, ok := p.memo.get(key); ok {
		out.MemoHits = 1
		return v != 0, out, nil
	}
	callCtx, cancel := context.WithTimeout(ctx, p.lim.RankPerCandidate)
	defer cancel()
	cs := p.hotCallState(callCtx, start)
	in, err := p.hot.get(callCtx)
	if err != nil {
		out.Status = StatusTimeout
		out.SlowCandidates = 1
		return false, out, nil
	}
	got, callErr := in.condOne(callCtx, cs, cand.Facts, operator, value)
	out.Units = cs.finishUnits()
	if callErr != nil {
		out.Status, out.Trap = callErr.Status, callErr.Trap
		out.SlowCandidates = 1
		p.hot.put(in, !callErr.Trap && callErr.Status != StatusTimeout)
		return false, out, callErr
	}
	p.hot.put(in, true)
	v := int64(0)
	if got {
		v = 1
	}
	p.memo.put(key, v)
	return got, out, nil
}
