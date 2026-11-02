// Package adapters bridges runtime plugin contributions (WS9a list sources
// and WS9c Live TV channel sources) into Ether's collection, personalization
// and Live TV registries.
package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/braedonsaunders/ether/internal/collections"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/livetv"
	"github.com/braedonsaunders/ether/internal/personalize"
	"github.com/braedonsaunders/ether/internal/plugins"
)

const MaxPluginListItems = 2000

var yearPrefixRe = regexp.MustCompile(`^(\d{4})`)

// Bridge tracks the source kinds registered for each plugin so enable,
// disable, update and uninstall cleanly replace or remove them.
type Bridge struct {
	mgr  *plugins.Manager
	cols *collections.Service
	pers *personalize.Engine
	tv   *livetv.Service

	mu       sync.Mutex
	listIDs  map[string][]string
	livetvID map[string]string
}

// Attach wires plugin list sources (collections + Home rows) and Live TV
// channel sources to the plugin manager's lifecycle.
func Attach(mgr *plugins.Manager, cols *collections.Service, pers *personalize.Engine, tv *livetv.Service) *Bridge {
	b := &Bridge{
		mgr:      mgr,
		cols:     cols,
		pers:     pers,
		tv:       tv,
		listIDs:  map[string][]string{},
		livetvID: map[string]string{},
	}
	if tv != nil && mgr != nil {
		tv.SetStreamOpener(b.openPluginStream)
	}
	if mgr != nil {
		mgr.OnSync(b.Sync)
	}
	return b
}

// Sync updates all adapter registrations for one plugin record.
func (b *Bridge) Sync(_ context.Context, r plugins.Record) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, id := range b.listIDs[r.ID] {
		if b.cols != nil {
			b.cols.UnregisterSourceKind(id)
		}
		if b.pers != nil {
			b.pers.UnregisterRowSourceKind(id)
		}
	}
	delete(b.listIDs, r.ID)

	if id, ok := b.livetvID[r.ID]; ok {
		if b.tv != nil {
			b.tv.UnregisterSourceKind(id)
		}
		delete(b.livetvID, r.ID)
	}

	if !r.Enabled || r.Manifest.Ether == nil {
		return
	}

	c := r.Manifest.Ether.Contributes
	for _, ls := range c.ListSources {
		if !r.Manifest.HasCapability(plugins.CapListSource) || ls.Key == "" {
			continue
		}
		kind := &listSourceKind{
			mgr:        b.mgr,
			pluginID:   r.ID,
			pluginName: r.Name,
			meta:       ls,
			fields:     plugins.ParseListParamFields(ls.Params),
		}
		if b.cols != nil {
			_ = b.cols.RegisterSourceKind(kind)
		}
		if b.pers != nil {
			_ = b.pers.RegisterRowSourceKind(&rowSourceKind{list: kind})
		}
		b.listIDs[r.ID] = append(b.listIDs[r.ID], kind.ID())
	}

	if r.Manifest.HasCapability(plugins.CapLiveTV) && c.Channels != nil {
		chKind := &channelSourceKind{
			mgr:        b.mgr,
			pluginID:   r.ID,
			pluginName: r.Name,
			meta:       *c.Channels,
		}
		if b.tv != nil {
			_ = b.tv.RegisterSourceKind(chKind)
		}
		b.livetvID[r.ID] = chKind.ID()
	}
}

func pluginText(pluginID, raw string) extend.Text {
	if strings.HasPrefix(raw, "$t:") {
		return extend.PluginText(pluginID, strings.TrimPrefix(raw, "$t:"))
	}
	return extend.Text{Resolved: raw}
}

func resolvePlainText(mgr *plugins.Manager, pluginID, raw, fallback string) string {
	if raw == "" {
		return fallback
	}
	if strings.HasPrefix(raw, "$t:") {
		key := strings.TrimPrefix(raw, "$t:")
		if mgr != nil {
			if s, ok := mgr.ResolveText(context.Background(), pluginID, key, "en"); ok && s != "" {
				return s
			}
		}
		return fallback
	}
	return raw
}

type listSourceKind struct {
	mgr        *plugins.Manager
	pluginID   string
	pluginName string
	meta       plugins.ListSourceMeta
	fields     []plugins.ListParamField
}

func (k *listSourceKind) MinRefresh() time.Duration {
	d, err := time.ParseDuration(k.meta.Refresh)
	if err != nil || d < time.Hour {
		return time.Hour
	}
	return d
}

