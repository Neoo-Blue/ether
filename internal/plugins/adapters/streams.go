// Package adapters bridges installed Ether plugins (internal/plugins) into
// the core media, catalogue, live TV, subtitle, marker, notification and
// ranking interfaces (SPEC §5.1.3, WS9).
package adapters

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/debrid"
	"github.com/braedonsaunders/ether/internal/newznab"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/releaseparse"
)

const (
	localPayloadMagic = "ETHER-LOCAL/1\n"
	openDeadline      = 3 * time.Second
	movieCategory     = 2000
	tvCategory        = 5000
	maxPluginStreams  = 250
)

// LocalPayload describes a local file stream authorized through a plugin's
// folders: read grant (SPEC §5.1.4). Permission and symlink containment are
// re-verified on every CheckRelease and Open call so revoking a folder grant
// takes effect immediately.
type LocalPayload struct {
	PluginID  string `json:"pluginId"`
	ReleaseID string `json:"releaseId"`
	FilePath  string `json:"filePath"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	Origin    string `json:"origin,omitempty"`
	Remote    bool   `json:"remote,omitempty"`
}

// IsLocalPayload reports whether b is an encoded LocalPayload.
func IsLocalPayload(b []byte) bool {
	return bytes.HasPrefix(b, []byte(localPayloadMagic))
}

// Encode serializes p with the local payload prefix.
func (p LocalPayload) Encode() []byte {
	body, _ := json.Marshal(p)
	return append([]byte(localPayloadMagic), body...)
}

// DecodeLocalPayload parses an encoded LocalPayload.
func DecodeLocalPayload(b []byte) (LocalPayload, error) {
	if !IsLocalPayload(b) {
		return LocalPayload{}, errors.New("plugins: not a local file payload")
	}
	var p LocalPayload
	if err := json.Unmarshal(b[len(localPayloadMagic):], &p); err != nil {
		return LocalPayload{}, fmt.Errorf("plugins: corrupt local payload: %w", err)
	}
	if p.PluginID == "" || p.FilePath == "" {
		return LocalPayload{}, errors.New("plugins: incomplete local payload")
	}
	return p, nil
}

// PluginStream is one stream object returned from GET /stream/{type}/{id}.json
// (SPEC §5.1.4). Standard Stremio fields keep their Stremio meaning; Ether
// additions live under Ether.
type PluginStream struct {
	Name          string            `json:"name,omitempty"`
	Title         string            `json:"title,omitempty"`
	Description   string            `json:"description,omitempty"`
	InfoHash      string            `json:"infoHash,omitempty"`
	FileIdx       *int              `json:"fileIdx,omitempty"`
	URL           string            `json:"url,omitempty"`
	NZBURL        string            `json:"nzbUrl,omitempty"`
	Sources       []string          `json:"sources,omitempty"`
	BehaviorHints *behaviorHints    `json:"behaviorHints,omitempty"`
	Ether         *EtherStreamBlock `json:"ether,omitempty"`
}

type behaviorHints struct {
	Filename     string `json:"filename,omitempty"`
	VideoSize    int64  `json:"videoSize,omitempty"`
	ProxyHeaders struct {
		Request map[string]string `json:"request,omitempty"`
	} `json:"proxyHeaders,omitempty"`
}

// EtherStreamBlock is the optional `ether` block on a stream object (SPEC §5.1.4).
type EtherStreamBlock struct {
	ID          string   `json:"id,omitempty"`
	ReleaseName string   `json:"releaseName,omitempty"`
	Size        int64    `json:"size,omitempty"`
	Languages   []string `json:"languages,omitempty"`
	Lazy        bool     `json:"lazy,omitempty"`
	File        string   `json:"file,omitempty"`
}

type streamResponse struct {
	Streams []PluginStream `json:"streams"`
}

type openResponse struct {
	URL       string            `json:"url,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	ExpiresAt string            `json:"expiresAt,omitempty"`
	File      string            `json:"file,omitempty"`
}

type sourceLimits struct {
	SearchPerHour int `json:"searchPerHour,omitempty"`
	GrabPerHour   int `json:"grabPerHour,omitempty"`
	DailySearches int `json:"dailySearches,omitempty"`
	DailyGrabs    int `json:"dailyGrabs,omitempty"`
	Searches      int `json:"searches,omitempty"`
	Grabs         int `json:"grabs,omitempty"`
}

type rememberedStream struct {
	pluginID   string
	pluginName string
	streamID   string
	lazy       bool
	file       string
	url        string
	headers    map[string]string
	origin     string
}

// SourceStats reports per-plugin stream-source activity for the Sources -> Usage tab.
type SourceStats struct {
	PluginID string `json:"pluginId"`
	Name     string `json:"name"`
	Class    string `json:"class"`
	Searches int64  `json:"searches"`
	Grabs    int64  `json:"grabs"`
	Failures int64  `json:"failures"`
}

type pluginUsage struct {
	searchTimes []time.Time
	grabTimes   []time.Time
	searches    int64
	grabs       int64
	failures    int64
}

// StreamSources adapts enabled `stream_source` plugins into a core.Indexer
// (and core.StreamingIndexer / core.RescueIndexer) alongside an optional
// inner indexer (typically *newznab.Manager).
type StreamSources struct {
	inner       core.Indexer
	usenetReady func() bool

	mu      sync.RWMutex
	mgr     *plugins.Manager
	debrid  *debrid.Service
	order   []string
	streams map[string]rememberedStream
	usage   map[string]*pluginUsage
	now     func() time.Time
}

var (
	_ core.Indexer                = (*StreamSources)(nil)
	_ core.StreamingIndexer       = (*StreamSources)(nil)
	_ core.RescueIndexer          = (*StreamSources)(nil)
	_ core.StreamingRescueIndexer = (*StreamSources)(nil)
)

