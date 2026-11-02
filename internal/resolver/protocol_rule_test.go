package resolver

import (
	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
	"testing"
)

func TestProtocolRuleMatching(t *testing.T) {
	tests := []struct {
		rule config.QualityRule
		caps core.ClientCaps
		want bool
	}{
		{config.QualityRule{Protocol: "jellyfin"}, core.ClientCaps{Protocol: "jellyfin"}, true},
		{config.QualityRule{Protocol: "jellyfin"}, core.ClientCaps{Protocol: "plex"}, false},
		{config.QualityRule{Protocol: "PLEX", Product: "tv"}, core.ClientCaps{Protocol: "plex", Product: "Plex TV"}, true},
		{config.QualityRule{Product: "tv"}, core.ClientCaps{Protocol: "silo", Product: "Silo TV"}, true},
		{config.QualityRule{}, core.ClientCaps{Protocol: "plex"}, false},
		// The console's "Fire TV" preset matches the real app, which calls
		// itself "Plex for Android (TV)" and sends the AFT model number.
		{config.QualityRule{Product: "Fire TV"}, core.ClientCaps{Product: "Plex for Android (TV)", Platform: "Android", Device: "AFTKRT"}, true},
		{config.QualityRule{Product: "Fire TV"}, core.ClientCaps{Product: "Plex for Android (TV)", Platform: "Android", Device: "SHIELD Android TV"}, false},
		{config.QualityRule{Product: "Fire TV"}, core.ClientCaps{Product: "Plex for Android", Platform: "Android", Device: "Pixel 9"}, false},
	}
	for _, v := range tests {
		if got := ruleMatches(v.rule, v.caps, nil); got != v.want {
			t.Fatalf("%+v %+v: %v", v.rule, v.caps, got)
		}
	}
}
