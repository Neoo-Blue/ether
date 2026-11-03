package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/store"
)

func init() {
	store.Register("catalog", 25, `CREATE TABLE catalog_metadata_refs (
 provider TEXT NOT NULL, type TEXT NOT NULL, resource_id TEXT NOT NULL,
 key INTEGER NOT NULL, PRIMARY KEY(provider,type,resource_id));`)
}

type metadataRegistration struct{ provider core.MetadataProvider }

// RegisterMetadataProvider replaces one provider atomically. Unregistering also
// prevents an outstanding resolution from materializing after revocation.
func (c *Catalog) RegisterMetadataProvider(id string, provider core.MetadataProvider) {
	c.metadataMu.Lock()
	defer c.metadataMu.Unlock()
	if c.metadataProviders == nil {
		c.metadataProviders = map[string]*metadataRegistration{}
	}
	if provider == nil {
		delete(c.metadataProviders, id)
		return
	}
	c.metadataProviders[id] = &metadataRegistration{provider}
}

// MaterializeMetadata is called by collection/row builds, not by Get, Browse or
// Search. Existing canonical titles are reused without replacing their data.
// New records are available to the requesting collection but are not admitted
// to the global browse library just because a provider returned them.
func (c *Catalog) MaterializeMetadata(ctx context.Context, provider string, req core.MetadataRequest) (*core.Title, error) {
	if req.ID == "" || len(req.ID) > 2048 || (req.Type != core.TypeMovie && req.Type != core.TypeShow) {
		return nil, fmt.Errorf("metadata: unsupported or missing identity")
	}
	c.metadataMu.RLock()
	reg := c.metadataProviders[provider]
	c.metadataMu.RUnlock()
	if reg == nil {
		return nil, core.ErrNotAvailable
	}
	flightKey := provider + "\x00" + string(req.Type) + "\x00" + req.ID
	title, err := c.metadataFlights.Do(ctx, flightKey, func(ctx context.Context) (*core.Title, error) {
		// Registration is checked even for cached references: disabled providers
		// cannot keep supplying contributions through an old materialization handle.
		c.metadataMu.RLock()
		active := c.metadataProviders[provider] == reg
		c.metadataMu.RUnlock()
		if !active {
			return nil, core.ErrNotAvailable
		}
		var key int64
		err := c.db.QueryRowContext(ctx, `SELECT key FROM catalog_metadata_refs WHERE provider=? AND type=? AND resource_id=?`, provider, string(req.Type), req.ID).Scan(&key)
		if err == nil {
			t, err := c.StoredTitle(ctx, core.RatingKey(key))
			if err == nil {
				return c.visibleMetadata(t)
			}
			if !errors.Is(err, core.ErrNotFound) && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		title, err := reg.provider.ResolveMetadata(ctx, core.MetadataRequest{ID: req.ID, Type: req.Type})
		if err != nil {
			return nil, err
		}
		if title != nil && title.IDs.TMDB <= 0 {
			return nil, core.ErrMetadataIdentity
		}
		if title == nil || title.Type != req.Type || strings.TrimSpace(title.Title) == "" {
			return nil, fmt.Errorf("metadata: provider must return a matching movie/show with a canonical TMDB identity")
		}
		canonical := core.MovieKey(title.IDs.TMDB)
		if title.Type == core.TypeShow {
			canonical = core.ShowKey(title.IDs.TMDB)
		}
		if canonical.TMDB() != title.IDs.TMDB {
			return nil, fmt.Errorf("metadata: canonical ID is out of range")
		}
		if title.Key != 0 && title.Key != canonical {
			return nil, fmt.Errorf("metadata: returned key is not canonical")
		}
		record := *title
		record.Key = canonical
		// Hold registration through the local commit, never through network I/O.
		c.metadataMu.RLock()
		defer c.metadataMu.RUnlock()
		if c.metadataProviders[provider] != reg {
			return nil, core.ErrNotAvailable
		}
		existing, err := c.StoredTitle(ctx, canonical)
		if err == nil {
			if (existing.IDs.IMDB != "" && record.IDs.IMDB != "" && existing.IDs.IMDB != record.IDs.IMDB) || (existing.IDs.TVDB > 0 && record.IDs.TVDB > 0 && existing.IDs.TVDB != record.IDs.TVDB) {
				return nil, fmt.Errorf("metadata: provider conflicts with existing catalogue identity")
			}
			record = *existing
		} else {
			if !errors.Is(err, core.ErrNotFound) && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			if err = c.upsert(ctx, record, upsertOpts{detail: true}); err != nil {
				return nil, err
			}
		}
		_, err = c.db.ExecContext(ctx, `INSERT INTO catalog_metadata_refs(provider,type,resource_id,key) VALUES(?,?,?,?) ON CONFLICT(provider,type,resource_id) DO UPDATE SET key=excluded.key`, provider, string(req.Type), req.ID, int64(canonical))
		if err != nil {
			return nil, err
		}
		return c.visibleMetadata(&record)
	})
	if err != nil {
		return nil, err
	}
	// Expectations belong to each caller, including cache hits and callers
	// coalesced behind a request with different expected IDs.
	if !metadataMatches(title, req.IDs) {
		return nil, fmt.Errorf("metadata: returned identity does not match the request")
	}
	return title, nil
}
func metadataMatches(t *core.Title, ids core.IDs) bool {
	return t != nil && (ids.TMDB == 0 || ids.TMDB == t.IDs.TMDB) && (ids.IMDB == "" || ids.IMDB == t.IDs.IMDB) && (ids.TVDB == 0 || ids.TVDB == t.IDs.TVDB)
}
func (c *Catalog) visibleMetadata(t *core.Title) (*core.Title, error) {
	if !c.visibleTitle(t) {
		return nil, fmt.Errorf("%w: %w", core.ErrNotFound, core.ErrMetadataFiltered)
	}
	return t, nil
}
