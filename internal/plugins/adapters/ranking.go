package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/resolver"
)

// Ranking bridges the resolver to installed hosted hot-path plugins.
type RankingManager interface {
	HotReady(string) (plugins.HotRuntime, bool)
	RecordSlow(context.Context, string, int)
}
type Ranking struct {
	Manager  RankingManager
	Registry *extend.Registry
}

func (a Ranking) NewResolve(ctx context.Context, caps core.ClientCaps) resolver.RankingSession {
	return &rankingSession{ctx: ctx, manager: a.Manager, registry: a.Registry, caps: caps, runtimes: map[string]*rankingRuntime{}, matches: map[string]bool{}, slow: map[string]int{}}
}

type rankingRuntime struct {
	hot           plugins.HotRuntime
	conditionTime time.Duration
}
type rankingSession struct {
	ctx      context.Context
	manager  RankingManager
	registry *extend.Registry
	caps     core.ClientCaps
	runtimes map[string]*rankingRuntime
	matches  map[string]bool
	slow     map[string]int
}

func (s *rankingSession) Close() {
	for id, count := range s.slow {
		s.manager.RecordSlow(context.WithoutCancel(s.ctx), id, count)
	}
	clear(s.slow)
}
func splitHotID(id string) (string, string, bool) {
	s, ok := strings.CutPrefix(id, "plugin:")
	if !ok {
		return "", "", false
	}
	p, k, ok := strings.Cut(s, "/")
	return p, k, ok && p != "" && k != "" && !strings.Contains(k, "/")
}
func (s *rankingSession) runtime(id string) *rankingRuntime {
	if rt, ok := s.runtimes[id]; ok {
		return rt
	}
	s.runtimes[id] = nil
	if s.manager == nil {
		return nil
	}
	hot, ok := s.manager.HotReady(id)
	if !ok {
		return nil
	}
	rt := &rankingRuntime{hot: hot}
	s.runtimes[id] = rt
	return rt
}
func (s *rankingSession) declared(kind extend.Kind, step, owner string) bool {
	if s.registry == nil {
		return false
	}
	d, ok := s.registry.Get(kind, step)
	return ok && d.Owner == owner
}
func (s *rankingSession) Rank(step string, config json.RawMessage, releases []core.Release, verified []bool) []int64 {
	id, _, ok := splitHotID(step)
	if !ok || !s.declared(extend.KindRankingStep, step, id) {
		return nil
	}
	desc, _ := s.registry.Get(extend.KindRankingStep, step)
	if err := resolver.ValidateStepConfig(desc.Meta, config); err != nil {
		return nil
	}
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}
	rt := s.runtime(id)
	if rt == nil || !rt.hot.HasRankKey() {
		return nil
	}
	facts := make([]plugins.CandidateFacts, len(releases))
	for i, r := range releases {
		v := i < len(verified) && verified[i]
		facts[i] = candidateFacts(r, s.caps, v)
	}
	keys, out, err := rt.hot.RankBatch(s.ctx, facts, config)
	slow := out.Slow
	if (err != nil || (out.Status != "" && out.Status != "ok")) && slow == 0 {
		slow = len(releases)
	}
	if slow > 0 {
		s.slow[id] += slow
	}
	if err != nil || (out.Status != "" && out.Status != "ok") || out.Slow > 0 || len(keys) != len(releases) {
		return nil
	}
	return keys
}
func (s *rankingSession) Match(rel core.Release, cond core.ReleaseCondition, verified bool) (bool, bool) {
	id, key, ok := splitHotID(cond.Field)
	if !ok || !s.declared(extend.KindRuleField, cond.Field, id) {
		return false, false
	}
	rt := s.runtime(id)
	if rt == nil || !rt.hot.HasCondMatch() {
		return false, true
	}
	value, _ := json.Marshal(cond.Value)
	if cond.Operator == "range" {
		value, _ = json.Marshal(map[string]float64{"min": cond.Min, "max": cond.Max})
	}
	memo := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s", id, key, rel.ID, cond.Operator, value)
	if v, ok := s.matches[memo]; ok {
		return v, true
	}
	remain := rt.hot.Budgets().PerBatch - rt.conditionTime
	if remain <= 0 {
		s.slow[id]++
		return false, true
	}
	ctx, cancel := context.WithTimeout(s.ctx, remain)
	start := time.Now()
	matched, out, err := rt.hot.CondMatch(ctx, candidateFacts(rel, s.caps, verified), cond.Operator, value)
	cancel()
	rt.conditionTime += time.Since(start)
	if err != nil || (out.Status != "" && out.Status != "ok") || out.Slow > 0 {
		s.slow[id] += max(1, out.Slow)
		return false, true
	}
	s.matches[memo] = matched
	return matched, true
}
func candidateFacts(r core.Release, caps core.ClientCaps, verified bool) plugins.CandidateFacts {
	info := r.Info
	protocol := "usenet"
	if r.Protocol == core.ProtocolTorrent {
		protocol = "torrent"
	} else if r.Protocol == core.ProtocolHTTP {
		protocol = "direct"
	}
	source := "other"
	switch info.Source {
	case core.SourceRemux:
		source = "remux"
	case core.SourceBluRay:
		source = "disc"
	case core.SourceWEBDL, core.SourceWEBRip:
		source = "web"
	}
	res := ""
	if info.Height > 0 {
		res = fmt.Sprintf("%dp", info.Height)
	}
	var hdr, audio, clientHDR []string
	if info.DolbyVision {
		hdr = append(hdr, "dolby_vision")
	}
	if info.HDR10 {
		hdr = append(hdr, "hdr10")
	}
	if info.AudioCodec != "" {
		audio = append(audio, info.AudioCodec)
	}
	if info.Atmos {
		audio = append(audio, "atmos")
	}
	if caps.DolbyVision {
		clientHDR = append(clientHDR, "dolby_vision")
	}
	if caps.HDR10 {
		clientHDR = append(clientHDR, "hdr10")
	}
	return plugins.CandidateFacts{ReleaseID: r.ID, Name: r.Name, Size: r.Size, Protocol: protocol, SourceClass: source, Indexer: r.Indexer, Resolution: res, HDR: hdr, VideoCodec: info.VideoCodec, AudioCodecs: audio, AudioChannels: info.Channels, Languages: info.Languages, Subtitles: info.SubtitleLanguages, ReleaseType: string(info.Source), Group: info.Group, Cached: len(r.DebridCached) > 0, Verified: verified, ClientCaps: plugins.ClientCaps{MaxHeight: caps.MaxHeight, HDR: clientHDR, Codecs: caps.VideoCodecs}}
}
