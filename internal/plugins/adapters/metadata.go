package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/plugins"
)

type metadataRegistry interface {
	RegisterMetadataProvider(string, core.MetadataProvider)
}

// AttachMetadata makes both native and adapted providers use the catalogue's
// shared registration, identity checks, persistence and materialization policy.
func AttachMetadata(mgr *plugins.Manager, registry metadataRegistry) {
	if mgr == nil || registry == nil {
		return
	}
	mgr.OnSync(func(_ context.Context, r plugins.Record) {
		id := extend.PluginID(r.ID, "metadata")
		if r.Enabled && r.Manifest.HasCapability(plugins.CapMetadata) {
			registry.RegisterMetadataProvider(id, &metadataProvider{mgr, r.ID})
		} else {
			registry.RegisterMetadataProvider(id, nil)
		}
	})
}

type metadataProvider struct {
	mgr *plugins.Manager
	id  string
}

func (p *metadataProvider) ResolveMetadata(ctx context.Context, req core.MetadataRequest) (*core.Title, error) {
	rec, err := p.mgr.Get(ctx, p.id)
	if err != nil {
		return nil, err
	}
	if !rec.Enabled {
		return nil, plugins.ErrDisabled
	}
	meta := rec.Manifest.EffectiveEther().Contributes.Metadata
	if !rec.Manifest.HasCapability(plugins.CapMetadata) || meta == nil {
		return nil, core.ErrNotAvailable
	}
	accepts := false
	for _, typ := range meta.Types {
		if typ == string(req.Type) {
			accepts = true
		}
	}
	if !accepts {
		return nil, core.ErrNotAvailable
	}
	if rec.Manifest.Ether == nil {
		typ := string(req.Type)
		if req.Type == core.TypeShow {
			typ = "series"
		}
		response, err := p.mgr.StremioResource(ctx, p.id, "meta", typ, req.ID, nil)
		if err != nil {
			return nil, err
		}
		return decodeStremioMetadata(response.Body, req)
	}
	body, _ := json.Marshal(req)
	resp, err := p.mgr.Call(ctx, p.id, plugins.Request{Method: "POST", Path: "/ether/metadata/resolve", Body: body})
	if err != nil {
		return nil, err
	}
	if resp.Status != 200 {
		return nil, fmt.Errorf("metadata provider returned HTTP %d", resp.Status)
	}
	var result struct {
		Title *core.Title `json:"title"`
	}
	if err = json.Unmarshal(resp.Body, &result); err != nil {
		return nil, err
	}
	return result.Title, nil
}

// decodeStremioMetadata is the protocol boundary. No Stremio-shaped values
// escape into catalogue consumers. Unsupported identities fail explicitly.
func decodeStremioMetadata(body json.RawMessage, req core.MetadataRequest) (*core.Title, error) {
	var result struct {
		Meta *struct {
			ID          string   `json:"id"`
			Type        string   `json:"type"`
			Name        string   `json:"name"`
			Description string   `json:"description"`
			IMDB        string   `json:"imdb_id"`
			TMDB        any      `json:"moviedb_id"`
			TMDBAlt     any      `json:"tmdb_id"`
			TVDB        any      `json:"tvdb_id"`
			Poster      string   `json:"poster"`
			Background  string   `json:"background"`
			Logo        string   `json:"logo"`
			Genres      []string `json:"genres"`
			Cast        []string `json:"cast"`
			Directors   []string `json:"director"`
			ReleaseInfo string   `json:"releaseInfo"`
			Released    string   `json:"released"`
			Runtime     string   `json:"runtime"`
			Rating      string   `json:"imdbRating"`
			Adult       bool     `json:"adult"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	m := result.Meta
	if m == nil {
		return nil, core.ErrNotFound
	}
	typ := core.TypeMovie
	if m.Type == "series" {
		typ = core.TypeShow
	} else if m.Type != "movie" {
		return nil, fmt.Errorf("metadata: unsupported content type %q", m.Type)
	}
	if m.ID != req.ID || typ != req.Type {
		return nil, fmt.Errorf("metadata: add-on returned a different identity")
	}
	id := parseAnyInt64(m.TMDB)
	if id == 0 {
		id = parseAnyInt64(m.TMDBAlt)
	}
	if id == 0 && strings.HasPrefix(m.ID, "tmdb:") {
		id, _ = strconv.ParseInt(strings.TrimPrefix(m.ID, "tmdb:"), 10, 64)
	}
	imdb := m.IMDB
	if imdb == "" && strings.HasPrefix(m.ID, "tt") {
		imdb = m.ID
	}
	title := &core.Title{Type: typ, Title: m.Name, Summary: m.Description, IDs: core.IDs{TMDB: id, IMDB: imdb, TVDB: parseAnyInt64(m.TVDB)}, PosterPath: m.Poster, BackdropPath: m.Background, LogoPath: m.Logo, Genres: m.Genres, Adult: m.Adult}
	if match := yearPrefixRe.FindStringSubmatch(m.ReleaseInfo); len(match) == 2 {
		title.Year, _ = strconv.Atoi(match[1])
	}
	title.Released, _ = time.Parse(time.RFC3339Nano, m.Released)
	if title.Year == 0 && !title.Released.IsZero() {
		title.Year = title.Released.Year()
	}
	// A year alone does not invent an exact release date.
	minutes := strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(m.Runtime, "min"), "m"))
	if n, err := strconv.Atoi(minutes); err == nil && n > 0 {
		title.Runtime = time.Duration(n) * time.Minute
	}
	title.Rating, _ = strconv.ParseFloat(m.Rating, 64)
	for _, name := range m.Cast {
		title.Cast = append(title.Cast, core.Person{Name: name})
	}
	for _, name := range m.Directors {
		title.Directors = append(title.Directors, core.Person{Name: name})
	}
	return title, nil
}
