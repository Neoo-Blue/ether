package adapters

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/livetv"
	"github.com/braedonsaunders/ether/internal/plugins"
)

const openStreamDeadline = 3 * time.Second

type channelSourceKind struct {
	mgr        *plugins.Manager
	pluginID   string
	pluginName string
	meta       plugins.ChannelsMeta
}

func (k *channelSourceKind) ID() string {
	return extend.PluginID(k.pluginID, "channels")
}

func (k *channelSourceKind) Describe() livetv.SourceDescribe {
	label := firstNonEmpty(k.pluginName, k.pluginID)
	return livetv.SourceDescribe{
		Label:          pluginText(k.pluginID, label),
		Hint:           extend.T("An M3U playlist of live streams, with an optional XMLTV guide."),
		Icon:           "tv",
		Order:          50,
		Editor:         "plugin",
		YieldsChannels: true,
		YieldsGuide:    true,
		Params: []extend.ParamField{
			{Name: "name", Kind: "text", Label: extend.T("Source name"), Required: true},
			{Name: "refreshHours", Kind: "number", Label: extend.T("Keep it fresh")},
		},
	}
}

func (k *channelSourceKind) Matches(src livetv.Source) bool {
	id := k.ID()
	shortID := "plugin:" + k.pluginID
	schemePrefix := "plugin://" + k.pluginID
	for _, candidate := range []string{strings.TrimSpace(src.Kind), strings.TrimSpace(src.PlaylistURL)} {
		if candidate == "" {
			continue
		}
		if candidate == id || candidate == shortID || candidate == schemePrefix || strings.HasPrefix(candidate, schemePrefix+"/") {
			return true
		}
	}
	return false
}

func (k *channelSourceKind) Fetch(ctx context.Context, s *livetv.Service, src *livetv.Source) (livetv.Fetched, error) {
	if k.mgr == nil {
		return livetv.Fetched{}, fmt.Errorf("plugin manager unavailable")
	}
	src.Kind = k.ID()
	if strings.TrimSpace(src.PlaylistURL) == "" {
		src.PlaylistURL = k.ID()
	}
	if src.RefreshHours == 0 {
		if hrs := parseRefreshHours(k.meta.Refresh); hrs > 0 {
			src.RefreshHours = hrs
		}
	}

	resp, err := k.mgr.Call(ctx, k.pluginID, plugins.Request{
		Method: "GET",
		Path:   "/ether/channels.json",
	})
	if err != nil {
		return livetv.Fetched{}, err
	}
	if resp.Status >= 400 {
		reason := fmt.Sprintf("/ether/channels.json returned HTTP %d", resp.Status)
		k.mgr.RecordFailure(ctx, k.pluginID, reason)
		return livetv.Fetched{}, fmt.Errorf("plugin %s: %s", k.pluginID, reason)
	}
	rawChannels, err := decodeChannelsPayload(resp.Body)
	if err != nil {
		reason := "/ether/channels.json returned invalid JSON"
		k.mgr.RecordFailure(ctx, k.pluginID, reason)
		return livetv.Fetched{}, fmt.Errorf("plugin %s: %s: %w", k.pluginID, reason, err)
	}

	now := time.Now().UTC()
	if s != nil {
		now = s.Now().UTC()
	}
	from := now.Add(-6 * time.Hour)
	until := now.Add(24 * time.Hour)

	guide := livetv.XMLGuide{
		Channels: map[string]livetv.Channel{},
		Names:    map[string][]string{},
	}
	channels := make([]livetv.Channel, 0, len(rawChannels))
	seen := map[string]bool{}

	for idx, raw := range rawChannels {
		rawID := strings.TrimSpace(raw.ID)
		if rawID == "" {
			rawID = strconv.Itoa(idx + 1)
		}
		chID := stableID(src.ID, rawID)
		if seen[chID] {
			continue
		}
		seen[chID] = true

		name := strings.TrimSpace(firstNonEmpty(raw.Name, raw.Title, rawID))
		number := strings.TrimSpace(anyString(firstNonNil(raw.Number, raw.ChannelNumber)))
		if number == "" {
			number = strconv.Itoa(len(channels) + 1)
		}
		guideID := strings.TrimSpace(firstNonEmpty(raw.GuideID, raw.TvgID, rawID))
		logo := strings.TrimSpace(firstNonEmpty(raw.Logo, raw.Poster, raw.Icon))
		group := strings.TrimSpace(firstNonEmpty(raw.Group, raw.Category))
		rating := strings.TrimSpace(firstNonEmpty(raw.Rating, raw.ContentRating))

		streams := k.buildStreamSources(chID, rawID, raw)
		if len(streams) == 0 {
			continue
		}

		ch := livetv.Channel{
			ID:            chID,
			SourceID:      src.ID,
			Name:          name,
			Number:        number,
			Kind:          "iptv",
			Enabled:       true,
			Logo:          logo,
			Group:         group,
			GuideID:       guideID,
			Rating:        rating,
			StreamURL:     streams[0].URL,
			StreamSources: streams,
		}
		channels = append(channels, ch)

		guide.Channels[guideID] = livetv.Channel{
			GuideID: guideID,
			Name:    name,
			Logo:    logo,
		}
		guide.Names[guideID] = []string{name}

		var progs []pluginProgramme
		if len(raw.Programmes) > 0 {
			progs = raw.Programmes
		} else {
			fetchedProgs, err := k.fetchChannelGuide(ctx, rawID, from, until)
			if err != nil {
				return livetv.Fetched{}, err
			}
			progs = fetchedProgs
		}
		for _, p := range progs {
			if prog, ok := convertProgramme(guideID, p); ok {
				guide.Programmes = append(guide.Programmes, prog)
			}
		}
	}

	return livetv.Fetched{
		Channels: channels,
		Guide:    guide,
	}, nil
}