// NewStreamSources constructs a StreamSources adapter wrapping inner.
func NewStreamSources(inner core.Indexer) *StreamSources {
	return &StreamSources{
		inner:   inner,
		streams: map[string]rememberedStream{},
		usage:   map[string]*pluginUsage{},
		now:     func() time.Time { return time.Now().UTC() },
	}
}

// Bind attaches the plugin manager and debrid service once they are built.
func (s *StreamSources) Bind(mgr *plugins.Manager, deb *debrid.Service) {
	s.mu.Lock()
	s.mgr = mgr
	s.debrid = deb
	s.mu.Unlock()
}

// SetUsenetReady wires the Usenet readiness check so non-plugin Usenet
// releases still require an enabled Usenet provider when plugin sources are active.
func (s *StreamSources) SetUsenetReady(fn func() bool) {
	s.mu.Lock()
	s.usenetReady = fn
	s.mu.Unlock()
}

// SetClock overrides the clock used for rate-limit windows in tests.
func (s *StreamSources) SetClock(now func() time.Time) {
	s.mu.Lock()
	if now != nil {
		s.now = now
	}
	s.mu.Unlock()
}

// SetOrder sets the preferred plugin source order (plugin IDs).
func (s *StreamSources) SetOrder(ids []string) {
	s.mu.Lock()
	s.order = append([]string(nil), ids...)
	s.mu.Unlock()
}

// Order returns the current preferred plugin source order.
func (s *StreamSources) Order() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.order...)
}

// Stats returns per-plugin search and grab counters for enabled stream_source plugins.
func (s *StreamSources) Stats(ctx context.Context) []SourceStats {
	recs := s.enabledPlugins(ctx)
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]SourceStats, 0, len(recs))
	for _, r := range recs {
		var class string
		if r.Manifest.Ether != nil && r.Manifest.Ether.Contributes.Source != nil {
			class = r.Manifest.Ether.Contributes.Source.Class
		}
		st := SourceStats{PluginID: r.ID, Name: r.Name, Class: class}
		if u := s.usage[r.ID]; u != nil {
			st.Searches = u.searches
			st.Grabs = u.grabs
			st.Failures = u.failures
		}
		out = append(out, st)
	}
	return out
}

// HasEnabledPlugins reports whether at least one stream_source plugin is enabled.
func (s *StreamSources) HasEnabledPlugins(ctx context.Context) bool {
	return len(s.enabledPlugins(ctx)) > 0
}

// MissingSources implements the debrid/resolver sourceInventory contract.
func (s *StreamSources) MissingSources() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if s.HasEnabledPlugins(ctx) {
		return nil
	}
	if inv, ok := s.inner.(interface{ MissingSources() error }); ok {
		return inv.MissingSources()
	}
	return nil
}

// HasTorrentIndexer implements the debrid sourceInventory contract.
func (s *StreamSources) HasTorrentIndexer() bool {
	if inv, ok := s.inner.(interface{ HasTorrentIndexer() bool }); ok && inv.HasTorrentIndexer() {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.HasEnabledPlugins(ctx)
}

// ResolveTorrent implements debrid.TorrentResolver.
func (s *StreamSources) ResolveTorrent(ctx context.Context, r core.Release) (core.Release, error) {
	if r.InfoHash != "" {
		return r, nil
	}
	if tr, ok := s.inner.(debrid.TorrentResolver); ok {
		return tr.ResolveTorrent(ctx, r)
	}
	return r, fmt.Errorf("plugins: torrent %q has no info hash: %w", r.Name, core.ErrReleaseRejected)
}

// Name implements core.Indexer.
func (s *StreamSources) Name() string {
	if s.inner != nil {
		return s.inner.Name()
	}
	return "plugins"
}

// Search implements core.Indexer.
func (s *StreamSources) Search(ctx context.Context, req core.SearchRequest) ([]core.Release, error) {
	return s.SearchWith(ctx, req, nil)
}

// SearchWith implements core.StreamingIndexer.
func (s *StreamSources) SearchWith(ctx context.Context, req core.SearchRequest, arrived func([]core.Release)) ([]core.Release, error) {
	var (
		innerRels []core.Release
		innerErr  error
		wg        sync.WaitGroup
	)
	if s.inner != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if st, ok := s.inner.(core.StreamingIndexer); ok && arrived != nil {
				innerRels, innerErr = st.SearchWith(ctx, req, arrived)
			} else {
				innerRels, innerErr = s.inner.Search(ctx, req)
				if arrived != nil && len(innerRels) > 0 {
					arrived(innerRels)
				}
			}
		}()
	}

	pluginRels, pluginErr := s.searchPlugins(ctx, req, arrived)
	wg.Wait()

	if len(innerRels) == 0 && len(pluginRels) == 0 {
		if innerErr != nil && !errors.Is(innerErr, core.ErrNoSources) {
			return nil, innerErr
		}
		if pluginErr != nil {
			return nil, pluginErr
		}
		return nil, innerErr
	}
	out := make([]core.Release, 0, len(pluginRels)+len(innerRels))
	out = append(out, pluginRels...)
	out = append(out, innerRels...)
	return out, nil
}

// SearchRescue implements core.RescueIndexer.
func (s *StreamSources) SearchRescue(ctx context.Context, req core.SearchRequest) ([]core.Release, error) {
	return s.SearchRescueWith(ctx, req, nil)
}

// SearchRescueWith implements core.StreamingRescueIndexer.
func (s *StreamSources) SearchRescueWith(ctx context.Context, req core.SearchRequest, arrived func([]core.Release)) ([]core.Release, error) {
	if s.inner == nil {
		return nil, nil
	}
	if sr, ok := s.inner.(core.StreamingRescueIndexer); ok && arrived != nil {
		return sr.SearchRescueWith(ctx, req, arrived)
	}
	if r, ok := s.inner.(core.RescueIndexer); ok {
		rels, err := r.SearchRescue(ctx, req)
		if arrived != nil && len(rels) > 0 {
			arrived(rels)
		}
		return rels, err
	}
	return nil, nil
}

