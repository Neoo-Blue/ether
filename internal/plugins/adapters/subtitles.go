package adapters

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/langtag"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/stremio"
	"github.com/braedonsaunders/ether/internal/subtitles"
)

type subtitleBridge struct {
	mgr *plugins.Manager
	svc *subtitles.Service
}

func AttachSubtitles(mgr *plugins.Manager, svc *subtitles.Service) {
	if mgr == nil || svc == nil {
		return
	}
	b := &subtitleBridge{mgr, svc}
	mgr.OnSync(b.sync)
}
func (b *subtitleBridge) sync(_ context.Context, r plugins.Record) {
	b.svc.UnregisterPluginProvider(r.ID)
	if r.Enabled && r.Manifest.HasCapability(plugins.CapSubtitles) {
		b.svc.RegisterPluginProvider(&subtitleProvider{b.mgr, r.ID, r.Name})
	}
}

type subtitleProvider struct {
	mgr      *plugins.Manager
	id, name string
}

func (p *subtitleProvider) ID() string { return p.id }

// contentID is a title's protocol content id (stremio.TitleID), an
// episode's season and number read from its key when missing. imdb reports
// an id led by an IMDb id.
func contentID(t *core.Title) (typ, id string, imdb, ok bool) {
	tt := *t
	if tt.Type == core.TypeEpisode && tt.Season <= 0 && tt.Key.Kind() == core.KindEpisode {
		tt.Season, tt.Episode = tt.Key.Season(), tt.Key.Episode()
	}
	if tt.Type == core.TypeEpisode && (tt.Season <= 0 || tt.Episode <= 0) {
		return "", "", false, false
	}
	if typ, id, ok = stremio.TitleID(&tt); !ok {
		return "", "", false, false
	}
	return typ, id, stremio.IMDbID(strings.SplitN(id, ":", 2)[0]) != "", true
}

// subtitleLookup is how to ask a provider for a title's subtitles. A plugin
// is asked by IMDb id. A plain add-on is asked only for ids its subtitles
// resource takes, and by a non-IMDb id only when it declares id prefixes.
func subtitleLookup(m stremio.Manifest, plain bool, t *core.Title) (typ, id string, ok bool) {
	typ, id, imdb, ok := contentID(t)
	if !ok {
		return "", "", false
	}
	if !plain {
		return typ, id, imdb
	}
	r, declared := m.Resource(stremio.ResourceSubtitles)
	if !declared || !m.TakesID(stremio.ResourceSubtitles, typ, id) || (!imdb && len(r.IDPrefixes) == 0) {
		return "", "", false
	}
	return typ, id, true
}

// Search lists the provider's subtitles for one title and language. It is
// a listing only: nothing is downloaded until a viewer picks one. The
// moviehash and size are sent when the caller already has them, and an
// offer the add-on marks as a hash match (or whose id, link or file name
// carries the hash) ranks above the rest.
func (p *subtitleProvider) Search(ctx context.Context, t *core.Title, lang, hash string, size int64) ([]subtitles.Candidate, error) {
	record, err := p.mgr.Get(ctx, p.id)
	if err != nil {
		return nil, err
	}
	plain := record.Manifest.IsStremioOnly()
	addon := p.mgr.Addon(record)
	typ, id, ok := subtitleLookup(addon.Manifest, plain, t)
	if !ok {
		return nil, nil
	}
	extra := map[string]string{}
	if hash != "" {
		extra["videoHash"] = hash
	}
	if size > 0 {
		extra["videoSize"] = strconv.FormatInt(size, 10)
	}
	fetcher := &healthFetcher{inner: addon.Fetcher}
	addon.Fetcher = fetcher
	subs, err := addon.Subtitles(ctx, typ, id, extra)
	if err != nil {
		if !fetcher.recorded {
			p.mgr.RecordFailure(ctx, p.id, "subtitle search: "+err.Error())
		}
		return nil, err
	}
	hash = strings.ToLower(hash)
	var out []subtitles.Candidate
	for _, sub := range subs {
		l := langtag.Normalize(sub.Lang)
		if l == "" || langtag.MatchTier(lang, l) == 0 {
			continue
		}
		// A plain add-on's file is fetched from its link; a plugin can
		// also serve it by id.
		if sub.URL == "" && (plain || sub.ID == "") {
			continue
		}
		file := subtitleFileName(sub)
		h := fnv.New64a()
		_, _ = h.Write([]byte(p.id + "\x00" + sub.ID + "\x00" + sub.URL))
		out = append(out, subtitles.Candidate{
			FileID:   int64(1)<<62 | int64(h.Sum64()&((1<<62)-1)),
			FileName: file, Language: l, Release: strings.TrimSuffix(file, path.Ext(file)),
			HashMatch: sub.HashMatch || (hash != "" && strings.Contains(strings.ToLower(sub.ID+" "+sub.URL+" "+file), hash)),
			Provider:  p.name, ProviderID: p.id, PluginID: p.id, PluginSubtitleID: sub.ID, PluginFormat: sub.Format, DownloadURL: sub.URL,
		})
	}
	return out, nil
}

