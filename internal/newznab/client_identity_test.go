package newznab

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
)

const servarrAccept = "application/rss+xml, text/rss+xml, application/xml, text/xml"

type wireRequest struct {
	ua          string
	accept      string
	contentType string
	rawQuery    string
}

// wireServer records what the client sends and answers like a Newznab API.
type wireServer struct {
	mu  sync.Mutex
	got []wireRequest
	srv *httptest.Server
}

func newWireServer(t *testing.T) *wireServer {
	t.Helper()
	ws := &wireServer{}
	ws.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws.mu.Lock()
		ws.got = append(ws.got, wireRequest{
			ua:          r.Header.Get("User-Agent"),
			accept:      r.Header.Get("Accept"),
			contentType: r.Header.Get("Content-Type"),
			rawQuery:    r.URL.RawQuery,
		})
		ws.mu.Unlock()
		switch r.URL.Query().Get("t") {
		case "caps":
			fmt.Fprint(w, loadFixture(t, "caps.xml", ws.srv.URL))
		case "get":
			fmt.Fprint(w, `<?xml version="1.0"?><nzb><file/></nzb>`)
		case "tvsearch", "movie":
			fmt.Fprint(w, loadFixture(t, "tv.xml", ws.srv.URL))
		default:
			fmt.Fprint(w, loadFixture(t, "movie.xml", ws.srv.URL))
		}
	}))
	t.Cleanup(ws.srv.Close)
	return ws
}

func (ws *wireServer) last() wireRequest {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return ws.got[len(ws.got)-1]
}

func (ws *wireServer) reset() {
	ws.mu.Lock()
	ws.got = nil
	ws.mu.Unlock()
}

func (ws *wireServer) all() []wireRequest {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return append([]wireRequest(nil), ws.got...)
}

func wireConfig(url string) config.Indexer {
	return config.Indexer{ID: "idx1", Name: "Fake", URL: url, APIKey: "secret", Enabled: true}
}

func tvSearch() core.SearchRequest {
	return core.SearchRequest{
		Type: core.TypeEpisode, IDs: core.IDs{TVDB: 81797}, Title: "Show", Season: 1, Episode: 2,
	}
}

func TestClientIdentityOnWire(t *testing.T) {
	ws := newWireServer(t)
	ctx := context.Background()

	// The default policy keeps today's bytes.
	c := New(wireConfig(ws.srv.URL), nil, Options{})
	if _, err := c.Caps(ctx); err != nil {
		t.Fatal(err)
	}
	if got := ws.last(); got.ua != "Ether/1.0" || got.rawQuery != "apikey=secret&t=caps" || got.accept != "" {
		t.Fatalf("default caps: %+v", got)
	}
	ws.reset()
	if _, err := c.Search(ctx, tvSearch()); err != nil {
		t.Fatal(err)
	}
	const defaultTV = "apikey=secret&cat=5000&ep=2&extended=1&limit=100&season=1&t=tvsearch&tvdbid=81797"
	if got := ws.last(); got.rawQuery != defaultTV || got.ua != "Ether/1.0" {
		t.Fatalf("default tv search: %+v", got)
	}

	// A persona orders its parameters and sends its own headers.
	ws.reset()
	c = New(wireConfig(ws.srv.URL), nil, Options{
		ClientIdentity: config.ClientIdentity{TVSearch: config.IdentitySonarr},
	})
	if _, err := c.Caps(ctx); err != nil {
		t.Fatal(err)
	}
	if got := ws.last(); got.ua != "Sonarr/4.0.20.3014 (ubuntu 26.04)" || got.rawQuery != "t=caps&apikey=secret" || got.accept != servarrAccept {
		t.Fatalf("sonarr caps: %+v", got)
	}
	ws.reset()
	if _, err := c.Search(ctx, tvSearch()); err != nil {
		t.Fatal(err)
	}
	const sonarrTV = "t=tvsearch&cat=5000&extended=1&apikey=secret&offset=0&limit=100&tvdbid=81797&season=1&ep=2"
	if got := ws.last(); got.rawQuery != sonarrTV {
		t.Fatalf("sonarr tv search: %+v", got)
	}
}

