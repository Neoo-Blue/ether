package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/plextv"
	"github.com/braedonsaunders/ether/internal/resolver"
	"github.com/braedonsaunders/ether/internal/safety"
)

func postQualityExplain(t *testing.T, a *App, body string) (int, resolver.QualityAnswer) {
	t.Helper()
	w := httptest.NewRecorder()
	a.qualityHandler().ServeHTTP(w, httptest.NewRequest("POST", "/explain", strings.NewReader(body)))
	var out resolver.QualityAnswer
	if w.Code == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
	}
	return w.Code, out
}

func TestQualityExplainHandler(t *testing.T) {
	ctx := context.Background()
	a := setupTestApp(t, true)
	if err := a.users.SyncFriends(ctx, []plextv.Friend{{PlexID: 77, Username: "alex", AllLibs: true}}); err != nil {
		t.Fatal(err)
	}
	users, err := a.users.Users(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var viewer int64
	for _, u := range users {
		if u.PlexID == 77 {
			viewer = u.ID
		}
	}
	if viewer == 0 {
		t.Fatal("viewer missing")
	}
	off := false
	if err := a.cfg.Update(func(c *config.Config) error {
		c.Quality = config.Quality{
			Profiles: []core.QualityProfile{{Name: "At home", MaxHeight: 2160, AllowHDR: true, AllowDolbyVision: true}, {Name: "Away", MaxHeight: 1080}},
			Default:  "At home", Remote: "Away",
		}
		c.Transcode.RemoteMaxBitrateKbps = 12000
		c.Clients.Overrides = []core.ClientCapabilityOverride{{ID: "roku", DolbyVision: &off, HDR10: &off}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.safety.Put(ctx, viewer, safety.Policy{MaxBitrateKbps: 6000}); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	a.qualityHandler().ServeHTTP(w, httptest.NewRequest("GET", "/clients", nil))
	var list struct{ Clients []qualityClient }
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Clients) == 0 {
		t.Fatal(w.Code, w.Body.String())
	}

	code, got := postQualityExplain(t, a, `{"userId":`+strconv.FormatInt(viewer, 10)+`,"clientId":"plex-apple-tv","remote":false}`)
	if code != 200 || got.Profile != "At home" || got.MaxHeight != 2160 || !got.HDR || !got.DolbyVision || got.BitrateKbps != 6000 {
		t.Fatalf("%d %+v", code, got)
	}
	code, got = postQualityExplain(t, a, `{"userId":`+strconv.FormatInt(viewer, 10)+`,"clientId":"plex-apple-tv","remote":true}`)
	if code != 200 || got.Profile != "Away" || got.MaxHeight != 1080 || got.BitrateKbps != 6000 {
		t.Fatalf("%d %+v", code, got)
	}
	// The owner's App compatibility edit turns HDR off on Roku.
	code, got = postQualityExplain(t, a, `{"userId":`+strconv.FormatInt(viewer, 10)+`,"clientId":"plex-roku"}`)
	if code != 200 || got.HDR {
		t.Fatalf("%d %+v", code, got)
	}
	for _, l := range got.Layers {
		if l.Kind == resolver.LayerDevice && !l.Edited {
			t.Fatalf("edit not reported: %+v", l)
		}
	}
	// A custom identity goes through the same registry.
	code, got = postQualityExplain(t, a, `{"userId":`+strconv.FormatInt(viewer, 10)+`,"client":{"product":"Plex for Android (Mobile)","platform":"Android"}}`)
	if code != 200 || got.MaxHeight != 1080 {
		t.Fatalf("%d %+v", code, got)
	}

	for _, bad := range []string{`{"userId":` + strconv.FormatInt(viewer, 10) + `}`, `{"userId":` + strconv.FormatInt(viewer, 10) + `,"clientId":"nope"}`, `nope`} {
		if code, _ := postQualityExplain(t, a, bad); code != 400 {
			t.Fatalf("%s: %d", bad, code)
		}
	}
	if code, _ := postQualityExplain(t, a, `{"userId":999999,"clientId":"plex-apple-tv"}`); code != 404 {
		t.Fatal(code)
	}
}