func (k *listSourceKind) ID() string {
	return extend.PluginID(k.pluginID, k.meta.Key)
}

func (k *listSourceKind) label() string {
	return resolvePlainText(k.mgr, k.pluginID, k.meta.Label, firstNonEmpty(k.pluginName, k.meta.Key))
}

func (k *listSourceKind) mediaTypes() []string {
	catType, _ := splitCatalogSpec(k.meta.Catalog)
	switch catType {
	case "movie":
		return []string{"movie"}
	case "series", "show", "tv":
		return []string{"show"}
	default:
		return []string{"movie", "show"}
	}
}

func (k *listSourceKind) Describe() collections.KindDescribe {
	params, defaults := plugins.ParamFieldsFromList(k.pluginID, k.fields)
	if defaults != "{}" {
		defaults = `{"params":` + defaults + `}`
	}
	return collections.KindDescribe{
		Label:      pluginText(k.pluginID, firstNonEmpty(k.meta.Label, k.meta.Key)),
		Hint:       pluginText(k.pluginID, k.meta.Hint),
		Icon:       "list-video",
		Order:      500,
		Params:     params,
		Defaults:   defaults,
		MediaTypes: k.mediaTypes(),
	}
}

func (k *listSourceKind) Validate(ve *collections.ValidationError, path string, src core.CollectionSource) {
	for _, f := range k.fields {
		if !f.Required {
			continue
		}
		key := firstNonEmpty(f.Key, f.Name)
		if key == "" {
			continue
		}
		val := strings.TrimSpace(src.Params[key])
		if val == "" {
			ve.Add(path+".params."+key, key+" is required")
		}
	}
}

func (k *listSourceKind) Gather(g *collections.Gather) {
	if k.mgr == nil {
		g.Warn("%s is unavailable", k.label())
		return
	}
	maxItems := k.meta.MaxItems
	if maxItems <= 0 || maxItems > MaxPluginListItems {
		maxItems = MaxPluginListItems
	}
	if collections.IsPreview(g.Ctx) && maxItems > collections.PreviewItems {
		maxItems = collections.PreviewItems
	}
	targets := k.catalogTargets(g)
	var refs []collections.ExternalRef
	for _, target := range targets {
		if len(refs) >= maxItems {
			break
		}
		rem := maxItems - len(refs)
		tRefs, ok := k.fetchCatalogPages(g, target.catType, target.catID, rem)
		if !ok && len(refs) == 0 && len(tRefs) == 0 {
			return
		}
		refs = append(refs, tRefs...)
	}
	if len(refs) > maxItems {
		refs = refs[:maxItems]
	}
	if len(refs) == 0 {
		g.Warn("%s is empty", k.label())
		return
	}
	g.ResolveExternal(k.label(), refs)
}

type catalogTarget struct {
	catType string
	catID   string
}

func splitCatalogSpec(spec string) (string, string) {
	spec = strings.Trim(strings.TrimSpace(spec), "/")
	spec = strings.TrimPrefix(spec, "catalog/")
	spec = strings.TrimSuffix(spec, ".json")
	if i := strings.IndexByte(spec, '/'); i >= 0 {
		return strings.ToLower(strings.TrimSpace(spec[:i])), strings.TrimSpace(spec[i+1:])
	}
	return "", spec
}

func (k *listSourceKind) catalogTargets(g *collections.Gather) []catalogTarget {
	catType, catID := splitCatalogSpec(k.meta.Catalog)
	if catID == "" {
		catID = k.meta.Key
	}
	if catType != "" {
		return []catalogTarget{{catType: catType, catID: catID}}
	}
	var out []catalogTarget
	seen := map[string]bool{}
	for _, kind := range g.Kinds {
		ct := "movie"
		if kind == "tv" || kind == "show" || kind == "series" {
			ct = "series"
		}
		if !seen[ct] {
			seen[ct] = true
			out = append(out, catalogTarget{catType: ct, catID: catID})
		}
	}
	if len(out) == 0 {
		out = append(out, catalogTarget{catType: "movie", catID: catID})
	}
	return out
}

