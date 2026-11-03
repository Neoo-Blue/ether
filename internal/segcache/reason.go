package segcache

import (
	"sort"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
)

// ReleaseReason is the owner-facing explanation for one cached release.
type ReleaseReason struct {
	Causes    []core.CacheCause    `json:"causes"`
	Retention RetentionExplanation `json:"retention"`
	File      string               `json:"file"`
	Source    string               `json:"source"`
}

// RetentionExplanation says why bytes are still retained, or how they leave.
type RetentionExplanation struct {
	Kind  string    `json:"kind"` // pinned | expiry | replay | lru
	Pins  []string  `json:"pins,omitempty"`
	Days  int       `json:"days,omitempty"`
	Hours float64   `json:"hours,omitempty"`
	Until time.Time `json:"until,omitzero"`
}

// AddCause merges a response-time cause such as a live session state.
func (r *ReleaseReason) AddCause(add core.CacheCause) {
	if r == nil || add.Code == "" || add.Code == core.CacheCauseUnrecorded {
		return
	}
	if len(r.Causes) == 1 && r.Causes[0].Code == core.CacheCauseUnrecorded {
		r.Causes = r.Causes[:0]
	}
	for i := range r.Causes {
		if r.Causes[i].Code != add.Code {
			continue
		}
		if r.Causes[i].FirstSeen.IsZero() || (!add.FirstSeen.IsZero() && add.FirstSeen.Before(r.Causes[i].FirstSeen)) {
			r.Causes[i].FirstSeen = add.FirstSeen
		}
		return
	}
	r.Causes = append(r.Causes, add)
	sort.SliceStable(r.Causes, func(i, j int) bool { return causeRank[r.Causes[i].Code] < causeRank[r.Causes[j].Code] })
}

var causeRank = map[string]int{
	core.CacheCausePlaying:      0,
	core.CacheCausePaused:       1,
	core.CacheCauseUnfinished:   2,
	core.CacheCauseOwnerPin:     3,
	core.CacheCauseShelf:        4,
	core.CacheCauseNextEpisode:  5,
	core.CacheCausePostPlay:     6,
	core.CacheCauseResumeWarm:   7,
	core.CacheCauseDetailWarm:   8,
	core.CacheCauseTrailerWarm:  9,
	core.CacheCauseBrowseWarm:   10,
	core.CacheCauseWatchlist:    11,
	core.CacheCauseQualityCheck: 12,
	core.CacheCauseFailover:     13,
	core.CacheCauseRepair:       14,
}

// reasonFor renders stored causes and current policy. Unknown codes are
// ignored; with nothing known the only reported cause is unrecorded.
func reasonFor(u *ReleaseUsage, policy Policy, now time.Time) *ReleaseReason {
	causes := make([]core.CacheCause, 0, len(u.causes))
	for _, c := range u.causes {
		if _, ok := causeRank[c.Code]; ok {
			causes = append(causes, c)
		}
	}
	sort.SliceStable(causes, func(i, j int) bool { return causeRank[causes[i].Code] < causeRank[causes[j].Code] })
	if len(causes) == 0 {
		causes = []core.CacheCause{{Code: core.CacheCauseUnrecorded}}
	}
	ret := RetentionExplanation{Kind: "lru"}
	switch {
	case len(u.Pins) > 0:
		ret = RetentionExplanation{Kind: "pinned", Pins: append([]string(nil), u.Pins...)}
	case policy.Retention > 0:
		ret = RetentionExplanation{Kind: "expiry", Days: int(policy.Retention / (24 * time.Hour)), Until: u.LastUsed.Add(policy.Retention)}
	case policy.Mode == "adaptive" && policy.ReplayBoost > 0 && u.Plays > 1 && !u.LastPlayed.IsZero() && now.Sub(u.LastPlayed) < 30*24*time.Hour:
		if hours := float64(min(max(u.Plays-1, 0), 8)) * policy.ReplayBoost.Hours(); hours > 0 {
			ret = RetentionExplanation{Kind: "replay", Hours: hours}
		}
	}
	return &ReleaseReason{Causes: causes, Retention: ret, File: u.file, Source: u.source}
}
