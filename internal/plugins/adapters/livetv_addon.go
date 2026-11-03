package adapters

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/braedonsaunders/ether/internal/egress"
	"github.com/braedonsaunders/ether/internal/livetv"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/safefetch"
	"github.com/braedonsaunders/ether/internal/stremio"
)

// A plain Stremio add-on that lists `tv` catalogs and answers `tv` streams
// is a Live TV channel source. Its channels are the catalog entries, its
// inputs the entries' HTTP(S) streams, and its guide either the add-on's
// own dated catalogs (behaviorHints.epgProvider) or the XMLTV guide its
// manifest links (behaviorHints.epgUrl), which wins when both exist.
//
// Channel ids are stableID(source id, entry id) and input ids
// livetv.AddonInputID(channel id, n): the same ids the built-in Stremio
// source minted, so a migrated source keeps every owner choice.
const (
	// addonStreamWorkers bounds concurrent stream lookups in one import.
	addonStreamWorkers = 8
	// addonGuideWorkers bounds concurrent dated guide pages.
	addonGuideWorkers = 4
	// addonGuideMaxBytes caps an add-on's XMLTV download.
	addonGuideMaxBytes = 32 << 20
	// addonInputNameMax keeps an input name within livetv's limit.
	addonInputNameMax = 200
)

// addonGuideClient fetches an add-on's XMLTV guide: Stremio egress behind
// the SSRF guard. Only a LAN add-on's own origin may be private.
var addonGuideClient = sync.OnceValue(func() *http.Client {
	return (&safefetch.Guard{}).Client(egress.Client(egress.CapStremio, time.Minute))
})

type addonEntry struct {
	meta  stremio.MetaPreview
	group string
}

func (k *channelSourceKind) fetchAddon(ctx context.Context, s *livetv.Service, src *livetv.Source) (livetv.Fetched, error) {
	if s == nil {
		return livetv.Fetched{}, errors.New("live TV is unavailable")
	}
	rec, err := k.mgr.Get(ctx, k.pluginID)
	if err != nil {
		return livetv.Fetched{}, err
	}
	addon := k.mgr.Addon(rec)
	entries, read, err := readAddonChannels(ctx, addon)
	if err != nil {
		return livetv.Fetched{}, err
	}

	previous := map[string]livetv.Channel{}
	stored, err := s.SourceChannels(ctx, src.ID)
	if err != nil {
		return livetv.Fetched{}, err
	}
	for _, c := range stored {
		previous[c.ID] = c
	}

	streams := make([][]stremio.Stream, len(entries))
	errs := make([]error, len(entries))
	forEachBounded(ctx, len(entries), addonStreamWorkers, func(i int) {
		streams[i], errs[i] = addon.StreamsFor(ctx, stremio.TypeTV, entries[i].meta.ID)
	})
	if err := ctx.Err(); err != nil {
		return livetv.Fetched{}, err
	}

	channels := make([]livetv.Channel, 0, len(entries))
	var failed int
	var firstErr error
	for i, e := range entries {
		chID := stableID(src.ID, e.meta.ID)
		ch := livetv.Channel{
			ID:       chID,
			SourceID: src.ID,
			GuideID:  e.meta.ID,
			Name:     strings.TrimSpace(firstNonEmpty(e.meta.Name, e.meta.ID)),
			Logo:     strings.TrimSpace(firstNonEmpty(e.meta.Logo, e.meta.Poster)),
			Group:    e.group,
			Kind:     "iptv",
			Enabled:  true,
		}
		if errs[i] != nil {
			// One channel's failed lookup does not fail the import. A
			// channel imported before keeps its last inputs (and so every
			// owner choice); a new one waits for the next refresh.
			failed++
			if firstErr == nil {
				firstErr = errs[i]
			}
			prior, ok := previous[chID]
			if !ok {
				continue
			}
			ch.StreamSources = addonInputsOf(prior.StreamSources)
		} else {
			ch.StreamSources = addonStreamInputs(chID, streams[i])
		}
		if len(ch.StreamSources) > 0 {
			ch.StreamURL = ch.StreamSources[0].URL
		}
		channels = append(channels, ch)
	}
	if failed == len(entries) {
		return livetv.Fetched{}, fmt.Errorf("the add-on's streams could not be read for any channel: %w", firstErr)
	}

	guide := livetv.XMLGuide{Channels: map[string]livetv.Channel{}, Names: map[string][]string{}}
	switch raw := strings.TrimSpace(addon.Manifest.GuideURL()); {
	case raw != "":
		guide, err = k.addonXMLTV(ctx, s, rec, *src, channels, raw)
	case addon.Manifest.BehaviorHints.EPGProvider:
		guide, err = addonNativeGuide(ctx, s, addon, read, channels)
	}
	if err != nil {
		return livetv.Fetched{}, err
	}
	return livetv.Fetched{Channels: channels, Guide: guide, AddonInputs: true}, nil
}

