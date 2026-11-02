package collections

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/stremio"
)

// WithStremioHTTPClient sets the HTTP client used for Stremio catalog
// requests (tests). When nil, egress.CapStremio is used.
func WithStremioHTTPClient(c *http.Client) Option {
	return func(s *Service) { s.opts.stremioHTTP = c }
}

// WithSealer enables sealing of secret Stremio add-on install URLs inside
// stored collection specs.
func WithSealer(sealer config.FieldSealer) Option {
	return func(s *Service) { s.sealer = sealer }
}

// StremioCatalogOption describes one movie/series catalog offered by a
// Stremio add-on manifest.
type StremioCatalogOption struct {
	ID        string   `json:"id"`
	Type      string   `json:"type"` // "movie" or "show"
	Name      string   `json:"name"`
	Genres    []string `json:"genres,omitempty"`
	CanSearch bool     `json:"canSearch,omitempty"`
}

// StremioManifestSummary is returned when the console inspects a Stremio
// manifest URL for the collection builder.
type StremioManifestSummary struct {
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	Catalogs []StremioCatalogOption `json:"catalogs"`
}

// InspectStremioManifest fetches a Stremio add-on manifest and lists its
// movie and series catalogs for the console picker.
func (s *Service) InspectStremioManifest(ctx context.Context, rawURL string) (*StremioManifestSummary, error) {
	c, err := s.stremioClient(rawURL)
	if err != nil {
		return nil, err
	}
	m, err := c.Manifest(ctx)
	if err != nil {
		return nil, err
	}
	out := &StremioManifestSummary{
		ID:       m.ID,
		Name:     m.Name,
		Catalogs: []StremioCatalogOption{},
	}
	for _, cat := range m.Catalogs {
		mediaType := ""
		switch strings.ToLower(strings.TrimSpace(cat.Type)) {
		case "movie":
			mediaType = "movie"
		case "series", "show":
			mediaType = "show"
		default:
			continue
		}
		genres := append([]string(nil), cat.Genres...)
		canSearch := false
		for _, ex := range cat.Extra {
			switch strings.ToLower(strings.TrimSpace(ex.Name)) {
			case "genre":
				genres = append(genres, ex.Options...)
			case "search":
				canSearch = true
			}
		}
		name := strings.TrimSpace(cat.Name)
		if name == "" {
			name = cat.ID
		}
		out.Catalogs = append(out.Catalogs, StremioCatalogOption{
			ID:        cat.ID,
			Type:      mediaType,
			Name:      name,
			Genres:    genres,
			CanSearch: canSearch,
		})
	}
	return out, nil
}

func (s *Service) stremioClient(rawURL string) (*stremio.Client, error) {
	return stremio.NewClient(rawURL, s.opts.stremioHTTP)
}

func hasStremioCatalog(sp core.CollectionSpec) bool {
	for _, src := range sp.Sources {
		if src.Kind == "stremioCatalog" {
			return true
		}
	}
	return false
}

// ---- stremioCatalog ----

type stremioCatalogKind struct{}

func (stremioCatalogKind) ID() string { return "stremioCatalog" }

func (stremioCatalogKind) Describe() KindDescribe {
	return KindDescribe{
		Label:      extend.T("Stremio catalog"),
		Hint:       extend.T("A catalog from a Stremio add-on"),
		Icon:       "puzzle",
		Order:      160,
		Editor:     "stremioCatalog",
		Defaults:   `{"url":"","list":"","mediaType":"movie","maxItems":100}`,
		MediaTypes: []string{"movie", "show"},
		Params: []extend.ParamField{
			{Name: "url", Kind: "secret", Label: extend.T("Manifest URL"), Required: true, Placeholder: "https://example.com/manifest.json"},
			{Name: "mediaType", Kind: "select", Label: extend.T("Type"), Required: true, Options: []extend.ParamOption{
				{Value: "movie", Label: extend.T("Movies")},
				{Value: "show", Label: extend.T("TV")},
			}},
			{Name: "list", Kind: "text", Label: extend.T("Catalog ID"), Required: true, Placeholder: "top"},
			{Name: "maxItems", Kind: "number", Label: extend.T("Maximum items"), Hint: extend.T("Up to 2,000 items per refresh")},
		},
	}
}

