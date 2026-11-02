package studio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/langtag"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/plugins/wasm"
	"github.com/braedonsaunders/ether/internal/releaseparse"
	"github.com/braedonsaunders/ether/internal/releasepolicy"
	"github.com/braedonsaunders/ether/internal/resolver"
)

// RankingReplayReport summarizes the before/after impact of a plugin ranking
// step or rule condition across recorded resolver_history traces.
type RankingReplayReport struct {
	Mode                string             `json:"mode"` // "ranking_step" or "rule_condition"
	StepKey             string             `json:"stepKey,omitempty"`
	ConditionKey        string             `json:"conditionKey,omitempty"`
	Position            int                `json:"position"`
	LadderBefore        []string           `json:"ladderBefore"`
	LadderAfter         []string           `json:"ladderAfter"`
	TracesLoaded        int                `json:"tracesLoaded"`
	TracesEvaluated     int                `json:"tracesEvaluated"`
	CandidatesEvaluated int                `json:"candidatesEvaluated"`
	ChangedCount        int                `json:"changedCount"`
	UnchangedCount      int                `json:"unchangedCount"`
	Changed             []ChangedPick      `json:"changed"`
	Timings             RankingTimings     `json:"timings"`
	Budget              RankingBudgetStats `json:"budget"`
}

// ChangedPick records one title whose winning release changed when the plugin
// step or condition was active.
type ChangedPick struct {
	HistoryID         int64  `json:"historyId"`
	TitleKey          string `json:"titleKey"`
	TitleName         string `json:"titleName"`
	Profile           string `json:"profile,omitempty"`
	BeforeReleaseID   string `json:"beforeReleaseId"`
	BeforeReleaseName string `json:"beforeReleaseName"`
	AfterReleaseID    string `json:"afterReleaseId"`
	AfterReleaseName  string `json:"afterReleaseName"`
	BeforePluginKey   int64  `json:"beforePluginKey"`
	AfterPluginKey    int64  `json:"afterPluginKey"`
}

// RankingTimings reports wall-clock and hot-path execution times.
type RankingTimings struct {
	TotalMS        int64 `json:"totalMs"`
	PluginUS       int64 `json:"pluginUs"`
	AvgBatchUS     int64 `json:"avgBatchUs"`
	MaxBatchUS     int64 `json:"maxBatchUs"`
	AvgCandidateUS int64 `json:"avgCandidateUs"`
}

// RankingBudgetStats reports hot-path governance units, slow candidates, and
// memo hits across the replay run.
type RankingBudgetStats struct {
	TotalUnits          int64   `json:"totalUnits"`
	MaxBatchUnits       int64   `json:"maxBatchUnits"`
	SlowCandidates      int     `json:"slowCandidates"`
	MemoHits            int     `json:"memoHits"`
	Timeouts            int     `json:"timeouts"`
	Traps               int     `json:"traps"`
	PerCandidateLimitMS float64 `json:"perCandidateLimitMs"`
	BatchLimitMS        float64 `json:"batchLimitMs"`
}

type historyRow struct {
	id          int64
	resolvedAt  int64
	ratingKey   int64
	titleName   string
	profileName string
	releaseID   string
	releaseName string
	pinned      bool
	trace       resolver.Trace
}

