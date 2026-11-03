package adapters_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/plugins/adapters"
	"github.com/braedonsaunders/ether/internal/store"
	"github.com/braedonsaunders/ether/internal/subtitles"
)

// subtitleAddon is a plain Stremio subtitle add-on: its listing answers
// through a fake runtime, its files from files (fetched only on a pick).
type subtitleAddon struct {
	mgr       *plugins.Manager
	rt        *listChannelFakeRuntime
	svc       *subtitles.Service
	rec       plugins.Record
	files     *httptest.Server
	downloads atomic.Int32
}

func newSubtitleAddon(t *testing.T, listing func(req plugins.Request, files string) string) *subtitleAddon {
	t.Helper()
	a := &subtitleAddon{rt: &listChannelFakeRuntime{routes: map[string]func(plugins.Request) (plugins.Response, error){}}}
	a.files = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.downloads.Add(1)
		fmt.Fprint(w, "1\n00:00:01,000 --> 00:00:03,000\nHello from the add-on.\n\n")
	}))
	t.Cleanup(a.files.Close)
	mgr, err := plugins.New(plugins.Deps{DB: store.OpenTest(t), Registry: extend.New(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		IsLeader: func() bool { return true }, Now: func() time.Time { return time.Now().UTC() }, FetchClient: a.files.Client()})
	if err != nil {
		t.Fatal(err)
	}
	a.mgr = mgr
	mgr.RegisterRuntime("remote", func(plugins.Installed) (plugins.Runtime, error) { return a.rt, nil })
	a.rt.routes["/subtitles/"] = func(req plugins.Request) (plugins.Response, error) {
		return plugins.Response{Status: 200, Body: json.RawMessage(listing(req, a.files.URL))}, nil
	}
	raw := json.RawMessage(`{"id":"org.subsense.addon","name":"SubSense","version":"1.2.0","types":["movie","series"],"idPrefixes":["tt"],"resources":["subtitles"]}`)
	ctx := context.Background()
	preview, err := mgr.PreviewInstall(ctx, plugins.Source{Kind: "url", URL: "https://subs.example/secret-cfg/manifest.json", Manifest: raw})
	if err != nil {
		t.Fatal(err)
	}
	if a.rec, err = mgr.Install(ctx, preview, plugins.InstallOptions{}); err != nil {
		t.Fatal(err)
	}
	kind := extend.PluginID(a.rec.ID, "subtitles")
	a.svc = subtitles.New(subtitles.Options{CacheDir: t.TempDir(), Providers: []config.SubtitleProvider{{ID: kind, Kind: kind, Enabled: true}}})
	adapters.AttachSubtitles(mgr, a.svc)
	return a
}

func TestAddonSubtitlesRankByHashAndReleaseAndDownloadOnlyOnPick(t *testing.T) {
	a := newSubtitleAddon(t, func(_ plugins.Request, files string) string {
		return fmt.Sprintf(`{"subtitles":[
			{"id":"sub-1","url":"%[1]s/files/My.Movie.2024.1080p.WEB-DL-GRP.en.srt?token=secret","lang":"eng"},
			{"id":"sub-2","url":"%[1]s/files/a1b2c3.srt","lang":"eng","SubFileName":"Other.Release.720p.en.srt","hashMatch":true},
			{"id":"sub-fr","url":"%[1]s/files/My.Movie.2024.fr.srt","lang":"fre"}
		]}`, files)
	})
	ctx := context.Background()
	title := &core.Title{Key: core.MovieKey(550), Type: core.TypeMovie, Title: "My Movie", IDs: core.IDs{IMDB: "tt0137523", TMDB: 550}}
	// A moviehash already known from cached ranges is sent with the size.
	a.svc.RememberVideoHash(title.Key, "My.Movie.2024.1080p.WEB-DL-GRP", "8e245d9679d31e12", 1468006400)
	cands, err := a.svc.SearchOnDemand(ctx, title, "en", "My.Movie.2024.1080p.WEB-DL-GRP")
	if err != nil || len(cands) != 2 {
		t.Fatalf("candidates = %+v, err %v; want the two English offers", cands, err)
	}
	paths := strings.Join(a.rt.paths(), " ")
	if !strings.Contains(paths, "/subtitles/movie/tt0137523/videoHash=8e245d9679d31e12&videoSize=1468006400.json") {
		t.Fatalf("listing route = %s", paths)
	}
	if a.downloads.Load() != 0 {
		t.Fatal("a subtitle was downloaded while listing")
	}
	// The add-on's hash match ranks first, then the release-name match.
	if !cands[0].HashMatch || cands[0].FileName != "Other.Release.720p.en.srt" || cands[1].Release != "My.Movie.2024.1080p.WEB-DL-GRP.en" {
		t.Fatalf("ranking = %+v", cands)
	}
	for _, c := range cands {
		if strings.Contains(c.FileName+c.Release, "secret") || c.Provider != "SubSense" {
			t.Fatalf("offer %+v leaks the link's query or misnames the add-on", c)
		}
	}
	if _, err := a.svc.DownloadOnDemand(ctx, title, 999999); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("unoffered pick err = %v", err)
	}
	stream, err := a.svc.DownloadOnDemand(ctx, title, cands[1].FileID)
	if err != nil || a.downloads.Load() != 1 {
		t.Fatalf("pick: %v, downloads %d", err, a.downloads.Load())
	}
	rc, format, err := a.svc.OpenFormat(strings.TrimPrefix(stream.ExternalURL, subtitles.URLPrefix), "vtt")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	body, _ := io.ReadAll(rc)
	if format != "vtt" || !strings.Contains(string(body), "Hello from the add-on.") {
		t.Fatalf("vtt = %q", body)
	}
}

func TestAddonSubtitlesAskByTheIDsTheAddonTakes(t *testing.T) {
	a := newSubtitleAddon(t, func(plugins.Request, string) string {
		return `{"subtitles":[{"id":"ep1","url":"https://subs.example/s01e02.srt","lang":"eng"}]}`
	})
	ctx := context.Background()
	ep := &core.Title{Key: core.EpisodeKey(1396, 1, 2), Type: core.TypeEpisode, ShowIDs: core.IDs{IMDB: "tt0903747", TMDB: 1396}, Season: 1, Episode: 2}
	if cands, err := a.svc.SearchOnDemand(ctx, ep, "en", ""); err != nil || len(cands) != 1 {
		t.Fatalf("episode = %+v, %v", cands, err)
	}
	if got := a.rt.paths(); len(got) != 1 || got[0] != "/subtitles/series/tt0903747:1:2.json" {
		t.Fatalf("episode route = %v", got)
	}
	// The add-on takes IMDb ids only: a title known only by TMDB is not asked.
	a.rt.resetCalls()
	if cands, err := a.svc.SearchOnDemand(ctx, &core.Title{Key: core.MovieKey(7), Type: core.TypeMovie, IDs: core.IDs{TMDB: 7}}, "en", ""); err != nil || len(cands) != 0 || len(a.rt.paths()) != 0 {
		t.Fatalf("tmdb-only title: %+v %v, calls %v", cands, err, a.rt.paths())
	}
}
