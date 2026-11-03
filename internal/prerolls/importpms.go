package prerolls

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/braedonsaunders/ether/internal/core"
)

// ImportPMS creates pre-rolls from a Plex Media Server
// CinemaTrailersPrerollID value. PMS plays semicolon-separated paths in
// order and picks one of comma-separated paths at random. Ether plays one
// pre-roll per play, so each semicolon group gets a lower priority than
// the one before it (the first group wins) and the paths inside a group
// share a priority (ties are chosen at random). Every path is validated
// before anything is created.
func (s *Service) ImportPMS(ctx context.Context, value string) ([]Preroll, error) {
	type item struct {
		path     string
		priority int
	}
	var items []item
	seen := map[string]bool{}
	groups := strings.Split(value, ";")
	for gi, group := range groups {
		for _, raw := range strings.Split(group, ",") {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			path, err := checkSourcePath(raw, s.maxFile)
			if err != nil {
				return nil, invalid("paths", "%s: %s", raw, err.(*ValidationError).Message)
			}
			if seen[path] {
				continue
			}
			seen[path] = true
			items = append(items, item{path: path, priority: min((len(groups)-gi)*10, 1000)})
		}
	}
	if len(items) == 0 {
		return nil, invalid("paths", "paste the CinemaTrailersPrerollID value from Plex")
	}
	if len(items) > 100 {
		return nil, invalid("paths", "import at most 100 videos at once")
	}
	created := make([]Preroll, 0, len(items))
	for _, it := range items {
		name := strings.TrimSuffix(filepath.Base(it.path), filepath.Ext(it.path))
		p, err := s.Create(ctx, Input{Name: clip(name, maxNameLen), Priority: it.priority, Order: core.PrerollBeforeTrailers})
		if err != nil {
			return created, err
		}
		if p, err = s.AttachPath(ctx, p.ID, it.path); err != nil {
			return created, err
		}
		created = append(created, p)
	}
	return created, nil
}
