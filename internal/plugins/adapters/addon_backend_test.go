package adapters_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/newznab"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/stremio"
)

const (
	abHashA = "a584351126afcf50d32be068f3c9a62550d41ecc"
	abHashB = "f0fd1cad0e24b69ff6aaadb9df40c50286327188"
	abHashC = "1b42b99b9fbc0e0416a379787f250eb73b7434e3"
	abHashD = "aa1a43787ff85fe5d029969857b0d64a6c9a05e4"
	// abSecret stands in for a debrid key inside an add-on's settings path.
	abSecret = "SECRETKEY123"

	abManifest     = `{"id":"org.example.streams","version":"1.0.0","name":"Example","resources":[{"name":"stream","types":["movie","series"],"idPrefixes":["tt","kitsu"]}],"types":["movie","series"],"catalogs":[]}`
	abKitsuAddon   = `{"id":"org.example.anime","version":"1.0.0","name":"Anime","resources":[{"name":"stream","types":["series","movie"],"idPrefixes":["kitsu:"]}],"types":["series","movie"]}`
	abPackEpisodes = `{"streams":[
 {"name":"Example\n4k","title":"Show (2008) S01 (2160p WEB-DL H265 DDP 5.1 - GRP)\nShow (2008) S01E02 (2160p WEB-DL H265 DDP 5.1 - GRP).mkv\n👤 73 💾 5.17 GB ⚙️ SiteA","infoHash":"` + abHashA + `","fileIdx":1,"sources":["tracker:udp://tracker.example:1337/announce"]},
 {"name":"Example\n1080p","title":"Show S01E02 1080p WEB-DL DDP5 1 H 264-XYZ\n👤 25 💾 1.5 GB","infoHash":"` + abHashD + `","fileIdx":0},
 {"name":"Usenet","title":"Show S01E02 1080p","nzbUrl":"https://nzb.example/x.nzb"},
 {"name":"Direct","title":"Show S01E02 720p","url":"https://cdn.example/video.mp4"}
]}`
	abSeasonPacks = `{"streams":[
 {"name":"Example","title":"Show (2008) S01 (2160p WEB-DL H265 DDP 5.1 - GRP)\nShow (2008) S01E01 (2160p WEB-DL H265 DDP 5.1 - GRP).mkv\n👤 73 💾 5.17 GB","infoHash":"` + abHashA + `","fileIdx":0},
 {"name":"Example","title":"Show COMPLETE S01-S05 2160p WEB-DL x264-GRP\nShow.S01.2160p/Show.S01E01.2160p.WEB-DL.x264-GRP.mkv\n👤 37 💾 35.61 GB","infoHash":"` + abHashC + `","fileIdx":1},
 {"name":"Example","title":"Show S01E01 1080p WEB-DL H 264-XYZ\n👤 25 💾 1.5 GB","infoHash":"` + abHashD + `","fileIdx":0}
]}`
	abKeyedStreams = `{"streams":[
 {"name":"[RD+] Example\n4k","title":"Movie 2014 2160p REMUX DV HDR10+ TrueHD 7.1 Atmos-GRP\n👤 263 💾 95.63 GB","url":"https://addon.example/resolve/realdebrid/` + abSecret + `/` + abHashA + `/null/2/Movie.mkv"},
 {"name":"[RD download] Example\n1080p","title":"Movie 2014 1080p BluRay x264-GRP\n👤 120 💾 12 GB","url":"https://addon.example/resolve/realdebrid/` + abSecret + `/` + abHashB + `/null/0/Movie.mkv"},
 {"name":"[TB⚡] [RD⬇️] Other 4K","description":"Movie.2014.2160p.WEB-DL.DDP5.1.HEVC-GRP.mkv\n💾 20.1 GB 👤 50","url":"https://addon.example/playback?info_hash=` + abHashC + `&file=0"}
]}`
	abExternalOnly = `{"streams":[
 {"name":"Web","title":"Movie 2014 1080p BluRay x264-GRP","externalUrl":"https://cdn.example/watch"},
 {"name":"Trailer","title":"Movie 2014 720p BluRay x264-GRP","ytId":"abcdefghijk"}
]}`
)