// FetchNZB implements core.Indexer. Plugin-backed local file and lazy streams
// are resolved here; built-in Usenet releases delegate to the inner indexer.
func (s *StreamSources) FetchNZB(ctx context.Context, r core.Release) ([]byte, error) {
	s.mu.RLock()
	rem, isPlugin := s.streams[r.ID]
	if !isPlugin && r.GUID != "" {
		rem, isPlugin = s.streams[r.GUID]
	}
	usenetReady := s.usenetReady
	s.mu.RUnlock()

	if !isPlugin {
		if r.Protocol == core.ProtocolUsenet && usenetReady != nil && !usenetReady() {
			return nil, fmt.Errorf("usenet release %q: %w", r.Name, core.ErrNotAvailable)
		}
		if s.inner == nil {
			return nil, fmt.Errorf("release %q: %w", r.Name, core.ErrNotAvailable)
		}
		return s.inner.FetchNZB(ctx, r)
	}

	if err := s.spendGrab(ctx, rem.pluginID); err != nil {
		return nil, err
	}

	if rem.lazy {
		return s.openLazyStream(ctx, r, rem)
	}
	if rem.file != "" {
		return s.prepareLocalFile(ctx, r, rem.pluginID, rem.pluginName, rem.file)
	}
	if rem.url != "" {
		return s.prepareDirectURL(ctx, r, rem.url, rem.headers, rem.origin)
	}
	if s.inner != nil {
		return s.inner.FetchNZB(ctx, r)
	}
	return nil, fmt.Errorf("plugin %s stream %q: %w", rem.pluginID, r.Name, core.ErrNotAvailable)
}

func (s *StreamSources) openLazyStream(ctx context.Context, r core.Release, rem rememberedStream) ([]byte, error) {
	s.mu.RLock()
	mgr := s.mgr
	s.mu.RUnlock()
	if mgr == nil {
		return nil, core.ErrSourceUnavailable
	}
	if rem.streamID == "" {
		return nil, fmt.Errorf("plugin %s lazy stream missing id: %w", rem.pluginID, core.ErrSourceUnavailable)
	}

	openCtx, cancel := context.WithTimeout(ctx, openDeadline)
	defer cancel()

	resp, err := mgr.Call(openCtx, rem.pluginID, plugins.Request{
		Method: http.MethodGet,
		Path:   "/ether/open/" + url.PathEscape(rem.streamID),
	})
	if err != nil {
		s.noteFailure(rem.pluginID)
		return nil, fmt.Errorf("plugin %s open %s: %w", rem.pluginID, rem.streamID, core.ErrSourceUnavailable)
	}
	if resp.Status < 200 || resp.Status >= 300 {
		s.noteFailure(rem.pluginID)
		return nil, fmt.Errorf("plugin %s open %s status %d: %w", rem.pluginID, rem.streamID, resp.Status, core.ErrSourceUnavailable)
	}

	var opened openResponse
	if err := json.Unmarshal(resp.Body, &opened); err != nil {
		s.noteFailure(rem.pluginID)
		return nil, fmt.Errorf("plugin %s open %s invalid json: %w", rem.pluginID, rem.streamID, core.ErrSourceUnavailable)
	}
	if opened.File != "" {
		return s.prepareLocalFile(ctx, r, rem.pluginID, rem.pluginName, opened.File)
	}
	if opened.URL != "" {
		headers := opened.Headers
		if len(headers) == 0 {
			headers = rem.headers
		}
		return s.prepareDirectURL(ctx, r, opened.URL, headers, rem.origin)
	}
	s.noteFailure(rem.pluginID)
	return nil, fmt.Errorf("plugin %s open %s returned no url or file: %w", rem.pluginID, rem.streamID, core.ErrSourceUnavailable)
}

func (s *StreamSources) prepareDirectURL(ctx context.Context, r core.Release, rawURL string, headers map[string]string, origin string) ([]byte, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("plugin stream %q invalid url: %w", r.Name, core.ErrSourceUnavailable)
	}
	s.mu.RLock()
	deb := s.debrid
	s.mu.RUnlock()

	httpRel := r
	httpRel.Protocol = core.ProtocolHTTP
	httpRel.StreamURL = u.String()
	httpRel.StreamHeaders = headers
	httpRel.StreamOrigin = origin
	if deb != nil {
		return deb.DirectPayload(ctx, httpRel)
	}
	size := r.Size
	if size <= 0 {
		size = 100 << 20
	}
	p := debrid.Payload{
		Key:       r.ID,
		ReleaseID: r.ID,
		Provider:  "direct",
		Kind:      debrid.KindHTTP,
		Path:      r.Name,
		Size:      size,
		Link:      u.String(),
		URL:       u.String(),
		Headers:   headers,
		Origin:    origin,
		Release:   r.Name,
	}
	return p.Encode(), nil
}

func (s *StreamSources) prepareLocalFile(ctx context.Context, r core.Release, pluginID, pluginName, rawPath string) ([]byte, error) {
	if s.isRemote(ctx, pluginID) {
		full, _, err := s.validateRemoteFile(ctx, pluginID, rawPath)
		if err != nil { return nil, err }
		size, err := s.remoteFileSize(ctx, pluginID, full)
		if err != nil { return nil, err }
		origin := pluginName; if origin == "" { origin = pluginID }
		return (LocalPayload{PluginID:pluginID, ReleaseID:r.ID, FilePath:full, Name:path.Base(full), Size:size, Origin:origin, Remote:true}).Encode(), nil
	}
	resolved, info, err := s.validateLocalFile(ctx, pluginID, rawPath)
	if err != nil {
		s.noteFailure(pluginID)
		return nil, err
	}
	origin := pluginName
	if origin == "" {
		origin = pluginID
	}
	p := LocalPayload{
		PluginID:  pluginID,
		ReleaseID: r.ID,
		FilePath:  resolved,
		Name:      filepath.Base(resolved),
		Size:      info.Size(),
		Origin:    origin,
	}
	return p.Encode(), nil
}

