package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/braedonsaunders/ether/internal/remote"
)

type lanCheck struct {
	state, reason, fingerprint string
	checked                    time.Time
}
type lanTarget struct {
	uri    string
	source net.IP
}

// Local addresses are selected only from this machine's interfaces. Binding the
// source prevents a proxy or another route from turning a local probe into WAN evidence.
func lanTargets(urls []string, interfaces []net.Addr) []lanTarget {
	ips := map[string]net.IP{}
	for _, a := range interfaces {
		ip, _, err := net.ParseCIDR(a.String())
		if err == nil && !ip.IsLoopback() && !ip.IsUnspecified() && !ip.IsLinkLocalUnicast() {
			ips[ip.String()] = ip
		}
	}
	var out []lanTarget
	seen := map[string]bool{}
	for _, raw := range urls {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			continue
		}
		ip := net.ParseIP(u.Hostname())
		if ip == nil {
			ip, _, _ = remote.ParsePlexDirectHost(u.Hostname())
		}
		if ip == nil {
			continue
		}
		src := ips[ip.String()]
		if src == nil || seen[raw] {
			continue
		}
		seen[raw] = true
		out = append(out, lanTarget{raw, src})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].uri < out[j].uri })
	return out
}
func lanFingerprint(targets []lanTarget, id string) string {
	var s strings.Builder
	s.WriteString(id)
	for _, t := range targets {
		s.WriteByte('|')
		s.WriteString(t.uri)
	}
	return s.String()
}

func probeLAN(ctx context.Context, targets []lanTarget, machine string) lanCheck {
	result := lanCheck{state: "unknown", reason: "no_addresses", checked: time.Now(), fingerprint: lanFingerprint(targets, machine)}
	if machine == "" {
		result.reason = "no_identity"
		return result
	}
	if len(targets) == 0 {
		return result
	}
	if len(targets) > 32 {
		result.reason = "too_many"
		return result
	}
	type outcome struct {
		ok     bool
		reason string
	}
	ch := make(chan outcome, len(targets))
	for _, target := range targets {
		go func(t lanTarget) {
			dialer := &net.Dialer{Timeout: 3 * time.Second, LocalAddr: &net.TCPAddr{IP: t.source}}
			transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(address)
				if err != nil {
					return nil, err
				}
				resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
				if err != nil {
					return nil, err
				}
				match := false
				for _, ip := range resolved {
					if ip.IP.Equal(t.source) {
						match = true
						break
					}
				}
				if !match {
					return nil, fmt.Errorf("address does not resolve to its local interface")
				}
				return dialer.DialContext(ctx, network, net.JoinHostPort(t.source.String(), port))
			}}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			check := remote.ReachabilityChecker{Client: client, Timeout: 4 * time.Second}
			res := check.Check(ctx, []string{t.uri}, machine)
			reason := "probe_failed"
			if len(res.Results) > 0 && strings.Contains(res.Results[0].Error, "different server") {
				reason = "wrong_identity"
			}
			ch <- outcome{res.Reachable, reason}
		}(target)
	}
	result.state = "reachable"
	result.reason = "verified"
	for range targets {
		o := <-ch
		if !o.ok {
			result.state = "unreachable"
			if result.reason != "wrong_identity" {
				result.reason = o.reason
			}
		}
	}
	return result
}

func (r *frontendRuntime) lanInputs() ([]lanTarget, string) {
	r.mu.Lock()
	addr := r.mainAddr
	r.mu.Unlock()
	var urls []string
	addresses, _ := net.InterfaceAddrs()
	if addr != nil {
		urls = append(urls, connectionURLs(addr.String(), "", addresses)...)
	}
	if r.app.registrar != nil {
		for _, c := range r.app.registrar.Status().Connections {
			if c.Local && !c.Custom {
				urls = append(urls, c.URI)
			}
		}
	}
	return lanTargets(urls, addresses), r.app.cfg.Get().Server.MachineIdentifier
}
func (r *frontendRuntime) refreshLAN() {
	r.mu.Lock()
	if r.lanChecking || r.closed {
		r.mu.Unlock()
		return
	}
	r.lanChecking = true
	r.mu.Unlock()
	go func() {
		targets, id := r.lanInputs()
		result := probeLAN(r.ctx, targets, id)
		r.mu.Lock()
		r.lan = result
		r.lanChecking = false
		r.mu.Unlock()
	}()
}
