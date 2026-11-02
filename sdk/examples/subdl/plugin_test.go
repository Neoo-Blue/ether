package subdl_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/plugins/wasm"
)

type fixtureHost struct {
	plugins.Host
	calls      []string
	badArchive bool
}

func (h *fixtureHost) SecretGet(_ context.Context, name string) (string, error) {
	if name != "apiKey" {
		return "", nil
	}
	return "fixture-key", nil
}
func (h *fixtureHost) HTTPFetch(_ context.Context, req plugins.FetchRequest) (plugins.FetchResponse, error) {
	h.calls = append(h.calls, req.URL)
	file := "testdata/search.json"
	if strings.HasPrefix(req.URL, "https://dl.subdl.com/") {
		if h.badArchive {
			return plugins.FetchResponse{Status: 200, Body: []byte("not a zip")}, nil
		}
		file = "testdata/subtitle.zip"
	}
	b, err := os.ReadFile(file)
	return plugins.FetchResponse{Status: 200, Body: b}, err
}
func TestHostedSearchAndPick(t *testing.T) {
	manBytes, err := os.ReadFile("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	man, err := plugins.ParseManifest(manBytes)
	if err != nil {
		t.Fatal(err)
	}
	host := &fixtureHost{}
	eng := wasm.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer eng.Close()
	rt, err := plugins.HostedRuntimeFactory(eng)(plugins.Installed{ID: man.ID, Manifest: man, Host: host, Files: func(path string) ([]byte, bool) { b, e := os.ReadFile(path); return b, e == nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	call := func(req plugins.Request) plugins.Response {
		t.Helper()
		r, e := rt.Call(context.Background(), req)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	configure := call(plugins.Request{Method: "POST", Path: "/ether/configure", Body: []byte(`{"apiKey":"fixture-key"}`)})
	if configure.Status != 200 {
		t.Fatal(string(configure.Body))
	}
	list := call(plugins.Request{Method: "GET", Path: "/subtitles/movie/tt1234567.json"})
	if list.Status != 200 {
		t.Fatalf("list %d %s", list.Status, list.Body)
	}
	var found struct {
		Subtitles []struct{ ID, Lang, Format string } `json:"subtitles"`
	}
	if err := json.Unmarshal(list.Body, &found); err != nil || len(found.Subtitles) != 1 {
		t.Fatalf("list %s %v", list.Body, err)
	}
	if len(host.calls) != 1 || !strings.HasPrefix(host.calls[0], "https://api.subdl.com/") {
		t.Fatal(host.calls)
	}
	body := []byte(`{"id":"/subtitle/123-456.zip","lang":"en","format":"srt"}`)
	denied := call(plugins.Request{Method: "POST", Path: "/ether/subtitles/download", Body: body})
	if denied.Status != 403 || len(host.calls) != 1 {
		t.Fatalf("unpicked: %d %v", denied.Status, host.calls)
	}
	got := call(plugins.Request{Method: "POST", Path: "/ether/subtitles/download", Headers: map[string]string{"X-Ether-Pick": "fixture"}, Body: body})
	if got.Status != 200 || !strings.Contains(string(got.Body), "Hello there") {
		t.Fatalf("download %d %s", got.Status, got.Body)
	}
	if len(host.calls) != 2 || !strings.HasPrefix(host.calls[1], "https://dl.subdl.com/") {
		t.Fatal(host.calls)
	}
	host.badArchive = true
	invalid := call(plugins.Request{Method: "POST", Path: "/ether/subtitles/download", Headers: map[string]string{"X-Ether-Pick": "another"}, Body: body})
	if invalid.Status < 400 {
		t.Fatalf("invalid archive accepted: %d %s", invalid.Status, invalid.Body)
	}

}
