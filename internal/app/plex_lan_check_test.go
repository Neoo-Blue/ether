package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLANProbeIdentity(t *testing.T) {
	for _, tc := range []struct{ name, identity, want, reason string }{
		{"reachable", "server-id", "reachable", "verified"},
		{"wrong identity", "another-id", "unreachable", "wrong_identity"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/identity" {
					t.Errorf("path %s", r.URL.Path)
				}
				fmt.Fprintf(w, `<MediaContainer machineIdentifier="%s"/>`, tc.identity)
			}))
			defer s.Close()
			got := probeLAN(context.Background(), []lanTarget{{s.URL, net.ParseIP("127.0.0.1")}}, "server-id")
			if got.state != tc.want || got.reason != tc.reason || got.checked.IsZero() {
				t.Fatalf("got %+v", got)
			}
		})
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	l.Close()
	got := probeLAN(context.Background(), []lanTarget{{"http://" + address, net.ParseIP("127.0.0.1")}}, "server-id")
	if got.state != "unreachable" || got.reason != "probe_failed" || got.checked.IsZero() {
		t.Fatalf("refused: %+v", got)
	}
	if got := probeLAN(context.Background(), nil, "server-id"); got.state != "unknown" || got.reason != "no_addresses" {
		t.Fatalf("missing: %+v", got)
	}
}

func TestLANTargetsOnlyAdvertisedLocalInterfaces(t *testing.T) {
	_, subnet, err := net.ParseCIDR("192.168.1.20/24")
	if err != nil {
		t.Fatal(err)
	}
	subnet.IP = net.ParseIP("192.168.1.20")
	targets := lanTargets([]string{"http://192.168.1.20:32400", "https://192-168-1-20.hash.plex.direct:32400", "http://8.8.8.8:32400", "http://localhost:32400"}, []net.Addr{subnet})
	if len(targets) != 2 {
		t.Fatalf("targets: %+v", targets)
	}
	for _, target := range targets {
		if !target.source.Equal(net.ParseIP("192.168.1.20")) {
			t.Fatalf("wrong source %+v", target)
		}
	}
}
