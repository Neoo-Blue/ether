package adapters_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/plugins/adapters"
	"github.com/braedonsaunders/ether/internal/plugins/remote"
	"github.com/braedonsaunders/ether/internal/store"
	"github.com/braedonsaunders/ether/internal/subtitles"
)

// Opt-in network conformance probe. All installed records, offers, selections
// and downloaded sidecars live in test-owned disposable storage. It never
// creates playback sessions or uses a viewer/Plex/account identity.
func TestLiveStremioSubtitleBroker(t *testing.T) {
	if os.Getenv("ETHER_LIVE_STREMIO_TEST") != "1" {
		t.Skip("set ETHER_LIVE_STREMIO_TEST=1 for public upstream checks")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr, err := plugins.New(plugins.Deps{DB: store.OpenTest(t), Registry: extend.New(), Logger: log})
	if err != nil {
		t.Fatal(err)
	}
	mgr.RegisterRuntime("remote", (&remote.Factory{Logger: log}).RuntimeFactory())
	svc := subtitles.New(subtitles.Options{CacheDir: t.TempDir(), Logger: log})
	adapters.AttachSubtitles(mgr, svc)
	preview, err := mgr.PreviewInstall(ctx, plugins.Source{Kind: "url", URL: "https://opensubtitles-v3.strem.io/manifest.json"})
	if err != nil {
		t.Fatalf("manifest fetch: %v", err)
	}
	rec, err := mgr.Install(ctx, preview, plugins.InstallOptions{})
	if err != nil {
		t.Fatalf("persisted import: %v", err)
	}
	title := &core.Title{Key: core.MovieKey(19), Type: core.TypeMovie, Title: "Metropolis", Year: 1927, IDs: core.IDs{TMDB: 19, IMDB: "tt0017136"}}
	candidates, err := svc.SearchOnDemand(ctx, title, "en", "")
	if err != nil {
		t.Fatalf("shared broker list: %v", err)
	}
	t.Logf("imported %s %s; shared broker returned %d English candidates", rec.ID, rec.Version, len(candidates))
	if len(candidates) == 0 {
		t.Fatal("upstream returned no usable English subtitle offers")
	}
	// The caller explicitly selects a listed offer. No download occurs on list.
	chosen := candidates[0]
	info, err := svc.DownloadOnDemand(ctx, title, chosen.FileID)
	if err != nil {
		t.Fatalf("explicit pick %s: %v", chosen.PluginSubtitleID, err)
	}
	id := strings.TrimPrefix(info.ExternalURL, "ether-subtitle:")
	reader, contentType, err := svc.Open(id)
	if err != nil {
		t.Fatalf("open normalized subtitle: %v", err)
	}
	defer reader.Close()
	body, err := io.ReadAll(io.LimitReader(reader, 2<<20))
	if err != nil || !strings.Contains(string(body), " --> ") {
		t.Fatalf("download not usable subtitle: bytes=%d err=%v", len(body), err)
	}
	cues, err := subtitles.ParseCues(string(body))
	if err != nil || len(cues) == 0 {
		t.Fatalf("subtitle cue parsing: %v", err)
	}
	evidence, _ := json.Marshal(map[string]any{"plugin": rec.ID, "version": rec.Version, "title": title.Title, "imdb": title.IDs.IMDB, "offers": len(candidates), "picked": chosen.PluginSubtitleID, "contentType": contentType, "bytes": len(body), "normalizedCueSyntax": true, "parsedCues": len(cues)})
	t.Log(string(evidence))
}

func TestLiveTorrentioNativeSourceMapping(t *testing.T) {
	if os.Getenv("ETHER_LIVE_STREMIO_TEST") != "1" {
		t.Skip("set ETHER_LIVE_STREMIO_TEST=1 for public upstream checks")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr, err := plugins.New(plugins.Deps{DB: store.OpenTest(t), Registry: extend.New(), Logger: log})
	if err != nil {
		t.Fatal(err)
	}
	factory := (&remote.Factory{Logger: log}).RuntimeFactory()
	mgr.RegisterRuntime("remote", func(inst plugins.Installed) (plugins.Runtime, error) {
		rt, err := factory(inst)
		if err != nil {
			return nil, err
		}
		return &recordedPublicRuntime{Runtime: rt, t: t}, nil
	})
	preview, err := mgr.PreviewInstall(ctx, plugins.Source{Kind: "url", URL: "https://torrentio.strem.fun/manifest.json"})
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if preview.Manifest.Ether != nil {
		t.Fatal("test requires unchanged ordinary manifest")
	}
	rec, err := mgr.Install(ctx, preview, plugins.InstallOptions{})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	source := adapters.NewStreamSources(nil)
	source.Bind(mgr, nil)
	releases, err := source.Search(ctx, core.SearchRequest{Type: core.TypeMovie, Title: "Metropolis", Year: 1927, IDs: core.IDs{IMDB: "tt0017136", TMDB: 19}})
	if err != nil {
		t.Fatalf("native source search: %v", err)
	}
	if len(releases) == 0 {
		t.Fatal("native source returned no mapped results")
	}
	protocols := map[string]int{}
	for _, r := range releases {
		protocols[r.Protocol]++
		if r.ID == "" || r.Indexer != rec.Name || r.TitleIDs.IMDB != "tt0017136" {
			t.Fatalf("invalid shared release: %+v", r)
		}
	}
	evidence, _ := json.Marshal(map[string]any{"plugin": rec.ID, "version": rec.Version, "nativeReleases": len(releases), "protocols": protocols, "playbackTested": false})
	t.Log(string(evidence))
	// Resource discovery only. No torrent/debrid/file or playback session is opened.
}

// Only used by this credential-free public endpoint test. Retain exact request,
// response status and a content digest without logging headers or HTML tokens.
type recordedPublicRuntime struct {
	plugins.Runtime
	t *testing.T
}

func (r *recordedPublicRuntime) Call(ctx context.Context, req plugins.Request) (plugins.Response, error) {
	response, err := r.Runtime.Call(ctx, req)
	evidence, _ := json.Marshal(map[string]any{"at": time.Now().UTC().Format(time.RFC3339Nano), "method": req.Method, "url": "https://torrentio.strem.fun" + req.Path, "query": req.Query, "status": response.Status, "responseBytes": len(response.Body), "responseSHA256": fmt.Sprintf("%x", sha256.Sum256(response.Body)), "jsonResponse": json.Valid(response.Body)})
	r.t.Log(string(evidence))
	return response, err
}