func TestClientIdentityProbe(t *testing.T) {
	ws := newWireServer(t)
	ctx := context.Background()
	// The save-time probe must match the searches the persona sends: a paged
	// client always includes offset.
	if err := TestWithOptions(ctx, wireConfig(ws.srv.URL), Options{
		ClientIdentity: config.ClientIdentity{TVSearch: config.IdentitySonarr},
	}); err != nil {
		t.Fatal(err)
	}
	got := ws.last()
	if got.rawQuery != "t=search&extended=1&apikey=secret&offset=0&limit=1&q=test" {
		t.Fatalf("sonarr probe: %+v", got)
	}
	if got.ua != "Sonarr/4.0.20.3014 (ubuntu 26.04)" {
		t.Fatalf("sonarr probe user agent: %q", got.ua)
	}
	ws.reset()
	if err := TestWithOptions(ctx, wireConfig(ws.srv.URL), Options{}); err != nil {
		t.Fatal(err)
	}
	if got := ws.last(); got.rawQuery != "apikey=secret&extended=1&limit=1&q=test&t=search" || got.ua != "Ether/1.0" {
		t.Fatalf("default probe: %+v", got)
	}
}

func TestClientIdentityCacheSeparated(t *testing.T) {
	ws := newWireServer(t)
	ctx := context.Background()
	c := New(wireConfig(ws.srv.URL), nil, Options{
		ClientIdentity: config.ClientIdentity{TVSearch: config.IdentitySonarr},
	})
	if _, err := c.Search(ctx, tvSearch()); err != nil {
		t.Fatal(err)
	}
	before := len(ws.all())
	if _, err := c.Search(ctx, tvSearch()); err != nil {
		t.Fatal(err)
	}
	if got := len(ws.all()); got != before {
		t.Fatalf("same identity re-searched the indexer: %d -> %d requests", before, got)
	}
	// A different persona may be answered differently, so its answer cannot
	// come from the previous persona's cache entry.
	c.SetClientIdentity(config.ClientIdentity{TVSearch: config.IdentityProwlarr})
	if _, err := c.Search(ctx, tvSearch()); err != nil {
		t.Fatal(err)
	}
	if got := len(ws.all()); got == before {
		t.Fatal("switching the identity served the old identity's cached answer")
	}
	if got := ws.last().ua; got != "Prowlarr/2.7.0.5723 (ubuntu 26.04)" {
		t.Fatalf("re-search used %q", got)
	}
	// A custom identity is keyed by what it sends, not by its name: editing
	// the user agent must not reuse the previous custom identity's answer.
	c.SetClientIdentity(config.ClientIdentity{TVSearch: config.IdentityCustom, UserAgent: "MyClient/1.0"})
	if _, err := c.Search(ctx, tvSearch()); err != nil {
		t.Fatal(err)
	}
	before = len(ws.all())
	c.SetClientIdentity(config.ClientIdentity{TVSearch: config.IdentityCustom, UserAgent: "MyClient/2.0"})
	if _, err := c.Search(ctx, tvSearch()); err != nil {
		t.Fatal(err)
	}
	if got := len(ws.all()); got == before {
		t.Fatal("editing the custom user agent kept the old cached answer")
	}
	if got := ws.last().ua; got != "MyClient/2.0" {
		t.Fatalf("edited user agent not sent: %q", got)
	}
}

