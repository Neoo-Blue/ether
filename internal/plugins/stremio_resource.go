package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
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
		var fields []StremioExtra
		_ = json.Unmarshal(cat.Extra, &fields)
		declared := map[string]bool{}
		for _, field := range fields {
			declared[field.Name] = true
			if field.Required && strings.TrimSpace(extra[field.Name]) == "" {
				return Response{}, &ValidationError{[]FieldError{{field.Name, "is required by the add-on"}}}
			}
			if value := extra[field.Name]; value != "" && len(field.Options) > 0 {
				found := false
				for _, option := range field.Options {
					if value == option {
						found = true
					}
				}
				if !found {
					return Response{}, &ValidationError{[]FieldError{{field.Name, "must be one of the add-on's declared options"}}}
				}
			}
		}
		for key := range extra {
			if !declared[key] {
				return Response{}, &ValidationError{[]FieldError{{key, "is not declared by the add-on"}}}
			}
		}
	} else if len(extra) > 0 {
		return Response{}, &ValidationError{[]FieldError{{"extra", "Metadata requests do not accept catalogue filters."}}}
	}
	if typ == "" || id == "" || len(id) > 2048 {
		return Response{}, &ValidationError{[]FieldError{{"id", "A type and identifier are required."}}}
	}
	route := "/" + resource + "/" + url.PathEscape(typ) + "/" + url.PathEscape(id)
	keys := make([]string, 0, len(extra))
	for key, value := range extra {
		if value != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var parts []string
	for _, key := range keys {
		parts = append(parts, url.QueryEscape(key)+"="+url.QueryEscape(extra[key]))
	}
	if len(parts) > 0 {
		route += "/" + strings.Join(parts, "&")
	}
	route += ".json"
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
