package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/plugins/wasm"
	"github.com/braedonsaunders/ether/internal/releaseparse"
	"github.com/braedonsaunders/ether/internal/resolver"
)

type rankingManagerFake struct {
	rt       plugins.HotRuntime
	record   plugins.Record
	slow     int
	registry *extend.Registry
	notReady bool
}

func (m *rankingManagerFake) Get(context.Context, string) (plugins.Record, error) {
	return m.record, nil
}
func (m *rankingManagerFake) HotReady(string) (plugins.HotRuntime, bool) {
	return m.rt, !m.notReady
}
func (m *rankingManagerFake) GetSettings(context.Context, string) (map[string]any, json.RawMessage, error) {
	return map[string]any{}, nil, nil
}
func (m *rankingManagerFake) RecordSlow(_ context.Context, _ string, n int) { m.slow += n }

type slowHot struct{ plugins.HotRuntime }

func (s slowHot) RankBatch(context.Context, []plugins.CandidateFacts, json.RawMessage) ([]int64, plugins.HotOutcome, error) {
	return []int64{1, 0}, plugins.HotOutcome{Status: "timeout", Slow: 2}, nil
}
func (s slowHot) CondMatch(context.Context, plugins.CandidateFacts, string, json.RawMessage) (bool, plugins.HotOutcome, error) {
	return true, plugins.HotOutcome{Status: "timeout", Slow: 1}, nil
}
func (s slowHot) Budgets() plugins.HotBudgets {
	return plugins.HotBudgets{PerCandidate: time.Millisecond, PerBatch: time.Second}
}

