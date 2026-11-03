package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestSubtitlePickEnforcedAndSingleUse(t *testing.T) {
	e := newTestEnv(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e.m.now = func() time.Time { return now }
	req := Request{Method: "POST", Path: "/ether/subtitles/download", Body: json.RawMessage(`{"id":"sub","lang":"en","format":"srt"}`), Headers: map[string]string{"X-Ether-Title": "title"}}
	if resp, err := e.m.Call(context.Background(), "plugin", req); !errors.Is(err, ErrPermissionDenied) || resp.Status != 403 {
		t.Fatalf("missing pick: %+v %v", resp, err)
	}
	token, err := e.m.MintSubtitlePick("plugin", "title", "sub")
	if err != nil {
		t.Fatal(err)
	}
	if !e.m.consumeSubtitlePick(token, "plugin", "title", "sub") {
		t.Fatal("valid token rejected")
	}
	if e.m.consumeSubtitlePick(token, "plugin", "title", "sub") {
		t.Fatal("token reused")
	}
	token, _ = e.m.MintSubtitlePick("plugin", "title", "sub")
	now = now.Add(time.Minute)
	if e.m.consumeSubtitlePick(token, "plugin", "title", "sub") {
		t.Fatal("expired token accepted")
	}
	token, _ = e.m.MintSubtitlePick("plugin", "title", "sub")
	if e.m.consumeSubtitlePick(token, "plugin", "other", "sub") {
		t.Fatal("wrong title accepted")
	}
	if e.m.consumeSubtitlePick(token, "plugin", "title", "sub") {
		t.Fatal("mismatched token reused")
	}
}

func TestSubtitlePickManagerCall(t *testing.T) {
	e := newTestEnv(t)
	rec := e.installHello(t, "1.0.0")
	ctx := context.Background()
	e.fake.routes["/ether/subtitles/download"] = func(req Request) (Response, error) {
		if req.Headers["X-Ether-Pick"] == "" {
			t.Fatal("missing pick header")
		}
		return Response{Status: 200, Body: json.RawMessage(`{"format":"srt","encoding":"utf8","data":"x"}`)}, nil
	}
	token, err := e.m.MintSubtitlePick(rec.ID, "title", "sub")
	if err != nil {
		t.Fatal(err)
	}
	req := Request{Method: "POST", Path: "/ether/subtitles/download", Headers: map[string]string{"X-Ether-Pick": token, "X-Ether-Title": "title"}, Body: json.RawMessage(`{"id":"sub"}`)}
	resp, err := e.m.Call(ctx, rec.ID, req)
	if err != nil || resp.Status != 200 {
		t.Fatalf("picked call: %+v %v", resp, err)
	}
	if _, err = e.m.Call(ctx, rec.ID, req); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("reuse: %v", err)
	}
	if _, _, err = e.m.CallStream(ctx, rec.ID, req); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("stream bypass: %v", err)
	}
}

func TestDirectSubtitlePickAndDeclaredHosts(t *testing.T) {
	ctx := context.Background()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; _, _ = w.Write([]byte("subtitle")) }))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	e := newTestEnv(t)
	e.m.client = server.Client()
	var man map[string]any
	_ = json.Unmarshal(helloManifest("1.0.0"), &man)
	eth := man["ether"].(map[string]any)
	eth["permissions"].(map[string]any)["network"] = []string{u.Hostname()}
	raw, _ := json.Marshal(man)
	preview, err := e.m.PreviewInstall(ctx, Source{Kind: "studio", Manifest: raw, Files: helloFiles()})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := e.m.Install(ctx, preview, InstallOptions{AcceptAddedPermissions: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.m.FetchPickedSubtitle(ctx, rec.ID, "title", "sub", "", server.URL); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("missing token: %v", err)
	}
	token, _ := e.m.MintSubtitlePick(rec.ID, "title", "sub")
	data, err := e.m.FetchPickedSubtitle(ctx, rec.ID, "title", "sub", token, server.URL)
	if err != nil || string(data) != "subtitle" || calls != 1 {
		t.Fatalf("picked fetch %q %d %v", data, calls, err)
	}
	if _, err = e.m.FetchPickedSubtitle(ctx, rec.ID, "title", "sub", token, server.URL); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("reused token: %v", err)
	}
	token, _ = e.m.MintSubtitlePick(rec.ID, "title", "sub")
	if _, err = e.m.FetchPickedSubtitle(ctx, rec.ID, "title", "sub", token, "https://unlisted.example/sub.srt"); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("unlisted host: %v", err)
	}
}

func TestForeignSubtitleDownloadRequiresBoundSelection(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("1\n00:00:00,000 --> 00:00:01,000\nTest\n"))
	}))
	defer server.Close()
	e.m.client = server.Client() // fixture transport; production retains safefetch
	raw := json.RawMessage(`{"id":"org.example.subs","version":"1.0.0","name":"Subtitles","resources":["subtitles"],"types":["movie"]}`)
	preview, err := e.m.PreviewInstall(ctx, Source{Kind: "studio", Manifest: raw})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := e.m.Install(ctx, preview, InstallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	token, _ := e.m.MintSubtitlePick(rec.ID, "title", "sub")
	if _, err = e.m.FetchPickedSubtitle(ctx, rec.ID, "title", "sub", token, server.URL); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("unbound URL authorized: %v", err)
	}
	token, _ = e.m.MintSubtitleURLPick(rec.ID, "title", "sub", server.URL)
	if _, err = e.m.FetchPickedSubtitle(ctx, rec.ID, "title", "sub", token, server.URL+"/other"); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("substituted URL authorized: %v", err)
	}
	token, _ = e.m.MintSubtitleURLPick(rec.ID, "title", "sub", server.URL)
	data, err := e.m.FetchPickedSubtitle(ctx, rec.ID, "title", "sub", token, server.URL)
	if err != nil || len(data) == 0 {
		t.Fatalf("selected download: %q %v", data, err)
	}
	if _, err = e.m.FetchPickedSubtitle(ctx, rec.ID, "title", "sub", token, server.URL); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("pick reused: %v", err)
	}
}
