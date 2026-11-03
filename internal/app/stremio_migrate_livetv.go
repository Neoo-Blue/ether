package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/stremio"
)

// migrateLegacyStremioLiveTV moves each built-in Stremio Live TV source
// onto its installed add-on (see migrateLegacyStremio). The source keeps
// its id, name and channels: the add-on channel source mints the same
// channel and input ids the built-in source did, so channel numbers,
// enabled flags, favourites, ratings, guide matches and transcode settings
// carry over. A source that already moved is skipped; one that fails stays
// legacy and is retried on the next start.
func (a *App) migrateLegacyStremioLiveTV(ctx context.Context) error {
	if a.livetv == nil {
		return nil
	}
	srcs, err := a.livetv.LegacyStremioSources(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, src := range srcs {
		rec, err := a.installAddon(ctx, plugins.AddonInstall{
			Link:      src.PlaylistURL,
			Name:      src.Name,
			Roles:     []plugins.Capability{plugins.CapLiveTV},
			Offline:   []string{stremio.TypeTV},
			Exclusive: true,
		})
		if err == nil {
			err = a.livetv.AdoptAddonSource(ctx, src.ID, extend.PluginID(rec.ID, plugins.StremioChannelsKey))
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("live TV source %q: %w", src.Name, err))
		}
	}
	return errors.Join(errs...)
}
