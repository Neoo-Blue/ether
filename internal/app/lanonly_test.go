package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLANOnly(t *testing.T) {
	h := lanOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), nil, nil)
	for addr, want := range map[string]int{
		"127.0.0.1:5000":  http.StatusOK,
		"192.168.1.20:1":  http.StatusOK,
		"10.0.0.43:1":     http.StatusOK,
		"100.101.102.1:1": http.StatusOK, // Tailscale
		"[fd00::1]:1":     http.StatusOK,
		"203.0.113.9:1":   http.StatusForbidden,
		"[2001:db8::1]:1": http.StatusForbidden,
		"not-an-address":  http.StatusForbidden,
	} {
		r := httptest.NewRequest("GET", "/ether/", nil)
		r.RemoteAddr = addr
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != want {
			t.Errorf("%s: %d, want %d", addr, rec.Code, want)
		}
	}
}

func TestLANOnlyForwardedPublicClient(t *testing.T) {
	for _, header := range []string{"X-Forwarded-For", "Forwarded", "X-Real-IP"} {
		for _, peer := range []string{"127.0.0.1:1234", "10.0.0.2:1234"} {
			h := lanOnly(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), func() []string { return []string{"10.0.0.2/32"} }, nil)
			r := httptest.NewRequest("GET", "/ether/", nil)
			r.RemoteAddr = peer
			value := "203.0.113.1"
			if header == "Forwarded" {
				value = "for=" + value
			}
			r.Header.Set(header, value)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusForbidden {
				t.Fatalf("%s %s: %d", peer, header, w.Code)
			}
		}
	}
}

// A proxy on another LAN machine is an untrusted private peer; a public
// client it forwards must still be refused, as it is behind a same-host
// proxy. The owner's opt-in lets the console answer them (sign-in still
// gates it).
func TestLANOnlyRefusesForwardedOutsideClientsUnlessOptedIn(t *testing.T) {
	remote := false
	h := lanOnly(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), nil, func() bool { return remote })
	try := func(peer, xff string) int {
		r := httptest.NewRequest("GET", "/ether/", nil)
		r.RemoteAddr = peer
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if c := try("192.168.1.50:1234", "203.0.113.1"); c != http.StatusForbidden {
		t.Fatalf("LAN proxy forwarding a public client: %d", c)
	}
	if c := try("192.168.1.50:1234", "192.168.1.20"); c != http.StatusOK {
		t.Fatalf("LAN proxy forwarding a LAN client: %d", c)
	}
	if c := try("127.0.0.1:1234", "203.0.113.1"); c != http.StatusForbidden {
		t.Fatalf("same-host proxy forwarding a public client: %d", c)
	}
	remote = true
	for _, peer := range []string{"192.168.1.50:1234", "127.0.0.1:1234", "203.0.113.9:1"} {
		if c := try(peer, "203.0.113.1"); c != http.StatusOK {
			t.Fatalf("opted in, %s: %d", peer, c)
		}
	}
}