func (k *channelSourceKind) buildStreamSources(chID, rawChID string, raw pluginChannel) []livetv.StreamSource {
	inputs := raw.Streams
	if len(inputs) == 0 {
		inputs = raw.StreamSources
	}
	var out []livetv.StreamSource
	seenURL := map[string]bool{}
	add := func(id, name, streamURL string, priority int) {
		streamURL = strings.TrimSpace(streamURL)
		if streamURL == "" || seenURL[streamURL] {
			return
		}
		seenURL[streamURL] = true
		if id == "" {
			if len(out) == 0 {
				id = stableID(chID, "playlist")
			} else {
				id = stableID(chID, streamURL)
			}
		}
		if name == "" {
			if len(out) == 0 {
				name = "Primary stream"
			} else {
				name = fmt.Sprintf("Stream %d", len(out)+1)
			}
		}
		out = append(out, livetv.StreamSource{
			ID:       id,
			Name:     name,
			URL:      streamURL,
			Enabled:  true,
			Priority: priority,
		})
	}

	for i, in := range inputs {
		u := strings.TrimSpace(in.URL)
		switch {
		case strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "plugin://"):
			// use directly
		case strings.HasPrefix(u, "/ether/open/"):
			u = "plugin://" + k.pluginID + u
		case in.OpenID != "":
			u = "plugin://" + k.pluginID + "/open/" + url.PathEscape(strings.TrimSpace(in.OpenID))
		case u == "" && in.ID != "":
			u = "plugin://" + k.pluginID + "/stream/tv/" + url.PathEscape(strings.TrimSpace(in.ID))
		}
		prio := in.Priority
		if prio <= 0 {
			prio = i
		}
		sourceID := ""
		if len(out) == 0 {
			sourceID = stableID(chID, "playlist")
		}
		add(sourceID, firstNonEmpty(in.Name, in.Title), u, prio)
	}

	if len(out) == 0 {
		u := strings.TrimSpace(firstNonEmpty(raw.StreamURL, raw.URL))
		switch {
		case strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "plugin://"):
			add(stableID(chID, "playlist"), "Primary stream", u, 0)
		case strings.HasPrefix(u, "/ether/open/"):
			add(stableID(chID, "playlist"), "Primary stream", "plugin://"+k.pluginID+u, 0)
		case raw.OpenID != "":
			add(stableID(chID, "playlist"), "Primary stream", "plugin://"+k.pluginID+"/open/"+url.PathEscape(strings.TrimSpace(raw.OpenID)), 0)
		default:
			add(stableID(chID, "playlist"), "Primary stream", "plugin://"+k.pluginID+"/stream/tv/"+url.PathEscape(rawChID), 0)
		}
	}
	return out
}

func (k *channelSourceKind) fetchChannelGuide(ctx context.Context, rawChID string, from, until time.Time) ([]pluginProgramme, error) {
	path := fmt.Sprintf(
		"/ether/guide/%s.json?from=%s&to=%s",
		url.PathEscape(rawChID),
		url.QueryEscape(from.UTC().Format(time.RFC3339)),
		url.QueryEscape(until.UTC().Format(time.RFC3339)),
	)
	resp, err := k.mgr.Call(ctx, k.pluginID, plugins.Request{
		Method: "GET",
		Path:   path,
	})
	if err != nil {
		return nil, err
	}
	if resp.Status == http.StatusNotFound {
		return nil, nil
	}
	if resp.Status >= 400 {
		reason := fmt.Sprintf("%s returned HTTP %d", path, resp.Status)
		k.mgr.RecordFailure(ctx, k.pluginID, reason)
		return nil, fmt.Errorf("plugin %s: %s", k.pluginID, reason)
	}
	progs, err := decodeGuidePayload(resp.Body)
	if err != nil {
		reason := fmt.Sprintf("%s returned invalid JSON", path)
		k.mgr.RecordFailure(ctx, k.pluginID, reason)
		return nil, fmt.Errorf("plugin %s: %s: %w", k.pluginID, reason, err)
	}
	return progs, nil
}

