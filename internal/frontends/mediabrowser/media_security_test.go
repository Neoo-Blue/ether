package mediabrowser

import (
	"context"
	"github.com/braedonsaunders/ether/internal/frontend"
	"github.com/braedonsaunders/ether/internal/frontend/frontendtest"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type unsafeLazy struct{}

func (unsafeLazy) Issue(context.Context, string, frontend.PlanRequest) (string, error) {
	return "", nil
}
func (unsafeLazy) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	_, _ = w.Write([]byte(`<svg onload="bad()"/>`))
}
func TestMediaRoutesIsolateActiveContent(t *testing.T) {
	host := frontend.Host{Identity: frontendtest.Identity{}, Sessions: frontendtest.Sessions{ServeMediaFunc: func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<script>bad()</script>`))
	}}, LazyStreams: unsafeLazy{}}
	for _, tc := range []struct {
		name  string
		front frontend.Frontend
	}{{"jellyfin", Jellyfin{MachineID: "test"}}, {"emby", Emby{MachineID: "test"}}} {
		inst, err := tc.front.Mount(context.Background(), host)
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{"/sessions/media/a/b/file", "/sessions/lazy/a/file"} {
			w := httptest.NewRecorder()
			inst.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/"+tc.name+path, nil))
			if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") || w.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatalf("%s%s: %d %v %q", tc.name, path, w.Code, w.Header(), w.Body.String())
			}
		}
	}
}