func (s *StreamSources) isRemote(ctx context.Context, id string) bool {
	s.mu.RLock(); mgr := s.mgr; s.mu.RUnlock()
	if mgr == nil { return false }
	rec, err := mgr.Get(ctx, id)
	return err == nil && rec.Manifest.Ether != nil && rec.Manifest.Ether.Runtime.Kind == "remote"
}

// validateRemoteFile checks the sidecar's POSIX path lexically against an
// owner-granted root. The sidecar must also enforce containment when opening
// the file, including symlinks; Ether cannot inspect its filesystem.
func (s *StreamSources) validateRemoteFile(ctx context.Context, id, raw string) (string, string, error) {
	s.mu.RLock(); mgr := s.mgr; s.mu.RUnlock()
	if mgr == nil { return "", "", plugins.ErrUnavailable }
	rec, err := mgr.Get(ctx, id); if err != nil { return "", "", err }
	if !rec.Enabled { return "", "", plugins.ErrDisabled }
	if rec.Manifest.Ether == nil || rec.Manifest.Ether.Runtime.Kind != "remote" || rec.Manifest.Ether.Permissions.Folders != "read" { return "", "", plugins.ErrPermissionDenied }
	g, err := mgr.GrantsOf(ctx, id); if err != nil { return "", "", err }
	for _, revoked := range g.Revoked { if revoked == "folders" { return "", "", plugins.ErrPermissionDenied } }
	if raw == "" || strings.ContainsAny(raw, "\\\x00") { return "", "", plugins.ErrPermissionDenied }
	for _, seg := range strings.Split(raw, "/") { if seg == ".." || seg == "." || seg == "" && raw != "/" && !strings.HasPrefix(raw, "/") { return "", "", plugins.ErrPermissionDenied } }
	var full, rel string; matches := 0
	for _, root := range g.Folders {
		if !path.IsAbs(root) || strings.Contains(root, "\\") { continue }
		root = path.Clean(root)
		candidate := raw; if !path.IsAbs(candidate) { candidate = path.Join(root, candidate) }; candidate = path.Clean(candidate)
		prefix := strings.TrimSuffix(root, "/") + "/"
		if strings.HasPrefix(candidate, prefix) { full = candidate; rel = strings.TrimPrefix(candidate, prefix); matches++ }
	}
	if matches != 1 || rel == "" { return "", "", plugins.ErrPermissionDenied }
	return full, rel, nil
}

func headerValue(h map[string]string, name string) string { for k,v := range h { if strings.EqualFold(k,name) { return v } }; return "" }

func (s *StreamSources) remoteRange(ctx context.Context, id, file string, start, end int64) (plugins.Response, io.ReadCloser, error) {
	_, rel, err := s.validateRemoteFile(ctx, id, file); if err != nil { return plugins.Response{}, nil, err }
	s.mu.RLock(); mgr := s.mgr; s.mu.RUnlock()
	return mgr.CallStream(ctx,id,plugins.Request{Method:http.MethodGet,Path:"/ether/file/"+rel,Headers:map[string]string{"Range":fmt.Sprintf("bytes=%d-%d",start,end)}})
}

func parseRemoteRange(h string) (start, end, total int64, err error) {
	if _, err = fmt.Sscanf(h, "bytes %d-%d/%d", &start, &end, &total); err != nil || start < 0 || end < start || total <= end || h != fmt.Sprintf("bytes %d-%d/%d",start,end,total) { return 0,0,0,errors.New("plugins: invalid remote content range") }
	return
}
func (s *StreamSources) remoteFileSize(ctx context.Context,id,file string) (int64,error) {
	ctx,cancel := context.WithTimeout(ctx,30*time.Second); defer cancel()
	resp,body,err := s.remoteRange(ctx,id,file,0,0); if err != nil { return 0,err }; defer body.Close()
	start,end,total,err := parseRemoteRange(headerValue(resp.Headers,"Content-Range"))
	if err != nil || resp.Status != http.StatusPartialContent || start != 0 || end != 0 { return 0,errors.New("plugins: remote file does not support byte ranges") }
	var b [2]byte; n,err := io.ReadFull(body,b[:1]); if n!=1 || err!=nil { return 0,io.ErrUnexpectedEOF }
	return total,nil
}

// validateLocalFile verifies that pluginID is enabled, declares folders: read,
// has a non-revoked folders grant, and that rawPath resolves (after
// filepath.EvalSymlinks on both root and target) to a regular file inside one
// of the owner-granted folder roots.
func (s *StreamSources) validateLocalFile(ctx context.Context, pluginID, rawPath string) (string, os.FileInfo, error) {
	s.mu.RLock()
	mgr := s.mgr
	s.mu.RUnlock()
	if mgr == nil {
		return "", nil, plugins.ErrUnavailable
	}
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return "", nil, fmt.Errorf("%w: empty file path", plugins.ErrPermissionDenied)
	}
	for _, seg := range strings.FieldsFunc(rawPath, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return "", nil, fmt.Errorf("%w: path traversal is not allowed", plugins.ErrPermissionDenied)
		}
	}

	rec, err := mgr.Get(ctx, pluginID)
	if err != nil {
		return "", nil, err
	}
	if !rec.Enabled {
		return "", nil, plugins.ErrDisabled
	}
	if rec.Manifest.Ether == nil || rec.Manifest.Ether.Permissions.Folders != "read" {
		return "", nil, fmt.Errorf("%w: plugin does not declare folders: read permission", plugins.ErrPermissionDenied)
	}
	grants, err := mgr.GrantsOf(ctx, pluginID)
	if err != nil {
		return "", nil, err
	}
	for _, rev := range grants.Revoked {
		if rev == "folders" {
			return "", nil, fmt.Errorf("%w: folders permission is revoked", plugins.ErrPermissionDenied)
		}
	}
	if len(grants.Folders) == 0 {
		return "", nil, fmt.Errorf("%w: no folders granted to plugin", plugins.ErrPermissionDenied)
	}

	for _, root := range grants.Folders {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		evalRoot, err := filepath.EvalSymlinks(filepath.Clean(root))
		if err != nil {
			continue
		}
		var candidate string
		if filepath.IsAbs(rawPath) {
			candidate = filepath.Clean(rawPath)
		} else {
			candidate = filepath.Join(evalRoot, rawPath)
		}
		evalTarget, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		prefix := evalRoot
		if !strings.HasSuffix(prefix, string(filepath.Separator)) {
			prefix += string(filepath.Separator)
		}
		if !strings.HasPrefix(evalTarget, prefix) {
			continue
		}
		fi, err := os.Stat(evalTarget)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		return evalTarget, fi, nil
	}
	return "", nil, fmt.Errorf("%w: file is outside the folders granted to %s", plugins.ErrPermissionDenied, pluginID)
}

