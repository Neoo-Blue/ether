package livetv

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStremioImportRefreshAndGuide(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/secret/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"tv.example","resources":["catalog","stream"],"types":["tv"],"catalogs":[{"type":"tv","id":"live","name":"News"}],"behaviorHints":{"epgUrl":"/guide.xml"}}`)
	})
	mux.HandleFunc("/secret/catalog/tv/live.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"metas":[{"id":"channel1","name":"Example","logo":"https://img.example/logo.png"}]}`)
	})
	mux.HandleFunc("/secret/stream/tv/channel1.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"streams":[{"url":"https://stream.example/one"},{"url":"https://stream.example/two"}]}`)
	})
	mux.HandleFunc("/guide.xml", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<tv><channel id="channel1"><display-name>Example</display-name></channel><programme channel="channel1" start="20261102090000 +0000" stop="20261102100000 +0000"><title>News</title></programme></tv>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	svc := testService(t, Options{StremioHTTP: srv.Client(), Now: func() time.Time { return time.Date(2026, 11, 2, 9, 30, 0, 0, time.UTC) }})
	src, err := svc.ImportSource(context.Background(), Source{ID: "s", Name: "Test", Kind: "stremio", PlaylistURL: srv.URL + "/secret/manifest.json"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(src.PlaylistURL, "secret") || src.PlaylistURL != redactedSecret {
		t.Fatalf("source leaked: %+v", src)
	}
	channels, err := svc.Channels(context.Background())
	if err != nil || len(channels) != 1 || len(channels[0].StreamSources) != 2 || channels[0].Number != "1" {
		t.Fatalf("channels=%+v err=%v", channels, err)
	}
	var count int
	if err := svc.o.DB.QueryRow(`SELECT COUNT(*) FROM livetv_programmes WHERE source_id='s'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("guide count=%d err=%v", count, err)
	}
	if _, err := svc.RefreshSource(context.Background(), "s"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ImportSource(context.Background(), src); err != nil {
		t.Fatal(err)
	}
}

func TestStremioNativeGuide(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/manifest.json":
			fmt.Fprint(w, `{"id":"guide","resources":["catalog","stream"],"types":["tv"],"catalogs":[{"type":"tv","id":"live","extra":[{"name":"date"}]}],"behaviorHints":{"epgProvider":true}}`)
		case strings.Contains(r.URL.Path, "date=") && !strings.Contains(r.URL.Path, "skip="):
			fmt.Fprint(w, `{"metasDetailed":[{"id":"c","name":"Channel","videos":[{"id":"p","title":"News","startTime":"2026-11-02T09:00:00Z","endTime":"2026-11-02T10:00:00Z"}]}]}`)
		case strings.Contains(r.URL.Path, "skip="):
			fmt.Fprint(w, `{"metasDetailed":[]}`)
		case strings.HasPrefix(r.URL.Path, "/catalog/"):
			fmt.Fprint(w, `{"metas":[{"id":"c","name":"Channel"}]}`)
		case strings.HasPrefix(r.URL.Path, "/stream/"):
			fmt.Fprint(w, `{"streams":[{"url":"https://example.test/live"}]}`)
		}
	}))
	defer srv.Close()
	s := testService(t, Options{StremioHTTP: srv.Client(), Now: func() time.Time { return time.Date(2026, 11, 2, 9, 30, 0, 0, time.UTC) }})
	if _, err := s.ImportSource(context.Background(), Source{ID: "native", Name: "Native", Kind: "stremio", PlaylistURL: srv.URL + "/manifest.json"}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.o.DB.QueryRow(`SELECT COUNT(*) FROM livetv_programmes WHERE source_id='native'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("programmes=%d error=%v", n, err)
	}
}

func TestStremioStreamURLsRedactedAndRestored(t *testing.T) {
	s := testService(t, Options{})
	ctx := context.Background()
	src := Source{ID: "s", Name: "Source", Kind: "stremio", PlaylistURL: "https://addon.example/secret/manifest.json"}
	raw, err := s.encodeSource(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.o.DB.Exec(`INSERT INTO livetv_sources(id,data) VALUES(?,?)`, src.ID, raw); err != nil {
		t.Fatal(err)
	}
	original := Channel{ID: "c", Name: "Channel", Number: "1", SourceID: "s", Kind: "iptv", Enabled: true, StreamURL: "https://example.test/live?token=secret", StreamSources: []StreamSource{{ID: "stremio-1", URL: "https://example.test/live?token=secret", Enabled: true}}}
	if _, err = s.SaveChannel(ctx, original); err != nil {
		t.Fatal(err)
	}
	cs, err := s.Channels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s.redactAddonChannels(ctx, cs)
	if cs[0].StreamURL != redactedSecret || cs[0].StreamSources[0].URL != redactedSecret {
		t.Fatalf("not redacted: %+v", cs)
	}
	if err = s.restoreAddonChannel(ctx, &cs[0]); err != nil {
		t.Fatal(err)
	}
	if cs[0].StreamSources[0].URL != original.StreamURL {
		t.Fatal("stream URL was not restored")
	}
	cs[0].StreamSources[0].ID = "stremio-other"
	cs[0].StreamSources[0].URL = redactedSecret
	if s.restoreAddonChannel(ctx, &cs[0]) == nil {
		t.Fatal("moved secret was restored")
	}
}