// streamAddon is a fake Stremio add-on behind a plugin runtime: streams by
// route path, an optional forced status, and the paths it was asked.
type streamAddon struct {
	mu      sync.Mutex
	paths   []string
	queries int
	streams map[string]string
	status  int
	retry   string
	failure error
}

func (a *streamAddon) handle(_ context.Context, req plugins.Request) (plugins.Response, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.paths = append(a.paths, req.Path)
	a.queries += len(req.Query)
	if a.failure != nil {
		return plugins.Response{}, a.failure
	}
	if a.status != 0 {
		h := map[string]string{}
		if a.retry != "" {
			h["Retry-After"] = a.retry
		}
		return plugins.Response{Status: a.status, Headers: h, Body: json.RawMessage(`{"err":"` + req.Path + `"}`)}, nil
	}
	if body, ok := a.streams[req.Path]; ok {
		return plugins.Response{Status: 200, Body: json.RawMessage(body)}, nil
	}
	return plugins.Response{Status: 200, Body: json.RawMessage(`{"streams":[]}`)}, nil
}

func (a *streamAddon) asked() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.paths...)
}

func (a *streamAddon) set(f func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	f()
}

// installAddon installs a plain Stremio add-on from its install link and
// serves its runtime from fake.
func (e *streamTestEnv) installAddon(link, manifest string, fake *streamAddon) plugins.Record {
	e.t.Helper()
	id, _, err := plugins.StremioInstallID(link)
	if err != nil {
		e.t.Fatal(err)
	}
	e.mu.Lock()
	e.rts[id] = &fakePluginRuntime{handler: fake.handle}
	e.mu.Unlock()
	preview, err := e.mgr.PreviewInstall(context.Background(), plugins.Source{Kind: "url", URL: link, Manifest: json.RawMessage(manifest)})
	if err != nil {
		e.t.Fatal(err)
	}
	rec, err := e.mgr.Install(context.Background(), preview, plugins.InstallOptions{AcceptAddedPermissions: true})
	if err != nil {
		e.t.Fatal(err)
	}
	return rec
}

func (e *streamTestEnv) addonBackend(rec plugins.Record, instance string) newznab.Backend {
	e.t.Helper()
	b, err := e.sources.Backend(config.Indexer{ID: instance, Name: "addon", Kind: config.IndexerKindAddon, Plugin: rec.ID})
	if err != nil {
		e.t.Fatal(err)
	}
	return b
}

func abNoReserve(context.Context, string) error { return nil }

type abAnime struct {
	mu    sync.Mutex
	calls []string
	ids   map[string][2]int64 // db -> id, entry episode
	err   error
}

func (f *abAnime) Lookup(_ context.Context, _ core.SearchRequest, db string) (int64, int, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, db)
	if f.err != nil {
		return 0, 0, false, f.err
	}
	v, ok := f.ids[db]
	return v[0], int(v[1]), ok, nil
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

func movieRequest() core.SearchRequest {
	return core.SearchRequest{Type: core.TypeMovie, IDs: core.IDs{IMDB: "tt0816692"}, Title: "Movie", Year: 2014}
}

