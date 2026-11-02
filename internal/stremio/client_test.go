package stremio

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCatalogPaginatesShortPages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "skip=1"):
			fmt.Fprint(w, `{"metas":[{"id":"tt0000002"}]}`)
		case strings.Contains(r.URL.Path, "skip=2"):
			fmt.Fprint(w, `{"metas":[]}`)
		default:
			fmt.Fprint(w, `{"metas":[{"id":"tt0000001"}]}`)
		}
	}))
	defer srv.Close()
	c, err := NewClient(srv.URL+"/manifest.json", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	items, err := c.Catalog(context.Background(), "movie", "test", nil, 10)
	if err != nil || len(items) != 2 {
		t.Fatalf("items=%v err=%v", items, err)
	}
}

// Fixture add-ons modelled on real shapes (Cinemeta-like catalog, a tv
// add-on, a SubSense-like subtitle add-on), keys removed.
func fixtureAddon(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/cfg123/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"org.example.all","name":"Example","version":"1.0.0","types":["movie","series","tv"],
"idPrefixes":["tt","tmdb:"],"resources":["catalog","stream","meta","subtitles"],
"catalogs":[{"type":"movie","id":"top","name":"Top","extra":[{"name":"skip"}]},
{"type":"tv","id":"live","name":"Live","extra":[{"name":"skip"},{"name":"genre","options":["News"]}]}]}`)
	})
	mux.HandleFunc("/cfg123/catalog/movie/top.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"metas":[{"id":"tt0111161","type":"movie","name":"The Shawshank Redemption"},
{"id":"tmdb:550","type":"movie","name":"Fight Club"},{"id":"kitsu:99","type":"movie","name":"Unknown"}]}`)
	})
	mux.HandleFunc("/cfg123/catalog/tv/", func(w http.ResponseWriter, r *http.Request) {
		// Paged: 25 per page via skip extra segment.
		skip := 0
		if strings.Contains(r.URL.Path, "skip=") {
			fmt.Sscanf(r.URL.Path[strings.Index(r.URL.Path, "skip="):], "skip=%d", &skip)
		}
		var b strings.Builder
		b.WriteString(`{"metas":[`)
		for i := skip; i < skip+25 && i < 30; i++ {
			if i > skip {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `{"id":"ch-%d","type":"tv","name":"Channel %d","logo":"https://img.example/%d.png","genres":["News"]}`, i, i, i)
		}
		b.WriteString(`]}`)
		fmt.Fprint(w, b.String())
	})
	mux.HandleFunc("/cfg123/stream/tv/ch-1.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"streams":[{"name":"HD","url":"https://cdn.example/ch1.m3u8"},{"name":"SD","url":"https://cdn.example/ch1-sd.m3u8"}]}`)
	})
	mux.HandleFunc("/cfg123/meta/tv/ch-1.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"meta":{"id":"ch-1","type":"tv","name":"Channel 1","videos":[
{"id":"v1","title":"Morning News","released":"2026-11-01T08:00:00Z","overview":"News"},
{"id":"v2","title":"Midday","released":"2026-11-01T09:00:00Z"}]}}`)
	})
	mux.HandleFunc("/cfg123/subtitles/movie/tt0111161/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "videoHash=abc123") {
			t.Errorf("subtitle path missing hash: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"subtitles":[{"id":"1","url":"https://subs.example/en.srt","lang":"eng"},{"id":"2","url":"https://subs.example/fr.srt","lang":"fre"}]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestClientCatalogTVSubtitles(t *testing.T) {
	srv := fixtureAddon(t)
	c, err := NewClient(srv.URL+"/cfg123/manifest.json", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	m, err := c.Manifest(ctx)
	if err != nil || !m.HasResource("catalog", "tv") || !m.HasResource("subtitles", "movie") {
		t.Fatalf("manifest = %+v err=%v", m, err)
	}
	if got := m.CatalogsOfType("tv"); len(got) != 1 || !got[0].SupportsSkip() {
		t.Fatalf("tv catalogs = %+v", got)
	}
	metas, err := c.Catalog(ctx, "movie", "top", nil, 100)
	if err != nil || len(metas) != 3 {
		t.Fatalf("catalog = %v err=%v", metas, err)
	}
	if imdb, tmdb := ParseCatalogRef(metas[0].ID); imdb != "tt0111161" || tmdb != 0 {
		t.Fatalf("ref0 = %q %d", imdb, tmdb)
	}
	if imdb, tmdb := ParseCatalogRef(metas[1].ID); imdb != "" || tmdb != 550 {
		t.Fatalf("ref1 = %q %d", imdb, tmdb)
	}
	if imdb, tmdb := ParseCatalogRef(metas[2].ID); imdb != "" || tmdb != 0 {
		t.Fatalf("ref2 should be unmatched, got %q %d", imdb, tmdb)
	}
	tv, err := c.Catalog(ctx, "tv", "live", nil, 2000)
	if err != nil || len(tv) != 30 {
		t.Fatalf("tv catalog = %d err=%v", len(tv), err)
	}
	streams, err := c.Streams(ctx, "tv", "ch-1")
	if err != nil || len(streams) != 2 || streams[0].URL == "" {
		t.Fatalf("streams = %+v err=%v", streams, err)
	}
	meta, err := c.Meta(ctx, "tv", "ch-1")
	if err != nil || len(meta.Videos) != 2 || meta.Videos[0].Released.IsZero() {
		t.Fatalf("meta = %+v err=%v", meta, err)
	}
	subs, err := c.Subtitles(ctx, "movie", "tt0111161", map[string]string{"videoHash": "abc123", "videoSize": "12345"})
	if err != nil || len(subs) != 2 || subs[0].Lang != "eng" {
		t.Fatalf("subs = %+v err=%v", subs, err)
	}
	if c.Host() == "" || strings.Contains(c.Host(), "cfg123") {
		t.Fatalf("host leaks path: %q", c.Host())
	}
}

func TestCatalogMaxItemsClamp(t *testing.T) {
	srv := fixtureAddon(t)
	c, _ := NewClient(srv.URL+"/cfg123", srv.Client())
	metas, err := c.Catalog(context.Background(), "tv", "live", nil, 10)
	if err != nil || len(metas) != 10 {
		t.Fatalf("metas = %d err=%v", len(metas), err)
	}
}

type errorTransport struct{}

func (errorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("proxy failed for /SECRET/manifest.json")
}
func TestAddonErrorsDoNotExposeConfiguredPath(t *testing.T) {
	c, err := NewClient("https://addon.example/SECRET/manifest.json", &http.Client{Transport: errorTransport{}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Manifest(context.Background())
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func TestAddonClientPreservesConfigurationQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "secret" {
			t.Error("configuration query missing")
		}
		fmt.Fprint(w, `{"id":"example","resources":["catalog"]}`)
	}))
	defer srv.Close()
	c, err := NewClient(srv.URL+"/manifest.json?key=secret", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Manifest(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestManifestRedirectDoesNotLeakInstallURL(t *testing.T) {
	const secret = "sensitive-config-12345"
	got := make(chan string, 1)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Referer()
		fmt.Fprint(w, `{"id":"test","resources":["catalog"]}`)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer source.Close()
	c, err := NewClient(source.URL+"/"+secret+"/manifest.json", source.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.Manifest(context.Background())
	select {
	case referer := <-got:
		if strings.Contains(referer, secret) {
			t.Fatal("redirect leaked install secret in Referer")
		}
	default:
	}
}
