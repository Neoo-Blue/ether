package livetv

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLogoIndexMatchingAndAmbiguity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"name":"News One","logo":"https://images.example/one.png"},{"name":"Same","logo":"https://images.example/a.png"},{"name":"Same","logo":"https://images.example/b.png"}]`)
	}))
	defer srv.Close()
	s := testService(t, Options{LogoIndexURL: func() string { return srv.URL }, StremioHTTP: srv.Client()})
	cs := []Channel{{Name: "NEWS-ONE"}, {Name: "Same"}, {Name: "News One", Logo: "https://own.example/logo.png"}, {Name: "Missing"}}
	s.matchLogos(context.Background(), cs)
	if cs[0].Logo != "https://images.example/one.png" || cs[1].Logo != "" || cs[2].Logo != "https://own.example/logo.png" || cs[3].Logo != "" {
		t.Fatalf("logos: %+v", cs)
	}
}
