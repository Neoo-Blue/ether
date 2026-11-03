package app

import (
	"context"
	"fmt"

	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/stremio"
)

// migrateLegacyStremioSubtitles turns every subtitle provider entered as a
// Stremio add-on link into the installed add-on's subtitle provider, in the
// same place in the owner's order, keeping its name and on/off state. The
// same add-on entered twice keeps its first place.
func (a *App) migrateLegacyStremioSubtitles(ctx context.Context) error {
	type move struct {
		at   int
		id   string
		kind string
	}
	var moves []move
	var firstErr error
	for i, p := range a.cfg.Get().Subtitles.Providers {
		if p.Kind != config.SubtitleProviderStremio {
			continue
		}
		rec, err := a.installAddon(ctx, plugins.AddonInstall{
			Link: p.URL, Name: p.Name, Roles: []plugins.Capability{plugins.CapSubtitles},
			Offline: []string{stremio.ResourceSubtitles}, Exclusive: true,
		})
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("subtitle provider %q: %w", p.Name, err)
			}
			continue
		}
		moves = append(moves, move{at: i, id: p.ID, kind: extend.PluginID(rec.ID, "subtitles")})
	}
	if len(moves) == 0 {
		return firstErr
	}
	err := a.cfg.Update(func(c *config.Config) error {
		ps := c.Subtitles.Providers
		for _, mv := range moves {
			if mv.at >= len(ps) || ps[mv.at].Kind != config.SubtitleProviderStremio || ps[mv.at].ID != mv.id {
				continue // the list changed meanwhile; the next start retries
			}
			ps[mv.at] = config.SubtitleProvider{ID: mv.kind, Kind: mv.kind, Name: ps[mv.at].Name, Enabled: ps[mv.at].Enabled}
		}
		seen := map[string]bool{}
		out := ps[:0]
		for _, p := range ps {
			if !seen[p.ID] {
				seen[p.ID] = true
				out = append(out, p)
			}
		}
		c.Subtitles.Providers = out
		return nil
	})
	if err != nil {
		return err
	}
	return firstErr
}