// readAddonChannels reads every `tv` catalog the add-on lists, up to
// stremio.MaxCatalogItems channels in all. Catalogs that need an extra the
// add-on gives no default for are skipped; any other failure fails the
// import, so a half-read add-on never drops channels.
func readAddonChannels(ctx context.Context, addon stremio.Addon) ([]addonEntry, []stremio.Catalog, error) {
	var entries []addonEntry
	var read []stremio.Catalog
	seen := map[string]bool{}
	for _, cat := range addon.Manifest.CatalogsOfType(stremio.TypeTV) {
		left := stremio.MaxCatalogItems - len(entries)
		if left <= 0 {
			break
		}
		if needsExtra(cat) {
			continue
		}
		metas, err := addon.Catalog(ctx, cat, nil, left)
		if err != nil {
			return nil, nil, fmt.Errorf("the add-on's %q channel list: %w", firstNonEmpty(cat.Name, cat.ID), err)
		}
		read = append(read, cat)
		for _, m := range metas {
			if m.ID == "" || seen[m.ID] {
				continue
			}
			seen[m.ID] = true
			entries = append(entries, addonEntry{meta: m, group: strings.TrimSpace(cat.Name)})
		}
	}
	if len(entries) == 0 {
		return nil, nil, errors.New("the add-on lists no TV channels")
	}
	return entries, read, nil
}

func needsExtra(c stremio.Catalog) bool {
	for _, e := range c.Extras() {
		if e.IsRequired && e.Name != "skip" {
			return true
		}
	}
	return false
}

// addonStreamInputs turns a channel's streams into its inputs: HTTP(S)
// URLs only, each once, at most livetv.MaxChannelInputs, with the request
// headers each stream asks for.
func addonStreamInputs(chID string, streams []stremio.Stream) []livetv.StreamSource {
	var out []livetv.StreamSource
	seen := map[string]bool{}
	for _, st := range streams {
		if len(out) == livetv.MaxChannelInputs {
			break
		}
		u := strings.TrimSpace(st.URL)
		if !httpURL(u) || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, livetv.StreamSource{
			ID:       livetv.AddonInputID(chID, len(out)),
			Name:     inputName(st),
			URL:      u,
			Enabled:  true,
			Priority: len(out),
			Headers:  livetv.CleanInputHeaders(st.BehaviorHints.ProxyHeaders.Request),
		})
	}
	return out
}

// addonInputsOf keeps the add-on's own inputs of a stored channel.
func addonInputsOf(inputs []livetv.StreamSource) []livetv.StreamSource {
	var out []livetv.StreamSource
	for _, in := range inputs {
		if strings.HasPrefix(in.ID, livetv.AddonInputPrefix) {
			out = append(out, in)
		}
	}
	return out
}

func httpURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// inputName is the stream's first labelled line, within livetv's limit.
func inputName(st stremio.Stream) string {
	for _, v := range []string{st.Name, st.Title, st.Description} {
		line, _, _ := strings.Cut(strings.TrimSpace(v), "\n")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for len(line) > addonInputNameMax {
			_, size := utf8.DecodeLastRuneInString(line)
			line = line[:len(line)-size]
		}
		return line
	}
	return ""
}