// openPluginStream resolves a lazy plugin://<pluginID>/... StreamSource at
// tune time under the 3-second Open deadline (SPEC §5.1.2).
func (b *Bridge) openPluginStream(ctx context.Context, _ livetv.Channel, src livetv.StreamSource) (string, http.Header, error) {
	if b.mgr == nil {
		return "", nil, core.ErrNotAvailable
	}
	u, err := url.Parse(src.URL)
	if err != nil || u.Scheme != "plugin" || u.Host == "" {
		return "", nil, core.ErrNotAvailable
	}
	pluginID := u.Host
	openCtx, cancel := context.WithTimeout(ctx, openStreamDeadline)
	defer cancel()

	paths := candidateStreamPaths(u.Path)
	for _, p := range paths {
		resp, err := b.mgr.Call(openCtx, pluginID, plugins.Request{
			Method: "GET",
			Path:   p,
		})
		if err != nil {
			return "", nil, err
		}
		if resp.Status == http.StatusNotFound && len(paths) > 1 {
			continue
		}
		if resp.Status >= 400 {
			reason := fmt.Sprintf("%s returned HTTP %d", p, resp.Status)
			b.mgr.RecordFailure(openCtx, pluginID, reason)
			return "", nil, core.ErrNotAvailable
		}
		resolvedURL, hdr, ok := decodeOpenedStream(resp.Body)
		if ok {
			return resolvedURL, hdr, nil
		}
	}
	b.mgr.RecordFailure(openCtx, pluginID, "stream open returned no playable HTTP(S) URL")
	return "", nil, core.ErrNotAvailable
}

func candidateStreamPaths(rawPath string) []string {
	trimmed := strings.TrimPrefix(rawPath, "/")
	switch {
	case strings.HasPrefix(trimmed, "ether/open/"):
		return []string{"/" + trimmed}
	case strings.HasPrefix(trimmed, "open/"):
		id := strings.TrimPrefix(trimmed, "open/")
		return []string{"/ether/open/" + id, "/stream/tv/" + strings.TrimSuffix(id, ".json") + ".json"}
	case strings.HasPrefix(trimmed, "stream/tv/"):
		id := strings.TrimSuffix(strings.TrimPrefix(trimmed, "stream/tv/"), ".json")
		return []string{"/stream/tv/" + id + ".json", "/ether/open/" + id}
	default:
		id := strings.TrimSuffix(trimmed, ".json")
		return []string{"/stream/tv/" + id + ".json", "/ether/open/" + id}
	}
}

type openedStreamDoc struct {
	URL           string            `json:"url"`
	Headers       map[string]string `json:"headers"`
	BehaviorHints *struct {
		ProxyHeaders *struct {
			Request map[string]string `json:"request"`
		} `json:"proxyHeaders"`
	} `json:"behaviorHints"`
	Streams []openedStreamDoc `json:"streams"`
}

func decodeOpenedStream(body json.RawMessage) (string, http.Header, bool) {
	if len(body) == 0 {
		return "", nil, false
	}
	var doc openedStreamDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", nil, false
	}
	candidates := []openedStreamDoc{doc}
	candidates = append(candidates, doc.Streams...)
	for _, c := range candidates {
		u := strings.TrimSpace(c.URL)
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			continue
		}
		hdr := http.Header{}
		for k, v := range c.Headers {
			if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
				hdr.Set(strings.TrimSpace(k), v)
			}
		}
		if c.BehaviorHints != nil && c.BehaviorHints.ProxyHeaders != nil {
			for k, v := range c.BehaviorHints.ProxyHeaders.Request {
				if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
					hdr.Set(strings.TrimSpace(k), v)
				}
			}
		}
		return u, hdr, true
	}
	return "", nil, false
}

type pluginChannel struct {
	ID            string              `json:"id"`
	Name          string              `json:"name"`
	Title         string              `json:"title"`
	Number        any                 `json:"number"`
	ChannelNumber any                 `json:"channelNumber"`
	Logo          string              `json:"logo"`
	Poster        string              `json:"poster"`
	Icon          string              `json:"icon"`
	Group         string              `json:"group"`
	Category      string              `json:"category"`
	GuideID       string              `json:"guideId"`
	TvgID         string              `json:"tvgId"`
	Rating        string              `json:"rating"`
	ContentRating string              `json:"contentRating"`
	StreamURL     string              `json:"streamUrl"`
	URL           string              `json:"url"`
	OpenID        string              `json:"openId"`
	Streams       []pluginStreamInput `json:"streams"`
	StreamSources []pluginStreamInput `json:"streamSources"`
	Programmes    []pluginProgramme   `json:"programmes"`
}

