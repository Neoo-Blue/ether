package wrapped

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
	"github.com/braedonsaunders/ether/internal/viewspec"
)

type fixtureHost struct {
	plugins.Host
	history []byte
}

func (h fixtureHost) ViewerGranted(context.Context) (json.RawMessage, error) {
	return json.RawMessage(`[{"id":1,"name":"Viewer"}]`), nil
}
func (h fixtureHost) ViewerHistory(_ context.Context, id int64, _, _ int) (json.RawMessage, error) {
	if id != 1 {
		return nil, plugins.ErrPermissionDenied
	}
	return h.history, nil
}
func (h fixtureHost) CatalogGet(_ context.Context, id string) (json.RawMessage, error) {
	if id != "1000000000000001" {
		return nil, nil
	}
	return json.RawMessage(`{"id":"1000000000000001","title":"Example","type":"movie","imdbId":"tt0000001","genres":["Drama"]}`), nil
}
func TestHostedPageAndList(t *testing.T) {
	raw, e := os.ReadFile("manifest.json")
	if e != nil {
		t.Fatal(e)
	}
	m, e := plugins.ParseManifest(raw)
	if e != nil {
		t.Fatal(e)
	}
	js, e := os.ReadFile("dist/plugin.js")
	if e != nil {
		t.Fatal(e)
	}
	history, e := os.ReadFile("tests/fixtures/history.json")
	if e != nil {
		t.Fatal(e)
	}
	eng := wasm.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer eng.Close()
	rt, e := plugins.HostedRuntimeFactory(eng)(plugins.Installed{ID: m.ID, Manifest: m, Host: fixtureHost{history: history}, Files: func(p string) ([]byte, bool) { return js, p == "dist/plugin.js" }})
	if e != nil {
		t.Fatal(e)
	}
	defer rt.Close()
	for _, req := range []plugins.Request{{Method: "GET", Path: "/ether/view/year", Query: map[string]string{"viewer": "1", "year": "2025"}}, {Method: "GET", Path: "/catalog/movie/year/viewerId=1&year=2025.json"}} {
		resp, e := rt.Call(context.Background(), req)
		if e != nil || resp.Status != 200 || !strings.Contains(string(resp.Body), "Example") {
			t.Fatalf("response=%+v err=%v body=%s", resp, e, resp.Body)
		}
		if strings.HasPrefix(req.Path, "/ether/view/") {
			var doc struct {
				Spec json.RawMessage `json:"spec"`
			}
			if err := json.Unmarshal(resp.Body, &doc); err != nil {
				t.Fatal(err)
			}
			if result := viewspec.ValidatePlugin(doc.Spec); !result.OK {
				t.Fatalf("invalid view: %+v", result.Errors)
			}
		}
	}
}
