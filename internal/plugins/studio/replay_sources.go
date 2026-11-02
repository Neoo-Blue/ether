package studio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/braedonsaunders/ether/internal/plugins"
)

// ListItem is one entry returned by a list source plugin's catalog route.
type ListItem struct {
	ID     string `json:"id"`
	IMDbID string `json:"imdbId,omitempty"`
	TMDBID int    `json:"tmdbId,omitempty"`
	Title  string `json:"title"`
	Type   string `json:"type,omitempty"`
	Year   int    `json:"year,omitempty"`
}

// MatchedTitle is one catalogue title matched from a list source item.
type MatchedTitle struct {
	RatingKey  string `json:"ratingKey,omitempty"`
	ExternalID string `json:"externalId,omitempty"`
	Title      string `json:"title"`
	Type       string `json:"type,omitempty"`
	Year       int    `json:"year,omitempty"`
}

// ListReplayReport summarizes a list source replay: pages fetched, catalogue
// match rate, and a preview of matched titles.
type ListReplayReport struct {
	SourceKey       string         `json:"sourceKey,omitempty"`
	Catalog         string         `json:"catalog"`
	MediaType       string         `json:"mediaType"`
	PagesFetched    int            `json:"pagesFetched"`
	TotalItems      int            `json:"totalItems"`
	MatchedItems    int            `json:"matchedItems"`
	UnmatchedItems  int            `json:"unmatchedItems"`
	MatchRate       float64        `json:"matchRate"`
	Preview         []MatchedTitle `json:"preview"`
	UnmatchedSample []string       `json:"unmatchedSample,omitempty"`
	TotalUnits      int64          `json:"totalUnits"`
}

// ChannelReplayReport summarizes a Live TV channel source replay: channel
// count, guide coverage, and a sample probe of up to 5 stream inputs.
type ChannelReplayReport struct {
	ChannelCount    int                 `json:"channelCount"`
	WithGuideCount  int                 `json:"withGuideCount"`
	GuideCoverage   float64             `json:"guideCoverage"`
	TotalPrograms   int                 `json:"totalPrograms"`
	ProbesAttempted int                 `json:"probesAttempted"`
	ProbesOK        int                 `json:"probesOk"`
	Probes          []StreamProbeResult `json:"probes"`
	TotalUnits      int64               `json:"totalUnits"`
}

