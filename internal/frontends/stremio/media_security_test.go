package stremio

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
func TestMediaRouteIsolatesActiveContent(t *testing.T) {
	inst, err := (Frontend{}).Mount(context.Background(), frontend.Host{Identity: frontendtest.Identity{}, Experience: frontendtest.Experience{}, Sessions: frontendtest.Sessions{}, LazyStreams: unsafeLazy{}})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	inst.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/stremio/sessions/lazy/grant/file", nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("%d %v %q", w.Code, w.Header(), w.Body.String())
	}
}
