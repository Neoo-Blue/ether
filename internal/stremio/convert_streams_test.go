package stremio

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
)

// Shaped like real add-on answers: torrent name, the file inside a pack,
// then a statistics line and a flags line.
var episodeStreams = `{"streams":[
 {"name":"Example\n4k","title":"Show (2008) S01 (2160p WEB-DL H265 DDP 5.1 - GRP)\nShow (2008) S01E02 (2160p WEB-DL H265 DDP 5.1 - GRP).mkv\n👤 73 💾 5.17 GB ⚙️ SiteA","infoHash":"` + strings.ToUpper(hashA) + `","fileIdx":1,"behaviorHints":{"filename":"Show (2008) S01E02 (2160p WEB-DL H265 DDP 5.1 - GRP).mkv"},"sources":["tracker:udp://tracker.example:1337/announce","dht:` + hashA + `"]},
 {"name":"Example\n4k","title":"Шоу / Show / Сезон: 1 [2008 WEB-DL 2160p]\nShow.S01E02.2160p.AMZN.WEB-DL.DTS-HD.MA.5.1.HEVC.mkv\n👤 69 💾 9.55 GB ⚙️ SiteB\n🇬🇧 / 🇷🇺","infoHash":"` + hashB + `","fileIdx":1},
 {"name":"Example\n1080p","title":"Show S01E02 1080p WEB-DL DDP5 1 H 264-XYZ\n👤 25 💾 1.5 GB ⚙️ SiteA","infoHash":"` + hashD + `","fileIdx":0,"behaviorHints":{"videoSize":1610612736}},
 {"name":"Example\n1080p","title":"Show S01E02 1080p WEB-DL DDP5 1 H 264-XYZ duplicate","infoHash":"` + hashD + `","fileIdx":0},
 {"name":"Usenet","title":"Show S01E02 1080p","nzbUrl":"https://nzb.example/x.nzb"},
 {"name":"Direct","title":"Show S01E02 720p","url":"https://cdn.example/video.mp4"}
]}`

var seasonStreams = `{"streams":[
 {"name":"Example","title":"Show (2008) S01 (2160p WEB-DL H265 DDP 5.1 - GRP)\nShow (2008) S01E01 (2160p WEB-DL H265 DDP 5.1 - GRP).mkv\n👤 73 💾 5.17 GB","infoHash":"` + hashA + `","fileIdx":0},
 {"name":"Example","title":"Show COMPLETE S01-S05 2160p WEB-DL x264-GRP\nShow.S01.2160p/Show.S01E01.2160p.WEB-DL.x264-GRP.mkv\n👤 37 💾 35.61 GB","infoHash":"` + hashC + `","fileIdx":1},
 {"name":"Example","title":"Show S01E01 1080p WEB-DL H 264-XYZ\n👤 25 💾 1.5 GB","infoHash":"` + hashD + `","fileIdx":0}
]}`

// A keyed add-on answers with links to its own resolver and cached markers.
var keyedStreams = `{"streams":[
 {"name":"[RD+] Example\n4k","title":"Movie 2014 2160p REMUX DV HDR10+ TrueHD 7.1 Atmos-GRP\n👤 263 💾 95.63 GB","url":"https://addon.example/resolve/realdebrid/` + secret + `/` + hashA + `/null/2/Movie.mkv"},
 {"name":"[RD download] Example\n1080p","title":"Movie 2014 1080p BluRay x264-GRP\n👤 120 💾 12 GB","url":"https://addon.example/resolve/realdebrid/` + secret + `/` + hashB + `/null/0/Movie.mkv"},
 {"name":"[TB⚡] [RD⬇️] Other 4K","description":"Movie.2014.2160p.WEB-DL.DDP5.1.HEVC-GRP.mkv\n💾 20.1 GB 👤 50","url":"https://addon.example/playback?info_hash=` + hashC + `&file=0"}
]}`

func decodeStreams(t testing.TB, body string) []Stream {
	t.Helper()
	var ans struct {
		Streams []Stream `json:"streams"`
	}
	if err := json.Unmarshal([]byte(body), &ans); err != nil {
		t.Fatal(err)
	}
	return ans.Streams
}

