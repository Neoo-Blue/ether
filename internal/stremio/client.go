package stremio

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/braedonsaunders/ether/internal/egress"
	"github.com/braedonsaunders/ether/internal/safefetch"
)

const (
	maxCatalogPage = 8 << 20
	maxMeta        = 4 << 20
	maxSubtitles   = 4 << 20
	// MaxCatalogItems bounds one catalog read (SPEC WS3: maxItems ≤ 2000).
	MaxCatalogItems = 2000
)

// DefaultClient is the HTTP client for add-on API calls outside the
// indexer backend: egress class CapStremio behind the safefetch SSRF
// guard (public destinations only).
func DefaultClient() *http.Client {
	return (&safefetch.Guard{}).Client(egress.Client(egress.CapStremio, defaultTimeout))
}

// Client talks to one add-on for the catalog, tv and subtitle roles.
// The install URL is a secret: it is never logged and errors name only
// the host (see getJSON).
type Client struct {
	base *url.URL
	http *http.Client
}

// NewClient builds a Client for an install URL. A nil httpClient uses
// DefaultClient (tests pass an httptest client).
func NewClient(rawURL string, httpClient *http.Client) (*Client, error) {
	base, err := BaseURL(rawURL)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = DefaultClient()
	}
	return &Client{base: base, http: httpClient}, nil
}

// Host is the add-on host, safe to display and log.
func (c *Client) Host() string { return c.base.Host }

// Base is a copy of the add-on base URL.
func (c *Client) Base() *url.URL { u := *c.base; return &u }

// Manifest reads the add-on manifest (no stream-indexer check).
func (c *Client) Manifest(ctx context.Context) (Manifest, error) {
	var m Manifest
	if err := getJSON(ctx, c.http, c.base, "/manifest.json", maxManifest, &m); err != nil {
		return Manifest{}, err
	}
	if m.ID == "" && len(m.Resources) == 0 && len(m.Catalogs) == 0 {
		return Manifest{}, fmt.Errorf("stremio: %s did not answer with an addon manifest", c.base.Host)
	}
	return m, nil
}

// extraSuffix encodes Stremio extra properties as /k=v&k2=v2 (sorted).
func extraSuffix(extra map[string]string) string {
	if len(extra) == 0 {
		return ""
	}
	keys := make([]string, 0, len(extra))
	for k, v := range extra {
		if k != "" && v != "" {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(extra[k]))
	}
	return "/" + strings.Join(parts, "&")
}

// MetaPreview is one catalog entry.
type MetaPreview struct {
	ID          string   `json:"id"`
	Type        string   `json:"type"`
	Name        string   `json:"name"`
	Poster      string   `json:"poster,omitempty"`
	Logo        string   `json:"logo,omitempty"`
	Description string   `json:"description,omitempty"`
	Genres      []string `json:"genres,omitempty"`
	ReleaseInfo string   `json:"releaseInfo,omitempty"`
	IMDBRating  string   `json:"imdbRating,omitempty"`
	BehaviorHints struct {
		DefaultVideoID     string `json:"defaultVideoId,omitempty"`
		HasScheduledVideos bool   `json:"hasScheduledVideos,omitempty"`
	} `json:"behaviorHints,omitempty"`
}

// CatalogPage reads one catalog page.
func (c *Client) CatalogPage(ctx context.Context, typ, id string, extra map[string]string) ([]MetaPreview, error) {
	var ans struct {
		Metas []MetaPreview `json:"metas"`
	}
	suffix := "/catalog/" + url.PathEscape(typ) + "/" + url.PathEscape(id) + extraSuffix(extra) + ".json"
	if err := getJSON(ctx, c.http, c.base, suffix, maxCatalogPage, &ans); err != nil {
		return nil, err
	}
	return ans.Metas, nil
}

// Catalog reads a whole catalog, paging with skip until a short page or
// maxItems (clamped to MaxCatalogItems). Duplicate ids keep first place.
func (c *Client) Catalog(ctx context.Context, typ, id string, extra map[string]string, maxItems int) ([]MetaPreview, error) {
	if maxItems <= 0 || maxItems > MaxCatalogItems {
		maxItems = MaxCatalogItems
	}
	var out []MetaPreview
	seen := map[string]bool{}
	skip := 0
	for len(out) < maxItems {
		ex := map[string]string{}
		for k, v := range extra {
			ex[k] = v
		}
		if skip > 0 {
			ex["skip"] = strconv.Itoa(skip)
		}
		page, err := c.CatalogPage(ctx, typ, id, ex)
		if err != nil {
			return out, err
		}
		if len(page) == 0 {
			break
		}
		added := 0
		for _, m := range page {
			if m.ID != "" && seen[m.ID] {
				continue
			}
			seen[m.ID] = true
			out = append(out, m)
			added++
			if len(out) >= maxItems {
				break
			}
		}
		skip += len(page)
		if added == 0 || len(page) < 20 {
			// A short page is the last page for well-behaved add-ons;
			// a page of pure duplicates means skip is ignored.
			if added == 0 {
				break
			}
			if len(page) < 20 {
				// One more probe only when the catalog declares skip is
				// unnecessary: short first pages are complete catalogs.
				break
			}
		}
	}
	return out, nil
}