type pluginStreamInput struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	OpenID   string `json:"openId"`
	Priority int    `json:"priority"`
}

type pluginProgramme struct {
	Title       string `json:"title"`
	Name        string `json:"name"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	Rating      string `json:"rating"`
	Thumb       string `json:"thumb"`
	Icon        string `json:"icon"`
	Poster      string `json:"poster"`
	Start       any    `json:"start"`
	End         any    `json:"end"`
	Stop        any    `json:"stop"`
}

func decodeChannelsPayload(body json.RawMessage) ([]pluginChannel, error) {
	if len(body) == 0 {
		return nil, nil
	}
	var env struct {
		Channels []pluginChannel `json:"channels"`
	}
	if err := json.Unmarshal(body, &env); err == nil && env.Channels != nil {
		return env.Channels, nil
	}
	var arr []pluginChannel
	if err := json.Unmarshal(body, &arr); err != nil {
		return nil, err
	}
	return arr, nil
}

func decodeGuidePayload(body json.RawMessage) ([]pluginProgramme, error) {
	if len(body) == 0 {
		return nil, nil
	}
	var env struct {
		Programmes []pluginProgramme `json:"programmes"`
		Programs   []pluginProgramme `json:"programs"`
	}
	if err := json.Unmarshal(body, &env); err == nil && (env.Programmes != nil || env.Programs != nil) {
		if env.Programmes != nil {
			return env.Programmes, nil
		}
		return env.Programs, nil
	}
	var arr []pluginProgramme
	if err := json.Unmarshal(body, &arr); err != nil {
		return nil, err
	}
	return arr, nil
}

func convertProgramme(guideID string, p pluginProgramme) (livetv.Programme, bool) {
	start, ok := parseFlexibleTime(p.Start)
	if !ok {
		return livetv.Programme{}, false
	}
	end, ok := parseFlexibleTime(firstNonNil(p.End, p.Stop))
	if !ok || !end.After(start) {
		return livetv.Programme{}, false
	}
	title := strings.TrimSpace(firstNonEmpty(p.Title, p.Name, "Programme"))
	summary := strings.TrimSpace(firstNonEmpty(p.Summary, p.Description))
	thumb := strings.TrimSpace(firstNonEmpty(p.Thumb, p.Icon, p.Poster))
	return livetv.Programme{
		ChannelID: guideID,
		Start:     start.UTC(),
		End:       end.UTC(),
		Item: livetv.Item{
			Title:      title,
			Summary:    summary,
			Rating:     strings.TrimSpace(p.Rating),
			Thumb:      thumb,
			DurationMS: end.Sub(start).Milliseconds(),
		},
	}, true
}

func parseFlexibleTime(v any) (time.Time, bool) {
	switch val := v.(type) {
	case string:
		s := strings.TrimSpace(val)
		if s == "" {
			return time.Time{}, false
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05Z", "2006-01-02 15:04:05"} {
			if t, err := time.Parse(layout, s); err == nil {
				return t.UTC(), true
			}
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return unixOrMilli(n), true
		}
	case float64:
		return unixOrMilli(int64(val)), true
	case int64:
		return unixOrMilli(val), true
	case int:
		return unixOrMilli(int64(val)), true
	case json.Number:
		if n, err := val.Int64(); err == nil {
			return unixOrMilli(n), true
		}
	}
	return time.Time{}, false
}

func unixOrMilli(n int64) time.Time {
	if n > 1e11 {
		return time.UnixMilli(n).UTC()
	}
	return time.Unix(n, 0).UTC()
}

func parseRefreshHours(raw string) int {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "":
		return 0
	case "hourly":
		return 1
	case "daily":
		return 24
	case "weekly":
		return 24 * 7
	}
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		hrs := int(d.Hours())
		if hrs < 1 {
			hrs = 1
		}
		return hrs
	}
	if n, err := strconv.Atoi(strings.TrimSuffix(s, "h")); err == nil && n > 0 {
		return n
	}
	return 0
}

func stableID(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:10])
}

func firstNonNil(vals ...any) any {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

func anyString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatInt(int64(val), 10)
	case int64:
		return strconv.FormatInt(val, 10)
	case int:
		return strconv.Itoa(val)
	case json.Number:
		return val.String()
	default:
		return ""
	}
}