func (r *Runner) replayRanking(ctx context.Context, target *resolvedTarget, params ReplayParams) (*RankingReplayReport, error) {
	start := time.Now()
	if target.wasmPlug == nil {
		return nil, errors.New("ranking replay requires a hosted plugin runtime")
	}

	isCondition := params.ConditionKey != "" || (!target.wasmPlug.HasRankKey() && target.wasmPlug.HasCondMatch())
	if isCondition {
		if !target.wasmPlug.HasCondMatch() {
			return nil, errors.New("plugin does not export cond_match")
		}
	} else {
		if !target.wasmPlug.HasRankKey() {
			return nil, errors.New("plugin does not export rank_key")
		}
	}

	limit := params.Limit
	if limit <= 0 {
		limit = DefaultRankingTraces
	}
	if limit > MaxRankingTraces {
		limit = MaxRankingTraces
	}

	rows, err := r.loadHistoryTraces(ctx, limit)
	if err != nil {
		return nil, err
	}

	stepKey := params.StepKey
	defaultPos := 0
	if target.manifest.Ether != nil {
		for _, rs := range target.manifest.Ether.Contributes.RankingSteps {
			if stepKey == "" || rs.Key == stepKey {
				stepKey = rs.Key
				if rs.DefaultPosition > 0 {
					defaultPos = rs.DefaultPosition
				}
				break
			}
		}
	}
	if stepKey == "" {
		stepKey = "default"
	}
	condKey := params.ConditionKey
	if isCondition && condKey == "" && target.manifest.Ether != nil {
		if len(target.manifest.Ether.Contributes.RuleConditions) > 0 {
			condKey = target.manifest.Ether.Contributes.RuleConditions[0].Key
		} else {
			condKey = "default"
		}
	}

	pluginStepID := fmt.Sprintf("plugin:%s/%s", target.id, stepKey)
	if isCondition {
		pluginStepID = core.RankRulePrefix + fmt.Sprintf("plugin:%s/%s", target.id, condKey)
	}

	baseProfile := r.resolveProfile(params.Profile, "")
	baseLadder := baseProfile.EffectiveRanking()
	pos := resolveLadderPosition(baseLadder, params.Position, params.AfterStep, defaultPos)
	afterLadder := insertStepAt(baseLadder, pluginStepID, pos)

	mode := "ranking_step"
	if isCondition {
		mode = "rule_condition"
	}

	report := &RankingReplayReport{
		Mode:         mode,
		StepKey:      stepKey,
		ConditionKey: condKey,
		Position:     pos,
		LadderBefore: baseLadder,
		LadderAfter:  afterLadder,
		TracesLoaded: len(rows),
		Changed:      []ChangedPick{},
		Budget: RankingBudgetStats{
			PerCandidateLimitMS: 2.0,
			BatchLimitMS:        50.0,
		},
	}

	cfgJSON := params.Config
	if len(cfgJSON) == 0 && len(target.settings) > 0 {
		cfgJSON, _ = json.Marshal(target.settings)
	}
	if len(cfgJSON) == 0 {
		cfgJSON = json.RawMessage("{}")
	}

	operator := params.Operator
	if operator == "" {
		operator = "equals"
	}
	condAction := strings.ToLower(strings.TrimSpace(params.Action))
	if condAction == "" {
		condAction = "prefer"
	}

	var totalPluginDur time.Duration
	var maxBatchDur time.Duration
	var batchCount int64

	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			break
		}
		if row.pinned || row.trace.PinStatus == "used" || len(row.trace.Candidates) == 0 {
			continue
		}

		prof := r.resolveProfile(params.Profile, row.profileName)
		if row.trace.Limits != nil {
			if row.trace.Limits.ProfileMaxHeight > 0 {
				prof.MaxHeight = row.trace.Limits.ProfileMaxHeight
			}
			prof.AllowHDR = row.trace.Limits.AllowHDR
			prof.AllowDolbyVision = row.trace.Limits.AllowDolbyVision
		}

		policy, _ := releasepolicy.Compile(prof.ReleaseRules)
		eligible := buildReplayCandidates(row, prof, policy)
		if len(eligible) == 0 {
			continue
		}

		report.TracesEvaluated++
		report.CandidatesEvaluated += len(eligible)

		wasmCands := make([]wasm.Candidate, len(eligible))
		for i, c := range eligible {
			rawFacts, _ := json.Marshal(c.facts)
			wasmCands[i] = wasm.Candidate{
				ReleaseID: c.facts.ReleaseID,
				Facts:     rawFacts,
			}
		}

		scores := make([]int64, len(eligible))
		condMatched := make([]bool, len(eligible))

		if isCondition {
			var batchDur time.Duration
			var batchUnits int64
			for i, wc := range wasmCands {
				matched, out, _ := target.wasmPlug.CondMatch(ctx, wc, operator, params.Value)
				condMatched[i] = matched
				if matched {
					scores[i] = 1
				}
				batchDur += out.Duration
				batchUnits += out.Units
				accumulateOutcome(&report.Budget, out)
			}
			totalPluginDur += batchDur
			batchCount++
			if batchDur > maxBatchDur {
				maxBatchDur = batchDur
			}
			if batchUnits > report.Budget.MaxBatchUnits {
				report.Budget.MaxBatchUnits = batchUnits
			}
		} else {
			gotScores, out, _ := target.wasmPlug.RankBatch(ctx, wasmCands, cfgJSON)
			if len(gotScores) == len(eligible) {
				scores = gotScores
			}
			totalPluginDur += out.Duration
			batchCount++
			if out.Duration > maxBatchDur {
				maxBatchDur = out.Duration
			}
			if out.Units > report.Budget.MaxBatchUnits {
				report.Budget.MaxBatchUnits = out.Units
			}
			accumulateOutcome(&report.Budget, out)
		}

		for i := range eligible {
			eligible[i].pluginKey = scores[i]
			eligible[i].condMatch = condMatched[i]
		}

		ladderBefore := prof.EffectiveRanking()
		rowPos := resolveLadderPosition(ladderBefore, params.Position, params.AfterStep, defaultPos)
		ladderAfter := insertStepAt(ladderBefore, pluginStepID, rowPos)

		beforeCands := slices.Clone(eligible)
		sortReplayCandidates(beforeCands, prof, ladderBefore, "")

		afterCands := slices.Clone(eligible)
		if isCondition && (condAction == "require" || condAction == "avoid") {
			filtered := afterCands[:0]
			for _, c := range afterCands {
				if condAction == "require" && !c.condMatch {
					continue
				}
				if condAction == "avoid" && c.condMatch {
					continue
				}
				filtered = append(filtered, c)
			}
			afterCands = filtered
			sortReplayCandidates(afterCands, prof, ladderBefore, "")
		} else {
			sortReplayCandidates(afterCands, prof, ladderAfter, pluginStepID)
		}

		var beforeWinner, afterWinner replayCandidate
		if len(beforeCands) > 0 {
			beforeWinner = beforeCands[0]
		}
		if len(afterCands) > 0 {
			afterWinner = afterCands[0]
		}

		if beforeWinner.rel.ID != afterWinner.rel.ID {
			report.ChangedCount++
			if len(report.Changed) < MaxChangedPreview {
				titleName := row.titleName
				if titleName == "" {
					titleName = row.trace.TitleName
				}
				report.Changed = append(report.Changed, ChangedPick{
					HistoryID:         row.id,
					TitleKey:          fmt.Sprintf("%d", row.ratingKey),
					TitleName:         titleName,
					Profile:           row.profileName,
					BeforeReleaseID:   beforeWinner.rel.ID,
					BeforeReleaseName: beforeWinner.rel.Name,
					AfterReleaseID:    afterWinner.rel.ID,
					AfterReleaseName:  afterWinner.rel.Name,
					BeforePluginKey:   beforeWinner.pluginKey,
					AfterPluginKey:    afterWinner.pluginKey,
				})
			}
		} else {
			report.UnchangedCount++
		}
	}

	report.Timings.TotalMS = time.Since(start).Milliseconds()
	report.Timings.PluginUS = totalPluginDur.Microseconds()
	if batchCount > 0 {
		report.Timings.AvgBatchUS = totalPluginDur.Microseconds() / batchCount
	}
	report.Timings.MaxBatchUS = maxBatchDur.Microseconds()
	if report.CandidatesEvaluated > 0 {
		report.Timings.AvgCandidateUS = totalPluginDur.Microseconds() / int64(report.CandidatesEvaluated)
	}

	return report, nil
}

