package collections

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
)

// LegacyStremioCatalogKind is the retired source kind of a Stremio catalog
// entered with its add-on link. Stored specs may still carry it, with the
// link sealed, until the add-on migration (internal/app) rewrites them to
// the installed add-on's list source. No kind is registered for it, so such
// a spec neither validates, builds nor saves: it keeps serving the members
// of its last build until it is migrated.
const LegacyStremioCatalogKind = "stremioCatalog"

// backgroundOnlySource reports whether a source is a remote list (a
// plugin's or add-on's list source): gathered in the background, previewed
// one page at a time by the owner, never fetched on a viewer request.
func backgroundOnlySource(src core.CollectionSource) bool {
	return strings.HasPrefix(src.Kind, "plugin:")
}

// hasRemoteListSource reports a spec with a remote list source.
func hasRemoteListSource(sp core.CollectionSpec) bool {
	for _, src := range sp.Sources {
		if backgroundOnlySource(src) {
			return true
		}
	}
	return false
}

// legacyMediaKinds are the media kinds a legacy source read its catalog
// for, as gather chose them: the source's own type, else the collection's.
func legacyMediaKinds(sp core.CollectionSpec, src core.CollectionSource) []string {
	switch src.MediaType {
	case MediaMovie:
		return kindsFor(MediaMovie)
	case MediaShow, "series", "tv":
		return kindsFor(MediaShow)
	}
	switch sp.MediaType {
	case "series", "tv":
		return kindsFor(MediaShow)
	case "":
		return kindsFor(MediaMovie)
	}
	return kindsFor(sp.MediaType)
}

// WithSealer lets the service open the add-on links that legacy Stremio
// catalog sources stored sealed, for MigrateLegacyStremioSources.
func WithSealer(sealer config.FieldSealer) Option {
	return func(s *Service) { s.sealer = sealer }
}

// MigrateLegacyStremioSources rewrites every stored source of kind
// LegacyStremioCatalogKind in place. migrate receives the source with its
// add-on link opened and its MediaType set ("movie" or "show"), and returns
// the source that replaces it. A legacy source without a media type read
// its catalog for each media type of the collection, so it is migrated
// once per type and replaced by one source each. The collection keeps its
// id, members, build state and refresh schedule; its next scheduled
// refresh reads the new sources. A source whose link cannot be opened or
// whose migrate fails stays as it was (still sealed), and the first such
// error is returned so the caller can retry later. Running it again after
// a full migration finds nothing to do.
func (s *Service) MigrateLegacyStremioSources(ctx context.Context, migrate func(context.Context, core.CollectionSource) (core.CollectionSource, error)) error {
	type stored struct {
		id  int64
		raw string
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, spec_json FROM collections_specs WHERE builtin=0 ORDER BY id`)
	if err != nil {
		return err
	}
	var legacy []stored
	for rows.Next() {
		var st stored
		if err := rows.Scan(&st.id, &st.raw); err != nil {
			rows.Close()
			return err
		}
		if strings.Contains(st.raw, `"`+LegacyStremioCatalogKind+`"`) {
			legacy = append(legacy, st)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	var firstErr error
	fail := func(id int64, err error) {
		if firstErr == nil {
			firstErr = fmt.Errorf("collection %d: %w", id, err)
		}
	}
	for _, st := range legacy {
		var sp core.CollectionSpec
		if err := json.Unmarshal([]byte(st.raw), &sp); err != nil {
			fail(st.id, err)
			continue
		}
		changed := false
		sources := make([]core.CollectionSource, 0, len(sp.Sources))
		for _, src := range sp.Sources {
			if src.Kind != LegacyStremioCatalogKind {
				sources = append(sources, src)
				continue
			}
			link, _, err := config.OpenWith(s.sealer, src.URL, "collections_specs", "sources.url", strconv.FormatInt(st.id, 10), src.Kind)
			if err != nil {
				fail(st.id, err)
				sources = append(sources, src)
				continue
			}
			var next []core.CollectionSource
			for _, kind := range legacyMediaKinds(sp, src) {
				in := src
				in.URL, in.MediaType = link, MediaMovie
				if kind == "tv" {
					in.MediaType = MediaShow
				}
				out, err := migrate(ctx, in)
				if err == nil && (out.Kind == "" || out.Kind == LegacyStremioCatalogKind) {
					err = fmt.Errorf("the migration returned no replacement source")
				}
				if err != nil {
					fail(st.id, err)
					next = nil
					break
				}
				next = append(next, out)
			}
			if next == nil {
				sources = append(sources, src)
				continue
			}
			sources = append(sources, next...)
			changed = true
		}
		sp.Sources = sources
		if !changed {
			continue
		}
		js, err := specJSON(sp)
		if err != nil {
			fail(st.id, err)
			continue
		}
		// Only the document changes: build hash, members and timestamps stay.
		res, err := s.db.ExecContext(ctx, `UPDATE collections_specs SET spec_json=? WHERE id=? AND spec_json=?`, js, st.id, st.raw)
		if err != nil {
			fail(st.id, err)
			continue
		}
		if n, _ := res.RowsAffected(); n == 0 {
			fail(st.id, fmt.Errorf("changed while it was migrated"))
			continue
		}
		s.changed(st.id)
	}
	return firstErr
}