func TestAddonBackendEpisodeReadsPackFilesUsenetAndHTTPStreams(t *testing.T) {
	env := newStreamTestEnv(t)
	fake := &streamAddon{streams: map[string]string{"/stream/series/tt0903747:1:2.json": abPackEpisodes}}
	rec := env.installAddon("https://addon.example/providers=a,b%7Csort=size/manifest.json", abManifest, fake)
	b := env.addonBackend(rec, "ix1")
	req := core.SearchRequest{Type: core.TypeEpisode, IDs: core.IDs{IMDB: "tt0903747"}, Title: "Show", Year: 2008, Season: 1, Episode: 2}
	rels, err := b.Search(t.Context(), req, abNoReserve)
	if err != nil {
		t.Fatal(err)
	}
	// A third-party add-on is asked exactly the Stremio request: no hints.
	if got := fake.asked(); len(got) != 1 || got[0] != "/stream/series/tt0903747:1:2.json" || fake.queries != 0 {
		t.Fatalf("requests %v (query params %d)", got, fake.queries)
	}
	if len(rels) != 4 {
		t.Fatalf("want 2 torrents, a Usenet and an HTTP stream, got %d: %+v", len(rels), rels)
	}
	a := rels[0]
	if a.InfoHash != abHashA || a.Protocol != core.ProtocolTorrent || a.GUID != "btih:"+abHashA+"/1" || a.TitleIDs.IMDB != "tt0903747" ||
		a.Name != "Show (2008) S01E02 (2160p WEB-DL H265 DDP 5.1 - GRP)" || a.Seeders != 73 || !strings.Contains(a.MagnetURL, "tracker.example") {
		t.Fatalf("pack file release %+v", a)
	}
	if u := rels[2]; u.Protocol != core.ProtocolUsenet || u.NZBURL != "https://nzb.example/x.nzb" || !strings.HasPrefix(u.GUID, "nzb:ix1:") {
		t.Fatalf("usenet stream %+v", u)
	}
	if h := rels[3]; h.Protocol != core.ProtocolHTTP || h.StreamURL != "https://cdn.example/video.mp4" || h.StreamOrigin != "https://addon.example" || !strings.HasPrefix(h.GUID, "http:ix1:") {
		t.Fatalf("http stream %+v", h)
	}
}

func TestAddonBackendSeasonKeepsWholeSeasonPacks(t *testing.T) {
	env := newStreamTestEnv(t)
	fake := &streamAddon{streams: map[string]string{"/stream/series/tt0903747:1:1.json": abSeasonPacks}}
	rec := env.installAddon("https://addon.example/manifest.json", abManifest, fake)
	rels, err := env.addonBackend(rec, "ix1").Search(t.Context(), core.SearchRequest{Type: core.TypeEpisode, IDs: core.IDs{IMDB: "0903747"}, Title: "Show", Season: 1}, abNoReserve)
	if err != nil {
		t.Fatal(err)
	}
	if got := fake.asked(); len(got) != 1 || got[0] != "/stream/series/tt0903747:1:1.json" {
		t.Fatalf("requests %v", got)
	}
	if len(rels) != 2 || rels[0].InfoHash != abHashA || rels[1].InfoHash != abHashC {
		t.Fatalf("want the two packs, got %+v", rels)
	}
	for _, r := range rels {
		if r.Info.Episodes != nil || strings.Contains(r.GUID, "/") {
			t.Fatalf("pack release %+v", r)
		}
	}
}

func TestAddonBackendKeyedAddonReadsHashesAndCachedMarkers(t *testing.T) {
	env := newStreamTestEnv(t)
	fake := &streamAddon{streams: map[string]string{"/stream/movie/tt0816692.json": abKeyedStreams}}
	rec := env.installAddon("https://addon.example/realdebrid="+abSecret+"/manifest.json", abManifest, fake)
	rels, err := env.addonBackend(rec, "ix1").Search(t.Context(), movieRequest(), abNoReserve)
	if err != nil || len(rels) != 3 {
		t.Fatalf("%v %+v", err, rels)
	}
	want := []struct {
		hash             string
		cached, uncached []string
	}{
		{abHashA, []string{config.DebridRealDebrid}, nil},
		{abHashB, nil, []string{config.DebridRealDebrid}},
		{abHashC, []string{config.DebridTorBox}, []string{config.DebridRealDebrid}},
	}
	for i, w := range want {
		r := rels[i]
		if r.InfoHash != w.hash || fmt.Sprint(r.DebridCached) != fmt.Sprint(w.cached) || fmt.Sprint(r.DebridUncached) != fmt.Sprint(w.uncached) {
			t.Errorf("release %d: hash %s cached %v uncached %v; want %+v", i, r.InfoHash, r.DebridCached, r.DebridUncached, w)
		}
		if strings.Contains(r.MagnetURL, abSecret) || strings.Contains(r.GUID, abSecret) {
			t.Errorf("release %d leaks the add-on key: %+v", i, r)
		}
		if r.StreamOrigin != "https://addon.example" {
			t.Errorf("release %d: stream origin %q", i, r.StreamOrigin)
		}
	}
}

