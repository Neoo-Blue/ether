package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/braedonsaunders/ether/internal/catalog"
	"github.com/braedonsaunders/ether/internal/core"
)

// TitleSchedules backs the catalogue drawer's Episodes, Calendar and
// Releases tabs. Implemented by *catalog.Catalog.
type TitleSchedules interface {
	TitleShowSchedule(ctx context.Context, show core.RatingKey, since time.Time, limit int) (*catalog.ShowSchedule, error)
	TitleSeasonSchedule(ctx context.Context, show core.RatingKey, season int) ([]catalog.DatedEpisode, error)
	MovieReleaseTimeline(ctx context.Context, key core.RatingKey) ([]catalog.ReleaseDate, error)
}

var _ TitleSchedules = (*catalog.Catalog)(nil)

// recentWindow is how far back the drawer's calendar lists aired episodes.
const recentWindow = 45 * 24 * time.Hour

type episodeCard struct {
	Key            string  `json:"key"`
	Season         int     `json:"season"`
	Episode        int     `json:"episode"`
	Title          string  `json:"title"`
	Overview       string  `json:"overview,omitempty"`
	AirDate        string  `json:"airDate,omitempty"` // YYYY-MM-DD; empty when not announced
	RuntimeMinutes int     `json:"runtimeMinutes,omitempty"`
	Still          string  `json:"still,omitempty"`
	Rating         float64 `json:"rating,omitempty"`
	Aired          bool    `json:"aired"`
	// Premiere marks a season's first episode; Finale its last mirrored one.
	Premiere bool `json:"premiere,omitempty"`
	Finale   bool `json:"finale,omitempty"`
}

type seasonCard struct {
	Season   int    `json:"season"`
	Title    string `json:"title"`
	Overview string `json:"overview,omitempty"`
	AirDate  string `json:"airDate,omitempty"`
	Poster   string `json:"poster,omitempty"`
	Episodes int    `json:"episodes"`
	Aired    int    `json:"aired"`
	// Unlisted: TMDB's count only; episodes load when the season opens.
	Unlisted bool `json:"unlisted,omitempty"`
}

type showSchedule struct {
	Seasons  []seasonCard  `json:"seasons"`
	Next     *episodeCard  `json:"next"`
	Last     *episodeCard  `json:"last"`
	Episodes []episodeCard `json:"episodes"`
}

func airDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02")
}

func newEpisodeCard(e catalog.DatedEpisode, now time.Time, sizes map[int]int) episodeCard {
	c := episodeCard{
		Key: e.Key.String(), Season: e.Season, Episode: e.Episode, Title: e.Title, Overview: e.Summary,
		AirDate: airDate(e.Released), RuntimeMinutes: int(e.Runtime / time.Minute), Still: ImageURL(e.Still, 300),
		Rating: roundRating(e.Rating), Aired: !e.Released.IsZero() && !e.Released.After(now),
		Premiere: e.Episode == 1 && e.Season > 0,
	}
	if n := sizes[e.Season]; n > 1 && e.Episode == n {
		c.Finale = true
	}
	if c.Title == "" {
		c.Title = "Episode " + strconv.Itoa(e.Episode)
	}
	return c
}

func (h *Handler) titleSchedules() (TitleSchedules, bool) {
	s := h.deps.TitleSchedules
	return s, s != nil
}

func (h *Handler) handleCatalogTitleSchedule(w http.ResponseWriter, r *http.Request) {
	src, ok := h.titleSchedules()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Episode schedules are unavailable")
		return
	}
	key, err := core.ParseRatingKey(r.PathValue("key"))
	if err != nil || key.Kind() != core.KindShow {
		writeError(w, http.StatusNotFound, "not_found", "no such show")
		return
	}
	now := time.Now()
	s, err := src.TitleShowSchedule(r.Context(), key, now.Add(-recentWindow), 400)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	sizes := make(map[int]int, len(s.Seasons))
	out := showSchedule{Seasons: make([]seasonCard, 0, len(s.Seasons)), Episodes: make([]episodeCard, 0, len(s.Window))}
	for _, season := range s.Seasons {
		sizes[season.Season] = season.Episodes
		title := season.Title
		if title == "" {
			title = "Season " + strconv.Itoa(season.Season)
		}
		out.Seasons = append(out.Seasons, seasonCard{
			Season: season.Season, Title: title, Overview: season.Summary, AirDate: airDate(season.Released),
			Poster: ImageURL(season.Poster, 185), Episodes: season.Episodes, Aired: season.Aired, Unlisted: season.Unmirrored,
		})
	}
	for _, e := range s.Window {
		out.Episodes = append(out.Episodes, newEpisodeCard(e, now, sizes))
	}
	if s.Next != nil {
		c := newEpisodeCard(*s.Next, now, sizes)
		out.Next = &c
	}
	if s.Last != nil {
		c := newEpisodeCard(*s.Last, now, sizes)
		out.Last = &c
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) handleCatalogTitleSeason(w http.ResponseWriter, r *http.Request) {
	src, ok := h.titleSchedules()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Episode schedules are unavailable")
		return
	}
	key, err := core.ParseRatingKey(r.PathValue("key"))
	season, serr := strconv.Atoi(r.PathValue("season"))
	if err != nil || serr != nil || key.Kind() != core.KindShow || season < 0 {
		writeError(w, http.StatusNotFound, "not_found", "no such season")
		return
	}
	eps, err := src.TitleSeasonSchedule(r.Context(), key, season)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	now := time.Now()
	sizes := map[int]int{}
	for _, e := range eps {
		sizes[e.Season] = max(sizes[e.Season], e.Episode)
	}
	out := make([]episodeCard, 0, len(eps))
	for _, e := range eps {
		out = append(out, newEpisodeCard(e, now, sizes))
	}
	writeJSON(w, http.StatusOK, map[string]any{"season": season, "episodes": out})
}

type releaseDateCard struct {
	Country       string `json:"country"`
	Kind          string `json:"kind"`
	Date          string `json:"date"`
	Note          string `json:"note,omitempty"`
	Certification string `json:"certification,omitempty"`
}

func (h *Handler) handleCatalogTitleReleases(w http.ResponseWriter, r *http.Request) {
	src, ok := h.titleSchedules()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Release dates are unavailable")
		return
	}
	key, err := core.ParseRatingKey(r.PathValue("key"))
	if err != nil || key.Kind() != core.KindMovie {
		writeError(w, http.StatusNotFound, "not_found", "no such movie")
		return
	}
	dates, err := src.MovieReleaseTimeline(r.Context(), key)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) || r.Context().Err() != nil {
			h.writeServiceError(w, err)
			return
		}
		h.log.Debug("movie release dates", "key", key, "err", err)
		writeError(w, http.StatusBadGateway, "upstream", "TMDB release dates could not be loaded")
		return
	}
	out := make([]releaseDateCard, 0, len(dates))
	for _, d := range dates {
		out = append(out, releaseDateCard{Country: d.Country, Kind: d.Kind, Date: airDate(d.Date), Note: d.Note, Certification: d.Certification})
	}
	region := ""
	if h.deps.Config != nil {
		region = h.deps.Config.Read().Catalog.Region
	}
	writeJSON(w, http.StatusOK, map[string]any{"region": region, "dates": out})
}