func TestTorznabNumberAttribute(t *testing.T) {
	const xmlFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed">
<channel><item>
<title>Movie.2026.1080p.WEB-DL</title><guid>g1</guid>
<link>http://ix.example/api?t=get&amp;id=g1</link>
<torznab:attr name="category" number="2040"/>
</item></channel></rss>`
	rs, err := parseFeed([]byte(xmlFeed), "ix")
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || rs[0].Category != 2040 {
		t.Fatalf("xml number attribute: %+v", rs)
	}
	const jsonFeed = `{"rss":{"channel":{"item":[{"title":"Movie.2026.1080p.WEB-DL","guid":"g2","link":"http://ix.example/api?t=get&id=g2","attr":[{"@attributes":{"name":"category","number":"2040"}}]}]}}}`
	rs, err = parseFeed([]byte(jsonFeed), "ix")
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || rs[0].Category != 2040 {
		t.Fatalf("json number attribute: %+v", rs)
	}
}

func TestClientIdentityHeadersStayOnTheIndexerOrigin(t *testing.T) {
	c := New(wireConfig("https://ix.example.com"), nil, Options{})
	id := config.Identity{
		Preset: "custom", UserAgent: "MyClient/1.0",
		Headers: map[string]string{"X-Api-Client": "s3cret"},
	}
	// A feed-supplied download URL on another host is untrusted data.
	req, _ := http.NewRequest(http.MethodGet, "https://cdn.example.net/nzb", nil)
	c.applyIdentity(req, id)
	if got := req.Header.Get("User-Agent"); got != "MyClient/1.0" {
		t.Fatalf("user agent missing: %q", got)
	}
	if got := req.Header.Get("X-Api-Client"); got != "" {
		t.Fatalf("custom header sent to another host: %q", got)
	}
	req, _ = http.NewRequest(http.MethodGet, "https://ix.example.com/api?t=get", nil)
	c.applyIdentity(req, id)
	if got := req.Header.Get("X-Api-Client"); got != "s3cret" {
		t.Fatalf("custom header missing on the indexer origin: %q", got)
	}
	// A redirect that leaves the origin must not carry them either.
	next, _ := http.NewRequest(http.MethodGet, "https://cdn.example.net/x", nil)
	next.Header.Set("X-Api-Client", "s3cret")
	c.dropForeignIdentityHeaders(next, id)
	if got := next.Header.Get("X-Api-Client"); got != "" {
		t.Fatalf("redirect kept the custom header: %q", got)
	}
	next, _ = http.NewRequest(http.MethodGet, "https://ix.example.com/x", nil)
	next.Header.Set("X-Api-Client", "s3cret")
	c.dropForeignIdentityHeaders(next, id)
	if got := next.Header.Get("X-Api-Client"); got != "s3cret" {
		t.Fatalf("redirect within the origin dropped the header: %q", got)
	}
}

func TestClientIdentityGrab(t *testing.T) {
	ws := newWireServer(t)
	ctx := context.Background()
	c := New(wireConfig(ws.srv.URL), nil, Options{
		ClientIdentity: config.ClientIdentity{MovieGrab: config.IdentityNZBGet},
	})
	movie := core.Release{ID: "r1", Name: "Movie.2026.1080p", GUID: "g1", Category: 2040}
	if _, err := c.FetchNZB(ctx, movie); err != nil {
		t.Fatal(err)
	}
	if got := ws.last(); got.ua != "nzbget/26.3" || got.accept != "*/*" {
		t.Fatalf("movie grab: %+v", got)
	}
	episode := core.Release{ID: "r2", Name: "Show.S01E02.1080p", GUID: "g2", Category: 5040}
	if _, err := c.FetchNZB(ctx, episode); err != nil {
		t.Fatal(err)
	}
	if got := ws.last(); got.ua != "Ether/1.0" {
		t.Fatalf("only the movie grab was overridden: %+v", got)
	}
}

func TestClientIdentityNoEtherLeak(t *testing.T) {
	presets := []string{
		config.IdentitySonarr, config.IdentityRadarr, config.IdentityProwlarr,
		config.IdentityNZBHydra2, config.IdentitySABnzbd, config.IdentityNZBGet,
	}
	ctx := context.Background()
	for _, preset := range presets {
		ws := newWireServer(t)
		c := New(wireConfig(ws.srv.URL), nil, Options{
			ClientIdentity: config.ClientIdentity{
				TVSearch: preset, MovieSearch: preset, TVGrab: preset, MovieGrab: preset,
			},
		})
		if _, err := c.Caps(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Search(ctx, tvSearch()); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Search(ctx, core.SearchRequest{
			Type: core.TypeMovie, IDs: core.IDs{IMDB: "tt0133093"}, Title: "Movie", Year: 2026,
		}); err != nil {
			t.Fatal(err)
		}
		for _, r := range []core.Release{
			{ID: "t", Name: "Show.S01E02", GUID: "g1", Category: 5040},
			{ID: "m", Name: "Movie.2026", GUID: "g2", Category: 2040},
		} {
			if _, err := c.FetchNZB(ctx, r); err != nil {
				t.Fatal(err)
			}
		}
		for _, got := range ws.all() {
			if got.ua == "" || got.ua == "Ether/1.0" {
				t.Fatalf("%s: request carried %q", preset, got.ua)
			}
		}
	}
}