func (s *StreamSources) enabledPlugins(ctx context.Context) []plugins.Record {
	s.mu.RLock()
	mgr := s.mgr
	order := append([]string(nil), s.order...)
	s.mu.RUnlock()
	if mgr == nil {
		return nil
	}
	recs, err := mgr.List(ctx)
	if err != nil {
		return nil
	}
	var out []plugins.Record
	for _, r := range recs {
		if !r.Enabled || r.Manifest.Ether == nil {
			continue
		}
		if r.Manifest.Ether.Kind != plugins.KindSource || !r.Manifest.HasCapability(plugins.CapStreamSource) {
			continue
		}
		out = append(out, r)
	}
	if len(order) > 0 {
		pos := make(map[string]int, len(order))
		for i, id := range order {
			pos[id] = i
		}
		sort.SliceStable(out, func(i, j int) bool {
			pi, oki := pos[out[i].ID]
			pj, okj := pos[out[j].ID]
			switch {
			case oki && okj:
				return pi < pj
			case oki:
				return true
			case okj:
				return false
			default:
				return false
			}
		})
	}
	return out
}

func (s *StreamSources) searchPlugins(ctx context.Context, req core.SearchRequest, arrived func([]core.Release)) ([]core.Release, error) {
	switch req.Type {
	case core.TypeMovie:
	case core.TypeEpisode, core.TypeSeason:
		if req.Season <= 0 {
			return nil, nil
		}
	default:
		return nil, nil
	}

	recs := s.enabledPlugins(ctx)
	if len(recs) == 0 {
		return nil, nil
	}

	s.mu.RLock()
	mgr := s.mgr
	s.mu.RUnlock()
	if mgr == nil {
		return nil, nil
	}

	type pluginResult struct {
		rels []core.Release
		err  error
	}
	results := make([]pluginResult, len(recs))
	var wg sync.WaitGroup
	for i, rec := range recs {
		i, rec := i, rec
		wg.Add(1)
		go func() {
			defer wg.Done()
			rels, err := s.searchOnePlugin(ctx, mgr, rec, req)
			results[i] = pluginResult{rels: rels, err: err}
			if arrived != nil && len(rels) > 0 {
				arrived(rels)
			}
		}()
	}
	wg.Wait()

	var (
		all      []core.Release
		firstErr error
	)
	for _, res := range results {
		if len(res.rels) > 0 {
			all = append(all, res.rels...)
		} else if res.err != nil && firstErr == nil {
			firstErr = res.err
		}
	}
	return all, firstErr
}

func (s *StreamSources) searchOnePlugin(ctx context.Context, mgr *plugins.Manager, rec plugins.Record, req core.SearchRequest) ([]core.Release, error) {
	kind, lookupID, ok := pluginLookupID(rec.Manifest, req)
	if !ok {
		return nil, nil
	}
	if err := s.spendSearch(ctx, rec); err != nil {
		return nil, err
	}
	query := map[string]string{}
	if req.Title != "" {
		query["title"] = req.Title
	}
	if req.Year > 0 {
		query["year"] = strconv.Itoa(req.Year)
	}
	if req.Season > 0 {
		query["season"] = strconv.Itoa(req.Season)
	}
	if req.Episode > 0 {
		query["episode"] = strconv.Itoa(req.Episode)
	}
	if req.IDs.IMDB != "" {
		query["imdbId"] = req.IDs.IMDB
	}
	if req.IDs.TMDB > 0 {
		query["tmdbId"] = strconv.FormatInt(req.IDs.TMDB, 10)
	}

	path := "/stream/" + kind + "/" + url.PathEscape(lookupID) + ".json"
	resp, err := mgr.Call(ctx, rec.ID, plugins.Request{
		Method: http.MethodGet,
		Path:   path,
		Query:  query,
	})
	if err != nil {
		s.noteFailure(rec.ID)
		return nil, err
	}
	if resp.Status < 200 || resp.Status >= 300 {
		s.noteFailure(rec.ID)
		return nil, fmt.Errorf("plugin %s answered HTTP %d", rec.ID, resp.Status)
	}
	var payload streamResponse
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		s.noteFailure(rec.ID)
		return nil, fmt.Errorf("plugin %s returned invalid stream JSON: %w", rec.ID, err)
	}
	return s.convertPluginStreams(ctx, rec, req, payload.Streams), nil
}

