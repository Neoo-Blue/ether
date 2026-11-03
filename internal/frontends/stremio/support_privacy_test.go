package stremio

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/diagnostics"
	"github.com/braedonsaunders/ether/internal/frontend"
	"github.com/braedonsaunders/ether/internal/frontend/frontendtest"
)

func TestSupportJournalNeverExportsStremioCredential(t *testing.T) {
	const secret = "silo-grant-abc123456789"
	rec := diagnostics.New(diagnostics.Deps{})
	identity := frontendtest.Identity{ResolveFunc: func(_ context.Context, token string) (*core.Viewer, error) {
		if token != secret {
			return nil, core.ErrUnauthorized
		}
		return &core.Viewer{ID: 1}, nil
	}}
	inst, err := (Frontend{}).Mount(context.Background(), frontend.Host{Support: rec, Identity: identity, Experience: frontendtest.Experience{}, Sessions: frontendtest.Sessions{}})
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close(context.Background())
	call := func(path string) {
		t.Helper()
		w := httptest.NewRecorder()
		inst.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != 200 {
			t.Fatalf("response %d", w.Code)
		}
	}
	call("/stremio/" + secret + "/manifest.json")
	devices := rec.Devices()
	if len(devices) != 1 {
		t.Fatalf("devices: %v", devices)
	}
	cap, err := rec.Start("owner", diagnostics.StartIn{DeviceID: devices[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	call("/stremio/" + secret + "/catalog/movie/ether-movies.json")
	if _, err = rec.Finish("owner", cap.ID, diagnostics.FinishIn{}); err != nil {
		t.Fatal(err)
	}
	blob, err := rec.Archive("owner", cap.ID)
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
	if err != nil {
		t.Fatal(err)
	}
	var events string
	for _, f := range z.File {
		r, _ := f.Open()
		data, _ := io.ReadAll(r)
		r.Close()
		if strings.Contains(string(data), secret) {
			t.Fatalf("credential leaked in %s", f.Name)
		}
		if f.Name == "events.jsonl" {
			events = string(data)
		}
	}
	if !strings.Contains(events, `"protocol":"stremio"`) || !strings.Contains(events, "/catalog/movie/") {
		t.Fatalf("missing journal: %s", events)
	}
}
