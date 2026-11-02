package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// host implements Host for one plugin. Grants are re-read on every call,
// so a revoke takes effect mid-call. Writes are journalled with their
// inverse; in shadow mode they are recorded without applying.
type host struct {
	m      *Manager
	plugin string
}

func (h *host) record(ctx context.Context) (Record, Grants, error) {
	r, err := h.m.store.record(ctx, h.plugin)
	if err != nil {
		return Record{}, Grants{}, err
	}
	if !r.Enabled {
		return Record{}, Grants{}, ErrDisabled
	}
	g, err := h.m.store.grants(ctx, h.plugin)
	return r, g, err
}

func (h *host) deny(r Record, g Grants, key, level string, allowed func(string) bool) error {
	if g.revoked(key) || !allowed(level) {
		h.noteDenied(key)
		return fmt.Errorf("%w: %s", ErrPermissionDenied, key)
	}
	return nil
}

func (h *host) noteDenied(key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if hh, err := h.m.store.health(ctx, h.plugin); err == nil && hh.Status == "ok" {
		hh.Status = "attention"
		hh.Reason = "A permission it needs was taken back: " + key
		_ = h.m.store.putHealth(ctx, h.plugin, hh)
	}
}

func viewerOK(g Grants, level string, viewerID int64) bool {
	switch level {
	case "all":
		return true
	case "granted_viewers":
		return g.viewerGranted(viewerID)
	}
	return false
}

func (h *host) KVGet(ctx context.Context, key string) ([]byte, bool, error) {
	if _, _, err := h.record(ctx); err != nil {
		return nil, false, err
	}
	return h.m.store.kvGet(ctx, h.plugin, key)
}

func (h *host) KVPut(ctx context.Context, key string, value []byte) error {
	if _, _, err := h.record(ctx); err != nil {
		return err
	}
	return h.m.store.kvPut(ctx, h.plugin, key, value)
}

func (h *host) KVDelete(ctx context.Context, key string) error {
	if _, _, err := h.record(ctx); err != nil {
		return err
	}
	return h.m.store.kvDelete(ctx, h.plugin, key)
}

func (h *host) KVList(ctx context.Context, prefix string) ([]string, error) {
	if _, _, err := h.record(ctx); err != nil {
		return nil, err
	}
	return h.m.store.kvList(ctx, h.plugin, prefix)
}

func (h *host) SecretGet(ctx context.Context, name string) (string, error) {
	r, g, err := h.record(ctx)
	if err != nil {
		return "", err
	}
	declared := false
	for _, s := range r.Manifest.Ether.Permissions.Secrets {
		if s == name {
			declared = true
		}
	}
	if err := h.deny(r, g, "secrets", "", func(string) bool { return declared }); err != nil {
		return "", err
	}
	row, err := h.m.store.settings(ctx, h.plugin)
	if err != nil {
		return "", err
	}
	stored, ok := row.Secrets[name]
	if !ok {
		return "", fmt.Errorf("%w: secret %s is not saved", ErrUnavailable, name)
	}
	return h.m.store.openSecret(h.plugin, name, stored)
}

// hostAllowed matches a host against the manifest network list.
func hostAllowed(patterns []string, host string) bool {
	host = strings.ToLower(host)
	for _, p := range patterns {
		p = strings.ToLower(p)
		if strings.HasPrefix(p, "*.") {
			if strings.HasSuffix(host, p[1:]) && host != p[2:] {
				return true
			}
			continue
		}
		if p == host {
			return true
		}
	}
	return false
}