func TestConvertEpisodeReadsPackFilesUsenetAndHTTPStreams(t *testing.T) {
	const origin = "https://addon.example"
	req := core.SearchRequest{Type: core.TypeEpisode, IDs: core.IDs{IMDB: "tt0903747"}, Title: "Show", Year: 2008, Season: 1, Episode: 2}
	c := Convert(decodeStreams(t, episodeStreams), req, Lookup{Type: TypeSeries, ID: "tt0903747:1:2"}, ConvertOptions{Origin: origin, Instance: "ix1"})
	rels := c.Releases
	if len(rels) != 5 {
		t.Fatalf("want 3 torrents, a Usenet and an HTTP stream (duplicate skipped), got %d: %+v", len(rels), rels)
	}
	if len(c.Source) != 5 || c.Source[0] != 0 || c.Source[1] != 1 || c.Source[2] != 2 || c.Source[3] != 4 || c.Source[4] != 5 {
		t.Fatalf("source indexes %v", c.Source)
	}
	if u := rels[3]; u.Protocol != core.ProtocolUsenet || u.NZBURL != "https://nzb.example/x.nzb" || u.InfoHash != "" || !strings.HasPrefix(u.GUID, "nzb:ix1:") {
		t.Fatalf("usenet stream %+v", u)
	}
	if h := rels[4]; h.Protocol != core.ProtocolHTTP || h.StreamURL != "https://cdn.example/video.mp4" || h.StreamOrigin != origin || !strings.HasPrefix(h.GUID, "http:ix1:") || h.Info.Height != 720 {
		t.Fatalf("http stream %+v", h)
	}
	// Flags on the statistics line are audio languages.
	if got := rels[1].Info.Languages; len(got) != 2 || got[0] != "english" || got[1] != "russian" {
		t.Fatalf("flag languages %v", got)
	}
	a := rels[0]
	if a.InfoHash != hashA || a.Protocol != core.ProtocolTorrent || a.GUID != "btih:"+hashA+"/1" || a.Category != tvCategory {
		t.Fatalf("pack file release %+v", a)
	}
	if a.Name != "Show (2008) S01E02 (2160p WEB-DL H265 DDP 5.1 - GRP)" || a.Info.Height != 2160 {
		t.Fatalf("pack file should be named by its episode file: %q %+v", a.Name, a.Info)
	}
	if a.Seeders != 73 || a.Size != parseSize("💾 5.17 GB") || a.Size < 5<<30 {
		t.Fatalf("stats: seeders %d size %d", a.Seeders, a.Size)
	}
	if !strings.HasPrefix(a.MagnetURL, "magnet:?xt=urn:btih:"+hashA) || !strings.Contains(a.MagnetURL, "tracker.example") || strings.Contains(a.MagnetURL, "dht%3A") {
		t.Fatalf("magnet %q", a.MagnetURL)
	}
	// A foreign-language torrent name: the episode file names the release.
	if rels[1].Name != "Show.S01E02.2160p.AMZN.WEB-DL.DTS-HD.MA.5.1.HEVC" || rels[1].Seeders != 69 {
		t.Fatalf("foreign pack %q seeders %d", rels[1].Name, rels[1].Seeders)
	}
	// behaviorHints.videoSize wins over the text size.
	if rels[2].Size != 1610612736 || rels[2].Name != "Show S01E02 1080p WEB-DL DDP5 1 H 264-XYZ" {
		t.Fatalf("single episode %+v", rels[2])
	}
}

