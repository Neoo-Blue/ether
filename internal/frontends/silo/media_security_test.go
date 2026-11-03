package silo

import (
	"context"
	"github.com/braedonsaunders/ether/internal/frontend"
	"github.com/braedonsaunders/ether/internal/frontend/frontendtest"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMediaRoutesIsolateActiveContent(t *testing.T) {
	viewer := &frontend.Viewer{ID: 7}
	sessions := testScopedMedia{frontendtest.Sessions{ServeMediaFunc: func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<script>bad()</script>`))
	}}}
	inst, err := (Frontend{MachineID: "test"}).Mount(context.Background(), frontend.Host{Identity: testDeviceIdentity(frontendtest.Identity{}, viewer), Sessions: sessions})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v2/stream/session", "/api/v2/stream/session/subtitles/3.vtt", "/api/v2/playback/transcode/session/segment/file.ts"} {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", "Bearer grant")
		r.Header.Set("X-Silo-Device-Id", "tv")
		w := httptest.NewRecorder()
		inst.Handler().ServeHTTP(w, r)
		if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s: %d %v %q", path, w.Code, w.Header(), w.Body.String())
		}
	}
}