func TestAddonBackendMagnetStreamBecomesRelease(t *testing.T) {
	env := newStreamTestEnv(t)
	fake := &streamAddon{streams: map[string]string{"/stream/movie/tt0816692.json": `{"streams":[
	 {"name":"Magnet","title":"Movie 2014 1080p BluRay x264-GRP\n👤 12 💾 2 GB","url":"magnet:?xt=urn:btih:` + abHashA + `&dn=Movie"}]}`}}
	rec := env.installAddon("https://addon.example/manifest.json", abManifest, fake)
	rels, err := env.addonBackend(rec, "ix1").Search(t.Context(), movieRequest(), abNoReserve)
	if err != nil || len(rels) != 1 || rels[0].InfoHash != abHashA || !strings.HasPrefix(rels[0].MagnetURL, "magnet:?xt=urn:btih:"+abHashA) {
		t.Fatalf("magnet stream did not become a release: %v %+v", err, rels)
	}
}

func TestAddonBackendTwoConfigurationsNeverShareHTTPIdentity(t *testing.T) {
	env := newStreamTestEnv(t)
	body := `{"streams":[{"name":"[RD+] Addon 4K","title":"Movie 2014 2160p WEB-DL x265-GRP","url":"https://addon.example/play/` + abSecret + `/token","behaviorHints":{"videoSize":4294967296}}]}`
	fake := &streamAddon{streams: map[string]string{"/stream/movie/tt0816692.json": body}}
	rec := env.installAddon("https://addon.example/manifest.json", abManifest, fake)
	a, errA := env.addonBackend(rec, "ix1").Search(t.Context(), movieRequest(), abNoReserve)
	b, errB := env.addonBackend(rec, "ix2").Search(t.Context(), movieRequest(), abNoReserve)
	if errA != nil || errB != nil || len(a) != 1 || len(b) != 1 {
		t.Fatalf("%v %v %+v %+v", errA, errB, a, b)
	}
	if a[0].GUID == b[0].GUID || strings.Contains(a[0].GUID, abSecret) {
		t.Fatalf("GUIDs %q %q", a[0].GUID, b[0].GUID)
	}
}

func TestAddonBackendUnplayableStreamsAreSkippedAndLogged(t *testing.T) {
	buf := captureLog(t)
	env := newStreamTestEnv(t)
	fake := &streamAddon{streams: map[string]string{"/stream/movie/tt0816692.json": abExternalOnly}}
	rec := env.installAddon("https://addon.example/realdebrid="+abSecret+"/manifest.json", abManifest, fake)
	rels, err := env.addonBackend(rec, "ix1").Search(t.Context(), movieRequest(), abNoReserve)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 0 {
		t.Fatalf("want 0 releases from external and YouTube streams, got %+v", rels)
	}
	log := buf.String()
	if !strings.Contains(log, "level=WARN") || !strings.Contains(log, "skippedNoHash=2") || !strings.Contains(log, "skippedAmbiguous=0") || !strings.Contains(log, "addon="+rec.ID) {
		t.Fatalf("log without the add-on or the counters: %q", log)
	}
	if strings.Contains(log, abSecret) || strings.Contains(log, "cdn.example") || strings.Contains(log, "/stream/") {
		t.Fatalf("log leaked a URL or credential: %q", log)
	}
}