func pluginLookupID(man plugins.Manifest, req core.SearchRequest) (kind, id string, ok bool) {
	kind = "movie"
	if req.Type != core.TypeMovie {
		kind = "series"
	}
	if len(man.Types) > 0 {
		found := false
		for _, t := range man.Types {
			if strings.EqualFold(t, kind) {
				found = true
				break
			}
		}
		if !found {
			return "", "", false
		}
	}

	accepts := func(prefix string) bool {
		if len(man.IDPrefixes) == 0 {
			return true
		}
		for _, p := range man.IDPrefixes {
			p = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(p)), ":")
			if p == prefix {
				return true
			}
		}
		return false
	}

	baseID := ""
	switch {
	case req.IDs.IMDB != "" && accepts("tt"):
		baseID = normalizeIMDb(req.IDs.IMDB)
	case req.IDs.TMDB > 0 && accepts("tmdb"):
		baseID = fmt.Sprintf("tmdb:%d", req.IDs.TMDB)
	case req.IDs.TVDB > 0 && accepts("tvdb"):
		baseID = fmt.Sprintf("tvdb:%d", req.IDs.TVDB)
	case req.IDs.IMDB != "":
		baseID = normalizeIMDb(req.IDs.IMDB)
	case req.IDs.TMDB > 0:
		baseID = fmt.Sprintf("tmdb:%d", req.IDs.TMDB)
	case req.IDs.TVDB > 0:
		baseID = fmt.Sprintf("tvdb:%d", req.IDs.TVDB)
	case req.Title != "":
		baseID = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(req.Title), " ", "-"))
	}
	if baseID == "" {
		return "", "", false
	}
	if kind == "series" {
		ep := req.Episode
		if ep <= 0 {
			ep = 1
		}
		return kind, fmt.Sprintf("%s:%d:%d", baseID, req.Season, ep), true
	}
	return kind, baseID, true
}

func normalizeIMDb(id string) string {
	id = strings.TrimSpace(strings.ToLower(id))
	if id == "" {
		return ""
	}
	if !strings.HasPrefix(id, "tt") {
		id = "tt" + id
	}
	return id
}

func (s *StreamSources) convertPluginStreams(ctx context.Context, rec plugins.Record, req core.SearchRequest, streams []PluginStream) []core.Release {
	if len(streams) == 0 {
		return nil
	}
	origin := pluginOrigin(rec.SourceURL)
	indexerName := rec.Name
	if indexerName == "" {
		indexerName = rec.ID
	}
	cat := movieCategory
	if req.Type != core.TypeMovie {
		cat = tvCategory
	}

	out := make([]core.Release, 0, min(len(streams), maxPluginStreams))
	seen := make(map[string]bool, len(streams))

	for idx := range streams {
		if len(out) >= maxPluginStreams {
			break
		}
		st := &streams[idx]
		rel, rem, ok := s.convertOneStream(ctx, rec, indexerName, origin, cat, req, idx, st)
		if !ok || seen[rel.ID] {
			continue
		}
		seen[rel.ID] = true
		if rem.pluginID != "" {
			s.mu.Lock()
			s.streams[rel.ID] = rem
			if rel.GUID != "" {
				s.streams[rel.GUID] = rem
			}
			s.mu.Unlock()
		}
		out = append(out, rel)
	}
	return out
}

