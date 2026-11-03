package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/tmdb"
)

// The owner console's title drawer reads a show's whole episode schedule
// (aired and announced) and a movie's release timeline. These readers are
// deliberately separate from Children/Get: discovery, autoplay and playback
// must never acquire future episodes through them.

// DatedEpisode is one episode in a show's schedule. Released is zero when
// TMDB has not announced an air date.
type DatedEpisode struct {
	Key      core.RatingKey
	Season   int
	Episode  int
	Title    string
	Summary  string
	Released time.Time
	Runtime  time.Duration
	Still    string
	Rating   float64
}

// SeasonSummary is one season of a show, announced or aired.
type SeasonSummary struct {
	Season   int
	Title    string
	Summary  string
	Released time.Time
	Poster   string
	// Episodes is the mirrored episode count (TMDB lists announced
	// episodes too); Aired counts those already released.
	Episodes int
	Aired    int
	// Unmirrored: Episodes is TMDB's count; Aired is unknown.
	Unmirrored bool
}

// ShowSchedule is a show's seasons plus its episodes around today.
type ShowSchedule struct {
	Seasons []SeasonSummary
	// Window is every mirrored episode released from `since` onward, plus
	// announced episodes without a date, in air order.
	Window []DatedEpisode
	// Last is the most recently aired episode; Next the first announced
	// one after now (dated, or the first undated one of a later episode).
	Last, Next *DatedEpisode
}

const scheduleEpisodeCols = `key,season,episode,title,summary,released_ts,release_known,runtime_sec,still,rating`

func scanScheduleEpisodes(rows *sql.Rows) ([]DatedEpisode, error) {
	defer rows.Close()
	var out []DatedEpisode
	for rows.Next() {
		var key, released, runtime int64
		var known int
		var e DatedEpisode
		var title, summary, still sql.NullString
		var rating sql.NullFloat64
		if err := rows.Scan(&key, &e.Season, &e.Episode, &title, &summary, &released, &known, &runtime, &still, &rating); err != nil {
			return nil, err
		}
		e.Key = core.RatingKey(key)
		e.Title, e.Summary, e.Still, e.Rating = title.String, summary.String, still.String, rating.Float64
		if known == 1 {
			e.Released = time.Unix(released, 0).UTC()
		}
		e.Runtime = time.Duration(runtime) * time.Second
		out = append(out, e)
	}
	return out, rows.Err()
}