// addonNativeGuide reads the add-on's dated `tv` catalogs (those declaring
// a date extra) for each day of livetv's guide window. A day that fails is
// skipped; the guide fails only when every day did.
func addonNativeGuide(ctx context.Context, s *livetv.Service, addon stremio.Addon, read []stremio.Catalog, channels []livetv.Channel) (livetv.XMLGuide, error) {
	guide := livetv.XMLGuide{Channels: map[string]livetv.Channel{}, Names: map[string][]string{}}
	want := map[string]bool{}
	for _, c := range channels {
		want[c.GuideID] = true
	}
	from, until := s.GuideWindow()
	type page struct {
		catalog string
		day     string
	}
	var pages []page
	for _, cat := range read {
		if _, ok := cat.ExtraNamed("date"); !ok {
			continue
		}
		for day := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC); day.Before(until); day = day.AddDate(0, 0, 1) {
			pages = append(pages, page{catalog: cat.ID, day: day.Format("2006-01-02")})
		}
	}
	if len(pages) == 0 {
		return guide, nil
	}
	metas := make([][]stremio.Meta, len(pages))
	errs := make([]error, len(pages))
	forEachBounded(ctx, len(pages), addonGuideWorkers, func(i int) {
		metas[i], errs[i] = addon.GuideCatalog(ctx, pages[i].catalog, pages[i].day)
	})
	if err := ctx.Err(); err != nil {
		return guide, err
	}
	failed := 0
	for i := range pages {
		if errs[i] != nil {
			failed++
			continue
		}
		for _, meta := range metas[i] {
			if !want[meta.ID] {
				continue
			}
			guide.Channels[meta.ID] = livetv.Channel{GuideID: meta.ID, Name: meta.Name, Logo: meta.Logo}
			guide.Names[meta.ID] = []string{meta.Name}
			for _, v := range meta.Videos {
				if !v.EndTime.After(v.StartTime) || !v.StartTime.Before(until) || !v.EndTime.After(from) {
					continue
				}
				guide.Programmes = append(guide.Programmes, livetv.Programme{
					ID:        stableID(meta.ID, v.StartTime.UTC().Format(time.RFC3339Nano)),
					ChannelID: meta.ID,
					Start:     v.StartTime.UTC(),
					End:       v.EndTime.UTC(),
					Item: livetv.Item{
						Title:      strings.TrimSpace(firstNonEmpty(v.Title, v.Name)),
						Summary:    v.Overview,
						Thumb:      v.Thumbnail,
						DurationMS: v.EndTime.Sub(v.StartTime).Milliseconds(),
					},
				})
			}
		}
	}
	if failed == len(pages) {
		return guide, fmt.Errorf("the add-on's guide could not be read: %w", errs[0])
	}
	return guide, nil
}

// addonXMLTV downloads the XMLTV guide an add-on links. A relative link is
// resolved against the add-on's install link. Errors name only the host:
// the link can carry the add-on's configuration.
func (k *channelSourceKind) addonXMLTV(ctx context.Context, s *livetv.Service, rec plugins.Record, src livetv.Source, channels []livetv.Channel, raw string) (livetv.XMLGuide, error) {
	ref, err := url.Parse(raw)
	if err != nil {
		return livetv.XMLGuide{}, errors.New("the add-on's guide link is not valid")
	}
	var base *url.URL
	if rec.SourceURL != "" {
		base, _ = stremio.NormalizeURL(rec.SourceURL)
	}
	if !ref.IsAbs() {
		if base == nil {
			return livetv.XMLGuide{}, errors.New("the add-on's guide link is not valid")
		}
		ref = base.ResolveReference(ref)
	}
	if !httpURL(ref.String()) {
		return livetv.XMLGuide{}, errors.New("the add-on's guide link must use HTTP(S)")
	}
	fetchCtx := ctx
	if base != nil {
		if g, err := k.mgr.GrantsOf(ctx, rec.ID); err == nil && g.LAN {
			fetchCtx = safefetch.WithTrustedOriginFor(ctx, base.String(), ref.String())
		}
	}
	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, ref.String(), nil)
	if err != nil {
		return livetv.XMLGuide{}, errors.New("the add-on's guide link is not valid")
	}
	resp, err := addonGuideClient().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return livetv.XMLGuide{}, ctx.Err()
		}
		return livetv.XMLGuide{}, fmt.Errorf("the add-on's guide from %s could not be downloaded", ref.Host)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return livetv.XMLGuide{}, fmt.Errorf("the add-on's guide from %s answered HTTP %d", ref.Host, resp.StatusCode)
	}
	g, err := s.ParseSourceGuide(ctx, src, channels, &cappedReader{r: resp.Body, left: addonGuideMaxBytes})
	if err != nil {
		return livetv.XMLGuide{}, fmt.Errorf("the add-on's guide from %s: %w", ref.Host, err)
	}
	return g, nil
}

// cappedReader fails once more than left bytes are read.
type cappedReader struct {
	r    io.Reader
	left int64
}

var errGuideTooLarge = fmt.Errorf("the guide is larger than %d MB", addonGuideMaxBytes>>20)

func (c *cappedReader) Read(p []byte) (int, error) {
	if c.left < 0 {
		return 0, errGuideTooLarge
	}
	if int64(len(p)) > c.left+1 {
		p = p[:c.left+1]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	if c.left < 0 {
		return n, errGuideTooLarge
	}
	return n, err
}

// forEachBounded runs fn(0..n-1) with at most limit at once, stopping early
// when ctx ends.
func forEachBounded(ctx context.Context, n, limit int, fn func(i int)) {
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i)
		}(i)
	}
	wg.Wait()
}
