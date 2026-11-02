package livetv

import (
	"context"
	"encoding/json"
	"strings"
	"unicode"

	"github.com/braedonsaunders/ether/internal/stremio"
)

func logoName(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}

// matchLogos is called on the import path, never while serving the guide.
// The owner supplies the index; Ether ships none. A failed index leaves the
// channels intact and the existing name fallback remains available.
func (s *Service) matchLogos(ctx context.Context, cs []Channel) {
	if s.o.LogoIndexURL == nil {
		return
	}
	raw := strings.TrimSpace(s.o.LogoIndexURL())
	if raw == "" {
		return
	}
	client, err := stremio.NewClient(raw, s.o.StremioHTTP)
	if err != nil {
		return
	}
	data, err := client.FetchBytes(ctx, raw, 8<<20)
	if err != nil {
		return
	}
	var entries []struct {
		Name    string `json:"name"`
		Logo    string `json:"logo"`
		URL     string `json:"url"`
		Channel string `json:"channel"`
	}
	if json.Unmarshal(data, &entries) != nil {
		return
	}
	logos := map[string]string{}
	ambiguous := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name
		if name == "" {
			name = entry.Channel
		}
		key := logoName(name)
		logo := entry.Logo
		if logo == "" {
			logo = entry.URL
		}
		if key == "" || !validHTTP(logo) {
			continue
		}
		if prior, ok := logos[key]; ok && prior != logo {
			ambiguous[key] = true
		}
		logos[key] = logo
	}
	for i := range cs {
		if cs[i].Logo != "" {
			continue
		}
		key := logoName(cs[i].Name)
		if !ambiguous[key] {
			cs[i].Logo = logos[key]
		}
	}
}