// Three searches in a row with skips warn once, per backend.
func TestAddonBackendSkipWarnThrottledPerBackend(t *testing.T) {
	buf := captureLog(t)
	env := newStreamTestEnv(t)
	fake := &streamAddon{streams: map[string]string{"/stream/movie/tt0816692.json": `{"streams":[
	 {"name":"Web","title":"Movie 2014 1080p BluRay x264-GRP","externalUrl":"https://cdn.example/watch"}]}`}}
	rec := env.installAddon("https://addon.example/manifest.json", abManifest, fake)
	one, two := env.addonBackend(rec, "ix1"), env.addonBackend(rec, "ix2")
	for i := 0; i < 3; i++ {
		if _, err := one.Search(t.Context(), movieRequest(), abNoReserve); err != nil {
			t.Fatal(err)
		}
	}
	if n := strings.Count(buf.String(), "level=WARN"); n != 1 {
		t.Fatalf("3 searches in a row: %d WARN, want 1: %q", n, buf.String())
	}
	if _, err := two.Search(t.Context(), movieRequest(), abNoReserve); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(buf.String(), "level=WARN"); n != 2 {
		t.Fatalf("a second backend must warn on its own: %d WARN, want 2: %q", n, buf.String())
	}
	if env.addonBackend(rec, "ix1") != one {
		t.Fatal("a row's backend is not kept between searches, so its throttle state would be lost")
	}
}

func TestAddonBackendAsksNothingWithoutAUsableID(t *testing.T) {
	env := newStreamTestEnv(t)
	fake := &streamAddon{}
	rec := env.installAddon("https://addon.example/manifest.json", abManifest, fake)
	b := env.addonBackend(rec, "ix1")
	reserved := 0
	reserve := func(context.Context, string) error { reserved++; return nil }
	for _, req := range []core.SearchRequest{
		{Type: core.TypeMovie, Title: "No IDs"},
		{Type: core.TypeMovie, IDs: core.IDs{IMDB: "not-an-id"}},
		{Type: core.TypeShow, IDs: core.IDs{IMDB: "tt1"}},
		{Type: core.TypeEpisode, IDs: core.IDs{IMDB: "tt1"}},
	} {
		if rels, err := b.Search(t.Context(), req, reserve); rels != nil || err != nil {
			t.Fatalf("%+v: %v %v", req, rels, err)
		}
	}
	if reserved != 0 || len(fake.asked()) != 0 {
		t.Fatalf("reserved %d, requests %v", reserved, fake.asked())
	}
	// A real request reserves one search "hit".
	var kinds []string
	if _, err := b.Search(t.Context(), movieRequest(), func(_ context.Context, k string) error { kinds = append(kinds, k); return nil }); err != nil || len(kinds) != 1 || kinds[0] != "hit" {
		t.Fatalf("reservations %v err %v", kinds, err)
	}
	budget := errors.New("budget")
	if _, err := b.Search(t.Context(), movieRequest(), func(context.Context, string) error { return budget }); !errors.Is(err, budget) {
		t.Fatalf("search budget ignored: %v", err)
	}
}