func TestConvertSeasonKeepsWholeSeasonPacks(t *testing.T) {
	req := core.SearchRequest{Type: core.TypeEpisode, IDs: core.IDs{IMDB: "0903747"}, Title: "Show", Season: 1}
	c := Convert(decodeStreams(t, seasonStreams), req, Lookup{Type: TypeSeries, ID: "tt0903747:1:1", Packs: true}, ConvertOptions{})
	rels := c.Releases
	if len(rels) != 2 {
		t.Fatalf("want the two packs, got %+v", rels)
	}
	for _, r := range rels {
		if r.Info.Episodes != nil || strings.Contains(r.GUID, "/") {
			t.Fatalf("pack release %+v", r)
		}
	}
	if rels[0].Name != "Show (2008) S01 (2160p WEB-DL H265 DDP 5.1 - GRP)" || rels[1].InfoHash != hashC {
		t.Fatalf("packs %q %q", rels[0].Name, rels[1].Name)
	}
}

func TestConvertKeyedAddonReadsHashesAndCachedMarkers(t *testing.T) {
	req := core.SearchRequest{Type: core.TypeMovie, IDs: core.IDs{IMDB: "tt0816692"}, Title: "Movie", Year: 2014}
	rels := Convert(decodeStreams(t, keyedStreams), req, Lookup{Type: TypeMovie, ID: "tt0816692"}, ConvertOptions{Origin: "https://addon.example"}).Releases
	if len(rels) != 3 {
		t.Fatalf("got %+v", rels)
	}
	want := []struct {
		hash             string
		cached, uncached []string
	}{
		{hashA, []string{config.DebridRealDebrid}, nil},
		{hashB, nil, []string{config.DebridRealDebrid}},
		{hashC, []string{config.DebridTorBox}, []string{config.DebridRealDebrid}},
	}
	for i, w := range want {
		r := rels[i]
		if r.InfoHash != w.hash || fmt.Sprint(r.DebridCached) != fmt.Sprint(w.cached) || fmt.Sprint(r.DebridUncached) != fmt.Sprint(w.uncached) {
			t.Errorf("release %d: hash %s cached %v uncached %v; want %+v", i, r.InfoHash, r.DebridCached, r.DebridUncached, w)
		}
		if strings.Contains(r.MagnetURL, secret) || strings.Contains(r.GUID, secret) || r.Category != movieCategory {
			t.Errorf("release %d leaks the add-on key or has the wrong category: %+v", i, r)
		}
	}
	if rels[0].Info.Height != 2160 || !rels[0].Info.DolbyVision || rels[2].Name != "Movie.2014.2160p.WEB-DL.DDP5.1.HEVC-GRP" {
		t.Fatalf("names: %+v / %q", rels[0].Info, rels[2].Name)
	}
}

func TestConvertMagnetStreamBecomesRelease(t *testing.T) {
	body := `{"streams":[
	 {"name":"Magnet","title":"Movie 2014 1080p BluRay x264-GRP\n👤 12 💾 2 GB","url":"magnet:?xt=urn:btih:` + hashA + `&dn=Movie"}
	]}`
	req := core.SearchRequest{Type: core.TypeMovie, IDs: core.IDs{IMDB: "tt0816692"}, Title: "Movie", Year: 2014}
	rels := Convert(decodeStreams(t, body), req, Lookup{Type: TypeMovie, ID: "tt0816692"}, ConvertOptions{}).Releases
	if len(rels) != 1 || rels[0].InfoHash != hashA || !strings.HasPrefix(rels[0].MagnetURL, "magnet:?xt=urn:btih:"+hashA) {
		t.Fatalf("magnet stream did not become a release: %+v", rels)
	}
}

func TestConvertExternalAndYouTubeStreamsAreSkippedAndCounted(t *testing.T) {
	body := `{"streams":[
	 {"name":"Web","title":"Movie 2014 1080p BluRay x264-GRP","externalUrl":"https://cdn.example/watch"},
	 {"name":"Trailer","title":"Movie 2014 720p BluRay x264-GRP","ytId":"abcdefghijk"}
	]}`
	req := core.SearchRequest{Type: core.TypeMovie, Title: "Movie", Year: 2014}
	c := Convert(decodeStreams(t, body), req, Lookup{Type: TypeMovie, ID: "tt0816692"}, ConvertOptions{})
	if len(c.Releases) != 0 || c.SkippedNoHash != 2 || c.SkippedAmbiguous != 0 {
		t.Fatalf("rels=%d skippedNoHash=%d skippedAmbiguous=%d want 0/2/0", len(c.Releases), c.SkippedNoHash, c.SkippedAmbiguous)
	}
	// A stremio:/// deep link is no playable link either.
	if _, skipped, ambiguous := convert([]Stream{
		{Title: "Movie 2014 1080p", URL: "stremio:///detail/movie/tt0816692"},
		{Title: "Movie 2014 720p"},
	}, core.SearchRequest{Type: core.TypeMovie, Title: "Movie"}, false); skipped != 2 || ambiguous != 0 {
		t.Fatalf("skippedNoHash=%d skippedAmbiguous=%d want 2/0", skipped, ambiguous)
	}
}