type catalogMeta struct {
	ID          string `json:"id"`
	IMDBID      string `json:"imdb_id"`
	IMDBAlt     string `json:"imdbId"`
	TMDBID      any    `json:"tmdb_id"`
	TMDBAlt     any    `json:"tmdbId"`
	TVDBID      any    `json:"tvdb_id"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	ReleaseInfo string `json:"releaseInfo"`
	Year        int    `json:"year"`
}

func (k *listSourceKind) fetchCatalogPages(g *collections.Gather, catType, catID string, maxItems int) ([]collections.ExternalRef, bool) {
	defaultMedia := "movie"
	if catType == "series" || catType == "show" || catType == "tv" {
		defaultMedia = "show"
	}
	var refs []collections.ExternalRef
	seenRaw := map[string]bool{}
	skip := 0
	for pageNum := 0; pageNum < maxItems && len(refs) < maxItems; pageNum++ {
		path := buildCatalogPath(catType, catID, g.Src.Params, skip)
		resp, err := k.mgr.Call(g.Ctx, k.pluginID, plugins.Request{
			Method: "GET",
			Path:   path,
		})
		if err != nil {
			if collections.IsPreview(g.Ctx) && errors.Is(err, context.DeadlineExceeded) {
				g.Fail(fmt.Errorf("plugin list preview timed out: %w", err))
			} else {
				g.Warn("%s: %v", k.label(), err)
			}
			return refs, false
		}
		if resp.Status >= 400 {
			k.mgr.RecordFailure(g.Ctx, k.pluginID, fmt.Sprintf("catalog %s returned HTTP %d", path, resp.Status))
			g.Warn("%s returned HTTP %d", k.label(), resp.Status)
			return refs, false
		}
		items, hasMore, err := decodeCatalogPage(resp.Body)
		if err != nil {
			k.mgr.RecordFailure(g.Ctx, k.pluginID, fmt.Sprintf("catalog %s returned invalid JSON", path))
			g.Warn("%s returned invalid JSON", k.label())
			return refs, false
		}
		if len(items) == 0 {
			break
		}
		newOnPage := 0
		for _, item := range items {
			rawKey := itemIdentity(item)
			if rawKey != "" {
				if !seenRaw[rawKey] {
					seenRaw[rawKey] = true
					newOnPage++
				}
			} else {
				newOnPage++
			}
			ref, _ := parseCatalogMeta(item, defaultMedia)
			refs = append(refs, ref)
			if len(refs) >= maxItems {
				break
			}
		}
		if collections.IsPreview(g.Ctx) {
			break
		}
		if hasMore != nil && !*hasMore {
			break
		}
		if pageNum > 0 && newOnPage == 0 {
			break
		}
		skip += len(items)
	}
	return refs, true
}

func buildCatalogPath(catType, catID string, params map[string]string, skip int) string {
	var parts []string
	if len(params) > 0 {
		keys := make([]string, 0, len(params))
		for k, v := range params {
			if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" && k != "skip" {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(strings.TrimSpace(params[k])))
		}
	}
	if skip > 0 {
		parts = append(parts, "skip="+strconv.Itoa(skip))
	}
	base := "/catalog/" + url.PathEscape(catType) + "/" + url.PathEscape(catID)
	if len(parts) == 0 {
		return base + ".json"
	}
	return base + "/" + strings.Join(parts, "&") + ".json"
}

func decodeCatalogPage(body json.RawMessage) ([]catalogMeta, *bool, error) {
	var page struct {
		Metas   *[]catalogMeta `json:"metas"`
		HasMore *bool          `json:"hasMore,omitempty"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, nil, err
	}
	if page.Metas == nil {
		return nil, nil, fmt.Errorf("missing metas array")
	}
	return *page.Metas, page.HasMore, nil
}

func itemIdentity(m catalogMeta) string {
	if m.ID != "" {
		return m.ID
	}
	if m.IMDBID != "" {
		return m.IMDBID
	}
	if m.IMDBAlt != "" {
		return m.IMDBAlt
	}
	if tmdb := parseAnyInt64(m.TMDBID); tmdb > 0 {
		return "tmdb:" + strconv.FormatInt(tmdb, 10)
	}
	if tmdb := parseAnyInt64(m.TMDBAlt); tmdb > 0 {
		return "tmdb:" + strconv.FormatInt(tmdb, 10)
	}
	return firstNonEmpty(m.Name, m.Title)
}

