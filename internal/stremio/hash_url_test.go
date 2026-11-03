package stremio

import (
	"net/url"
	"strings"
	"testing"
)

func TestAddonURLHashIgnoresCredentials(t *testing.T) {
	key := strings.Repeat("e", 40)
	magnetA := "magnet:?xt=urn:btih:" + hashA + "&dn=Movie"
	magnetB := "magnet:?xt=urn:btih:" + hashB
	for _, tc := range []struct {
		name, url, want string
		ambiguous       bool
	}{
		{"resolver key before hash", "https://addon.test/resolve/realdebrid/" + key + "/" + hashA + "/null/0/movie.mkv", hashA, false},
		{"query key is not hash", "https://addon.test/playback?token=" + key, "", false},
		{"opaque path is not hash", "https://addon.test/playback/" + key + "/file.mkv", "", false},
		{"named query wins over path credential", "https://addon.test/auth/" + key + "/play?info_hash=" + hashA, hashA, false},
		{"conflicting named hashes", "https://addon.test/play?info_hash=" + hashA + "&infoHash=" + hashB, "", true},
		{"invalid named hash is absence", "https://addon.test/play?info_hash=lixo&info_hash=" + hashA, hashA, false},
		{"magnet URL", magnetA, hashA, false},
		{"magnet URL uppercase hash", "magnet:?xt=urn:btih:" + strings.ToUpper(hashA), hashA, false},
		{"magnet URL internally conflicting", "magnet:?xt=urn:btih:" + hashA + "&xt=urn:btih:" + hashB, "", true},
		{"query magnet", "https://addon.test/play?magnet=" + url.QueryEscape(magnetA), hashA, false},
		{"query magnet agrees with named hash", "https://addon.test/play?magnet=" + url.QueryEscape(magnetA) + "&info_hash=" + hashA, hashA, false},
		{"query magnet conflicts with named hash", "https://addon.test/play?magnet=" + url.QueryEscape(magnetA) + "&info_hash=" + hashB, "", true},
		{"query magnet conflicts with other magnet", "https://addon.test/play?magnet=" + url.QueryEscape(magnetA) + "&magnet=" + url.QueryEscape(magnetB), "", true},
		{"hex in query magnet name is not hash", "https://addon.test/play?magnet=" + url.QueryEscape("magnet:?dn="+hashA), "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ambiguous := hashFromURL(tc.url)
			if got != tc.want || ambiguous != tc.ambiguous {
				t.Fatalf("hash=%q ambiguous=%v want %q/%v", got, ambiguous, tc.want, tc.ambiguous)
			}
		})
	}
}

func TestStreamHashFromLabeledSources(t *testing.T) {
	magnetA := "magnet:?xt=urn:btih:" + hashA
	magnetB := "magnet:?xt=urn:btih:" + hashB
	for _, tc := range []struct {
		name      string
		s         Stream
		want      string
		ambiguous bool
	}{
		{"dht source", Stream{Sources: []string{"tracker:udp://tracker.example:1337/announce", "dht:" + hashA}}, hashA, false},
		{"dht source uppercase", Stream{Sources: []string{"dht:" + strings.ToUpper(hashA)}}, hashA, false},
		{"magnet source", Stream{Sources: []string{magnetB}}, hashB, false},
		{"dht and magnet agree", Stream{Sources: []string{"dht:" + hashA, magnetA}}, hashA, false},
		{"dht and magnet conflict", Stream{Sources: []string{"dht:" + hashA, magnetB}}, "", true},
		{"infoHash válido + URL internamente conflitante", Stream{InfoHash: hashA, URL: "https://addon.test/play?info_hash=" + hashA + "&info_hash=" + hashB}, "", true},
		{"sources internamente conflitantes + topo válido", Stream{InfoHash: hashA, Sources: []string{"dht:" + hashA, magnetB}}, "", true},
		{"inválido é ausência: infoHash inválido + URL válida", Stream{InfoHash: "não-é-hash", URL: "https://addon.test/play?info_hash=lixo&info_hash=" + hashA}, hashA, false},
		{"inválido é ausência: dht inválido + dht válido", Stream{Sources: []string{"dht:lixo", "dht:" + hashA}}, hashA, false},
		{"magnet URL and dht agree", Stream{URL: magnetA, Sources: []string{"dht:" + hashA}}, hashA, false},
		{"magnet URL and dht conflict", Stream{URL: magnetA, Sources: []string{"dht:" + hashB}}, "", true},
		{"infoHash and magnet URL conflict", Stream{InfoHash: hashB, URL: magnetA}, "", true},
		{"query magnet and dht conflict", Stream{URL: "https://addon.test/play?magnet=" + url.QueryEscape(magnetA), Sources: []string{"dht:" + hashB}}, "", true},
		{"no labeled hash in text fields", Stream{Name: hashA, Title: hashA, Description: hashA, BehaviorHints: StreamHints{Filename: hashA + ".mkv"}}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, ambiguous := streamHash(&tc.s); got != tc.want || ambiguous != tc.ambiguous {
				t.Fatalf("hash=%q ambiguous=%v want %q/%v", got, ambiguous, tc.want, tc.ambiguous)
			}
		})
	}
}