func TestAddonBackendAnimeOnlyAddonIsAskedByMappedEntryEpisode(t *testing.T) {
	env := newStreamTestEnv(t)
	anime := &abAnime{ids: map[string][2]int64{"kitsu": {41982, 3}}}
	env.sources.SetAnimeIDs(anime)
	body := `{"streams":[
	 {"name":"Anime","title":"[SubsPlease] Shingeki no Kyojin - 03 (1080p) [ABCD1234].mkv\n👤 40 💾 1.4 GB","infoHash":"` + abHashA + `"},
	 {"name":"Anime","title":"[Group] Shingeki no Kyojin - 52 (1080p)","infoHash":"` + abHashB + `"}]}`
	fake := &streamAddon{streams: map[string]string{"/stream/series/kitsu:41982:3.json": body}}
	rec := env.installAddon("https://anime.example/manifest.json", abKitsuAddon, fake)
	b := env.addonBackend(rec, "ix1")
	req := core.SearchRequest{Type: core.TypeEpisode, IDs: core.IDs{IMDB: "tt2560140", TMDB: 1429}, Title: "Attack on Titan", Season: 3, Episode: 15, AbsoluteEpisode: 52, Anime: true}
	rels, err := b.Search(t.Context(), req, abNoReserve)
	if err != nil {
		t.Fatal(err)
	}
	if got := fake.asked(); len(got) != 1 || got[0] != "/stream/series/kitsu:41982:3.json" {
		t.Fatalf("requests %v", got)
	}
	if len(rels) != 2 {
		t.Fatalf("releases %+v", rels)
	}
	if i := rels[0].Info; len(i.Seasons) != 1 || i.Seasons[0] != 3 || len(i.Episodes) != 1 || i.Episodes[0] != 15 {
		t.Fatalf("entry episode identity %+v", i)
	}
	if i := rels[1].Info; len(i.Seasons) != 0 || len(i.AbsoluteEpisodes) != 1 || i.AbsoluteEpisodes[0] != 52 {
		t.Fatalf("absolute release %+v", i)
	}
	// Unavailable or unmapped: the add-on is skipped quietly, nothing asked.
	anime.mu.Lock()
	anime.err = errors.New("unavailable")
	anime.mu.Unlock()
	if rels, err := b.Search(t.Context(), req, abNoReserve); err != nil || len(rels) != 0 || len(fake.asked()) != 1 {
		t.Fatalf("unavailable mapping: %v %v requests %v", rels, err, fake.asked())
	}
}

func TestAddonBackendSearchErrorsNeverLeakTheAddonPath(t *testing.T) {
	env := newStreamTestEnv(t)
	link := "https://addon.example/realdebrid=" + abSecret + "/manifest.json"
	fake := &streamAddon{status: http.StatusInternalServerError}
	rec := env.installAddon(link, abManifest, fake)
	b := env.addonBackend(rec, "ix1")
	req := core.SearchRequest{Type: core.TypeMovie, IDs: core.IDs{IMDB: "tt1"}}

	_, err := b.Search(t.Context(), req, abNoReserve)
	var he *stremio.HTTPError
	if !errors.As(err, &he) || he.Status != 500 || strings.Contains(err.Error(), abSecret) || strings.Contains(err.Error(), "realdebrid") {
		t.Fatalf("http error %v", err)
	}
	// An add-on that refuses servers gets an actionable message naming no URL.
	fake.set(func() { fake.status = http.StatusForbidden })
	if _, err = b.Search(t.Context(), req, abNoReserve); err == nil || !strings.Contains(err.Error(), "refused Ether") || strings.Contains(err.Error(), abSecret) {
		t.Fatalf("refusal %v", err)
	}
	// A transport failure that prints the full URL, as *url.Error would.
	fake.set(func() {
		fake.status = 0
		fake.failure = fmt.Errorf(`Get "https://addon.example/realdebrid=%s/stream/movie/tt1.json": connection refused`, abSecret)
	})
	if _, err = b.Search(t.Context(), req, abNoReserve); err == nil || strings.Contains(err.Error(), abSecret) {
		t.Fatalf("transport error %v", err)
	}
	// Invalid JSON.
	fake.set(func() {
		fake.failure = nil
		fake.streams = map[string]string{"/stream/movie/tt1.json": `<html>` + abSecret + `</html>`}
	})
	if _, err = b.Search(t.Context(), req, abNoReserve); err == nil || strings.Contains(err.Error(), abSecret) {
		t.Fatalf("invalid JSON %v", err)
	}
	// Configuration errors.
	for _, ix := range []config.Indexer{
		{Name: "x", Kind: config.IndexerKindAddon},
		{Name: "x", URL: "https://h/" + abSecret},
	} {
		if _, err := env.sources.Backend(ix); err == nil || strings.Contains(err.Error(), abSecret) {
			t.Fatalf("config err %v for %+v", err, ix)
		}
	}
}

