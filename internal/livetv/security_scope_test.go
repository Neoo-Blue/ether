package livetv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResourceCredentialOriginAndRedirect(t *testing.T) {
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-Provider-Secret") != "" {
			t.Error("source headers leaked to foreign origin")
		}
		_, _ = w.Write([]byte("public CDN segment"))
	}))
	defer foreign.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("X-Provider-Secret") != "secret" {
			t.Error("source headers missing")
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, foreign.URL, http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("origin"))
	}))
	defer origin.Close()
	s := testService(t, Options{HTTPClient: origin.Client()})
	headers := http.Header{"Authorization": {"Bearer secret"}, "X-Provider-Secret": {"secret"}}
	for _, target := range []string{origin.URL, origin.URL + "/redirect", foreign.URL} {
		res, err := s.upstreamWithHeaders(context.Background(), target, httptest.NewRequest("GET", "/", nil), headers, origin.URL)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("%s: %d", target, res.StatusCode)
		}
	}
}