func (s *StreamSources) convertOneStream(
	ctx context.Context,
	rec plugins.Record,
	indexerName, origin string,
	cat int,
	req core.SearchRequest,
	idx int,
	st *PluginStream,
) (core.Release, rememberedStream, bool) {
	var (
		etherID     string
		releaseName string
		size        int64
		langs       []string
		lazy        bool
		localFile   string
	)
	if st.Ether != nil {
		etherID = strings.TrimSpace(st.Ether.ID)
		releaseName = strings.TrimSpace(st.Ether.ReleaseName)
		size = st.Ether.Size
		langs = st.Ether.Languages
		lazy = st.Ether.Lazy
		localFile = strings.TrimSpace(st.Ether.File)
	}
	var headers map[string]string
	if st.BehaviorHints != nil {
		if releaseName == "" && st.BehaviorHints.Filename != "" {
			releaseName = strings.TrimSpace(st.BehaviorHints.Filename)
		}
		if size <= 0 && st.BehaviorHints.VideoSize > 0 {
			size = st.BehaviorHints.VideoSize
		}
		if len(st.BehaviorHints.ProxyHeaders.Request) > 0 {
			headers = st.BehaviorHints.ProxyHeaders.Request
		}
	}
	if releaseName == "" {
		for _, candidate := range []string{st.Description, st.Title, st.Name} {
			if line := firstReleaseLine(candidate); line != "" {
				releaseName = line
				break
			}
		}
	}
	if releaseName == "" && localFile != "" {
		releaseName = filepath.Base(localFile)
	}
	if releaseName == "" {
		return core.Release{}, rememberedStream{}, false
	}

	// Parse release facts through Ether's own parser so plugins never rank
	// their own results (SPEC §5.1.4).
	info := releaseparse.Parse(releaseName)
	if len(langs) > 0 {
		info.Languages = mergeLanguages(info.Languages, langs)
	}

	streamID := etherID
	if streamID == "" {
		streamID = strconv.Itoa(idx)
	}

	rel := core.Release{
		Indexer:  indexerName,
		Name:     releaseName,
		Size:     size,
		Category: cat,
		Info:     info,
		TitleIDs: req.IDs,
	}

	switch {
	case localFile != "":
		resolved, _, err := s.validateLocalFile(ctx, rec.ID, localFile)
		if err != nil {
			return core.Release{}, rememberedStream{}, false
		}
		// Ensure a local file in an owner-granted folder satisfies identity
		// and ranks at the top of its title's releases even when the file on
		// disk is named plainly (e.g. "Movie (2024).mkv").
		if rel.Info.Title == "" || (!releaseparse.TitleMatches(rel.Info, req.Title) && req.Title != "") {
			rel.Info.Title = req.Title
		}
		if rel.Info.Year == 0 && req.Year > 0 {
			rel.Info.Year = req.Year
		}
		if req.Type == core.TypeEpisode {
			if len(rel.Info.Seasons) == 0 && req.Season > 0 {
				rel.Info.Seasons = []int{req.Season}
			}
			if len(rel.Info.Episodes) == 0 && req.Episode > 0 {
				rel.Info.Episodes = []int{req.Episode}
			}
		}
		if rel.Info.Height == 0 {
			rel.Info.Height = 2160
		}
		if rel.Info.Source == core.SourceUnknown {
			rel.Info.Source = core.SourceRemux
		}
		rel.PubDate = s.now()
		rel.Grabs = 1_000_000
		rel.Protocol = "direct"
		rel.GUID = fmt.Sprintf("plugin:%s:file:%s", rec.ID, resolved)
		rel.ID = releaseIDFor(rec.ID, rel.GUID)
		return rel, rememberedStream{
			pluginID:   rec.ID,
			pluginName: indexerName,
			streamID:   streamID,
			file:       localFile,
			origin:     origin,
		}, true

	case lazy:
		if etherID == "" {
			return core.Release{}, rememberedStream{}, false
		}
		rel.Protocol = "direct"
		rel.GUID = fmt.Sprintf("plugin:%s:lazy:%s", rec.ID, etherID)
		rel.ID = releaseIDFor(rec.ID, rel.GUID)
		return rel, rememberedStream{
			pluginID:   rec.ID,
			pluginName: indexerName,
			streamID:   etherID,
			lazy:       true,
			headers:    headers,
			origin:     origin,
		}, true

	case st.InfoHash != "":
		hash := newznab.NormalizeInfoHash(st.InfoHash)
		if hash == "" {
			return core.Release{}, rememberedStream{}, false
		}
		suffix := ""
		if st.FileIdx != nil && *st.FileIdx >= 0 {
			suffix = "/" + strconv.Itoa(*st.FileIdx)
		} else if req.Type != core.TypeMovie && req.Season > 0 && req.Episode > 0 {
			suffix = fmt.Sprintf("/s%de%d", req.Season, req.Episode)
		}
		rel.Protocol = core.ProtocolTorrent
		rel.InfoHash = hash
		rel.MagnetURL = newznab.MagnetFor(hash, releaseName, extractTrackers(st.Sources))
		rel.GUID = "btih:" + hash + suffix
		rel.ID = releaseIDFor(rec.ID, rel.GUID)
		if u := validHTTPURL(st.URL); u != "" {
			rel.StreamURL = u
			rel.StreamHeaders = headers
			rel.StreamOrigin = origin
		}
		return rel, rememberedStream{}, true

	case validHTTPURL(st.NZBURL) != "":
		nzbURL := validHTTPURL(st.NZBURL)
		rel.Protocol = core.ProtocolUsenet
		rel.NZBURL = nzbURL
		rel.GUID = fmt.Sprintf("plugin:%s:nzb:%s", rec.ID, streamID)
		rel.ID = releaseIDFor(rec.ID, rel.GUID)
		return rel, rememberedStream{}, true

	case validHTTPURL(st.URL) != "":
		streamURL := validHTTPURL(st.URL)
		rel.Protocol = core.ProtocolHTTP
		rel.StreamURL = streamURL
		rel.StreamHeaders = headers
		rel.StreamOrigin = origin
		rel.GUID = fmt.Sprintf("plugin:%s:http:%s", rec.ID, streamID)
		rel.ID = releaseIDFor(rec.ID, rel.GUID)
		return rel, rememberedStream{
			pluginID:   rec.ID,
			pluginName: indexerName,
			streamID:   streamID,
			url:        streamURL,
			headers:    headers,
			origin:     origin,
		}, true
	}

	return core.Release{}, rememberedStream{}, false
}

func (s *StreamSources) spendSearch(ctx context.Context, rec plugins.Record) error {
	if err := core.SpendBackground(ctx, "search", rec.ID, 1); err != nil {
		return err
	}
	lim := parseLimits(rec.Manifest)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	u := s.usageForLocked(rec.ID)
	u.searchTimes = pruneTimes(u.searchTimes, now.Add(-24*time.Hour))
	hourly := countSince(u.searchTimes, now.Add(-time.Hour))
	daily := len(u.searchTimes)
	if (lim.SearchPerHour > 0 && hourly >= lim.SearchPerHour) ||
		(lim.Searches > 0 && hourly >= lim.Searches) ||
		(lim.DailySearches > 0 && daily >= lim.DailySearches) {
		return core.ErrBudgetExhausted
	}
	u.searchTimes = append(u.searchTimes, now)
	u.searches++
	return nil
}

