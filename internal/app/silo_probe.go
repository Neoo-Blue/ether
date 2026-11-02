package app

import (
	"encoding/json"
	"net/http"
	"strings"
)

// siloProbePath is a Silo client's first call: it decides from this one
// response whether the server speaks API v2. The client shows its
// "update the server" screen only when this call gets a 404 whose body is
// exactly Go's "404 page not found" — the answer Ether's routers give for
// any unknown path when Silo is not mounted there. siloProbeMiss keeps
// that one answer a problem+json 404 (an unreachable-server failure in the
// app) without touching any other 404 body. See GitHub #243.
const siloProbePath = "/api/v2/system/info"

func isSiloProbe(path string) bool {
	return strings.EqualFold(path, siloProbePath) || strings.EqualFold(path, "/silo"+siloProbePath)
}

// siloProbeMiss answers r when it is the Silo probe and reports whether it
// did. Callers invoke it only where Silo itself did not claim the request.
func siloProbeMiss(w http.ResponseWriter, r *http.Request) bool {
	if !isSiloProbe(r.URL.Path) {
		return false
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":     "about:blank",
		"title":    "Not Found",
		"status":   http.StatusNotFound,
		"detail":   "Silo is not available at this address.",
		"instance": siloProbePath,
	})
	return true
}
