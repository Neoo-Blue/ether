package plex

import (
	"context"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/diagnostics"
)

// SupportViewer is a bounded snapshot at archive creation, not a replay of
// policy decisions made when an earlier response was generated.
func (s *Server) SupportViewer(ctx context.Context, user *core.User) (diagnostics.ViewerFacts, error) {
	var out diagnostics.ViewerFacts
	for _, sec := range s.userSections(user) {
		out.Sections = append(out.Sections, diagnostics.ViewerSection{ID: sec.ID, Type: string(sec.Type), Title: sec.Title})
	}
	if user == nil || s.d.State == nil || s.d.Catalog == nil {
		return out, nil
	}
	pr := &Request{User: user}
	ctx = withStoredPolicy(ctx)
	states, err := s.d.State.InProgress(ctx, user.ID, 201)
	if err != nil {
		return out, err
	}
	if len(states) > 200 {
		out.CandidatesTruncated = true
		states = states[:200]
	}
	keys := make([]core.RatingKey, 0, len(states))
	for _, st := range states {
		keys = append(keys, st.Key)
	}
	titles := s.getTitles(ctx, keys)
	byKey := map[core.RatingKey]core.Title{}
	for _, ti := range titles {
		byKey[ti.Key] = ti
	}
	selected, _, _ := s.continueExperience(pr).Shelf(ctx, user, "", 200)
	visible := s.takeVisible(user, selected, 200)
	selectedSet := map[core.RatingKey]bool{}
	for _, ti := range visible {
		selectedSet[ti.Key] = true
	}
	seen := map[core.RatingKey]bool{}
	for _, st := range states {
		ti, ok := byKey[st.Key]
		c := diagnostics.ViewerCandidate{Key: int64(st.Key), OffsetMS: st.ViewOffset.Milliseconds()}
		switch {
		case !ok:
			c.Reason = "catalogue item unavailable"
		case selectedSet[st.Key]:
			c.Title = ti.Title
			c.Included = true
			c.Reason = "in progress and selected"
		case !s.visibility().Playable(ctx, user, &ti) || !s.titleAllowed(user, &ti):
			c.Title = ti.Title
			c.Reason = "excluded by library or content policy"
		case !s.visibleTitle(ctx, user, &ti):
			c.Title = ti.Title
			c.Reason = "not available to this viewer"
		case s.sameDayUnavailable(ctx, pr, &ti):
			c.Title = ti.Title
			c.Reason = "unavailable under release rules"
		default:
			c.Title = ti.Title
			c.Reason = "not selected: exclusion, duplicate show, or row limit"
		}
		out.Candidates = append(out.Candidates, c)
		seen[st.Key] = true
	}
	for _, ti := range visible {
		if !seen[ti.Key] {
			out.Candidates = append(out.Candidates, diagnostics.ViewerCandidate{Key: int64(ti.Key), Title: ti.Title, Included: true, Reason: "next episode selected from completed viewing history"})
		}
	}
	out.Notes = append(out.Notes, "Snapshot taken when the archive was built; selection may differ from an earlier response.")
	return out, ctx.Err()
}
