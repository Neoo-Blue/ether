package resolver

import (
	"testing"

	"github.com/braedonsaunders/ether/internal/core"
)

func TestSortRankingCandidatesPluginKey(t *testing.T) {
	step := "plugin:example/dub"
	profile := core.QualityProfile{Ranking: []string{step, core.RankResolution}}
	c := []RankingCandidate{
		{Release: core.Release{ID: "plain", Info: core.ReleaseInfo{Height: 2160}}, LanguageOK: true},
		{Release: core.Release{ID: "dub", Info: core.ReleaseInfo{Height: 1080}}, LanguageOK: true, PluginKeys: map[string]int64{step: 1}},
	}
	SortRankingCandidates(c, profile)
	if c[0].Release.ID != "dub" {
		t.Fatalf("plugin step did not take precedence: %v", c[0].Release.ID)
	}
	c[0].PluginKeys = nil
	SortRankingCandidates(c, profile)
	if c[0].Release.ID != "plain" {
		t.Fatalf("neutral step changed built-in ordering: %v", c[0].Release.ID)
	}
}
