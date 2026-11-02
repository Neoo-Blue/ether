package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/subtitles"
)

func TestPlexStremioSubtitleSearchDoesNotDownload(t *testing.T) {
	var downloads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/subtitles/") {
			fmt.Fprintf(w, `{"subtitles":[{"id":"one","lang":"eng","url":"http://%s/file.srt?token=secret"}]}`, r.Host)
			return
		}
		downloads.Add(1)
		fmt.Fprint(w, "subtitle")
	}))
	defer srv.Close()
	svc := subtitles.New(subtitles.Options{CacheDir: t.TempDir(), StremioHTTP: srv.Client(), Providers: []config.SubtitleProvider{{ID: "s", Kind: "stremio", Name: "Example", URL: srv.URL + "/cfg/manifest.json", Enabled: true}}})
	p := plexSubtitles{a: &App{subtitles: svc}}
	offers, err := p.SearchSubtitles(context.Background(), &core.Title{Key: core.MovieKey(1), Type: core.TypeMovie, IDs: core.IDs{IMDB: "tt1234567"}}, nil, "en", "")
	if err != nil || len(offers) != 1 || offers[0].Provider != "Example" || strings.Contains(offers[0].Title, "secret") || downloads.Load() != 0 {
		t.Fatalf("offers=%+v downloads=%d err=%v", offers, downloads.Load(), err)
	}
}
