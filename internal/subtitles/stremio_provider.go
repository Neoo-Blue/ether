package subtitles

// Stremio subtitle add-ons as subtitle providers beside OpenSubtitles.
//
// An add-on whose manifest resources include "subtitles" answers
// GET /subtitles/{type}/{id}[/videoHash=…&videoSize=…].json with
// {url, lang} entries. Listing is a search: Ether calls it only from
// the viewer-driven SearchOnDemand, and a file is downloaded only when
// the viewer picks it (DownloadOnDemand), through the same normalise
// and cache pipeline as OpenSubtitles. The moviehash is sent only when
// a caller already has it from cached ranges; nothing here ever fetches
// a release to compute one (see MovieHash).

import (
	"context"
	"fmt"
	"hash/fnv"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/langtag"
	"github.com/braedonsaunders/ether/internal/stremio"
)

// providerList is the effective provider order: the owner's configured
// order, or OpenSubtitles alone when none is configured.
func (s *Service) providerList() []config.SubtitleProvider {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []config.SubtitleProvider
	for _, p := range s.providers {
		if p.Enabled {
			out = append(out, p)
		}
	}
	if len(out) == 0 && len(s.providers) == 0 && s.cfg.Enabled {
		return []config.SubtitleProvider{{ID: "opensubtitles", Kind: config.SubtitleProviderOpenSubtitles, Enabled: true}}
	}
	return out
}

// searchEnabled reports whether any provider can search.
func (s *Service) searchEnabled() bool {
	if s.dir == "" {
		return false
	}
	for _, p := range s.providerList() {
		switch p.Kind {
		case config.SubtitleProviderOpenSubtitles:
			if s.Enabled() {
				return true
			}
		case config.SubtitleProviderStremio:
			if strings.TrimSpace(p.URL) != "" {
				return true
			}
		}
	}
	return false
}

// UpdateProviders swaps the subtitle provider order (config changes).
func (s *Service) UpdateProviders(ps []config.SubtitleProvider) {
	s.mu.Lock()
	s.providers = append([]config.SubtitleProvider(nil), ps...)
	s.mu.Unlock()
}

// stremioFileID is a stable synthetic FileID for an add-on subtitle, in
// a range OpenSubtitles file ids never reach.
func stremioFileID(providerID, subID, rawURL string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(providerID + "\x00" + subID + "\x00" + rawURL))
	return int64(1)<<62 | int64(h.Sum64()&((1<<62)-1))
}

// stremioLookup is the add-on request identity for a title: IMDb id,
// with :season:episode for episodes.
func stremioLookup(title *core.Title) (typ, id string, ok bool) {
	if title.Type == core.TypeEpisode {
		imdb := title.ShowIDs.IMDB
		if imdb == "" {
			imdb = title.IDs.IMDB
		}
		season, episode := title.Season, title.Episode
		if season == 0 && title.Key.Kind() == core.KindEpisode {
			season, episode = title.Key.Season(), title.Key.Episode()
		}
		if imdb == "" || season <= 0 || episode <= 0 {
			return "", "", false
		}
		return "series", fmt.Sprintf("%s:%d:%d", imdb, season, episode), true
	}
	if title.IDs.IMDB == "" {
		return "", "", false
	}
	return "movie", title.IDs.IMDB, true
}

// searchStremio lists one add-on's subtitles for a title+language. It
// never downloads a subtitle file.
func (s *Service) searchStremio(ctx context.Context, p config.SubtitleProvider, title *core.Title, lang, movieHash string, videoSize int64) ([]Candidate, error) {
	typ, id, ok := stremioLookup(title)
	if !ok {
		return nil, nil
	}
	client, err := stremio.NewClient(p.URL, s.stremioHTTP)
	if err != nil {
		return nil, err
	}
	extra := map[string]string{}
	if movieHash != "" {
		extra["videoHash"] = movieHash
	}
	if videoSize > 0 {
		extra["videoSize"] = strconv.FormatInt(videoSize, 10)
	}
	subs, err := client.Subtitles(ctx, typ, id, extra)
	if err != nil {
		return nil, err
	}
	name := p.Name
	if name == "" {
		name = client.Host()
	}
	var out []Candidate
	for _, sub := range subs {
		subLang := langtag.Normalize(sub.Lang)
		if subLang == "" || sub.URL == "" {
			continue
		}
		if langtag.MatchTier(lang, subLang) == 0 {
			continue
		}
		file := path.Base(sub.URL)
		out = append(out, Candidate{
			FileID:      stremioFileID(p.ID, sub.ID, sub.URL),
			FileName:    file,
			Language:    subLang,
			Release:     strings.TrimSuffix(file, path.Ext(file)),
			Provider:    name,
			ProviderID:  p.ID,
			DownloadURL: sub.URL,
		})
	}
	return out, nil
}

// searchProviders runs every enabled provider in owner order and merges
// the candidates into one ranked list (language, hash match and
// release-name match via Rank; provider order breaks score ties). One
// provider failing does not sink the others; the error surfaces only
// when every provider failed.
func (s *Service) searchProviders(ctx context.Context, title *core.Title, lang, releaseName, movieHash string, videoSize int64) ([]Candidate, error) {
	var all []Candidate
	var firstErr error
	ran := 0
	for i, p := range s.providerList() {
		var cands []Candidate
		var err error
		switch p.Kind {
		case config.SubtitleProviderOpenSubtitles:
			if !s.Enabled() {
				continue
			}
			cands, err = s.SearchWithHash(ctx, title, lang, releaseName, movieHash)
			for j := range cands {
				if cands[j].Provider == "" {
					cands[j].Provider = "OpenSubtitles"
					cands[j].ProviderID = "opensubtitles"
				}
			}
		case config.SubtitleProviderStremio:
			if strings.TrimSpace(p.URL) == "" {
				continue
			}
			cands, err = s.searchStremio(ctx, p, title, lang, movieHash, videoSize)
		default:
			continue
		}
		ran++
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			s.log.Debug("subtitle provider search failed", "provider", p.ID, "err", err)
			continue
		}
		for j := range cands {
			cands[j].providerOrder = i
		}
		all = append(all, cands...)
	}
	if len(all) == 0 && firstErr != nil {
		return nil, firstErr
	}
	Rank(all, releaseName)
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Score != all[j].Score {
			return all[i].Score > all[j].Score
		}
		return all[i].providerOrder < all[j].providerOrder
	})
	return all, nil
}