// subtitleFileName names an offer after its file: the add-on's filename,
// else the last path segment of its name or link (never the link's query,
// which may carry a token).
func subtitleFileName(sub stremio.Subtitle) string {
	file := firstNonEmpty(sub.Filename, sub.SubFileName, sub.Name)
	if file == "" {
		if u, err := url.Parse(sub.URL); err == nil {
			file = u.Path
		}
	}
	file = path.Base(strings.SplitN(strings.SplitN(file, "?", 2)[0], "#", 2)[0])
	if file == "." || file == "/" || file == "" {
		file = "Subtitle"
		if sub.ID != "" {
			file += " " + sub.ID
		}
		if format := strings.TrimPrefix(sub.Format, "."); format != "" {
			file += "." + format
		}
	}
	return file
}

type pickKey struct{}
type picked struct{ token, title string }

func (p *subtitleProvider) Pick(ctx context.Context, t *core.Title, c subtitles.Candidate) (context.Context, error) {
	title := t.Key.String()
	token, err := p.mgr.MintSubtitleURLPick(p.id, title, c.PluginSubtitleID, c.DownloadURL)
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, pickKey{}, picked{token, title}), nil
}
func (p *subtitleProvider) Download(ctx context.Context, c subtitles.Candidate) ([]byte, error) {
	pick, ok := ctx.Value(pickKey{}).(picked)
	if !ok {
		return nil, plugins.ErrPermissionDenied
	}
	if c.DownloadURL != "" {
		return p.mgr.FetchPickedSubtitle(ctx, p.id, pick.title, c.PluginSubtitleID, pick.token, c.DownloadURL)
	}
	body, _ := json.Marshal(map[string]string{"id": c.PluginSubtitleID, "lang": c.Language, "format": c.PluginFormat})
	resp, err := p.mgr.Call(ctx, p.id, plugins.Request{Method: "POST", Path: "/ether/subtitles/download", Headers: map[string]string{"X-Ether-Pick": pick.token, "X-Ether-Title": pick.title}, Body: body})
	if err != nil {
		return nil, err
	}
	if resp.Status >= 400 {
		p.mgr.RecordFailure(ctx, p.id, fmt.Sprintf("subtitle download HTTP %d", resp.Status))
		return nil, fmt.Errorf("subtitle download HTTP %d", resp.Status)
	}
	var result struct {
		Format   string `json:"format"`
		Encoding string `json:"encoding"`
		Data     string `json:"data"`
	}
	if json.Unmarshal(resp.Body, &result) != nil {
		p.mgr.RecordFailure(ctx, p.id, "invalid subtitle download response")
		return nil, fmt.Errorf("invalid subtitle download response")
	}
	var b []byte
	switch result.Encoding {
	case "utf8":
		b = []byte(result.Data)
	case "base64":
		b, err = base64.StdEncoding.DecodeString(result.Data)
	default:
		err = fmt.Errorf("invalid subtitle encoding")
	}
	if err != nil {
		p.mgr.RecordFailure(ctx, p.id, "invalid subtitle encoding")
		return nil, err
	}
	if len(b) > 2<<20 {
		p.mgr.RecordFailure(ctx, p.id, "subtitle exceeds 2 MiB")
		return nil, fmt.Errorf("subtitle exceeds 2 MiB")
	}
	return b, nil
}