func (h *host) HTTPFetch(ctx context.Context, req FetchRequest) (FetchResponse, error) {
	r, g, err := h.record(ctx)
	if err != nil {
		return FetchResponse{}, err
	}
	u, err := url.Parse(req.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return FetchResponse{}, fmt.Errorf("%w: bad url", ErrPermissionDenied)
	}
	if err := h.deny(r, g, "network", "", func(string) bool {
		return hostAllowed(r.Manifest.Ether.Permissions.Network, u.Hostname())
	}); err != nil {
		return FetchResponse{}, err
	}
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	hr, err := http.NewRequestWithContext(ctx, method, req.URL, bytes.NewReader(req.Body))
	if err != nil {
		return FetchResponse{}, err
	}
	for k, v := range req.Headers {
		hr.Header.Set(k, v)
	}
	// Recheck the current grant before every redirect, as well as the SSRF
	// guard's own redirect policy. A redirect is another network request.
	client := *h.m.client
	previousRedirect := client.CheckRedirect
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		current, grants, err := h.record(next.Context())
		if err != nil {
			return err
		}
		if err := h.deny(current, grants, "network", "", func(string) bool {
			return hostAllowed(current.Manifest.Ether.Permissions.Network, next.URL.Hostname())
		}); err != nil {
			return err
		}
		if previousRedirect != nil {
			return previousRedirect(next, via)
		}
		if len(via) >= 10 {
			return errors.New("plugins: stopped after 10 redirects")
		}
		return nil
	}
	resp, err := client.Do(hr)
	if err != nil {
		return FetchResponse{}, err
	}
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxFetchBytes+1))
	if err != nil {
		return FetchResponse{}, err
	}
	if len(body) > MaxFetchBytes {
		return FetchResponse{}, fmt.Errorf("plugins: response larger than %d bytes", MaxFetchBytes)
	}
	// Privacy label: record the observed host and bytes.
	if hh, err := h.m.store.health(ctx, h.plugin); err == nil {
		hh.Observed[u.Hostname()]++
		hh.EgressBytes += int64(len(body) + len(req.Body))
		_ = h.m.store.putHealth(ctx, h.plugin, hh)
	}
	out := FetchResponse{Status: resp.StatusCode, Headers: map[string]string{}, Body: body}
	for k := range resp.Header {
		out.Headers[k] = resp.Header.Get(k)
	}
	return out, nil
}

func (h *host) catalogCheck(ctx context.Context) (Record, error) {
	r, g, err := h.record(ctx)
	if err != nil {
		return Record{}, err
	}
	return r, h.deny(r, g, "catalog", normLevel(r.Manifest.Ether.Permissions.Catalog), func(l string) bool { return l == "read" })
}

