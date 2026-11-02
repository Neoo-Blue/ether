package plex

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/livetv"
	"github.com/braedonsaunders/ether/internal/store"
)

func TestStremioNativeGuideOnPlexGrid(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/secret/manifest.json":
			fmt.Fprint(w, `{"id":"tv","resources":["catalog","stream"],"types":["tv"],"catalogs":[{"type":"tv","id":"live","extra":[{"name":"date"}]}],"behaviorHints":{"epgProvider":true}}`)
		case strings.Contains(r.URL.Path, "date=") && !strings.Contains(r.URL.Path, "skip="):
			fmt.Fprint(w, `{"metasDetailed":[{"id":"c","name":"Test channel","videos":[{"id":"p","title":"Test broadcast","startTime":"2026-10-18T11:00:00Z","endTime":"2026-10-18T13:00:00Z"}]}]}`)
		case strings.Contains(r.URL.Path, "skip="):
			fmt.Fprint(w, `{"metasDetailed":[]}`)
		case strings.Contains(r.URL.Path, "/catalog/"):
			fmt.Fprint(w, `{"metas":[{"id":"c","name":"Test channel"}]}`)
		case strings.Contains(r.URL.Path, "/stream/"):
			fmt.Fprint(w, `{"streams":[{"url":"https://example.test/live"}]}`)
		}
	}))
	defer upstream.Close()
	db, err := store.OpenMemory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := livetv.New(livetv.Options{DB: db, Now: func() time.Time { return fixedNow }, StremioHTTP: upstream.Client()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.ImportSource(context.Background(), livetv.Source{Name: "Test", Kind: "stremio", PlaylistURL: upstream.URL + "/secret/manifest.json"}); err != nil {
		t.Fatal(err)
	}
	channels, err := s.Channels(context.Background())
	if err != nil || len(channels) != 1 {
		t.Fatalf("channels=%+v err=%v", channels, err)
	}
	f := newFixture()
	defer f.srv.Close()
	f.srv.d.LiveTV = s
	f.srv.d.Identity.MyPlexSubscription = true
	res := f.request(t, "GET", liveProviderPath+"/grid?date=2026-10-18&channelGridKey="+channels[0].ID, map[string]string{"Accept": "application/json"})
	if res.Code != 200 || !strings.Contains(res.Body.String(), "Test broadcast") || strings.Contains(res.Body.String(), "secret") {
		t.Fatalf("grid: %d %s", res.Code, res.Body.String())
	}
}
