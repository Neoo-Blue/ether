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
	if r.Enabled && r.Manifest.Ether != nil && r.Manifest.HasCapability(plugins.CapSubtitles) {
		b.svc.RegisterPluginProvider(&subtitleProvider{b.mgr, r.ID, r.Name})
	}
}

type subtitleProvider struct {
	mgr      *plugins.Manager
	id, name string
}

func (p *subtitleProvider) ID() string { return p.id }
func subtitleIdentity(t *core.Title) (string, string) {
	if t.Type == core.TypeEpisode {
		imdb := t.ShowIDs.IMDB
		if imdb == "" {
			imdb = t.IDs.IMDB
		}
		season, ep := t.Season, t.Episode
		if season == 0 {
			season, ep = t.Key.Season(), t.Key.Episode()
		}
		if imdb == "" || season <= 0 || ep <= 0 {
			return "", ""
		}
		return "series", fmt.Sprintf("%s:%d:%d", imdb, season, ep)
	}
	if t.IDs.IMDB == "" {
		return "", ""
	}
	return "movie", t.IDs.IMDB
}
func (p *subtitleProvider) Search(ctx context.Context, t *core.Title, lang, hash string, size int64) ([]subtitles.Candidate, error) {
	typ, id := subtitleIdentity(t)
	if id == "" {
		return nil, nil
	}
	extra := url.Values{}
	if hash != "" {
		extra.Set("videoHash", hash)
	}
	if size > 0 {
		extra.Set("videoSize", strconv.FormatInt(size, 10))
	}
	route := "/subtitles/" + url.PathEscape(typ) + "/" + url.PathEscape(id)
	if len(extra) > 0 {
		route += "/" + extra.Encode()
	}
	route += ".json"
	resp, err := p.mgr.Call(ctx, p.id, plugins.Request{Method: "GET", Path: route})
	if err != nil {
		return nil, err
	}
	if resp.Status >= 400 {
		p.mgr.RecordFailure(ctx, p.id, fmt.Sprintf("subtitle search HTTP %d", resp.Status))
		return nil, fmt.Errorf("subtitle search HTTP %d", resp.Status)
	}
	var result struct {
		Subtitles *[]struct {
			ID     string `json:"id"`
			Lang   string `json:"lang"`
			Format string `json:"format"`
			Name   string `json:"name"`
			URL    string `json:"url"`
		} `json:"subtitles"`
	}
	if err := json.Unmarshal(resp.Body, &result); err != nil || result.Subtitles == nil {
		p.mgr.RecordFailure(ctx, p.id, "invalid subtitle search response")
		return nil, fmt.Errorf("invalid subtitle search response")
	}
	var out []subtitles.Candidate
	for _, sub := range *result.Subtitles {
		l := langtag.Normalize(sub.Lang)
		if sub.ID == "" || l == "" || langtag.MatchTier(lang, l) == 0 {
			continue
		}
		h := fnv.New64a()
		_, _ = h.Write([]byte(p.id + "\x00" + sub.ID))
		file := path.Base(sub.Name)
		if file == "." || file == "/" || file == "" {
			file = "subtitle." + strings.TrimPrefix(sub.Format, ".")
		}
		out = append(out, subtitles.Candidate{FileID: int64(1)<<62 | int64(h.Sum64()&((1<<62)-1)), FileName: file, Language: l, Release: strings.TrimSuffix(file, path.Ext(file)), Provider: p.name, ProviderID: p.id, PluginID: p.id, PluginSubtitleID: sub.ID, PluginFormat: sub.Format, DownloadURL: sub.URL})
	}
	return out, nil
}

type pickKey struct{}
type picked struct{ token, title string }

func (p *subtitleProvider) Pick(ctx context.Context, t *core.Title, c subtitles.Candidate) (context.Context, error) {
	title := t.Key.String()
	token, err := p.mgr.MintSubtitlePick(p.id, title, c.PluginSubtitleID)
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
		return nil, err
	}
	if len(b) > 2<<20 {
		return nil, fmt.Errorf("subtitle exceeds 2 MiB")
	}
	return b, nil
}
