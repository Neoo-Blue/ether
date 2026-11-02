package config

import (
	"fmt"
	"regexp"
	"strings"
)

// Purpose values for ClientIdentity.Resolve.
const (
	IdentitySearch = "search"
	IdentityGrab   = "grab"
)

// Preset keys. A preset names the client Ether looks like on the wire.
const (
	IdentityEther     = "ether"
	IdentitySonarr    = "sonarr"
	IdentityRadarr    = "radarr"
	IdentityProwlarr  = "prowlarr"
	IdentityNZBHydra2 = "nzbhydra2"
	IdentitySABnzbd   = "sabnzbd"
	IdentityNZBGet    = "nzbget"
	// IdentityCustom is policy-only: its user agent and headers come from
	// the policy, so an indexer cannot select it.
	IdentityCustom = "custom"
)

// Identity is one resolved identity: what Ether writes on the wire.
type Identity struct {
	Preset    string
	UserAgent string
	Headers   map[string]string
	Params    []string // Newznab parameter order; nil keeps Go's alphabetical order
	Offset0   bool     // send offset=0 on every search, as a paged client does
}

// ClientIdentity is the owner's policy for the client Ether presents to
// Newznab/Torznab indexers.
type ClientIdentity struct {
	TVSearch    string            `json:"tvSearch,omitempty"`    // preset key; empty = "ether"
	MovieSearch string            `json:"movieSearch,omitempty"` // preset key; empty = "ether"
	TVGrab      string            `json:"tvGrab,omitempty"`      // preset key; empty = "ether"
	MovieGrab   string            `json:"movieGrab,omitempty"`   // preset key; empty = "ether"
	UserAgent   string            `json:"userAgent,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"` // sealed at rest, redacted in API responses
}

// IdentityFieldError is one rejected client-identity setting, keyed like the
// config field it belongs to ("tvSearch", "userAgent", "headers").
type IdentityFieldError struct {
	Field   string
	Message string
}

const servarrAccept = "application/rss+xml, text/rss+xml, application/xml, text/xml"

// identityPresets is the literal wire shape of each client. Versions and the
// Servarr OS token are a snapshot refreshed by editing this table: Servarr
// builds its user agent from the host's /etc/os-release ID and VERSION_ID,
// lowercased, quotes trimmed. Header maps here are shared and never mutated.
var identityPresets = map[string]Identity{
	IdentityEther: {Preset: IdentityEther, UserAgent: "Ether/1.0"},
	IdentitySonarr: {
		Preset:    IdentitySonarr,
		UserAgent: "Sonarr/4.0.20.3014 (ubuntu 26.04)",
		Headers:   map[string]string{"Accept": servarrAccept},
		Params:    []string{"t", "id", "cat", "extended", "apikey", "offset", "limit", "tvdbid", "tmdbid", "imdbid", "season", "ep", "q"},
		Offset0:   true,
	},
	IdentityRadarr: {
		Preset:    IdentityRadarr,
		UserAgent: "Radarr/6.4.4.10685 (ubuntu 26.04)",
		Headers:   map[string]string{"Accept": servarrAccept},
		Params:    []string{"t", "id", "cat", "extended", "apikey", "offset", "limit", "tvdbid", "tmdbid", "imdbid", "season", "ep", "q"},
		Offset0:   true,
	},
	IdentityProwlarr: {
		Preset:    IdentityProwlarr,
		UserAgent: "Prowlarr/2.7.0.5723 (ubuntu 26.04)",
		Headers:   map[string]string{"Accept": servarrAccept},
		Params:    []string{"t", "id", "extended", "cat", "apikey", "tvdbid", "tmdbid", "imdbid", "tvmazeid", "rid", "season", "ep", "q", "limit", "offset"},
		// Prowlarr forwards the caller's offset (Sonarr and Radarr always
		// send one), so its requests to an indexer carry offset=0.
		Offset0: true,
	},
	IdentityNZBHydra2: {
		Preset:    IdentityNZBHydra2,
		UserAgent: "NZBHydra2 9.2.0",
		Headers:   map[string]string{"Accept": "application/xml", "Content-Type": "application/xml"},
		Params:    []string{"apikey", "t", "id", "extended", "tvdbid", "tmdbid", "imdbid", "tvmazeid", "rid", "q", "season", "ep", "cat", "limit", "offset"},
		// Hydra2 pages every search from 0, so offset is always present.
		Offset0: true,
	},
	// SABnzbd and NZBGet are grab-side personas: their user agent and fixed
	// headers are what a downloader sends when it fetches an NZB.
	IdentitySABnzbd: {Preset: IdentitySABnzbd, UserAgent: "SABnzbd/5.1.3"},
	IdentityNZBGet:  {Preset: IdentityNZBGet, UserAgent: "nzbget/26.3", Headers: map[string]string{"Accept": "*/*"}},
}

