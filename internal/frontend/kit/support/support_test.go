package support

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/braedonsaunders/ether/internal/diagnostics"
	"github.com/braedonsaunders/ether/internal/frontend"
)

func TestAuthenticatedJournalIsBoundedAndDeviceScoped(t *testing.T) {
	rec := diagnostics.New(diagnostics.Deps{})
	dev := frontend.Device{ID: "tv", Product: "Test client", DeviceName: "Living room"}
	viewer := &frontend.Viewer{ID: 7, Username: "viewer"}
	Sighting(rec, "jellyfin", dev)
	devices := rec.Devices()
	if len(devices) != 1 {
		t.Fatal(devices)
	}
	capture, err := rec.Start("owner", diagnostics.StartIn{DeviceID: devices[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"Items":[{"Name":"An item"}],"TotalRecordCount":1}`)
	})
	handler := Wrap(rec, "jellyfin", func(r *http.Request) (*frontend.Viewer, frontend.Device) {
		if r.Header.Get("X-Test-Device") == "other" {
			d := dev
			d.ID = "other"
			return viewer, d
		}
		return viewer, dev
	}, func(path string) string { return path }, next)
	for _, id := range []string{"tv", "other"} {
		r := httptest.NewRequest("GET", "/Items?api_key=unknown-secret", nil)
		r.Header.Set("X-Test-Device", id)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "An item") {
			t.Fatalf("changed response: %d %s", w.Code, w.Body.String())
		}
	}
	_, err = rec.Finish("owner", capture.ID, diagnostics.FinishIn{SetFlags: true, IncludeTitles: true})
	if err != nil {
		t.Fatal(err)
	}
	blob, err := rec.Archive("owner", capture.ID)
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
	if err != nil {
		t.Fatal(err)
	}
	var events string
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(r)
		r.Close()
		if strings.Contains(string(data), "unknown-secret") {
			t.Fatalf("credential leaked in %s", f.Name)
		}
		if f.Name == "events.jsonl" {
			events = string(data)
		}
	}
	if strings.Count(events, `"kind":"request"`) != 1 || !strings.Contains(events, `"protocol":"jellyfin"`) || !strings.Contains(events, "An item") {
		t.Fatalf("events: %s", events)
	}
}