// The counter only counts what would have been a release had it carried a
// hash: an identical hashless duplicate counts once, a stream with no name
// does not count, and conflicting hashes count apart as ambiguous, never as
// "without hash".
func TestConvertSkippedCountsOnlyHashCausedSkips(t *testing.T) {
	rels, skipped, ambiguous := convert([]Stream{
		{Title: "Movie 2014 1080p BluRay x264-GRP", URL: "stremio:///video"},
		{Title: "Movie 2014 1080p BluRay x264-GRP", URL: "stremio:///video"}, // exact duplicate
		{Title: "Movie 2014 720p BluRay x264-GRP", URL: "stremio:///other"},
		{URL: "https://cdn.example/unnamed.mp4"},                                                          // no name: would die anyway
		{Title: "Movie 2014 2160p BluRay x264-GRP", InfoHash: hashA, URL: "magnet:?xt=urn:btih:" + hashB}, // ambiguous
		{Title: "Movie 2014 2160p BluRay x264-GRP", InfoHash: hashA, URL: "magnet:?xt=urn:btih:" + hashB}, // ambiguous duplicate
	}, core.SearchRequest{Type: core.TypeMovie, Title: "Movie"}, false)
	if len(rels) != 0 || skipped != 2 || ambiguous != 1 {
		t.Fatalf("rels=%d skippedNoHash=%d skippedAmbiguous=%d want 0/2/1", len(rels), skipped, ambiguous)
	}

	// Non-packs do not count in a pack search; a hashless pack counts once
	// even when duplicated.
	rels, skipped, ambiguous = convert([]Stream{
		{Title: "Show S01E02 1080p WEB-DL H 264-XYZ"},
		{Title: "Show (2008) S01 (2160p WEB-DL H265 DDP 5.1 - GRP)"},
		{Title: "Show (2008) S01 (2160p WEB-DL H265 DDP 5.1 - GRP)"},
	}, core.SearchRequest{Type: core.TypeEpisode, Title: "Show", Season: 1}, true)
	if len(rels) != 0 || skipped != 1 || ambiguous != 0 {
		t.Fatalf("packs: rels=%d skippedNoHash=%d skippedAmbiguous=%d want 0/1/0", len(rels), skipped, ambiguous)
	}
}

func TestDebridHints(t *testing.T) {
	for name, want := range map[string]string{
		"[RD+] Torrentio\n4k":           "[realdebrid] []",
		"[RD download] Torrentio":       "[] [realdebrid]",
		"[RD⚡] Comet 4K":                "[realdebrid] []",
		"[AD⬇️] Comet":                  "[] [alldebrid]",
		"MediaFusion PM ⏳ 1080p":        "[] [premiumize]",
		"[TB+] [PM+] Multi":             "[torbox premiumize] []",
		"[DL+] Torrentio\n1080p":        "[debridlink] []",
		"[OC download] Torrentio":       "[] [offcloud]",
		"[DL⚡] [OC⬇️] Comet":            "[debridlink] [offcloud]",
		"Torrentio 1080p WEB-DL+ x264":  "[] []",
		"Torrentio\n4k HDR":             "[] []",
		"BRAD+ADVENTURE no service tag": "[] []",
	} {
		c, u := debridHints(name)
		if got := fmt.Sprint(c) + " " + fmt.Sprint(u); got != want {
			t.Errorf("%q: %s, want %s", name, got, want)
		}
	}
}

