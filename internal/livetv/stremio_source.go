package livetv

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/stremio"
)

type stremioKind struct{}

func (stremioKind) ID() string              { return "stremio" }
func (stremioKind) Matches(src Source) bool { return src.Kind == "stremio" }
func (stremioKind) Describe() SourceDescribe {
	return SourceDescribe{Label: extend.T("Stremio add-on"), Hint: extend.T("Live channels from a Stremio add-on."), Icon: "tv", Order: 15, Editor: "stremio", YieldsChannels: true, YieldsGuide: true, Params: append(authParams(), extend.ParamField{Name: "playlistUrl", Kind: "secret", Label: extend.T("Add-on URL"), Required: true}, extend.ParamField{Name: "refreshHours", Kind: "number", Label: extend.T("Keep it fresh")})}
}
func (stremioKind) Fetch(ctx context.Context, s *Service, src *Source) (Fetched, error) {
	c, err := stremio.NewClient(src.PlaylistURL, s.o.StremioHTTP)
	if err != nil {
		return Fetched{}, err
	}
	manifest, err := c.Manifest(ctx)
	if err != nil {
		return Fetched{}, err
	}
	var out Fetched
	out.Guide = XMLGuide{Channels: map[string]Channel{}, Names: map[string][]string{}}
	seen := map[string]bool{}
	for _, catalog := range manifest.Catalogs {
		if catalog.Type != "tv" {
			continue
		}
		entries, err := c.Catalog(ctx, "tv", catalog.ID, nil, stremio.MaxCatalogItems)
		if err != nil {
			return Fetched{}, err
		}
		for _, entry := range entries {
			if entry.ID == "" || seen[entry.ID] {
				continue
			}
			seen[entry.ID] = true
			streams, err := c.Streams(ctx, "tv", entry.ID)
			if err != nil {
				return Fetched{}, err
			}
			ch := Channel{ID: stableID(src.ID, entry.ID), SourceID: src.ID, GuideID: entry.ID, Name: entry.Name, Logo: entry.Logo, Group: catalog.Name, Kind: "iptv", Enabled: true}
			for _, stream := range streams {
				if !validHTTP(stream.URL) || len(ch.StreamSources) == 16 {
					continue
				}
				duplicate := false
				for _, old := range ch.StreamSources {
					if old.URL == stream.URL {
						duplicate = true
						break
					}
				}
				if duplicate {
					continue
				}
				ch.StreamSources = append(ch.StreamSources, StreamSource{ID: "stremio-" + stableID(ch.ID, strconv.Itoa(len(ch.StreamSources))), Name: stream.Name, URL: stream.URL, Enabled: true})
			}
			if len(ch.StreamSources) > 0 {
				if err := normalizeStreamSources(&ch); err != nil {
					return Fetched{}, err
				}
			}
			out.Channels = append(out.Channels, ch)
		}
	}
	if len(out.Channels) == 0 {
		return Fetched{}, fmt.Errorf("stremio: add-on has no TV channels")
	}
	if manifest.BehaviorHints.EPGProvider {
		from, until := s.o.Now().UTC().Add(-guideLookback), s.o.Now().UTC().Add(guideLookahead)
		first := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
		for _, catalog := range manifest.Catalogs {
			if catalog.Type != "tv" {
				continue
			}
			dated := false
			for _, extra := range catalog.Extra {
				if extra.Name == "date" {
					dated = true
					break
				}
			}
			if !dated {
				continue
			}
			for day := first; day.Before(until); day = day.AddDate(0, 0, 1) {
				metas, err := c.GuideCatalog(ctx, catalog.ID, day.Format("2006-01-02"))
				if err != nil {
					return Fetched{}, err
				}
				for _, meta := range metas {
					if !seen[meta.ID] {
						continue
					}
					out.Guide.Channels[meta.ID] = Channel{GuideID: meta.ID, Name: meta.Name, Logo: meta.Logo}
					out.Guide.Names[meta.ID] = []string{meta.Name}
					for _, video := range meta.Videos {
						if !video.EndTime.After(video.StartTime) || !video.StartTime.Before(until) || !video.EndTime.After(from) {
							continue
						}
						out.Guide.Programmes = append(out.Guide.Programmes, Programme{ID: stableID(meta.ID, video.StartTime.UTC().Format(time.RFC3339Nano)), ChannelID: meta.ID, Start: video.StartTime, End: video.EndTime, Item: Item{Title: video.Title, Summary: video.Overview, Thumb: video.Thumbnail, DurationMS: video.EndTime.Sub(video.StartTime).Milliseconds()}})
					}
				}
			}
		}
	}
	if raw := manifest.GuideURL(); raw != "" {
		u, err := url.Parse(raw)
		if err != nil {
			return Fetched{}, fmt.Errorf("stremio: invalid guide URL")
		}
		u = c.Base().ResolveReference(u)
		b, err := c.FetchBytes(ctx, u.String(), maxGuideBytes)
		if err != nil {
			return Fetched{}, err
		}
		out.Guide, err = ParseXMLTV(strings.NewReader(string(b)))
		if err != nil {
			return Fetched{}, fmt.Errorf("stremio: invalid guide")
		}
	}
	return out, nil
}

// mergeStremioStreams refreshes add-on inputs while preserving their owner
// enablement and separately added inputs.
func mergeStremioStreams(fresh, previous []StreamSource) []StreamSource {
	old := make(map[string]StreamSource, len(previous))
	for _, input := range previous {
		old[input.ID] = input
	}
	out := make([]StreamSource, 0, len(fresh)+len(previous))
	for _, input := range fresh {
		if prior, ok := old[input.ID]; ok {
			input.Enabled = prior.Enabled
		}
		out = append(out, input)
	}
	for _, input := range previous {
		if !strings.HasPrefix(input.ID, "stremio-") {
			out = append(out, input)
		}
	}
	return out
}
