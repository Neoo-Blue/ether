package config

import (
	"strings"
	"testing"
)

func TestClientIdentityResolve(t *testing.T) {
	ether := identityPresets[IdentityEther].UserAgent
	stack := ClientIdentity{TVSearch: IdentitySonarr, MovieSearch: IdentityRadarr}
	tests := []struct {
		name     string
		policy   ClientIdentity
		override string
		purpose  string
		tv       bool
		want     string
	}{
		{"empty policy keeps ether", ClientIdentity{}, "", IdentitySearch, true, ether},
		{"empty policy movie search", ClientIdentity{}, "", IdentitySearch, false, ether},
		{"empty policy tv grab", ClientIdentity{}, "", IdentityGrab, true, ether},
		{"empty policy movie grab", ClientIdentity{}, "", IdentityGrab, false, ether},
		{"stack tv search", stack, "", IdentitySearch, true, "Sonarr/4.0.20.3014 (ubuntu 26.04)"},
		{"stack movie search", stack, "", IdentitySearch, false, "Radarr/6.4.4.10685 (ubuntu 26.04)"},
		// An unset field is the Ether identity; nothing is copied from another.
		{"unset tv grab stays ether", stack, "", IdentityGrab, true, ether},
		{"unset movie grab stays ether", stack, "", IdentityGrab, false, ether},
		{
			"tv grab only",
			ClientIdentity{TVGrab: IdentityNZBGet},
			"", IdentityGrab, true, "nzbget/26.3",
		},
		{
			"tv grab does not touch the searches",
			ClientIdentity{TVGrab: IdentityNZBGet},
			"", IdentitySearch, true, ether,
		},
		{
			"tv grab does not touch the movie grab",
			ClientIdentity{TVSearch: IdentitySonarr, MovieSearch: IdentityRadarr, TVGrab: IdentityNZBGet},
			"", IdentityGrab, false, ether,
		},
		{
			"movie grab only",
			ClientIdentity{MovieGrab: IdentitySABnzbd},
			"", IdentityGrab, false, "SABnzbd/5.1.3",
		},
		{
			"movie grab does not touch the movie search",
			ClientIdentity{TVSearch: IdentitySonarr, MovieGrab: IdentitySABnzbd},
			"", IdentitySearch, false, ether,
		},
		{
			"tv search only leaves the other three on ether",
			ClientIdentity{TVSearch: IdentitySonarr},
			"", IdentitySearch, false, ether,
		},
		{"indexer override wins for search", stack, IdentityProwlarr, IdentitySearch, true, "Prowlarr/2.7.0.5723 (ubuntu 26.04)"},
		{"indexer override wins for grab", stack, IdentityProwlarr, IdentityGrab, false, "Prowlarr/2.7.0.5723 (ubuntu 26.04)"},
		{"unknown selector keeps ether", ClientIdentity{TVSearch: "nope"}, "", IdentitySearch, true, ether},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.policy.Resolve(tc.override, tc.purpose, tc.tv)
			if got.UserAgent != tc.want {
				t.Fatalf("user agent = %q, want %q", got.UserAgent, tc.want)
			}
		})
	}
}

func TestClientIdentityCustom(t *testing.T) {
	headers := map[string]string{"X-Api-Client": "ether"}
	p := ClientIdentity{TVSearch: IdentityCustom, UserAgent: "MyClient/2.0", Headers: headers}
	got := p.Resolve("", IdentitySearch, true)
	if got.Preset != IdentityCustom || got.UserAgent != "MyClient/2.0" || got.Headers["X-Api-Client"] != "ether" {
		t.Fatalf("custom identity = %+v", got)
	}
	// A custom identity with no user agent falls back to Ether's.
	if ua := (ClientIdentity{TVSearch: IdentityCustom}).Resolve("", IdentitySearch, true).UserAgent; ua != "Ether/1.0" {
		t.Fatalf("empty custom user agent = %q", ua)
	}
}

func TestClientIdentityValidate(t *testing.T) {
	cases := []struct {
		name   string
		policy ClientIdentity
		field  string
	}{
		{"unknown tv search", ClientIdentity{TVSearch: "nzbgeon"}, "tvSearch"},
		{"unknown movie search", ClientIdentity{MovieSearch: "nzbgeon"}, "movieSearch"},
		{"unknown tv grab", ClientIdentity{TVGrab: "nzbgeon"}, "tvGrab"},
		{"sabnzbd as a tv search", ClientIdentity{TVSearch: IdentitySABnzbd}, "tvSearch"},
		{"nzbget as a movie search", ClientIdentity{MovieSearch: IdentityNZBGet}, "movieSearch"},
		{"unknown movie grab", ClientIdentity{MovieGrab: "nzbgeon"}, "movieGrab"},
		{"custom without a user agent", ClientIdentity{TVSearch: IdentityCustom}, "userAgent"},
		{"line break in user agent", ClientIdentity{UserAgent: "a\r\nb"}, "userAgent"},
		{"line break in a header", ClientIdentity{Headers: map[string]string{"X-Test": "a\nb"}}, "headers"},
		{"transport header", ClientIdentity{Headers: map[string]string{"Accept-Encoding": "gzip"}}, "headers"},
		{"host header", ClientIdentity{Headers: map[string]string{"Host": "other"}}, "headers"},
		{"user agent header", ClientIdentity{Headers: map[string]string{"User-Agent": "x"}}, "headers"},
		{"bad header name", ClientIdentity{Headers: map[string]string{"X Test": "v"}}, "headers"},
		{"duplicate header names", ClientIdentity{Headers: map[string]string{"X-Test": "v", "x-test": "w"}}, "headers"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := tc.policy.Validate()
			if len(errs) == 0 {
				t.Fatal("expected an error")
			}
			for _, e := range errs {
				if e.Field == tc.field {
					return
				}
			}
			t.Fatalf("no error for %q in %+v", tc.field, errs)
		})
	}
}

func TestClientIdentityValidateAccepts(t *testing.T) {
	headers := map[string]string{"X-Api-Client": "ether"}
	for i := 1; i < 16; i++ {
		headers["X-Test-"+strings.Repeat("a", i)] = "v"
	}
	ok := ClientIdentity{
		TVSearch: IdentitySonarr, MovieSearch: IdentityRadarr,
		TVGrab: IdentityNZBGet, MovieGrab: IdentitySABnzbd,
		// Dormant custom values survive while no selector is Custom.
		UserAgent: "MyClient/2.0",
		Headers:   headers,
	}
	if errs := ok.Validate(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if errs := (ClientIdentity{}).Validate(); len(errs) != 0 {
		t.Fatalf("empty policy: %+v", errs)
	}
}

func TestValidIndexerIdentity(t *testing.T) {
	for _, name := range []string{IdentityEther, IdentitySonarr, IdentityRadarr, IdentityProwlarr, IdentityNZBHydra2, IdentitySABnzbd, IdentityNZBGet} {
		if !ValidIndexerIdentity(name) {
			t.Errorf("%q rejected", name)
		}
	}
	for _, name := range []string{"", IdentityCustom, "nope"} {
		if ValidIndexerIdentity(name) {
			t.Errorf("%q accepted", name)
		}
	}
}
