package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/braedonsaunders/ether/internal/stremio"
	"net/http"
	"strings"
)

// StremioResource calls declared read-only resources through the same guarded,
// metered runtime used by native adapters. It never treats registration as a call.
func (m *Manager) StremioResource(ctx context.Context, plugin, resource, typ, id string, extra map[string]string) (Response, error) {
	rec, err := m.Get(ctx, plugin)
	if err != nil {
		return Response{}, err
	}
	man := rec.Manifest
	if !rec.Enabled {
		return Response{}, ErrDisabled
	}
	if man.ConfigurationRequired() {
		return Response{}, &ValidationError{[]FieldError{{"configuration", "This add-on requires configuration before use."}}}
	}
	switch resource {
	case "catalog", "meta", "addon_catalog":
	default:
		return Response{}, &ValidationError{[]FieldError{{"resource", "This resource is not available through the catalogue browser."}}}
	}
	if !man.AcceptsResource(resource, typ, id) {
		return Response{}, &ValidationError{[]FieldError{{"resource", "This type or identifier is not declared by the add-on."}}}
	}
	if resource == "catalog" || resource == "addon_catalog" {
		cats := man.Catalogs
		if resource == "addon_catalog" {
			cats = man.AddonCatalogs
		}
		var cat *StremioCat
		for _, c := range cats {
			if c.Type == typ && c.ID == id {
				c := c
				cat = &c
				break
			}
		}
		if cat == nil {
			return Response{}, &ValidationError{[]FieldError{{"catalog", "This catalogue is not declared by the add-on."}}}
		}
		check := make(map[string]string, len(extra))
		for k, v := range extra {
			if k != "skip" {
				check[k] = v
			}
		}
		if err := cat.ValidateExtras(check); err != nil {
			name, msg, _ := strings.Cut(err.Error(), " ")
			return Response{}, &ValidationError{[]FieldError{{name, msg}}}
		}
	} else if len(extra) > 0 {
		return Response{}, &ValidationError{[]FieldError{{"extra", "Metadata requests do not accept catalogue filters."}}}
	}
	if typ == "" || id == "" || len(id) > 2048 {
		return Response{}, &ValidationError{[]FieldError{{"id", "A type and identifier are required."}}}
	}
	route := stremio.Route(resource, typ, id, extra)
	resp, err := m.Call(ctx, plugin, Request{Method: http.MethodGet, Path: route})
	if err != nil {
		return Response{}, err
	}
	if resp.Status != http.StatusOK {
		return resp, fmt.Errorf("add-on returned HTTP %d", resp.Status)
	}
	if !json.Valid(resp.Body) {
		return resp, fmt.Errorf("add-on returned invalid JSON")
	}
	return resp, nil
}
func (m *Manager) handleStremioResource(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	extra := map[string]string{}
	for key, values := range q {
		if strings.HasPrefix(key, "extra.") && len(values) > 0 {
			extra[strings.TrimPrefix(key, "extra.")] = values[0]
		}
	}
	resp, err := m.StremioResource(r.Context(), r.PathValue("id"), r.PathValue("resource"), q.Get("type"), q.Get("contentId"), extra)
	if err != nil {
		if resp.Status != 0 {
			writeErr(w, http.StatusBadGateway, "upstream", fmt.Sprintf("The add-on resource failed (HTTP %d).", resp.Status), nil)
		} else {
			m.fail(w, err)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp.Body)
}

// AddonFetcher reads an installed add-on's routes through Call, so every
// request is guarded, metered, redacted and counted in its health.
type AddonFetcher struct {
	m      *Manager
	plugin string
}

// Fetcher returns the stremio.Fetcher for an installed plugin.
func (m *Manager) Fetcher(plugin string) AddonFetcher { return AddonFetcher{m: m, plugin: plugin} }

// Fetch implements stremio.Fetcher.
func (f AddonFetcher) Fetch(ctx context.Context, route string, limit int64) ([]byte, error) {
	resp, err := f.m.Call(ctx, f.plugin, Request{Method: http.MethodGet, Path: route})
	if err != nil {
		return nil, err
	}
	if resp.Status < 200 || resp.Status > 299 {
		retry := ""
		for k, v := range resp.Headers {
			if strings.EqualFold(k, "Retry-After") {
				retry = v
			}
		}
		return nil, &stremio.HTTPError{Status: resp.Status, RetryAfter: retry}
	}
	if limit > 0 && int64(len(resp.Body)) > limit {
		return nil, fmt.Errorf("the add-on answered with more than %d bytes", limit)
	}
	return resp.Body, nil
}

// Addon is the installed plugin as a Stremio add-on: its Stremio manifest
// read through Fetcher.
func (m *Manager) Addon(rec Record) stremio.Addon {
	return stremio.Addon{Manifest: rec.Manifest.Stremio(), Fetcher: m.Fetcher(rec.ID)}
}