func accumulateOutcome(b *RankingBudgetStats, out wasm.Outcome) {
	b.TotalUnits += out.Units
	b.SlowCandidates += out.SlowCandidates
	b.MemoHits += out.MemoHits
	if out.Status == wasm.StatusTimeout {
		b.Timeouts++
	}
	if out.Trap {
		b.Traps++
	}
}

func (r *Runner) loadHistoryTraces(ctx context.Context, limit int) ([]historyRow, error) {
	if r.deps.DB == nil {
		return nil, nil
	}
	rows, err := r.deps.DB.QueryContext(ctx,
		`SELECT id, resolved_at, rating_key, title_name, profile, release_id, release_name, pinned, trace
		 FROM resolver_history
		 ORDER BY resolved_at DESC, id DESC
		 LIMIT ?`, limit)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var out []historyRow
	for rows.Next() {
		var row historyRow
		var pinned int
		var rawTrace string
		if err := rows.Scan(&row.id, &row.resolvedAt, &row.ratingKey, &row.titleName, &row.profileName, &row.releaseID, &row.releaseName, &pinned, &rawTrace); err != nil {
			return nil, err
		}
		row.pinned = pinned == 1
		if rawTrace == "" {
			continue
		}
		if err := json.Unmarshal([]byte(rawTrace), &row.trace); err != nil {
			continue
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Runner) resolveProfile(override *core.QualityProfile, name string) core.QualityProfile {
	if override != nil {
		return *override
	}
	if r.deps.Profiles != nil {
		profiles := r.deps.Profiles()
		for _, p := range profiles {
			if name != "" && p.Name == name {
				return p
			}
		}
		if len(profiles) > 0 {
			return profiles[0]
		}
	}
	return core.QualityProfile{
		Name:             "default",
		MaxHeight:        2160,
		MinHeight:        480,
		AllowHDR:         true,
		AllowDolbyVision: true,
		PreferredSources: []core.Source{core.SourceRemux, core.SourceBluRay, core.SourceWEBDL, core.SourceWEBRip, core.SourceHDTV},
		PreferredCodecs:  []string{"av1", "hevc", "h264"},
		PreferredAudio:   []string{"truehd", "dtshd", "eac3", "ac3", "aac"},
		PreferLargest:    true,
		Ranking:          core.DefaultRanking(nil),
	}
}

func resolveLadderPosition(ladder []string, explicit *int, afterStep string, defaultPos int) int {
	if explicit != nil {
		pos := *explicit
		if pos < 0 {
			pos = 0
		}
		if pos > len(ladder) {
			pos = len(ladder)
		}
		return pos
	}
	if afterStep != "" {
		for i, s := range ladder {
			if s == afterStep {
				return i + 1
			}
		}
	}
	if defaultPos < 0 {
		return 0
	}
	if defaultPos > len(ladder) {
		return len(ladder)
	}
	return defaultPos
}

func insertStepAt(ladder []string, step string, pos int) []string {
	if pos < 0 {
		pos = 0
	}
	if pos > len(ladder) {
		pos = len(ladder)
	}
	out := make([]string, 0, len(ladder)+1)
	out = append(out, ladder[:pos]...)
	out = append(out, step)
	out = append(out, ladder[pos:]...)
	return out
}

type replayCandidate struct {
	rel        core.Release
	facts      plugins.CandidateFacts
	policy     releasepolicy.Result
	pluginKey  int64
	condMatch  bool
	langOK     bool
	langWanted bool
	verified   bool
	bandwidth  int
	heightRank int
	sourceRank int
	hdrRank    int
	codecRank  int
	audioRank  int
	sizeRank   int64
	wasWinner  bool
}

func buildReplayCandidates(row historyRow, prof core.QualityProfile, policy *releasepolicy.Policy) []replayCandidate {
	winnerID := row.trace.Winner
	if winnerID == "" {
		winnerID = row.releaseID
	}
	var out []replayCandidate
	for idx, ct := range row.trace.Candidates {
		if ct.Rejected {
			continue
		}
		if ct.HealthChecked && !ct.Healthy {
			continue
		}
		info := releaseparse.Parse(ct.Name)
		if ct.Height > 0 && info.Height == 0 {
			info.Height = ct.Height
		}
		relID := ct.ReleaseID
		if relID == "" {
			relID = fmt.Sprintf("cand-%d-%d", row.id, idx)
		}
		rel := core.Release{
			ID:      relID,
			Name:    ct.Name,
			Indexer: ct.Indexer,
			Size:    ct.Size,
			Score:   ct.Score,
			Info:    info,
		}
		var polRes releasepolicy.Result
		if ct.Preferences != nil {
			polRes = *ct.Preferences
		} else if policy != nil {
			polRes = policy.Evaluate(rel, releasepolicy.Context{})
		}
		if polRes.Rejected {
			continue
		}

		verified := (ct.HealthChecked && ct.Healthy) || strings.Contains(ct.CompatibilityReason, "recently verified playable")
		bandwidth := 1
		if strings.Contains(ct.CompatibilityReason, "larger than the client's connection carries") {
			bandwidth = -1
		}
		langOK := !strings.Contains(ct.CompatibilityReason, "ranked after releases in an accepted audio language")
		if langOK && len(prof.PreferredLanguages) > 0 && len(info.Languages) > 0 {
			langOK = matchesAnyLang(info.Languages, prof.PreferredLanguages)
		}
		langWanted := langOK && len(prof.PreferredLanguages) > 0 && matchesAnyLang(info.Languages, prof.PreferredLanguages)

		starved := strings.Contains(ct.CompatibilityReason, "ranked one resolution lower")
		effHeight := info.Height
		if starved {
			switch {
			case effHeight >= 2160:
				effHeight = 1080
			case effHeight >= 1080:
				effHeight = 720
			case effHeight >= 720:
				effHeight = 480
			}
		}
		hdr := computeHDRRank(info, prof, starved)
		var sizeRank int64
		if prof.PreferLargest {
			sizeRank = rel.Size
		}

		facts := buildCandidateFacts(rel, ct, row.trace.Limits, verified)
		out = append(out, replayCandidate{
			rel:        rel,
			facts:      facts,
			policy:     polRes,
			langOK:     langOK,
			langWanted: langWanted,
			verified:   verified,
			bandwidth:  bandwidth,
			heightRank: effHeight,
			sourceRank: sliceRank(prof.PreferredSources, info.Source),
			hdrRank:    hdr,
			codecRank:  sliceRank(prof.PreferredCodecs, info.VideoCodec),
			audioRank:  computeAudioRank(info, prof),
			sizeRank:   sizeRank,
			wasWinner:  winnerID != "" && relID == winnerID,
		})
	}
	return out
}

func buildCandidateFacts(rel core.Release, ct resolver.CandidateTrace, lim *resolver.TraceLimits, verified bool) plugins.CandidateFacts {
	info := rel.Info
	var hdr []string
	if info.DolbyVision {
		hdr = append(hdr, "dolby_vision")
	}
	if info.HDR10 {
		hdr = append(hdr, "hdr10")
	}
	var audioCodecs []string
	if info.AudioCodec != "" {
		audioCodecs = append(audioCodecs, info.AudioCodec)
	}
	if info.Atmos {
		audioCodecs = append(audioCodecs, "atmos")
	}
	protocol := "usenet"
	routeLower := strings.ToLower(ct.Route)
	if strings.Contains(routeLower, "debrid") || strings.Contains(routeLower, "torrent") {
		protocol = "torrent"
	} else if strings.Contains(routeLower, "direct") {
		protocol = "direct"
	}
	var clientCaps plugins.ClientCaps
	if lim != nil {
		clientCaps.MaxHeight = lim.ClientMaxHeight
		if clientCaps.MaxHeight == 0 {
			clientCaps.MaxHeight = lim.ProfileMaxHeight
		}
		if lim.AllowDolbyVision {
			clientCaps.HDR = append(clientCaps.HDR, "dolby_vision")
		}
		if lim.AllowHDR {
			clientCaps.HDR = append(clientCaps.HDR, "hdr10")
		}
	}
	return plugins.CandidateFacts{
		ReleaseID:     rel.ID,
		Name:          rel.Name,
		Size:          rel.Size,
		Protocol:      protocol,
		SourceClass:   classifySource(info.Source),
		Indexer:       rel.Indexer,
		Resolution:    resolutionString(info.Height),
		HDR:           hdr,
		VideoCodec:    info.VideoCodec,
		AudioCodecs:   audioCodecs,
		AudioChannels: info.Channels,
		Languages:     info.Languages,
		Subtitles:     info.SubtitleLanguages,
		ReleaseType:   string(info.Source),
		Group:         info.Group,
		Cached:        strings.Contains(routeLower, "cached"),
		Verified:      verified,
		ClientCaps:    clientCaps,
	}
}

func classifySource(s core.Source) string {
	switch s {
	case core.SourceRemux:
		return "remux"
	case core.SourceBluRay:
		return "disc"
	case core.SourceWEBDL, core.SourceWEBRip:
		return "web"
	default:
		return "other"
	}
}

func resolutionString(height int) string {
	switch {
	case height >= 2160:
		return "2160p"
	case height >= 1080:
		return "1080p"
	case height >= 720:
		return "720p"
	case height > 0:
		return fmt.Sprintf("%dp", height)
	default:
		return ""
	}
}

func computeHDRRank(info core.ReleaseInfo, profile core.QualityProfile, starved bool) int {
	if starved {
		return 0
	}
	switch {
	case info.DolbyVision && profile.AllowDolbyVision:
		return 2
	case profile.AllowHDR && (info.HDR10 || info.DolbyVision || (info.Height >= 2160 && info.Source == core.SourceRemux)):
		return 1
	default:
		return 0
	}
}

func computeAudioRank(info core.ReleaseInfo, profile core.QualityProfile) int {
	r := sliceRank(profile.PreferredAudio, info.AudioCodec) * 2
	if info.Atmos {
		r++
	}
	return r
}

func sliceRank[T comparable](list []T, v T) int {
	for i, x := range list {
		if x == v {
			return len(list) - i
		}
	}
	return 0
}

func matchesAnyLang(have, want []string) bool {
	for _, h := range have {
		if strings.EqualFold(h, "multi") {
			return true
		}
		ch := langtag.Normalize(h)
		for _, w := range want {
			if ch != "" && ch == langtag.Normalize(w) {
				return true
			}
		}
	}
	return false
}

func hasDefaultTail(ladder []string) bool {
	n := len(ladder)
	return n >= 3 &&
		ladder[n-3] == core.RankVideoCodec &&
		ladder[n-2] == core.RankAudioFormat &&
		ladder[n-1] == core.RankSize
}

func sortReplayCandidates(cands []replayCandidate, profile core.QualityProfile, ladder []string, pluginStep string) {
	defaultTail := hasDefaultTail(ladder)
	sort.SliceStable(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if a.policy.Fallback != b.policy.Fallback {
			return a.policy.Fallback < b.policy.Fallback
		}
		for _, step := range ladder {
			switch {
			case pluginStep != "" && step == pluginStep:
				if a.pluginKey != b.pluginKey {
					return a.pluginKey > b.pluginKey
				}
			case strings.HasPrefix(step, core.RankRulePrefix):
				id := strings.TrimPrefix(step, core.RankRulePrefix)
				if ap, bp := a.policy.PreferRule(id), b.policy.PreferRule(id); ap != bp {
					return ap > bp
				}
			case step == core.RankLanguage:
				if a.langOK != b.langOK {
					return a.langOK
				}
			case step == core.RankConnection:
				if a.bandwidth != b.bandwidth {
					return a.bandwidth > b.bandwidth
				}
			case step == core.RankResolution:
				if a.heightRank != b.heightRank {
					return a.heightRank > b.heightRank
				}
				if a.langWanted != b.langWanted {
					return a.langWanted
				}
			case step == core.RankReleaseType:
				if a.sourceRank != b.sourceRank {
					return a.sourceRank > b.sourceRank
				}
			case step == core.RankHDR:
				if a.hdrRank != b.hdrRank {
					return a.hdrRank > b.hdrRank
				}
			case step == core.RankVerified:
				if a.verified != b.verified {
					return a.verified
				}
			case step == core.RankVideoCodec:
				if !defaultTail && a.codecRank != b.codecRank {
					return a.codecRank > b.codecRank
				}
			case step == core.RankAudioFormat:
				if !defaultTail && a.audioRank != b.audioRank {
					return a.audioRank > b.audioRank
				}
			case step == core.RankSize:
				if !defaultTail && a.sizeRank != b.sizeRank {
					return a.sizeRank > b.sizeRank
				}
			}
		}
		if a.rel.Score != b.rel.Score {
			return a.rel.Score > b.rel.Score
		}
		if a.wasWinner != b.wasWinner {
			return a.wasWinner
		}
		return false
	})
}
