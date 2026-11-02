package app

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/braedonsaunders/ether/internal/frontend"
)

// probeFrontend is a fake frontend whose listener specs are supplied by the
// test, so a case can mount several shared prefixes or a dedicated port.
type probeFrontend struct {
	id      frontend.Protocol
	specs   []frontend.ListenerSpec
	handler http.Handler
}

func (f probeFrontend) Describe() frontend.Descriptor { return frontend.Descriptor{ID: f.id} }
func (f probeFrontend) Mount(context.Context, frontend.Host) (frontend.Instance, error) {
	return probeInstance(f), nil
}

type probeInstance probeFrontend

func (i probeInstance) Handler() http.Handler {
	if i.handler != nil {
		return i.handler
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, string(i.id)+":"+r.URL.Path)
	})
}
func (i probeInstance) Listeners() []frontend.ListenerSpec     { return i.specs }
func (i probeInstance) Discovery() []frontend.Announcer        { return nil }
func (i probeInstance) Status(context.Context) frontend.Status { return frontend.Status{} }
func (i probeInstance) Close(context.Context) error            { return nil }

// siloSharedSpecs mirrors the shared mounts the real Silo frontend declares
// (pinned by TestSiloDeclaresMainPortRootPrefixes in the silo package).
func siloSharedSpecs() []frontend.ListenerSpec {
	return []frontend.ListenerSpec{
		{Shared: true, PathPrefix: "/silo"},
		{Shared: true, PathPrefix: "/api/v2"},
		{Shared: true, PathPrefix: "/api/v1"},
	}
}

func probeRuntime(t *testing.T, fronts ...probeFrontend) *frontendRuntime {
	t.Helper()
	reg := frontend.NewRegistry(func(frontend.Protocol) frontend.Host { return frontend.Host{} })
	r := &frontendRuntime{registry: reg, control: newFrontendControl(reg, nil)}
	for _, f := range fronts {
		if err := reg.Register(f); err != nil {
			t.Fatal(err)
		}
		if err := r.control.listeners.start(context.Background(), f.id); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = r.control.listeners.close(context.Background()) })
	return r
}

// A Silo client given the main address probes /api/v2/system/info at the
// root; with Silo mounted that must reach Silo, as must /api/v1/health and
// the classic /silo mount.
func TestSiloProbeReachesSiloOnMainPort(t *testing.T) {
	r := probeRuntime(t, probeFrontend{id: "silo", specs: siloSharedSpecs()})
	h := r.plexHandler()
	for _, path := range []string{"/api/v2/system/info", "/api/v1/health", "/silo/api/v2/system/info"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK || w.Body.String() != "silo:"+path {
			t.Fatalf("%s: got %d %q", path, w.Code, w.Body.String())
		}
	}
}

// With Silo off (and Plex answering with its router's bare 404), the Silo
// probe must get a problem+json 404, never Go's "404 page not found", which
// Silo clients read as "update the server". Other paths keep their 404s.
func TestSiloProbeWhenSiloDisabledOnMainPort(t *testing.T) {
	r := probeRuntime(t, probeFrontend{
		id:      "plex",
		specs:   []frontend.ListenerSpec{{Shared: true, PathPrefix: "/"}},
		handler: http.NotFoundHandler(),
	})
	h := r.plexHandler()
	for _, path := range []string{"/api/v2/system/info", "/silo/api/v2/system/info"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s: got %d", path, w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/problem+json" {
			t.Fatalf("%s: content type %q", path, ct)
		}
		if strings.TrimSpace(w.Body.String()) == "404 page not found" || !strings.Contains(w.Body.String(), `"status":404`) {
			t.Fatalf("%s: body %q", path, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/identity", nil))
	if w.Code != http.StatusNotFound || w.Body.String() != "404 page not found\n" {
		t.Fatalf("non-probe 404 changed: %d %q", w.Code, w.Body.String())
	}
}

// The same holds with Plex disabled, where the main handler used to answer
// the probe with http.NotFound itself.
func TestSiloProbeWhenNothingMountedOnMainPort(t *testing.T) {
	r := probeRuntime(t)
	w := httptest.NewRecorder()
	r.plexHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v2/system/info", nil))
	if w.Code != http.StatusNotFound || w.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("got %d %q body %q", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
}

// A Silo client pointed at another frontend's dedicated port must not be
// told to update the server either.
func TestSiloProbeOnOtherDedicatedPort(t *testing.T) {
	r := probeRuntime(t, probeFrontend{
		id:      "test",
		specs:   []frontend.ListenerSpec{{Address: "127.0.0.1:0"}},
		handler: http.NotFoundHandler(),
	})
	addr := r.control.listeners.status(context.Background(), "test").Addresses[0]
	res, err := http.Get("http://" + addr + "/api/v2/system/info")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusNotFound || res.Header.Get("Content-Type") != "application/problem+json" || strings.TrimSpace(string(body)) == "404 page not found" {
		t.Fatalf("got %d %q body %q", res.StatusCode, res.Header.Get("Content-Type"), body)
	}
	res, err = http.Get("http://" + addr + "/System/Info/Public")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusNotFound || string(body) != "404 page not found\n" {
		t.Fatalf("non-probe 404 changed: %d %q", res.StatusCode, body)
	}
}

// Silo's extra root prefixes are routing aliases; the advertised shared
// connect address stays the canonical /silo mount.
func TestSiloSharedStatusAdvertisesCanonicalPrefixOnly(t *testing.T) {
	r := probeRuntime(t, probeFrontend{id: "silo", specs: siloSharedSpecs()})
	r.mainAddr = &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32400}
	st := frontend.Status{}
	r.sharedStatus("silo", &st)
	if len(st.Addresses) != 1 || !strings.HasSuffix(st.Addresses[0], "/silo") {
		t.Fatalf("addresses %v", st.Addresses)
	}
	for _, u := range st.ConnectURLs {
		if strings.Contains(u, "/api/") {
			t.Fatalf("connect URL %q advertises an API prefix", u)
		}
	}
}
