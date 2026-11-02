package subtitles

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
)

func TestStremioSubtitleProviderSearchRankAndPickOnlyDownload(t *testing.T) {
	var fileDownloads atomic.Int32
	var lastSubPath atomic.Value

	mux := http.NewServeMux()
	// SubSense-like Stremio subtitle add-on fixture (secret config segment in path).
	mux.HandleFunc("/subsense-secret-cfg/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"org.subsense.addon","name":"SubSense","version":"1.2.0","types":["movie","series"],"idPrefixes":["tt"],"resources":["subtitles"]}`)
	})
	mux.HandleFunc("/subsense-secret-cfg/subtitles/", func(w http.ResponseWriter, r *http.Request) {
		lastSubPath.Store(r.URL.Path)
		fileBase := "http://" + r.Host + "/files"
		fmt.Fprintf(w, `{"subtitles":[
			{"id":"sub-1","url":"%s/My.Movie.2024.1080p.WEB-DL-GRP.en.srt?token=secret","lang":"eng"},
			{"id":"sub-2","url":"%s/Other.Release.720p.en.srt","lang":"eng"},
			{"id":"sub-fr","url":"%s/My.Movie.2024.fr.srt","lang":"fre"}
		]}`, fileBase, fileBase, fileBase)
	})
	mux.HandleFunc("/files/", func(w http.ResponseWriter, r *http.Request) {
		fileDownloads.Add(1)
		fmt.Fprint(w, "1\n00:00:01,000 --> 00:00:03,000\nHello from SubSense.\n\n")
	})
	// OpenSubtitles fake on the same test server.
	mux.HandleFunc("/os/subtitles", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"attributes":{
			"language":"en","download_count":100,"ratings":8.0,
			"release":"My.Movie.2024.720p.BluRay",
			"uploader":{"name":"tester","rank":"silver"},
			"files":[{"file_id":42,"file_name":"My.Movie.2024.720p.BluRay.srt"}]
		}}]}`)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	svc := New(Options{
		Config:      config.OpenSubtitles{Enabled: true, APIKey: "os-key"},
		BaseURL:     srv.URL + "/os",
		HTTPClient:  srv.Client(),
		StremioHTTP: srv.Client(),
		CacheDir:    t.TempDir(),
		Providers: []config.SubtitleProvider{
			{
				ID:      "subsense",
				Kind:    config.SubtitleProviderStremio,
				Name:    "SubSense",
				URL:     srv.URL + "/subsense-secret-cfg/manifest.json",
				Enabled: true,
			},
			{
				ID:      "opensubtitles",
				Kind:    config.SubtitleProviderOpenSubtitles,
				Enabled: true,
			},
		},
	})

	title := &core.Title{
		Key:   core.MovieKey(550),
		Type:  core.TypeMovie,
		Title: "My Movie",
		IDs:   core.IDs{IMDB: "tt0137523", TMDB: 550},
	}

	// Remember a movieHash from cached ranges (as Enrich does on local files)
	// and verify SearchOnDemand sends videoHash and videoSize to the add-on.
	svc.RememberVideoHash(title.Key, "My.Movie.2024.1080p.WEB-DL-GRP", "8e245d9679d31e12", 1468006400)

	ctx := context.Background()
	cands, err := svc.SearchOnDemand(ctx, title, "en", "My.Movie.2024.1080p.WEB-DL-GRP")
	if err != nil {
		t.Fatalf("SearchOnDemand: %v", err)
	}
	// 2 English from SubSense + 1 English from OpenSubtitles (French filtered out).
	if len(cands) != 3 {
		t.Fatalf("candidates = %d (%+v), want 3", len(cands), cands)
	}
	gotPath, _ := lastSubPath.Load().(string)
	if !strings.Contains(gotPath, "/subtitles/movie/tt0137523/") ||
		!strings.Contains(gotPath, "videoHash=8e245d9679d31e12") ||
		!strings.Contains(gotPath, "videoSize=1468006400") {
		t.Fatalf("stremio request path = %q, want movie/tt0137523 with videoHash and videoSize", gotPath)
	}

	// Listing is a search: no subtitle file may be downloaded during SearchOnDemand.
	if n := fileDownloads.Load(); n != 0 {
		t.Fatalf("fileDownloads after search = %d, want 0", n)
	}

	// Find the exact release-matching Stremio candidate.
	var stremioCand Candidate
	foundOpenSubtitles := false
	for _, c := range cands {
		if strings.Contains(c.FileName, "secret") || strings.Contains(c.Release, "secret") {
			t.Fatal("subtitle URL query leaked into the offer")
		}
		if c.Provider == "SubSense" && strings.Contains(c.Release, "1080p.WEB-DL-GRP") {
			stremioCand = c
		}
		if c.Provider == "OpenSubtitles" && c.FileID == 42 {
			foundOpenSubtitles = true
		}
	}
	if stremioCand.FileID == 0 || !foundOpenSubtitles {
		t.Fatalf("missing expected providers in %+v", cands)
	}

	// Unoffered FileID must be refused (pick token / offer check).
	if _, err := svc.DownloadOnDemand(ctx, title, 999999); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("unoffered DownloadOnDemand err = %v, want ErrNotFound", err)
	}
	if n := fileDownloads.Load(); n != 0 {
		t.Fatalf("fileDownloads after rejected pick = %d, want 0", n)
	}

	// Picking the Stremio candidate downloads, normalises and caches it.
	stream, err := svc.DownloadOnDemand(ctx, title, stremioCand.FileID)
	if err != nil {
		t.Fatalf("DownloadOnDemand: %v", err)
	}
	if n := fileDownloads.Load(); n != 1 {
		t.Fatalf("fileDownloads after pick = %d, want 1", n)
	}
	if !strings.HasPrefix(stream.ExternalURL, URLPrefix) {
		t.Fatalf("stream.ExternalURL = %q", stream.ExternalURL)
	}

	rc, format, err := svc.OpenFormat(stream.ExternalURL, "vtt")
	if err != nil {
		t.Fatalf("OpenFormat vtt: %v", err)
	}
	defer rc.Close()
	body, _ := io.ReadAll(rc)
	if format != "vtt" || !strings.Contains(string(body), "Hello from SubSense.") {
		t.Fatalf("vtt body = %q", string(body))
	}
}

func TestStremioEpisodeSubtitleLookupWithoutOpenSubtitles(t *testing.T) {
	var requestedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		fmt.Fprint(w, `{"subtitles":[{"id":"ep1","url":"https://subs.example/s01e02.srt","lang":"eng"}]}`)
	}))
	t.Cleanup(srv.Close)

	// OpenSubtitles disabled; Stremio-only subtitle setup must still report Enabled().
	svc := New(Options{
		Config:      config.OpenSubtitles{Enabled: false},
		StremioHTTP: srv.Client(),
		CacheDir:    t.TempDir(),
	})
	if svc.Enabled() {
		t.Fatal("service with no providers should be disabled")
	}
	svc.UpdateProviders([]config.SubtitleProvider{
		{ID: "sub1", Kind: config.SubtitleProviderStremio, Name: "SubAddon", URL: srv.URL + "/cfg/manifest.json", Enabled: true},
	})
	if !svc.Enabled() {
		t.Fatal("service with Stremio provider should be enabled")
	}

	ep := &core.Title{
		Key:     core.EpisodeKey(1396, 1, 2),
		Type:    core.TypeEpisode,
		ShowIDs: core.IDs{IMDB: "tt0903747", TMDB: 1396},
		Season:  1,
		Episode: 2,
	}
	cands, err := svc.SearchOnDemand(context.Background(), ep, "en", "")
	if err != nil || len(cands) != 1 {
		t.Fatalf("SearchOnDemand episode = %+v, err=%v", cands, err)
	}
	if requestedPath != "/cfg/subtitles/series/tt0903747:1:2.json" {
		t.Fatalf("requestedPath = %q, want /cfg/subtitles/series/tt0903747:1:2.json", requestedPath)
	}
}