// StreamItem is one stream an add-on returns for a tv channel (or any id).
type StreamItem struct {
	Name          string `json:"name,omitempty"`
	Title         string `json:"title,omitempty"`
	Description   string `json:"description,omitempty"`
	URL           string `json:"url,omitempty"`
	BehaviorHints struct {
		NotWebReady  bool `json:"notWebReady,omitempty"`
		BingeGroup   string `json:"bingeGroup,omitempty"`
		ProxyHeaders struct {
			Request map[string]string `json:"request,omitempty"`
		} `json:"proxyHeaders,omitempty"`
	} `json:"behaviorHints,omitempty"`
}

// Streams reads stream/{type}/{id}.json.
func (c *Client) Streams(ctx context.Context, typ, id string) ([]StreamItem, error) {
	var ans struct {
		Streams []StreamItem `json:"streams"`
	}
	suffix := "/stream/" + url.PathEscape(typ) + "/" + url.PathEscape(id) + ".json"
	if err := getJSON(ctx, c.http, c.base, suffix, maxStreams, &ans); err != nil {
		return nil, err
	}
	return ans.Streams, nil
}

// Video is one entry of a meta's videos list. TV add-ons publish their
// schedule as videos with Released timestamps.
type Video struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Overview string    `json:"overview,omitempty"`
	Released time.Time `json:"released,omitempty"`
	Thumbnail string   `json:"thumbnail,omitempty"`
}

// Meta is the part of a meta document Ether reads.
type Meta struct {
	MetaPreview
	Videos []Video `json:"videos,omitempty"`
}

// Meta reads meta/{type}/{id}.json.
func (c *Client) Meta(ctx context.Context, typ, id string) (Meta, error) {
	var ans struct {
		Meta Meta `json:"meta"`
	}
	suffix := "/meta/" + url.PathEscape(typ) + "/" + url.PathEscape(id) + ".json"
	if err := getJSON(ctx, c.http, c.base, suffix, maxMeta, &ans); err != nil {
		return Meta{}, err
	}
	return ans.Meta, nil
}

// Subtitle is one subtitle an add-on offers.
type Subtitle struct {
	ID   string `json:"id,omitempty"`
	URL  string `json:"url"`
	Lang string `json:"lang"`
}

// Subtitles reads subtitles/{type}/{id}[/extra].json. Listing only:
// nothing is downloaded here.
func (c *Client) Subtitles(ctx context.Context, typ, id string, extra map[string]string) ([]Subtitle, error) {
	var ans struct {
		Subtitles []Subtitle `json:"subtitles"`
	}
	suffix := "/subtitles/" + url.PathEscape(typ) + "/" + url.PathEscape(id) + extraSuffix(extra) + ".json"
	if err := getJSON(ctx, c.http, c.base, suffix, maxSubtitles, &ans); err != nil {
		return nil, err
	}
	return ans.Subtitles, nil
}

// FetchBytes downloads one URL (a picked subtitle file, a guide) with a
// size cap. Errors name only the host.
func (c *Client) FetchBytes(ctx context.Context, raw string, limit int64) ([]byte, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("stremio: invalid download URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("stremio: %s: invalid request", u.Host)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("stremio: %s: download failed", u.Host)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return nil, &httpError{host: u.Host, status: resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("stremio: %s: %w", u.Host, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("stremio: %s answered with more than %d bytes", u.Host, limit)
	}
	return data, nil
}

// ParseCatalogRef splits a catalog meta id into an external identity:
// "tt0816692" → IMDb, "tmdb:550" / "tmdb:movie:550" → TMDB.
func ParseCatalogRef(id string) (imdb string, tmdb int64) {
	id = strings.TrimSpace(id)
	if v := imdbID(id); v != "" {
		return v, 0
	}
	if rest, ok := strings.CutPrefix(strings.ToLower(id), "tmdb:"); ok {
		parts := strings.Split(rest, ":")
		last := parts[len(parts)-1]
		// Series ids may be tmdb:ID:season:episode; the show id leads.
		if n, err := strconv.ParseInt(parts[0], 10, 64); err == nil && n > 0 {
			return "", n
		}
		if n, err := strconv.ParseInt(last, 10, 64); err == nil && n > 0 {
			return "", n
		}
	}
	return "", 0
}

// RedactedHost returns "host (saved)" style display text for an install
// URL without any path segment, for owner UI surfaces. Empty stays empty.
func RedactedHost(raw string) string {
	base, err := BaseURL(raw)
	if err != nil {
		return ""
	}
	return base.Host
}
