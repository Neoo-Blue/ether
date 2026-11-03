package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// DirectoryEntry is a discovery suggestion from a native or adapted provider.
// ManifestURL must pass the normal fetch/preview/install policy before use.
// Directory claims never confer verified status or install permissions.
type DirectoryEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version,omitempty"`
	ManifestURL string `json:"manifestURL,omitempty"`
	Unsupported string `json:"unsupported,omitempty"`
}
type DirectoryResult struct {
	Entries []DirectoryEntry `json:"entries"`
}

// BrowseDirectory is the common owner discovery path. No network request is
// made during registry rendering or ordinary media browse. Disabling/removing
// a provider prevents both cached descriptors and direct API calls from use.
func (m *Manager) BrowseDirectory(ctx context.Context, plugin, key string) (DirectoryResult, error) {
	result := DirectoryResult{Entries: []DirectoryEntry{}}
	rec, err := m.Get(ctx, plugin)
	if err != nil {
		return result, err
	}
	if !rec.Enabled {
		return result, ErrDisabled
	}
	if !rec.Manifest.HasCapability(CapPluginDirectory) {
		return result, ErrNotFound
	}
	declared := false
	for _, d := range rec.Manifest.EffectiveEther().Contributes.Directories {
		if d.Key == key {
			declared = true
			break
		}
	}
	if !declared {
		return result, ErrNotFound
	}
	var resp Response
	if rec.Manifest.Ether == nil {
		var cat *StremioCat
		for _, c := range rec.Manifest.AddonCatalogs {
			if "directory-"+strings.TrimPrefix(StremioCatalogKey(c), "catalog-") == key {
				cat = &c
				break
			}
		}
		if cat == nil {
			return result, ErrNotFound
		}
		resp, err = m.StremioResource(ctx, plugin, "addon_catalog", cat.Type, cat.ID, nil)
		if err != nil {
			return result, err
		}
		var payload struct {
			Addons []struct {
				Transport string   `json:"transportName"`
				URL       string   `json:"transportUrl"`
				Manifest  Manifest `json:"manifest"`
			} `json:"addons"`
		}
		if err = json.Unmarshal(resp.Body, &payload); err != nil {
			return result, fmt.Errorf("directory returned invalid JSON: %w", err)
		}
		if len(payload.Addons) > 1000 {
			return result, fmt.Errorf("directory returned more than 1000 entries")
		}
		for _, a := range payload.Addons {
			e := DirectoryEntry{ID: a.Manifest.ID, Name: a.Manifest.Name, Description: a.Manifest.Description, Version: a.Manifest.Version, ManifestURL: a.URL}
			if a.Transport != "http" {
				e.Unsupported = "Unsupported transport: " + a.Transport
				e.ManifestURL = ""
			}
			result.Entries = append(result.Entries, e)
		}
	} else {
		resp, err = m.Call(ctx, plugin, Request{Method: http.MethodGet, Path: "/ether/directory/" + url.PathEscape(key)})
		if err != nil {
			return result, err
		}
		if resp.Status != http.StatusOK {
			return result, fmt.Errorf("directory returned HTTP %d", resp.Status)
		}
		if err = json.Unmarshal(resp.Body, &result); err != nil {
			return DirectoryResult{}, fmt.Errorf("directory returned invalid JSON: %w", err)
		}
	}
	if len(result.Entries) > 1000 {
		return DirectoryResult{}, fmt.Errorf("directory returned more than 1000 entries")
	}
	for i := range result.Entries {
		e := &result.Entries[i]
		u, err := url.Parse(e.ManifestURL)
		if e.Unsupported != "" || err != nil || u == nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Fragment != "" {
			e.ManifestURL = ""
			if e.Unsupported == "" {
				e.Unsupported = "No supported HTTP manifest address"
			}
		}
		if e.ID == "" || strings.TrimSpace(e.Name) == "" {
			e.Unsupported = "Directory entry has no identity or name"
			e.ManifestURL = ""
		}
	}
	// Re-check lifecycle after the remote response, just as install preview does
	// not grant a removed provider authority to keep contributing.
	current, err := m.Get(ctx, plugin)
	if err != nil {
		return DirectoryResult{}, err
	}
	if !current.Enabled {
		return DirectoryResult{}, ErrDisabled
	}
	if result.Entries == nil {
		result.Entries = []DirectoryEntry{}
	}
	return result, nil
}
func (m *Manager) handleDirectory(w http.ResponseWriter, r *http.Request) {
	result, err := m.BrowseDirectory(r.Context(), r.PathValue("id"), r.PathValue("key"))
	if err != nil {
		m.fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, result)
}