func FuzzConvert(f *testing.F) {
	f.Add(episodeStreams, "Show", 1, 2)
	f.Add(keyedStreams, "Movie", 0, 0)
	f.Fuzz(func(t *testing.T, text, title string, season, episode int) {
		s := []Stream{{Name: text, Title: text, Description: text, InfoHash: hashA, URL: text}}
		req := core.SearchRequest{Type: core.TypeEpisode, Title: title, Season: season, Episode: episode}
		for _, packs := range []bool{false, true} {
			rels, _, _ := convert(s, req, packs)
			for _, r := range rels {
				if r.InfoHash != hashA || r.Protocol != core.ProtocolTorrent {
					t.Fatalf("%+v", r)
				}
			}
		}
	})
}

// A pack's episode file often names only the episode; the torrent name
// supplies the quality facts ranking needs.
func TestConvertEpisodeFileBorrowsQualityFromTorrentName(t *testing.T) {
	s := []Stream{{Title: "Show S01 2160p WEB-DL DV HEVC DDP5.1 Atmos-GRP\nShow S01E02 Episode Title.mkv\n👤 9 💾 7 GB", InfoHash: hashA}}
	rels, _, _ := convert(s, core.SearchRequest{Type: core.TypeEpisode, Title: "Show", Season: 1, Episode: 2}, false)
	if len(rels) != 1 {
		t.Fatalf("%+v", rels)
	}
	info := rels[0].Info
	if rels[0].Name != "Show S01E02 Episode Title" || info.Height != 2160 || !info.DolbyVision || !info.Atmos || info.Group != "GRP" || fmt.Sprint(info.Episodes) != "[2]" {
		t.Fatalf("%q %+v", rels[0].Name, info)
	}
}

func animeRequest() core.SearchRequest {
	return core.SearchRequest{Type: core.TypeEpisode, IDs: core.IDs{IMDB: "tt2560140", TMDB: 1429}, Title: "Attack on Titan",
		Season: 3, Episode: 15, AbsoluteEpisode: 52, Anime: true}
}

// An anime database entry numbers its own episodes: the entry's third
// episode is the requested S3E15, a series-absolute number keeps its own
// identity, and any other number is not relabelled as the requested episode.
func TestConvertAnimeEntryEpisodeIdentity(t *testing.T) {
	body := `{"streams":[
	 {"name":"Anime","title":"[SubsPlease] Shingeki no Kyojin - 03 (1080p) [ABCD1234].mkv\n👤 40 💾 1.4 GB","infoHash":"` + hashA + `"},
	 {"name":"Anime","title":"[Group] Shingeki no Kyojin - 52 (1080p)","infoHash":"` + hashB + `"},
	 {"name":"Anime","title":"[Other] Shingeki no Kyojin - 07 (1080p)","infoHash":"` + hashC + `"}
	]}`
	c := Convert(decodeStreams(t, body), animeRequest(), Lookup{Type: TypeSeries, ID: "kitsu:41982:3", EntryEpisode: 3}, ConvertOptions{})
	rels := c.Releases
	if len(rels) != 3 {
		t.Fatalf("releases %+v", rels)
	}
	if i := rels[0].Info; len(i.Seasons) != 1 || i.Seasons[0] != 3 || len(i.Episodes) != 1 || i.Episodes[0] != 15 {
		t.Fatalf("entry episode identity %+v", i)
	}
	if i := rels[1].Info; len(i.Seasons) != 0 || len(i.AbsoluteEpisodes) != 1 || i.AbsoluteEpisodes[0] != 52 {
		t.Fatalf("absolute release %+v", i)
	}
	if i := rels[2].Info; len(i.Seasons) != 0 || (len(i.Episodes) > 0 && i.Episodes[0] == 15) || len(i.AbsoluteEpisodes) == 0 || i.AbsoluteEpisodes[0] != 7 {
		t.Fatalf("other number relabelled %+v", i)
	}
}