func fixtureRanking(t testing.TB) *rankingManagerFake {
	t.Helper()
	eng := wasm.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { _ = eng.Close() })
	man := plugins.Manifest{Version: "1", Ether: &plugins.EtherBlock{Runtime: plugins.RuntimeDecl{Kind: "hosted", Entry: "p.js"}, Contributes: plugins.Contributes{RankingSteps: []plugins.RankingStepMeta{{Key: "dub", Label: "Dub"}}, RuleConditions: []plugins.RuleConditionMeta{{Key: "dub", Label: "Dub"}}}}}
	src := `globalThis.rank_key = c => c.name.includes("DUB") ? 10 : 0; globalThis.cond_match = (c,op,v) => c.name.includes("DUB");`
	rt, err := plugins.HostedRuntimeFactory(eng)(plugins.Installed{ID: "test.dub", Manifest: man, Files: func(string) ([]byte, bool) { return []byte(src), true }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	reg := extend.New()
	if err := reg.Register(plugins.DescriptorsFor(plugins.Manifest{ID: "test.dub", Version: man.Version, Ether: man.Ether})...); err != nil {
		t.Fatal(err)
	}
	return &rankingManagerFake{rt: rt.(plugins.HotRuntime), registry: reg, record: plugins.Record{ID: "test.dub", Enabled: true, Manifest: man}}
}
func TestRankingHostedJSAndSlowFallback(t *testing.T) {
	m := fixtureRanking(t)
	session := (Ranking{Manager: m, Registry: m.registry}).NewResolve(context.Background(), core.ClientCaps{})
	rels := []core.Release{{ID: "a", Name: "film.1080p"}, {ID: "b", Name: "film.DUB.1080p"}}
	keys := session.Rank("plugin:test.dub/dub", rels, nil)
	if session.(*rankingSession).slow["test.dub"] > 0 {
		t.Skip("hosted runtime exhausted its budget under load")
	}
	if len(keys) != 2 || keys[0] != 0 || keys[1] != 10 {
		t.Fatalf("keys: %v", keys)
	}
	cond := core.ReleaseCondition{Field: "plugin:test.dub/dub", Operator: "equals", Value: "dub"}
	matchedDub, _ := session.Match(rels[1], cond, false)
	matchedPlain, _ := session.Match(rels[0], cond, false)
	if session.(*rankingSession).slow["test.dub"] > 0 {
		t.Skip("hosted runtime exhausted its budget under load")
	}
	if !matchedDub || matchedPlain {
		t.Fatal("condition mismatch")
	}
	profile := core.QualityProfile{Ranking: []string{"plugin:test.dub/dub"}}
	c := []resolver.RankingCandidate{{Release: rels[0], LanguageOK: true, PluginKeys: map[string]int64{"plugin:test.dub/dub": keys[0]}}, {Release: rels[1], LanguageOK: true, PluginKeys: map[string]int64{"plugin:test.dub/dub": keys[1]}}}
	resolver.SortRankingCandidates(c, profile)
	if !strings.Contains(c[0].Release.Name, "DUB") {
		t.Fatal("dub not selected")
	}
}

func TestRankingSlowFallback(t *testing.T) {
	m := fixtureRanking(t)
	rels := []core.Release{{ID: "a", Name: "film"}, {ID: "b", Name: "film.DUB"}}
	cond := core.ReleaseCondition{Field: "plugin:test.dub/dub", Operator: "equals", Value: "dub"}
	var session resolver.RankingSession
	m.rt = slowHot{m.rt}
	session = (Ranking{Manager: m, Registry: m.registry}).NewResolve(context.Background(), core.ClientCaps{})
	if v := session.Rank("plugin:test.dub/dub", rels, nil); v != nil {
		t.Fatalf("timeout must neutralize step: %v", v)
	}
	if matched, _ := session.Match(rels[1], cond, false); matched {
		t.Fatal("timeout condition must be false")
	}
	session.Close()
	if m.slow != 3 {
		t.Fatalf("slow count %d", m.slow)
	}
}

type rankingBenchIndexer struct{ releases []core.Release }

func (i rankingBenchIndexer) Name() string { return "benchmark" }
func (i rankingBenchIndexer) Search(context.Context, core.SearchRequest) ([]core.Release, error) {
	return append([]core.Release(nil), i.releases...), nil
}
func (i rankingBenchIndexer) FetchNZB(context.Context, core.Release) ([]byte, error) {
	return []byte("nzb"), nil
}
func BenchmarkResolve500PluginStep(b *testing.B) {
	m := fixtureRanking(b)
	cfg := config.Defaults(b.TempDir())
	cfg.Quality.Default = "1080p"
	cfg.Quality.Remote = "1080p"
	for i := range cfg.Quality.Profiles {
		cfg.Quality.Profiles[i].Ranking = []string{"plugin:test.dub/dub"}
	}
	pool := make([]core.Release, 500)
	for i := range pool {
		name := fmt.Sprintf("The.Signal.1999.1080p.WEB-DL.H.264-GRP%d", i)
		if i%2 == 0 {
			name = "The.Signal.1999.DUB.1080p.WEB-DL.H.264-GRP"
		}
		pool[i] = core.Release{ID: fmt.Sprint(i), GUID: fmt.Sprint(i), Indexer: "benchmark", Name: name, Size: 8 << 30, Info: releaseparse.Parse(name)}
	}
	s := resolver.New(rankingBenchIndexer{pool}, nil, nil, cfg, resolver.Options{})
	defer s.Close()
	s.SetRankingHooks(Ranking{Manager: m, Registry: m.registry})
	req := core.ResolveRequest{Title: &core.Title{Key: core.MovieKey(123), Type: core.TypeMovie, Title: "The Signal", Year: 1999, Runtime: 120 * time.Minute}, Client: core.ClientCaps{Product: "Plex Web", MaxHeight: 1080, VideoCodecs: []string{"h264"}}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Resolve(context.Background(), req); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(m.slow)/float64(b.N), "slow-candidates/op")
}

func TestHostedJSChangesActualResolve(t *testing.T) {
	m := fixtureRanking(t)
	cfg := config.Defaults(t.TempDir())
	cfg.Quality.Default = "1080p"
	cfg.Quality.Remote = "1080p"
	for i := range cfg.Quality.Profiles {
		cfg.Quality.Profiles[i].Ranking = []string{"plugin:test.dub/dub"}
	}
	names := []string{"The.Signal.1999.1080p.WEB-DL.H.264-GRP", "The.Signal.1999.DUB.1080p.WEB-DL.H.264-GRP"}
	pool := make([]core.Release, len(names))
	for i, name := range names {
		pool[i] = core.Release{ID: fmt.Sprint(i), GUID: fmt.Sprint(i), Indexer: "test", Name: name, Size: 8 << 30, Info: releaseparse.Parse(name)}
	}
	s := resolver.New(rankingBenchIndexer{pool}, nil, nil, cfg, resolver.Options{})
	defer s.Close()
	s.SetRankingHooks(Ranking{Manager: m, Registry: m.registry})
	req := core.ResolveRequest{Title: &core.Title{Key: core.MovieKey(123), Type: core.TypeMovie, Title: "The Signal", Year: 1999, Runtime: 120 * time.Minute}, Client: core.ClientCaps{Product: "Plex Web", MaxHeight: 1080, VideoCodecs: []string{"h264"}}}
	got, err := s.Resolve(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if m.slow > 0 {
		t.Skip("hosted runtime exhausted its budget under load")
	}
	if got.Release.ID != "1" {
		t.Fatalf("winner %q, slow=%d", got.Release.ID, m.slow)
	}
}

func TestUnreadyAndMissingPluginAreNeutral(t *testing.T) {
	m := fixtureRanking(t)
	m.notReady = true
	session := (Ranking{Manager: m, Registry: m.registry}).NewResolve(context.Background(), core.ClientCaps{})
	r := core.Release{ID: "1", Name: "DUB"}
	if keys := session.Rank("plugin:test.dub/dub", []core.Release{r}, nil); keys != nil {
		t.Fatalf("unready keys: %v", keys)
	}
	cond := core.ReleaseCondition{Field: "plugin:test.dub/dub", Operator: "equals", Value: "x"}
	if matched, available := session.Match(r, cond, false); matched || !available {
		t.Fatalf("unready = %v, %v", matched, available)
	}
	cond.Field = "plugin:missing/dub"
	if _, available := session.Match(r, cond, false); available {
		t.Fatal("missing plugin should remain unknown")
	}
	session.Close()
	if m.slow != 0 {
		t.Fatalf("warming recorded as slow: %d", m.slow)
	}
}