// TitleShowSchedule refreshes a show's newest seasons when stale (the same
// TTLs as the calendar's background pass) and reads its schedule from the
// mirror. since bounds the episode window; limit bounds its size.
func (c *Catalog) TitleShowSchedule(ctx context.Context, show core.RatingKey, since time.Time, limit int) (*ShowSchedule, error) {
	if show.Kind() != core.KindShow {
		return nil, fmt.Errorf("%w: key %s", core.ErrNotFound, show)
	}
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	st, err := c.getShow(ctx, show.TMDB())
	if err != nil {
		return nil, err
	}
	if st.Adult {
		return nil, core.ErrNotFound
	}
	// RefreshSchedule skips settled shows; a drawer must still see the
	// newest season of one nobody follows, so mirror it once if missing.
	if err := c.RefreshSchedule(ctx, show); err != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	out := &ShowSchedule{}
	if out.Seasons, err = c.seasonSummaries(ctx, show); err != nil {
		return nil, err
	}
	if n := len(out.Seasons); n > 0 && out.Seasons[n-1].Episodes == 0 && out.Seasons[n-1].Season > 0 && episodeKeyFits(out.Seasons[n-1].Season, 0) {
		if _, ferr := c.fetchSeason(ctx, show.TMDB(), out.Seasons[n-1].Season); ferr == nil {
			if out.Seasons, err = c.seasonSummaries(ctx, show); err != nil {
				return nil, err
			}
		} else if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	now := c.now().Unix()
	showKey := int64(show)
	rows, err := c.db.QueryContext(ctx, `SELECT `+scheduleEpisodeCols+` FROM catalog_titles WHERE type='episode' AND show_key=? AND season>0 AND adult=0 AND (release_known=0 OR released_ts>=?) ORDER BY release_known DESC, released_ts, season, episode LIMIT ?`, showKey, since.Unix(), limit)
	if err != nil {
		return nil, err
	}
	if out.Window, err = scanScheduleEpisodes(rows); err != nil {
		return nil, err
	}
	// Undated episodes only matter in the newest seasons; older gaps in
	// TMDB's data are noise, not announcements.
	newest := 0
	for _, e := range out.Window {
		if !e.Released.IsZero() && e.Season > newest {
			newest = e.Season
		}
	}
	kept := out.Window[:0]
	for _, e := range out.Window {
		if e.Released.IsZero() && e.Season < newest {
			continue
		}
		kept = append(kept, e)
	}
	out.Window = kept
	if out.Last, err = c.scheduleOne(ctx, `SELECT `+scheduleEpisodeCols+` FROM catalog_titles WHERE type='episode' AND show_key=? AND season>0 AND adult=0 AND `+knownReleaseSQL+` AND released_ts<=? ORDER BY released_ts DESC, season DESC, episode DESC LIMIT 1`, showKey, now); err != nil {
		return nil, err
	}
	if out.Next, err = c.scheduleOne(ctx, `SELECT `+scheduleEpisodeCols+` FROM catalog_titles WHERE type='episode' AND show_key=? AND season>0 AND adult=0 AND `+knownReleaseSQL+` AND released_ts>? ORDER BY released_ts, season, episode LIMIT 1`, showKey, now); err != nil {
		return nil, err
	}
	if out.Next == nil {
		// Announced, not yet dated: the episode right after the last aired.
		for i := range out.Window {
			e := out.Window[i]
			if e.Released.IsZero() && (out.Last == nil || e.Season > out.Last.Season || (e.Season == out.Last.Season && e.Episode > out.Last.Episode)) {
				out.Next = &e
				break
			}
		}
	}
	return out, nil
}

func (c *Catalog) scheduleOne(ctx context.Context, q string, args ...any) (*DatedEpisode, error) {
	rows, err := c.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	got, err := scanScheduleEpisodes(rows)
	if err != nil || len(got) == 0 {
		return nil, err
	}
	return &got[0], nil
}

func (c *Catalog) seasonSummaries(ctx context.Context, show core.RatingKey) ([]SeasonSummary, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT s.season,s.title,s.summary,s.released_ts,s.release_known,s.poster,s.child_count,
		(SELECT COUNT(*) FROM catalog_titles e WHERE e.type='episode' AND e.show_key=s.show_key AND e.season=s.season AND e.adult=0),
		(SELECT COUNT(*) FROM catalog_titles e WHERE e.type='episode' AND e.show_key=s.show_key AND e.season=s.season AND e.adult=0 AND e.release_known=1 AND e.released_ts<=?)
		FROM catalog_titles s WHERE s.type='season' AND s.show_key=? AND s.adult=0 ORDER BY s.season`, c.now().Unix(), int64(show))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SeasonSummary
	for rows.Next() {
		var s SeasonSummary
		var title, summary, poster sql.NullString
		var released int64
		var known, listed int
		if err := rows.Scan(&s.Season, &title, &summary, &released, &known, &poster, &listed, &s.Episodes, &s.Aired); err != nil {
			return nil, err
		}
		// A season nobody opened yet is not mirrored episode by episode;
		// TMDB's episode count on the season row stands in, and a season
		// that premiered before the newest one has fully aired.
		if s.Episodes == 0 && listed > 0 {
			s.Episodes = listed
			s.Unmirrored = true
		}
		s.Title, s.Summary, s.Poster = title.String, summary.String, poster.String
		if known == 1 {
			s.Released = time.Unix(released, 0).UTC()
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Specials last, the way every media server lists them.
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Season == 0) != (out[j].Season == 0) {
			return out[j].Season == 0
		}
		return out[i].Season < out[j].Season
	})
	return out, nil
}

// TitleSeasonSchedule mirrors one season when stale and returns every
// episode TMDB lists for it, aired or announced.
func (c *Catalog) TitleSeasonSchedule(ctx context.Context, show core.RatingKey, season int) ([]DatedEpisode, error) {
	if show.Kind() != core.KindShow || !episodeKeyFits(season, 0) {
		return nil, fmt.Errorf("%w: season %d of %s", core.ErrNotFound, season, show)
	}
	st, err := c.getShow(ctx, show.TMDB())
	if err != nil {
		return nil, err
	}
	if st.Adult {
		return nil, core.ErrNotFound
	}
	if _, err := c.fetchSeason(ctx, show.TMDB(), season); err != nil && !errors.Is(err, core.ErrNotFound) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Serve whatever is mirrored when TMDB is unreachable.
	}
	rows, err := c.db.QueryContext(ctx, `SELECT `+scheduleEpisodeCols+` FROM catalog_titles WHERE type='episode' AND show_key=? AND season=? AND adult=0 ORDER BY episode LIMIT 1000`, int64(show), season)
	if err != nil {
		return nil, err
	}
	return scanScheduleEpisodes(rows)
}

// ReleaseDate is one dated release of a movie in one country.
type ReleaseDate struct {
	Country       string
	Kind          string // premiere, limited, theatrical, digital, physical, tv
	Date          time.Time
	Note          string
	Certification string
}

var releaseKinds = map[int]string{1: "premiere", 2: "limited", 3: "theatrical", 4: "digital", 5: "physical", 6: "tv"}

type releaseDatesTMDB interface {
	MovieReleaseDates(ctx context.Context, id int64) (*tmdb.ReleaseDates, error)
}

// MovieReleaseTimeline returns a movie's release dates from TMDB, sorted by
// date. It contacts TMDB (the owner opened one title's release tab); there
// is no mirror of every country's premieres and TV dates.
func (c *Catalog) MovieReleaseTimeline(ctx context.Context, key core.RatingKey) ([]ReleaseDate, error) {
	if key.Kind() != core.KindMovie {
		return nil, fmt.Errorf("%w: key %s", core.ErrNotFound, key)
	}
	api, ok := c.tmdb().(releaseDatesTMDB)
	if !ok || api == nil {
		return nil, errors.New("catalog: TMDB is not configured")
	}
	got, err := api.MovieReleaseDates(ctx, key.TMDB())
	if err != nil {
		return nil, err
	}
	var out []ReleaseDate
	for _, country := range got.Results {
		for _, r := range country.ReleaseDate {
			kind, ok := releaseKinds[r.Type]
			if !ok {
				continue
			}
			d := tmdb.ParseDate(strings.SplitN(r.ReleaseDate, "T", 2)[0])
			if d.IsZero() {
				continue
			}
			out = append(out, ReleaseDate{Country: strings.ToUpper(country.ISO3166), Kind: kind, Date: d, Note: strings.TrimSpace(r.Note), Certification: strings.TrimSpace(r.Certification)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Date.Equal(out[j].Date) {
			return out[i].Date.Before(out[j].Date)
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Country < out[j].Country
	})
	return out, nil
}
