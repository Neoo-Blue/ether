package mcp

import (
	"net"
	"net/http"
	"strings"
)

// Request-boundary validation, mirroring appkit-mcp's boundary module:
// Host checks stop DNS rebinding (a hostile page resolving its own name
// to this server still sends its own Host), and a cross-origin browser
// caller must be explicitly allowed. Non-browser clients send no Origin.
//
// In Go the request URL's host comes from the Host header, so accept only
// IP literals, localhost, and explicitly configured server hostnames.
type boundaryViolation struct {
	status  int
	code    int
	message string
}

func checkBoundary(r *http.Request, allowedHosts, allowedOrigins []string) *boundaryViolation {
	host := strings.ToLower(strings.TrimSpace(r.Host))
	if host == "" {
		return &boundaryViolation{status: http.StatusMisdirectedRequest, code: -32001, message: "Misdirected Request"}
	}
	if !hostAllowed(host, allowedHosts) {
		return &boundaryViolation{status: http.StatusMisdirectedRequest, code: -32001, message: "Misdirected Request"}
	}
	if origin := r.Header.Get("Origin"); origin != "" && !originAllowed(r, origin, allowedOrigins) {
		return &boundaryViolation{status: http.StatusForbidden, code: -32001, message: "Origin not allowed"}
	}
	return nil
}

func hostAllowed(hostport string, allowed []string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.ToLower(strings.TrimSuffix(strings.Trim(host, "[]"), "."))
	if host == "localhost" || net.ParseIP(host) != nil {
		return true
	}
	for _, a := range allowed {
		if strings.EqualFold(strings.TrimSuffix(strings.TrimSpace(a), "."), host) || strings.EqualFold(strings.TrimSpace(a), hostport) {
			return true
		}
	}
	return false
}

func originAllowed(r *http.Request, origin string, allowed []string) bool {
	for _, a := range allowed {
		if strings.TrimSpace(a) == origin {
			return true
		}
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return origin == scheme+"://"+r.Host
}