func (s *StreamSources) spendGrab(ctx context.Context, pluginID string) error {
	if pluginID == "" {
		return nil
	}
	if err := core.SpendBackground(ctx, "grab", pluginID, 1); err != nil {
		return err
	}
	s.mu.RLock()
	mgr := s.mgr
	s.mu.RUnlock()
	var lim sourceLimits
	if mgr != nil {
		if rec, err := mgr.Get(ctx, pluginID); err == nil {
			lim = parseLimits(rec.Manifest)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	u := s.usageForLocked(pluginID)
	u.grabTimes = pruneTimes(u.grabTimes, now.Add(-24*time.Hour))
	hourly := countSince(u.grabTimes, now.Add(-time.Hour))
	daily := len(u.grabTimes)
	if (lim.GrabPerHour > 0 && hourly >= lim.GrabPerHour) ||
		(lim.Grabs > 0 && hourly >= lim.Grabs) ||
		(lim.DailyGrabs > 0 && daily >= lim.DailyGrabs) {
		return core.ErrBudgetExhausted
	}
	u.grabTimes = append(u.grabTimes, now)
	u.grabs++
	return nil
}

func (s *StreamSources) noteFailure(pluginID string) {
	if pluginID == "" {
		return
	}
	s.mu.Lock()
	u := s.usageForLocked(pluginID)
	u.failures++
	s.mu.Unlock()
}

func (s *StreamSources) usageForLocked(pluginID string) *pluginUsage {
	u := s.usage[pluginID]
	if u == nil {
		u = &pluginUsage{}
		s.usage[pluginID] = u
	}
	return u
}

func parseLimits(man plugins.Manifest) sourceLimits {
	if man.Ether == nil || man.Ether.Contributes.Source == nil || len(man.Ether.Contributes.Source.Limits) == 0 {
		return sourceLimits{}
	}
	var lim sourceLimits
	_ = json.Unmarshal(man.Ether.Contributes.Source.Limits, &lim)
	return lim
}

func pruneTimes(ts []time.Time, cutoff time.Time) []time.Time {
	kept := ts[:0]
	for _, t := range ts {
		if !t.Before(cutoff) {
			kept = append(kept, t)
		}
	}
	return kept
}

func countSince(ts []time.Time, cutoff time.Time) int {
	n := 0
	for _, t := range ts {
		if !t.Before(cutoff) {
			n++
		}
	}
	return n
}

func releaseIDFor(pluginID, guid string) string {
	sum := sha1.Sum([]byte(pluginID + "|" + guid))
	return hex.EncodeToString(sum[:])
}

func pluginOrigin(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func validHTTPURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return u.String()
}

func firstReleaseLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func mergeLanguages(existing, extra []string) []string {
	seen := make(map[string]bool, len(existing)+len(extra))
	var out []string
	for _, list := range [][]string{existing, extra} {
		for _, l := range list {
			l = strings.TrimSpace(l)
			if l == "" {
				continue
			}
			key := strings.ToLower(l)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, l)
		}
	}
	return out
}

func extractTrackers(sources []string) []string {
	var out []string
	for _, s := range sources {
		if tr, ok := strings.CutPrefix(strings.TrimSpace(s), "tracker:"); ok && tr != "" {
			out = append(out, tr)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Engine & HealthChecker wrapper for local file streams (core.LocalFileSource)

// Engine wraps an inner core.Engine (and core.ReleaseHealthChecker) so
// ETHER-LOCAL/1 payloads open granted local files directly via os.Open and
// satisfy core.LocalFileSource.
type Engine struct {
	sources *StreamSources
	inner   core.Engine
	innerHC core.HealthChecker
}

var (
	_ core.Engine               = (*Engine)(nil)
	_ core.ReleaseHealthChecker = (*Engine)(nil)
)

// WrapEngine wraps inner with local-file payload support backed by s.
func (s *StreamSources) WrapEngine(inner core.Engine) *Engine {
	var hc core.HealthChecker
	if h, ok := inner.(core.HealthChecker); ok {
		hc = h
	}
	return &Engine{sources: s, inner: inner, innerHC: hc}
}

// Check implements core.HealthChecker.
func (e *Engine) Check(ctx context.Context, payload []byte) (bool, error) {
	return e.CheckRelease(ctx, "", payload)
}

// CheckRelease implements core.ReleaseHealthChecker.
func (e *Engine) CheckRelease(ctx context.Context, releaseID string, payload []byte) (bool, error) {
	if IsLocalPayload(payload) {
		lp, err := DecodeLocalPayload(payload)
		if err != nil {
			return false, err
		}
		if e.sources == nil {
			return false, plugins.ErrUnavailable
		}
		_, fi, err := e.sources.validateLocalFile(ctx, lp.PluginID, lp.FilePath)
		if err != nil {
			return false, err
		}
		return fi.Size() > 0, nil
	}
	if e.innerHC == nil {
		return true, nil
	}
	return core.CheckReleaseHealth(ctx, e.innerHC, releaseID, payload)
}

// Open implements core.Engine.
func (e *Engine) Open(ctx context.Context, releaseID string, payload []byte) (core.MediaSource, error) {
	if IsLocalPayload(payload) {
		lp, err := DecodeLocalPayload(payload)
		if err != nil {
			return nil, err
		}
		if e.sources == nil {
			return nil, plugins.ErrUnavailable
		}
		resolved, fi, err := e.sources.validateLocalFile(ctx, lp.PluginID, lp.FilePath)
		if err != nil {
			return nil, err
		}
		f, err := os.Open(resolved)
		if err != nil {
			return nil, err
		}
		relID := releaseID
		if relID == "" {
			relID = lp.ReleaseID
		}
		name := lp.Name
		if name == "" {
			name = filepath.Base(resolved)
		}
		return &localFileSource{
			file:      f,
			size:      fi.Size(),
			name:      name,
			releaseID: relID,
			origin:    lp.Origin,
		}, nil
	}
	if e.inner == nil {
		return nil, errors.New("plugins: no inner stream engine")
	}
	return e.inner.Open(ctx, releaseID, payload)
}

// Prefetch implements core.Engine.
func (e *Engine) Prefetch(ctx context.Context, releaseID string, payload []byte, offset, length int64) error {
	if IsLocalPayload(payload) {
		return nil
	}
	if e.inner == nil {
		return nil
	}
	return e.inner.Prefetch(ctx, releaseID, payload, offset, length)
}

type localFileSource struct {
	file      *os.File
	size      int64
	name      string
	releaseID string
	origin    string
}

var (
	_ core.MediaSource     = (*localFileSource)(nil)
	_ core.LocalFileSource = (*localFileSource)(nil)
	_ core.OriginSource    = (*localFileSource)(nil)
)

func (l *localFileSource) ReadAt(p []byte, off int64) (int, error) {
	return l.file.ReadAt(p, off)
}

func (l *localFileSource) Size() int64         { return l.size }
func (l *localFileSource) Name() string        { return l.name }
func (l *localFileSource) ReleaseID() string   { return l.releaseID }
func (l *localFileSource) Close() error        { return l.file.Close() }
func (l *localFileSource) LocalFile() *os.File { return l.file }
func (l *localFileSource) Origin() string      { return l.origin }