// ValidIndexerIdentity reports whether name can stand in for one indexer.
// "custom" is policy-only: its user agent and headers belong to the policy.
func ValidIndexerIdentity(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	_, ok := identityPresets[name]
	return ok
}

// ValidSearchIdentity reports whether name can present a search. SABnzbd and
// NZBGet only fetch NZBs, so a search presented as one is a client that does
// not exist.
func ValidSearchIdentity(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return ValidIndexerIdentity(name) && name != IdentitySABnzbd && name != IdentityNZBGet
}

// Resolve returns the identity for one purpose and media. override is the
// indexer's value; "" inherits the policy, and an unset policy field is the
// Ether identity. Nothing falls back across fields: what the owner sets is
// what is sent.
func (p ClientIdentity) Resolve(override, purpose string, tv bool) Identity {
	name := strings.TrimSpace(override)
	if name == "" {
		switch {
		case purpose == IdentityGrab && tv:
			name = p.TVGrab
		case purpose == IdentityGrab:
			name = p.MovieGrab
		case tv:
			name = p.TVSearch
		default:
			name = p.MovieSearch
		}
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == IdentityCustom {
		agent := strings.TrimSpace(p.UserAgent)
		if agent == "" {
			agent = identityPresets[IdentityEther].UserAgent
		}
		return Identity{Preset: IdentityCustom, UserAgent: agent, Headers: p.Headers}
	}
	if id, ok := identityPresets[name]; ok {
		return id
	}
	return identityPresets[IdentityEther]
}

// Validate rejects settings the transport or an indexer would choke on.
func (p ClientIdentity) Validate() []IdentityFieldError {
	var errs []IdentityFieldError
	custom := false
	for _, s := range []struct{ field, value string }{
		{"tvSearch", p.TVSearch},
		{"movieSearch", p.MovieSearch},
		{"tvGrab", p.TVGrab},
		{"movieGrab", p.MovieGrab},
	} {
		name := strings.ToLower(strings.TrimSpace(s.value))
		if name == IdentityCustom {
			custom = true
			continue
		}
		if name == "" {
			continue
		}
		if !ValidIndexerIdentity(name) {
			errs = append(errs, IdentityFieldError{s.field, "must be a known client identity"})
			continue
		}
		search := s.field == "tvSearch" || s.field == "movieSearch"
		if search && !ValidSearchIdentity(name) {
			errs = append(errs, IdentityFieldError{s.field, "must be a client that searches; SABnzbd and NZBGet only download"})
		}
	}
	if custom && strings.TrimSpace(p.UserAgent) == "" {
		errs = append(errs, IdentityFieldError{"userAgent", "a user agent is required when an identity is Custom"})
	}
	if msg := invalidHeaderValue(p.UserAgent, 512); msg != "" {
		errs = append(errs, IdentityFieldError{"userAgent", msg})
	}
	if len(p.Headers) > 16 {
		return append(errs, IdentityFieldError{"headers", "at most 16 headers"})
	}
	seen := make(map[string]bool, len(p.Headers))
	for name, value := range p.Headers {
		if !headerNameRE.MatchString(name) || len(name) > 64 {
			errs = append(errs, IdentityFieldError{"headers", fmt.Sprintf("%q is not a valid header name", name)})
			continue
		}
		lower := strings.ToLower(name)
		if seen[lower] {
			errs = append(errs, IdentityFieldError{"headers", fmt.Sprintf("%q is listed twice", name)})
			continue
		}
		seen[lower] = true
		if forbiddenHeader(lower) {
			errs = append(errs, IdentityFieldError{"headers", fmt.Sprintf("%q is sent by the transport and cannot be overridden", name)})
			continue
		}
		if msg := invalidHeaderValue(value, 1024); msg != "" {
			errs = append(errs, IdentityFieldError{"headers", fmt.Sprintf("%q %s", name, msg)})
		}
	}
	return errs
}

var headerNameRE = regexp.MustCompile("^[A-Za-z0-9!#$%&'*+.^_`|~-]+$")

// forbiddenHeader lists names the Go transport owns. accept-encoding is
// included because the transport only decompresses gzip when it added that
// header itself; a custom value would hand gzip bytes to the feed parser.
func forbiddenHeader(lower string) bool {
	switch lower {
	case "host", "content-length", "connection", "transfer-encoding", "upgrade",
		"user-agent", "accept-encoding", "proxy-authorization", "proxy-connection":
		return true
	}
	return false
}

// invalidHeaderValue returns a message when value cannot go on the wire.
func invalidHeaderValue(value string, max int) string {
	if strings.ContainsAny(value, "\r\n\x00") {
		return "must not contain line breaks"
	}
	if len(value) > max {
		return "is too long"
	}
	return ""
}
