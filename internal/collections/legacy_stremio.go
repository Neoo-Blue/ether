package collections

import (
	"strconv"
	"strings"

	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
)

// LegacyStremioCatalogKind is the retired source kind of a Stremio catalog
// entered with its add-on link. Stored specs may still carry it (with a
// sealed link) until the add-on migration (internal/app) rewrites them to
// the installed add-on's list source; nothing creates it any more.
const LegacyStremioCatalogKind = "stremioCatalog"

// backgroundOnlySource reports whether a source is a remote list (an
// add-on's list source): gathered in the background, previewed one page at
// a time by the owner, never fetched on a viewer request.
func backgroundOnlySource(src core.CollectionSource) bool {
	return strings.HasPrefix(src.Kind, "plugin:") || src.Kind == LegacyStremioCatalogKind
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

func (s *Service) sealSpec(sp core.CollectionSpec, collectionID int64) (core.CollectionSpec, error) {
	if s == nil || s.sealer == nil {
		return sp, nil
	}
	out := cloneSpec(sp)
	for i := range out.Sources {
		src := &out.Sources[i]
		if src.Kind != LegacyStremioCatalogKind || strings.TrimSpace(src.URL) == "" {
			continue
		}
		sealed, err := config.SealWith(s.sealer, src.URL, "collections_specs", "sources.url", strconv.FormatInt(collectionID, 10), src.Kind)
		if err != nil {
			return sp, err
		}
		src.URL = sealed
	}
	return out, nil
}

func openSpecSources(sealer config.FieldSealer, sp *core.CollectionSpec, collectionID int64) error {
	if sealer == nil || sp == nil {
		return nil
	}
	for i := range sp.Sources {
		src := &sp.Sources[i]
		if src.Kind != LegacyStremioCatalogKind || strings.TrimSpace(src.URL) == "" {
			continue
		}
		plain, _, err := config.OpenWith(sealer, src.URL, "collections_specs", "sources.url", strconv.FormatInt(collectionID, 10), src.Kind)
		if err != nil {
			return err
		}
		src.URL = plain
	}
	return nil
}

// WithSealer lets the service open legacy specs whose Stremio catalog
// links were sealed at rest, until the add-on migration rewrites them.
func WithSealer(sealer config.FieldSealer) Option {
	return func(s *Service) { s.sealer = sealer }
}