func (h *host) CatalogSearch(ctx context.Context, query string, limit, offset int) (json.RawMessage, error) {
	if _, err := h.catalogCheck(ctx); err != nil {
		return nil, err
	}
	if h.m.deps.Backends.CatalogSearch == nil {
		return nil, ErrUnavailable
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	return h.m.deps.Backends.CatalogSearch(ctx, query, limit, offset)
}

func (h *host) CatalogGet(ctx context.Context, id string) (json.RawMessage, error) {
	if _, err := h.catalogCheck(ctx); err != nil {
		return nil, err
	}
	if h.m.deps.Backends.CatalogGet == nil {
		return nil, ErrUnavailable
	}
	return h.m.deps.Backends.CatalogGet(ctx, id)
}

func (h *host) ViewerHistory(ctx context.Context, viewerID int64, limit, offset int) (json.RawMessage, error) {
	r, g, err := h.record(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.deny(r, g, "viewers.watch_history", normLevel(r.Manifest.Ether.Permissions.Viewers.WatchHistory),
		func(l string) bool { return viewerOK(g, l, viewerID) }); err != nil {
		return nil, err
	}
	if h.m.deps.Backends.ViewerHistory == nil {
		return nil, ErrUnavailable
	}
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	return h.m.deps.Backends.ViewerHistory(ctx, viewerID, limit, offset)
}

// journalWrite records a write (and, unless shadow, has already applied
// it; callers pass the inverse they obtained).
func (h *host) journal(ctx context.Context, r Record, op, detail string, inverse any) error {
	raw, err := json.Marshal(inverse)
	if err != nil {
		return err
	}
	_, err = h.m.store.journal(ctx, JournalEntry{Plugin: h.plugin, Op: op, Detail: detail, Inverse: raw, Shadow: r.Shadow})
	return err
}

type watchInverse struct {
	Op       string `json:"op"`
	ViewerID int64  `json:"viewerId"`
	TitleID  string `json:"titleId"`
	Watched  bool   `json:"watched"`
}

type collectionInverse struct {
	Op       string          `json:"op"`
	Previous json.RawMessage `json:"previous,omitempty"` // empty means delete
	Doc      json.RawMessage `json:"doc,omitempty"`
}

func (h *host) SetWatchState(ctx context.Context, viewerID int64, titleID string, watched bool) error {
	r, g, err := h.record(ctx)
	if err != nil {
		return err
	}
	if err := h.deny(r, g, "viewers.write_watch_state", normLevel(r.Manifest.Ether.Permissions.Viewers.WriteWatchState),
		func(l string) bool { return viewerOK(g, l, viewerID) }); err != nil {
		return err
	}
	if r.Shadow {
		return h.journal(ctx, r, "viewers.setWatchState", titleID, watchInverse{Op: "viewers.setWatchState", ViewerID: viewerID, TitleID: titleID, Watched: !watched})
	}
	if h.m.deps.Backends.SetWatchState == nil {
		return ErrUnavailable
	}
	prev, err := h.m.deps.Backends.SetWatchState(ctx, viewerID, titleID, watched)
	if err != nil {
		return err
	}
	return h.journal(ctx, r, "viewers.setWatchState", titleID, watchInverse{Op: "viewers.setWatchState", ViewerID: viewerID, TitleID: titleID, Watched: prev})
}

func (h *host) CollectionsUpsert(ctx context.Context, doc json.RawMessage) error {
	r, g, err := h.record(ctx)
	if err != nil {
		return err
	}
	if err := h.deny(r, g, "collections", normLevel(r.Manifest.Ether.Permissions.Collections),
		func(l string) bool { return l == "write" }); err != nil {
		return err
	}
	if r.Shadow {
		return h.journal(ctx, r, "collections.upsert", "", collectionInverse{Op: "collections.upsert", Doc: doc})
	}
	if h.m.deps.Backends.CollectionsUpsert == nil {
		return ErrUnavailable
	}
	prev, err := h.m.deps.Backends.CollectionsUpsert(ctx, doc)
	if err != nil {
		return err
	}
	return h.journal(ctx, r, "collections.upsert", "", collectionInverse{Op: "collections.upsert", Previous: prev, Doc: doc})
}

// UndoAll replays a plugin's live journal inverses, newest first, and
// returns how many entries were undone.
func (m *Manager) UndoAll(ctx context.Context, pluginID string) (int, error) {
	entries, err := m.store.journalList(ctx, pluginID, 10000, true)
	if err != nil {
		return 0, err
	}
	undone := 0
	for _, e := range entries { // journalList is newest first
		if err := m.applyInverse(ctx, e); err != nil {
			return undone, err
		}
		if err := m.store.markUndone(ctx, e.ID); err != nil {
			return undone, err
		}
		undone++
	}
	return undone, nil
}

func (m *Manager) applyInverse(ctx context.Context, e JournalEntry) error {
	switch e.Op {
	case "viewers.setWatchState":
		var inv watchInverse
		if err := json.Unmarshal(e.Inverse, &inv); err != nil {
			return err
		}
		if m.deps.Backends.SetWatchState == nil {
			return ErrUnavailable
		}
		_, err := m.deps.Backends.SetWatchState(ctx, inv.ViewerID, inv.TitleID, inv.Watched)
		return err
	case "collections.upsert":
		var inv collectionInverse
		if err := json.Unmarshal(e.Inverse, &inv); err != nil {
			return err
		}
		if len(inv.Previous) > 0 {
			if m.deps.Backends.CollectionsUpsert == nil {
				return ErrUnavailable
			}
			_, err := m.deps.Backends.CollectionsUpsert(ctx, inv.Previous)
			return err
		}
		if m.deps.Backends.CollectionsDelete == nil {
			return ErrUnavailable
		}
		return m.deps.Backends.CollectionsDelete(ctx, inv.Doc)
	default:
		return fmt.Errorf("plugins: no inverse for journal op %q", e.Op)
	}
}

func (h *host) folderCheck(ctx context.Context, root, path string) (string, error) {
	r, g, err := h.record(ctx)
	if err != nil {
		return "", err
	}
	if err := h.deny(r, g, "folders", normLevel(r.Manifest.Ether.Permissions.Folders),
		func(l string) bool { return l == "read" }); err != nil {
		return "", err
	}
	granted := false
	for _, f := range g.Folders {
		if f == root {
			granted = true
		}
	}
	if !granted {
		h.noteDenied("folders")
		return "", fmt.Errorf("%w: folder was not picked for this plugin", ErrPermissionDenied)
	}
	cleanRoot := filepath.Clean(root)
	joined := filepath.Clean(filepath.Join(cleanRoot, path))
	if joined != cleanRoot && !strings.HasPrefix(joined, cleanRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: path leaves the picked folder", ErrPermissionDenied)
	}
	return joined, nil
}

func (h *host) FoldersList(ctx context.Context, root, path string) ([]FolderEntry, error) {
	_, err := h.folderCheck(ctx, root, path)
	if err != nil {
		return nil, err
	}
	fs, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer fs.Close()
	name := path
	if name == "" {
		name = "."
	}
	dir, err := fs.Open(name)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(1000)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	out := make([]FolderEntry, 0, len(entries))
	for _, e := range entries {
		fe := FolderEntry{Name: e.Name(), Path: filepath.Join(path, e.Name()), Dir: e.IsDir()}
		if info, err := e.Info(); err == nil {
			fe.Size, fe.Mtime = info.Size(), info.ModTime().Unix()
		}
		out = append(out, fe)
		if len(out) >= 1000 {
			break
		}
	}
	return out, nil
}

func (h *host) FoldersStat(ctx context.Context, root, path string) (FolderEntry, error) {
	_, err := h.folderCheck(ctx, root, path)
	if err != nil {
		return FolderEntry{}, err
	}
	fs, err := os.OpenRoot(root)
	if err != nil {
		return FolderEntry{}, err
	}
	defer fs.Close()
	name := path
	if name == "" {
		name = "."
	}
	info, err := fs.Stat(name)
	if err != nil {
		return FolderEntry{}, err
	}
	return FolderEntry{Name: info.Name(), Path: path, Dir: info.IsDir(), Size: info.Size(), Mtime: info.ModTime().Unix()}, nil
}

func (m *Manager) rateOK(bucket map[string][]time.Time, key string, limit int, window time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	kept := bucket[key][:0:0]
	for _, t := range bucket[key] {
		if now.Sub(t) < window {
			kept = append(kept, t)
		}
	}
	if len(kept) >= limit {
		bucket[key] = kept
		return false
	}
	bucket[key] = append(kept, now)
	return true
}

func (h *host) NotifyOwner(ctx context.Context, text string) error {
	if _, _, err := h.record(ctx); err != nil {
		return err
	}
	if !h.m.rateOK(h.m.notifyTimes, h.plugin, 5, time.Hour) {
		return fmt.Errorf("plugins: owner notices are limited to five an hour")
	}
	if h.m.deps.Backends.Notice != nil {
		h.m.deps.Backends.Notice(truncate(text, 500))
	}
	h.m.store.addLog(ctx, h.plugin, "notice: "+text)
	return nil
}

func (h *host) Log(ctx context.Context, level, msg string, attrs map[string]any) error {
	if _, _, err := h.record(ctx); err != nil {
		return err
	}
	if !h.m.rateOK(h.m.logTimes, h.plugin, 600, time.Minute) {
		return nil // drop, never fail the caller for logging
	}
	line := level + ": " + msg
	if len(attrs) > 0 {
		if raw, err := json.Marshal(attrs); err == nil {
			line += " " + redactJSON(raw)
		}
	}
	h.m.store.addLog(ctx, h.plugin, line)
	return nil
}