func TestConvertHTTPStreamIdentityIgnoresCredentialsAndNamesItsAddon(t *testing.T) {
	mk := func(u string) []Stream {
		return []Stream{{Name: "[RD+] Addon 4K", Title: "Movie 2014 2160p WEB-DL x265-GRP", URL: u, BehaviorHints: StreamHints{VideoSize: 4 << 30}}}
	}
	req := core.SearchRequest{Type: core.TypeMovie, Title: "Movie"}
	conv := func(streams []Stream, instance string) []core.Release {
		return Convert(streams, req, Lookup{}, ConvertOptions{Instance: instance}).Releases
	}
	a := conv(mk("https://addon.example/play/"+secret+"/token-1"), "ix1")
	b := conv(mk("https://addon.example/play/"+secret+"/token-2"), "ix1")
	c := conv(mk("https://addon.example/play/"+secret+"/token-1"), "ix2")
	if len(a) != 1 || a[0].GUID != b[0].GUID {
		t.Fatalf("GUID changed with the link's token: %q vs %q", a[0].GUID, b[0].GUID)
	}
	if a[0].GUID == c[0].GUID {
		t.Fatal("two configured add-ons share an HTTP release identity")
	}
	if strings.Contains(a[0].GUID, secret) || a[0].DebridCached[0] != config.DebridRealDebrid {
		t.Fatalf("release %+v", a[0])
	}
	// A different file or size is a different release.
	d := conv([]Stream{{Name: "[RD+] Addon 4K", Title: "Movie 2014 2160p WEB-DL x265-GRP", URL: "https://addon.example/x", BehaviorHints: StreamHints{VideoSize: 5 << 30}}}, "ix1")
	if d[0].GUID == a[0].GUID {
		t.Fatal("size not part of the identity")
	}
}

func TestConvertTorrentStreamKeepsAddonLinkAsFallback(t *testing.T) {
	streams := []Stream{{Name: "[RD+] T", Title: "Movie 2014 1080p BluRay x264-GRP", URL: "https://addon.example/resolve/realdebrid/" + secret + "/" + hashA + "/null/0/Movie.mkv"}}
	rels := func() []core.Release {
		c := Convert(streams, core.SearchRequest{Type: core.TypeMovie, Title: "Movie"}, Lookup{}, ConvertOptions{Origin: "https://addon.example"})
		return c.Releases
	}()
	if len(rels) != 1 || rels[0].Protocol != core.ProtocolTorrent || rels[0].InfoHash != hashA || rels[0].StreamURL == "" || rels[0].StreamOrigin != "https://addon.example" {
		t.Fatalf("release %+v", rels)
	}
	// A magnet is no HTTP fallback.
	rels, _, _ = convert([]Stream{{Title: "Movie 2014 1080p BluRay x264-GRP", URL: "magnet:?xt=urn:btih:" + hashA}}, core.SearchRequest{Type: core.TypeMovie, Title: "Movie"}, false)
	if len(rels) != 1 || rels[0].StreamURL != "" {
		t.Fatalf("magnet release %+v", rels)
	}
}

func TestStreamRequestHeadersAreBounded(t *testing.T) {
	s := Stream{}
	s.BehaviorHints.ProxyHeaders.Request = map[string]string{
		"referer": "https://site.example/", "Range": "bytes=0-", "Host": "evil.example",
		"X-Inject": "a\r\nX-Other: b", "User-Agent": "AddonPlayer/1.0",
	}
	got := s.RequestHeaders()
	if len(got) != 2 || got["Referer"] != "https://site.example/" || got["User-Agent"] != "AddonPlayer/1.0" {
		t.Fatalf("headers %v", got)
	}
}

func TestConvertUncachedHTTPStreamKeepsItsLabel(t *testing.T) {
	rels, _, _ := convert([]Stream{{Name: "[RD download] Addon", Title: "Movie 2014 1080p BluRay x264-GRP", URL: "https://addon.example/x"}},
		core.SearchRequest{Type: core.TypeMovie, Title: "Movie"}, false)
	if len(rels) != 1 || len(rels[0].DebridUncached) != 1 || len(rels[0].DebridCached) != 0 {
		t.Fatalf("release %+v", rels)
	}
}