func parseCatalogMeta(m catalogMeta, defaultMedia string) (collections.ExternalRef, bool) {
	media := normalizeMediaType(m.Type, defaultMedia)
	ref := collections.ExternalRef{
		Type:  media,
		Title: strings.TrimSpace(firstNonEmpty(m.Name, m.Title)),
		Year:  m.Year,
	}
	if ref.Year == 0 && m.ReleaseInfo != "" {
		if match := yearPrefixRe.FindStringSubmatch(strings.TrimSpace(m.ReleaseInfo)); len(match) == 2 {
			ref.Year, _ = strconv.Atoi(match[1])
		}
	}
	for _, candidate := range []string{m.ID, m.IMDBID, m.IMDBAlt} {
		applyIDToken(&ref, candidate)
	}
	if ref.TMDB == 0 {
		if id := parseAnyInt64(m.TMDBID); id > 0 {
			ref.TMDB = id
		} else if id := parseAnyInt64(m.TMDBAlt); id > 0 {
			ref.TMDB = id
		}
	}
	if ref.TVDB == 0 {
		if id := parseAnyInt64(m.TVDBID); id > 0 {
			ref.TVDB = id
		}
	}
	// Plugin lists only match stable catalogue IDs, never title guesses.
	ref.Title, ref.TVDB = "", 0
	if ref.TMDB == 0 && ref.IMDB == "" {
		return collections.ExternalRef{}, false
	}
	return ref, true
}

func applyIDToken(ref *collections.ExternalRef, raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	// Strip episode suffix like "tt1234567:1:2" or "tmdb:12345:1:1".
	lower := strings.ToLower(raw)
	switch {
	case strings.HasPrefix(lower, "tt"):
		id := lower
		if i := strings.IndexByte(id, ':'); i > 0 {
			id = id[:i]
		}
		if len(id) > 2 && isDigits(id[2:]) && ref.IMDB == "" {
			ref.IMDB = id
		}
	case strings.HasPrefix(lower, "imdb:"):
		rest := strings.TrimPrefix(lower, "imdb:")
		applyIDToken(ref, rest)
	case strings.HasPrefix(lower, "tmdb:"):
		parts := strings.Split(strings.TrimPrefix(lower, "tmdb:"), ":")
		if len(parts) >= 2 && (parts[0] == "movie" || parts[0] == "show" || parts[0] == "series" || parts[0] == "tv") {
			ref.Type = normalizeMediaType(parts[0], ref.Type)
			if n, err := strconv.ParseInt(parts[1], 10, 64); err == nil && n > 0 && ref.TMDB == 0 {
				ref.TMDB = n
			}
		} else if len(parts) >= 1 {
			if n, err := strconv.ParseInt(parts[0], 10, 64); err == nil && n > 0 && ref.TMDB == 0 {
				ref.TMDB = n
			}
		}
	case strings.HasPrefix(lower, "tvdb:"):
		parts := strings.Split(strings.TrimPrefix(lower, "tvdb:"), ":")
		if len(parts) >= 1 {
			if n, err := strconv.ParseInt(parts[0], 10, 64); err == nil && n > 0 && ref.TVDB == 0 {
				ref.TVDB = n
			}
		}
	}
}

func normalizeMediaType(raw, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "movie", "movies", "film":
		return "movie"
	case "series", "show", "shows", "tv":
		return "show"
	default:
		if fallback != "" {
			return fallback
		}
		return "movie"
	}
}

func parseAnyInt64(v any) int64 {
	switch val := v.(type) {
	case float64:
		return int64(val)
	case int64:
		return val
	case int:
		return int64(val)
	case json.Number:
		n, _ := val.Int64()
		return n
	case string:
		s := strings.TrimSpace(val)
		s = strings.TrimPrefix(strings.ToLower(s), "tmdb:")
		n, _ := strconv.ParseInt(s, 10, 64)
		return n
	default:
		return 0
	}
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// rowSourceKind adapts a plugin list source to a Home Designer row kind.
type rowSourceKind struct {
	list *listSourceKind
}

func (r *rowSourceKind) Kind() string { return r.list.ID() }

func (r *rowSourceKind) Descriptor() core.RowDescriptor {
	id := r.list.ID()
	title := r.list.label()
	desc := resolvePlainText(r.list.mgr, r.list.pluginID, r.list.meta.Hint, "")
	sections := []string{"home"}
	for _, mt := range r.list.mediaTypes() {
		switch mt {
		case "movie":
			sections = append(sections, "movies")
		case "show":
			sections = append(sections, "shows")
		}
	}
	return core.RowDescriptor{
		HubID:       id,
		Title:       title,
		Kind:        id,
		Description: desc,
		Sections:    sections,
		Group:       "collections",
	}
}