func TestAddonBackendMissingAddonIsUnavailable(t *testing.T) {
	env := newStreamTestEnv(t)
	b, err := env.sources.Backend(config.Indexer{ID: "ix", Name: "gone", Kind: config.IndexerKindAddon, Plugin: "addon.removed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Search(t.Context(), movieRequest(), abNoReserve); !errors.Is(err, core.ErrSourceUnavailable) {
		t.Fatalf("search %v", err)
	}
}

// A 429 is a RateLimitError carrying Retry-After; through the indexer row
// it pauses the add-on (no retries, no further requests), and a repeated
// search is answered from the search cache.
func TestAddonBackendRateLimitPausesTheAddon(t *testing.T) {
	env := newStreamTestEnv(t)
	ctx := context.Background()
	fake := &streamAddon{streams: map[string]string{"/stream/movie/tt0816692.json": abKeyedStreams}}
	rec := env.installAddon("https://addon.example/manifest.json", abManifest, fake)

	// The backend alone reports the limit without pausing anything.
	limited := &streamAddon{status: http.StatusTooManyRequests, retry: "120"}
	limRec := env.installAddon("https://limited.example/manifest.json", abManifest, limited)
	_, err := env.addonBackend(limRec, "ix-lim").Search(ctx, movieRequest(), abNoReserve)
	var rl *newznab.RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfter != "120" {
		t.Fatalf("rate limit error %#v", err)
	}
	var he *stremio.HTTPError
	if !errors.As(err, &he) || he.Status != 429 {
		t.Fatalf("rate limit cause %v", err)
	}

	var c *newznab.Client
	for _, cl := range env.nzb.Clients() {
		if cl.Name() == rec.Name {
			c = cl
		}
	}
	if c == nil {
		t.Fatal("no indexer row for the add-on")
	}
	for i := 0; i < 2; i++ {
		rels, err := c.Search(ctx, movieRequest())
		if err != nil || len(rels) != 3 || rels[0].Indexer != rec.Name || rels[0].ID == "" {
			t.Fatalf("search %d: %v %+v", i, err, rels)
		}
	}
	if n := len(fake.asked()); n != 1 {
		t.Fatalf("cached search asked the add-on again: %d requests", n)
	}
	fake.set(func() { fake.status, fake.retry = http.StatusTooManyRequests, "120" })
	other := core.SearchRequest{Type: core.TypeMovie, IDs: core.IDs{IMDB: "tt0000002"}, Title: "Other"}
	if _, err := c.Search(ctx, other); !errors.Is(err, core.ErrBudgetExhausted) {
		t.Fatalf("429: %v", err)
	}
	third := core.SearchRequest{Type: core.TypeMovie, IDs: core.IDs{IMDB: "tt0000003"}, Title: "Third"}
	if _, err := c.Search(ctx, third); !errors.Is(err, core.ErrBudgetExhausted) {
		t.Fatalf("paused add-on: %v", err)
	}
	if n := len(fake.asked()); n != 2 {
		t.Fatalf("paused add-on was asked again: %d requests", n)
	}
}

func TestAddonBackendResolveTorrentNeedsAHash(t *testing.T) {
	env := newStreamTestEnv(t)
	rec := env.installAddon("https://addon.example/manifest.json", abManifest, &streamAddon{})
	b := env.addonBackend(rec, "ix1")
	if _, err := b.ResolveTorrent(t.Context(), core.Release{Name: "no hash", Protocol: core.ProtocolHTTP}, abNoReserve); !errors.Is(err, core.ErrReleaseRejected) {
		t.Fatalf("no hash: %v", err)
	}
	in := core.Release{Name: "m", Protocol: core.ProtocolTorrent, InfoHash: abHashA, MagnetURL: "magnet:?xt=urn:btih:" + abHashA}
	out, err := b.ResolveTorrent(t.Context(), in, abNoReserve)
	if err != nil || out.InfoHash != abHashA || out.MagnetURL != in.MagnetURL {
		t.Fatalf("with hash: %v %+v", err, out)
	}
}

func TestAddonBackendFetchNZBFromAddonOriginOnly(t *testing.T) {
	const nzb = `<?xml version="1.0"?><nzb xmlns="http://www.newzbin.com/DTD/2003/nzb"><file subject="x"></file></nzb>`
	addon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + abSecret + "/get.nzb":
			fmt.Fprint(w, nzb)
		case "/" + abSecret + "/page":
			fmt.Fprint(w, "<html>login</html>")
		case "/" + abSecret + "/gone":
			http.Error(w, "gone", http.StatusNotFound)
		case "/" + abSecret + "/elsewhere":
			http.Redirect(w, r, "http://127.0.0.2:1/x.nzb", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer addon.Close()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, nzb) }))
	defer other.Close()

	env := newStreamTestEnv(t)
	rec := env.installAddon(addon.URL+"/"+abSecret+"/manifest.json", abManifest, &streamAddon{})
	b := env.addonBackend(rec, "ix1")
	var grabs []string
	reserve := func(_ context.Context, kind string) error { grabs = append(grabs, kind); return nil }
	rel := core.Release{Name: "Movie 2014 1080p", Protocol: core.ProtocolUsenet, NZBURL: addon.URL + "/" + abSecret + "/get.nzb"}
	body, err := b.FetchNZB(t.Context(), rel, reserve)
	if err != nil || !strings.Contains(string(body), "<nzb") || len(grabs) != 1 || grabs[0] != "grab" {
		t.Fatalf("add-on NZB: %v %q grabs %v", err, body, grabs)
	}
	for name, link := range map[string]string{
		"private host other than the add-on": other.URL + "/x.nzb",
		"redirect away from the add-on":      addon.URL + "/" + abSecret + "/elsewhere",
		"not an NZB":                         addon.URL + "/" + abSecret + "/page",
		"HTTP error":                         addon.URL + "/" + abSecret + "/gone",
	} {
		rel.NZBURL = link
		_, err := b.FetchNZB(t.Context(), rel, reserve)
		if err == nil {
			t.Errorf("%s: fetched", name)
			continue
		}
		if errors.Is(err, core.ErrReleaseRejected) || errors.Is(err, core.ErrArticleMissing) || errors.Is(err, core.ErrArticleCorrupt) {
			t.Errorf("%s: server answer classified as permanent: %v", name, err)
		}
		if strings.Contains(err.Error(), abSecret) {
			t.Errorf("%s: error leaks the add-on path: %v", name, err)
		}
	}
	if _, err := b.FetchNZB(t.Context(), core.Release{Name: "t", Protocol: core.ProtocolTorrent, InfoHash: abHashA}, reserve); !errors.Is(err, core.ErrReleaseRejected) {
		t.Fatalf("torrent: %v", err)
	}
	if _, err := b.FetchNZB(t.Context(), core.Release{Name: "m", Protocol: core.ProtocolUsenet, NZBURL: "stremio:///x"}, reserve); !errors.Is(err, core.ErrReleaseRejected) {
		t.Fatalf("malformed NZB link: %v", err)
	}
	budget := errors.New("budget")
	rel.NZBURL = addon.URL + "/" + abSecret + "/get.nzb"
	if _, err := b.FetchNZB(t.Context(), rel, func(context.Context, string) error { return budget }); !errors.Is(err, budget) {
		t.Fatalf("grab budget ignored: %v", err)
	}
}