// StreamProbeResult is the outcome of probing one channel input stream.
type StreamProbeResult struct {
	ChannelID   string `json:"channelId"`
	ChannelName string `json:"channelName"`
	URL         string `json:"url"`
	OK          bool   `json:"ok"`
	Status      int    `json:"status,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	Kind        string `json:"kind,omitempty"` // "hls", "mpegts", "http"
	LatencyMS   int64  `json:"latencyMs"`
	Error       string `json:"error,omitempty"`
}

func (r *Runner) replayList(ctx context.Context, target *resolvedTarget, params ReplayParams) (*ListReplayReport, error) {
	pages := params.Pages
	if pages <= 0 {
		pages = DefaultListPages
	}
	if pages > MaxListPages {
		pages = MaxListPages
	}

	sourceKey := params.SourceKey
	catalogID := params.Catalog
	mediaType := params.MediaType

	if target.manifest.Ether != nil {
		for _, ls := range target.manifest.Ether.Contributes.ListSources {
			if sourceKey == "" || ls.Key == sourceKey {
				sourceKey = ls.Key
				if catalogID == "" {
					catalogID = ls.Catalog
				}
				break
			}
		}
	}
	for _, cat := range target.manifest.Catalogs {
		if catalogID == "" || cat.ID == catalogID {
			if catalogID == "" {
				catalogID = cat.ID
			}
			if mediaType == "" && cat.Type != "" {
				mediaType = cat.Type
			}
			break
		}
	}
	if catalogID == "" {
		catalogID = "default"
	}
	if mediaType == "" {
		mediaType = "movie"
	}

	report := &ListReplayReport{
		SourceKey: sourceKey,
		Catalog:   catalogID,
		MediaType: mediaType,
		Preview:   []MatchedTitle{},
	}

	seen := map[string]bool{}
	var items []ListItem
	skip := 0

	for page := 0; page < pages; page++ {
		if err := ctx.Err(); err != nil {
			break
		}
		reqPath := fmt.Sprintf("/catalog/%s/%s.json", mediaType, catalogID)
		query := map[string]string{}
		if page > 0 {
			reqPath = fmt.Sprintf("/catalog/%s/%s/skip=%d.json", mediaType, catalogID, skip)
			query["skip"] = strconv.Itoa(skip)
		}
		resp, out, err := target.call(ctx, plugins.Request{
			PluginID: target.id,
			Method:   "GET",
			Path:     reqPath,
			Query:    query,
		})
		report.TotalUnits += out.Units
		if err != nil {
			if page == 0 {
				return nil, err
			}
			break
		}
		if resp.Status == http.StatusNotFound {
			break
		}
		if resp.Status < 200 || resp.Status >= 300 {
			if page == 0 {
				return nil, fmt.Errorf("catalog request returned status %d", resp.Status)
			}
			break
		}

		pageItems := parseCatalogItems(resp.Body, mediaType)
		report.PagesFetched++
		if len(pageItems) == 0 {
			break
		}
		added := 0
		for _, it := range pageItems {
			dedupKey := it.ID
			if dedupKey == "" {
				dedupKey = fmt.Sprintf("%s:%d", strings.ToLower(it.Title), it.Year)
			}
			if seen[dedupKey] {
				continue
			}
			seen[dedupKey] = true
			items = append(items, it)
			added++
		}
		skip += len(pageItems)
		if added == 0 {
			break
		}
	}

	report.TotalItems = len(items)
	for _, it := range items {
		matched, ok := r.matchListItem(ctx, it)
		if ok {
			report.MatchedItems++
			if len(report.Preview) < MaxListPreview {
				report.Preview = append(report.Preview, matched)
			}
		} else {
			report.UnmatchedItems++
			if len(report.UnmatchedSample) < 10 {
				label := it.Title
				if label == "" {
					label = it.ID
				}
				report.UnmatchedSample = append(report.UnmatchedSample, label)
			}
		}
	}
	if report.TotalItems > 0 {
		report.MatchRate = float64(report.MatchedItems) / float64(report.TotalItems)
	}
	return report, nil
}

func parseCatalogItems(raw json.RawMessage, defaultType string) []ListItem {
	if len(raw) == 0 {
		return nil
	}
	type rawEntry struct {
		ID          string `json:"id"`
		IMDbID      string `json:"imdb_id"`
		IMDbIDAlt   string `json:"imdbId"`
		TMDBID      int    `json:"tmdb_id"`
		TMDBIDAlt   int    `json:"tmdbId"`
		Name        string `json:"name"`
		Title       string `json:"title"`
		Type        string `json:"type"`
		Year        int    `json:"year"`
		ReleaseInfo string `json:"releaseInfo"`
	}
	var wrapper struct {
		Metas []rawEntry `json:"metas"`
		Items []rawEntry `json:"items"`
	}
	var entries []rawEntry
	if json.Unmarshal(raw, &wrapper) == nil && (len(wrapper.Metas) > 0 || len(wrapper.Items) > 0) {
		entries = append(wrapper.Metas, wrapper.Items...)
	} else {
		_ = json.Unmarshal(raw, &entries)
	}

	out := make([]ListItem, 0, len(entries))
	for _, e := range entries {
		title := strings.TrimSpace(e.Title)
		if title == "" {
			title = strings.TrimSpace(e.Name)
		}
		imdbID := strings.TrimSpace(e.IMDbID)
		if imdbID == "" {
			imdbID = strings.TrimSpace(e.IMDbIDAlt)
		}
		tmdbID := e.TMDBID
		if tmdbID == 0 {
			tmdbID = e.TMDBIDAlt
		}
		id := strings.TrimSpace(e.ID)
		if imdbID == "" && strings.HasPrefix(id, "tt") {
			imdbID = id
		}
		if tmdbID == 0 && strings.HasPrefix(strings.ToLower(id), "tmdb:") {
			if n, err := strconv.Atoi(strings.TrimPrefix(strings.ToLower(id), "tmdb:")); err == nil {
				tmdbID = n
			}
		}
		year := e.Year
		if year == 0 && len(e.ReleaseInfo) >= 4 {
			if y, err := strconv.Atoi(e.ReleaseInfo[:4]); err == nil {
				year = y
			}
		}
		typ := e.Type
		if typ == "" {
			typ = defaultType
		}
		if id == "" && title == "" && imdbID == "" && tmdbID == 0 {
			continue
		}
		out = append(out, ListItem{
			ID:     id,
			IMDbID: imdbID,
			TMDBID: tmdbID,
			Title:  title,
			Type:   typ,
			Year:   year,
		})
	}
	return out
}

func (r *Runner) matchListItem(ctx context.Context, item ListItem) (MatchedTitle, bool) {
	if r.deps.CatalogMatch != nil {
		return r.deps.CatalogMatch(ctx, item)
	}
	if r.deps.DB == nil {
		return MatchedTitle{}, false
	}
	// Check SQLite/PostgreSQL catalogue tables when present in DB.
	for _, table := range []string{"catalog_titles", "titles"} {
		if item.IMDbID != "" {
			var key, title, typ string
			var year int
			err := r.deps.DB.QueryRowContext(ctx,
				fmt.Sprintf(`SELECT id, title, type, year FROM %s WHERE imdb_id = ? LIMIT 1`, table),
				item.IMDbID).Scan(&key, &title, &typ, &year)
			if err == nil {
				return MatchedTitle{RatingKey: key, ExternalID: item.IMDbID, Title: title, Type: typ, Year: year}, true
			}
		}
		if item.TMDBID > 0 {
			var key, title, typ string
			var year int
			err := r.deps.DB.QueryRowContext(ctx,
				fmt.Sprintf(`SELECT id, title, type, year FROM %s WHERE tmdb_id = ? LIMIT 1`, table),
				item.TMDBID).Scan(&key, &title, &typ, &year)
			if err == nil {
				return MatchedTitle{RatingKey: key, ExternalID: fmt.Sprintf("tmdb:%d", item.TMDBID), Title: title, Type: typ, Year: year}, true
			}
		}
		if item.Title != "" {
			var key, title, typ string
			var year int
			var err error
			if item.Year > 0 {
				err = r.deps.DB.QueryRowContext(ctx,
					fmt.Sprintf(`SELECT id, title, type, year FROM %s WHERE LOWER(title) = LOWER(?) AND year = ? LIMIT 1`, table),
					item.Title, item.Year).Scan(&key, &title, &typ, &year)
			} else {
				err = r.deps.DB.QueryRowContext(ctx,
					fmt.Sprintf(`SELECT id, title, type, year FROM %s WHERE LOWER(title) = LOWER(?) LIMIT 1`, table),
					item.Title).Scan(&key, &title, &typ, &year)
			}
			if err == nil {
				ext := item.ID
				if ext == "" {
					ext = item.IMDbID
				}
				return MatchedTitle{RatingKey: key, ExternalID: ext, Title: title, Type: typ, Year: year}, true
			}
		}
	}
	return MatchedTitle{}, false
}

type parsedChannel struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Number    string            `json:"number"`
	URL       string            `json:"url"`
	StreamURL string            `json:"streamUrl"`
	Input     string            `json:"input"`
	Headers   map[string]string `json:"headers"`
	HasGuide  bool              `json:"hasGuide"`
	Guide     []json.RawMessage `json:"guide"`
	Programs  []json.RawMessage `json:"programs"`
	EPG       []json.RawMessage `json:"epg"`
}

func (c parsedChannel) streamURL() string {
	switch {
	case c.URL != "":
		return c.URL
	case c.StreamURL != "":
		return c.StreamURL
	default:
		return c.Input
	}
}

func (c parsedChannel) programCount() int {
	return len(c.Guide) + len(c.Programs) + len(c.EPG)
}

func (r *Runner) replayChannels(ctx context.Context, target *resolvedTarget, params ReplayParams) (*ChannelReplayReport, error) {
	probeLimit := params.ProbeLimit
	if probeLimit <= 0 {
		probeLimit = DefaultChannelProbes
	}
	if probeLimit > MaxChannelProbes {
		probeLimit = MaxChannelProbes
	}

	resp, out, err := target.call(ctx, plugins.Request{
		PluginID: target.id,
		Method:   "GET",
		Path:     "/ether/channels.json",
	})
	if err != nil {
		return nil, err
	}
	if resp.Status < 200 || resp.Status >= 300 {
		return nil, fmt.Errorf("/ether/channels.json returned status %d", resp.Status)
	}

	var wrapper struct {
		Channels []parsedChannel `json:"channels"`
	}
	var channels []parsedChannel
	if json.Unmarshal(resp.Body, &wrapper) == nil && wrapper.Channels != nil {
		channels = wrapper.Channels
	} else {
		_ = json.Unmarshal(resp.Body, &channels)
	}

	report := &ChannelReplayReport{
		ChannelCount: len(channels),
		Probes:       []StreamProbeResult{},
		TotalUnits:   out.Units,
	}

	for i, ch := range channels {
		progs := ch.programCount()
		hasGuide := ch.HasGuide || progs > 0
		if !hasGuide && ch.ID != "" && i < 50 && ctx.Err() == nil {
			gresp, gout, gerr := target.call(ctx, plugins.Request{
				PluginID: target.id,
				Method:   "GET",
				Path:     fmt.Sprintf("/ether/guide/%s.json", ch.ID),
			})
			report.TotalUnits += gout.Units
			if gerr == nil && gresp.Status >= 200 && gresp.Status < 300 {
				var gdoc struct {
					Programs []json.RawMessage `json:"programs"`
					Guide    []json.RawMessage `json:"guide"`
				}
				if json.Unmarshal(gresp.Body, &gdoc) == nil {
					cnt := len(gdoc.Programs) + len(gdoc.Guide)
					if cnt > 0 {
						hasGuide = true
						progs += cnt
					}
				}
			}
		}
		if hasGuide {
			report.WithGuideCount++
		}
		report.TotalPrograms += progs
	}

	if report.ChannelCount > 0 {
		report.GuideCoverage = float64(report.WithGuideCount) / float64(report.ChannelCount)
	}

	for _, ch := range channels {
		if len(report.Probes) >= probeLimit || ctx.Err() != nil {
			break
		}
		u := strings.TrimSpace(ch.streamURL())
		if u == "" {
			continue
		}
		var pr StreamProbeResult
		if r.deps.ProbeStream != nil {
			start := time.Now()
			pr = r.deps.ProbeStream(ctx, u, ch.Headers)
			if pr.LatencyMS == 0 {
				pr.LatencyMS = time.Since(start).Milliseconds()
			}
		} else {
			pr = r.defaultProbeStream(ctx, u, ch.Headers)
		}
		if pr.ChannelID == "" {
			pr.ChannelID = ch.ID
		}
		if pr.ChannelName == "" {
			pr.ChannelName = ch.Name
		}
		if pr.URL == "" {
			pr.URL = u
		}
		report.Probes = append(report.Probes, pr)
		report.ProbesAttempted++
		if pr.OK {
			report.ProbesOK++
		}
	}

	return report, nil
}

func (r *Runner) defaultProbeStream(ctx context.Context, rawURL string, headers map[string]string) StreamProbeResult {
	start := time.Now()
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return StreamProbeResult{
			URL:       rawURL,
			OK:        false,
			LatencyMS: time.Since(start).Milliseconds(),
			Error:     "invalid stream url",
		}
	}

	if r.deps.HTTPClient != nil {
		pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(pctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return StreamProbeResult{URL: rawURL, OK: false, Error: err.Error()}
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := r.deps.HTTPClient.Do(req)
		if err != nil {
			return StreamProbeResult{
				URL:       rawURL,
				OK:        false,
				LatencyMS: time.Since(start).Milliseconds(),
				Error:     err.Error(),
			}
		}
		defer resp.Body.Close() //nolint:errcheck
		head, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		ct := resp.Header.Get("Content-Type")
		kind := classifyStreamKind(rawURL, ct, head)
		ok := resp.StatusCode >= 200 && resp.StatusCode < 400
		errMsg := ""
		if !ok {
			errMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return StreamProbeResult{
			URL:         rawURL,
			OK:          ok,
			Status:      resp.StatusCode,
			ContentType: ct,
			Kind:        kind,
			LatencyMS:   time.Since(start).Milliseconds(),
			Error:       errMsg,
		}
	}

	kind := classifyStreamKind(rawURL, "", nil)
	return StreamProbeResult{
		URL:       rawURL,
		OK:        true,
		Status:    http.StatusOK,
		Kind:      kind,
		LatencyMS: time.Since(start).Milliseconds(),
	}
}

func classifyStreamKind(rawURL, contentType string, sample []byte) string {
	lowerURL := strings.ToLower(rawURL)
	lowerCT := strings.ToLower(contentType)
	switch {
	case bytes.HasPrefix(bytes.TrimSpace(sample), []byte("#EXTM3U")),
		strings.Contains(lowerCT, "mpegurl"),
		strings.Contains(lowerURL, ".m3u8"):
		return "hls"
	case (len(sample) > 0 && sample[0] == 0x47),
		strings.Contains(lowerCT, "mp2t"),
		strings.HasSuffix(lowerURL, ".ts"):
		return "mpegts"
	default:
		return "http"
	}
}