func (stremioCatalogKind) Validate(ve *ValidationError, path string, src core.CollectionSource) {
	if _, err := stremio.BaseURL(src.URL); err != nil {
		ve.add(path+".url", "enter a valid http(s) Stremio add-on URL")
	}
	if strings.TrimSpace(src.List) == "" {
		ve.add(path+".list", "choose or enter a catalog id")
	}
	if src.MediaType != "" && src.MediaType != MediaMovie && src.MediaType != MediaShow && src.MediaType != "series" {
		ve.add(path+".mediaType", `must be "movie" or "show"`)
	}
	if src.MaxItems < 0 || src.MaxItems > stremio.MaxCatalogItems {
		ve.add(path+".maxItems", fmt.Sprintf("must be between 1 and %d", stremio.MaxCatalogItems))
	}
	if raw, ok := src.Params["maxItems"]; ok && strings.TrimSpace(raw) != "" {
		n, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || n < 1 || n > stremio.MaxCatalogItems {
			ve.add(path+".maxItems", fmt.Sprintf("must be between 1 and %d", stremio.MaxCatalogItems))
		}
	}
}

func stremioSourceMaxItems(src core.CollectionSource, fallback int) int {
	maxItems := fallback
	if src.MaxItems > 0 {
		maxItems = src.MaxItems
	} else if raw := strings.TrimSpace(src.Params["maxItems"]); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			maxItems = n
		}
	}
	if maxItems <= 0 {
		maxItems = 100
	}
	if maxItems > stremio.MaxCatalogItems {
		maxItems = stremio.MaxCatalogItems
	}
	return maxItems
}

func (stremioCatalogKind) Gather(g *Gather) {
	c, err := g.Svc.stremioClient(g.Src.URL)
	if err != nil {
		g.Warn("Stremio catalog: %v", err)
		return
	}
	maxItems := stremioSourceMaxItems(g.Src, g.Want)
	var extra map[string]string
	for _, k := range []string{"genre", "search"} {
		if v := strings.TrimSpace(g.Src.Params[k]); v != "" {
			if extra == nil {
				extra = map[string]string{}
			}
			extra[k] = v
		}
	}
	var refs []extRef
	totalItems := 0
	unmatched := 0
	fetchedAny := false
	for _, kind := range g.Kinds {
		stremioType := "movie"
		refType := "movie"
		if kind == "tv" {
			stremioType = "series"
			refType = "show"
		}
		metas, err := c.Catalog(g.Ctx, stremioType, strings.TrimSpace(g.Src.List), extra, maxItems)
		if err != nil {
			g.Warn("Stremio catalog %s: %v", g.Src.List, err)
			continue
		}
		fetchedAny = true
		for _, m := range metas {
			totalItems++
			imdb, tmdb := stremio.ParseCatalogRef(m.ID)
			if imdb == "" && tmdb <= 0 {
				// SPEC D7 & D11: Only items with tt… or tmdb: ids match the
				// catalogue; unmatched items are counted and shown, never invented.
				unmatched++
				continue
			}
			typ := refType
			if strings.EqualFold(m.Type, "series") || strings.EqualFold(m.Type, "show") || strings.EqualFold(m.Type, "tv") {
				typ = "show"
			} else if strings.EqualFold(m.Type, "movie") {
				typ = "movie"
			}
			refs = append(refs, extRef{
				IMDB: imdb,
				TMDB: tmdb,
				Type: typ,
			})
		}
	}
	if !fetchedAny {
		return
	}
	if totalItems == 0 {
		g.Warn("%s is empty", sourceName(g.Src))
		return
	}
	if len(refs) > 0 {
		titles, unresolved, _ := g.Svc.resolveExternal(g.Ctx, g.Src.Kind, refs)
		g.Res.titles = titles
		unmatched += unresolved
	}
	if unmatched > 0 {
		g.Warn("%d of %d items in %s could not be matched to the catalogue", unmatched, totalItems, sourceName(g.Src))
	}
}

func (s *Service) sealSpec(sp core.CollectionSpec, collectionID int64) (core.CollectionSpec, error) {
	if s == nil || s.sealer == nil {
		return sp, nil
	}
	out := cloneSpec(sp)
	for i := range out.Sources {
		src := &out.Sources[i]
		if src.Kind != "stremioCatalog" || strings.TrimSpace(src.URL) == "" {
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
		if src.Kind != "stremioCatalog" || strings.TrimSpace(src.URL) == "" {
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
