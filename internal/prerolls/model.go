// Package prerolls stores owner pre-roll videos, converts them for direct
// play and chooses at most one to play before a movie or episode
// (GitHub #298). Plex prepends the chosen pre-roll to the play queue next
// to Cinema Trailers; Jellyfin and Emby list it as an intro.
//
// Selection never scans the library: per-play work is one cached list of
// pre-rolls, one bounded query of the viewer's recent pre-roll plays and
// checks of the single feature title.
package prerolls

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
)

// Video states and problems (owner API contract).
const (
	StateNone       = ""
	StateProcessing = "processing"
	StateReady      = "ready"
	StateFailed     = "failed"

	ProblemUnreadable    = "unreadable"
	ProblemTooLong       = "too_long"
	ProblemConvertFailed = "convert_failed"

	SourceUpload = "upload"
	SourcePath   = "path"
)

// Selection modes.
const (
	SelectPriority = "priority"
	SelectShuffle  = "shuffle"
)

// Frequency modes.
const (
	FrequencyAlways = "always"
	FrequencyDaily  = "daily"
	FrequencyHours  = "hours"
	FrequencyTitle  = "title"
)

// Limits.
const (
	// MaxDuration is the longest clip accepted.
	MaxDuration = 3 * time.Minute
	// MaxRetention bounds the play log.
	MaxRetention = 90 * 24 * time.Hour
	// statsWindow is the plays30d window; the log keeps at least this.
	statsWindow = 30 * 24 * time.Hour
	maxPrerolls = 500
	maxNameLen  = 120
	maxListLen  = 64
	maxHours    = 90 * 24
	maxGapMins  = 24 * 60
)

// Preroll is the owner API representation.
type Preroll struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Enabled   bool      `json:"enabled"`
	Video     Video     `json:"video"`
	Rules     Rules     `json:"rules"`
	Frequency Frequency `json:"frequency"`
	Priority  int       `json:"priority"`
	Order     string    `json:"order"`
	Plays30d  int       `json:"plays30d"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	seq        int64
	sourcePath string
	sizeBytes  int64
	bitrate    int
}

// Key is the pre-roll's rating key.
func (p Preroll) Key() core.RatingKey { return core.PrerollKey(p.seq) }

// Video describes the converted clip.
type Video struct {
	State      string `json:"state"`
	DurationMs int64  `json:"durationMs"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	ThumbURL   string `json:"thumbUrl"`
	PreviewURL string `json:"previewUrl"`
	Problem    string `json:"problem"`
	Source     string `json:"source"`
}

// Rules decide which plays a pre-roll may precede. Empty lists match all.
type Rules struct {
	Kinds            []string `json:"kinds"`
	FirstEpisodeOnly bool     `json:"firstEpisodeOnly"`
	BingeGapMinutes  int      `json:"bingeGapMinutes"`
	Genres           []string `json:"genres"`
	ExcludeGenres    []string `json:"excludeGenres"`
	Collections      []string `json:"collections"`
	MaxContentRating string   `json:"maxContentRating"`
	DateFrom         string   `json:"dateFrom"` // MM-DD, inclusive
	DateTo           string   `json:"dateTo"`   // MM-DD, inclusive; may wrap the new year
	Audience         Audience `json:"audience"`
	Network          string   `json:"network"`
	Devices          []string `json:"devices"`
}

// Audience limits the viewers a pre-roll plays for.
type Audience struct {
	Mode    string  `json:"mode"` // everyone | only | except
	Viewers []int64 `json:"viewers"`
}

// Frequency limits how often one viewer sees a pre-roll.
type Frequency struct {
	Mode  string `json:"mode"`
	Hours int    `json:"hours"`
}

// Input is the create/update body.
type Input struct {
	Name      string    `json:"name"`
	Rules     Rules     `json:"rules"`
	Frequency Frequency `json:"frequency"`
	Priority  int       `json:"priority"`
	Order     string    `json:"order"`
	Enabled   *bool     `json:"enabled"`
}

// ValidationError is an owner input problem (HTTP 400).
type ValidationError struct{ Field, Message string }

func (e *ValidationError) Error() string { return e.Field + ": " + e.Message }

func invalid(field, format string, args ...any) error {
	return &ValidationError{Field: field, Message: fmt.Sprintf(format, args...)}
}

// ErrNotFound is returned for an unknown pre-roll.
var ErrNotFound = core.ErrNotFound

// ErrTooLarge is returned when an upload exceeds the per-file cap.
var ErrTooLarge = errors.New("prerolls: file too large")

// ErrStorageFull is returned when the pre-roll folder is at its total cap.
var ErrStorageFull = errors.New("prerolls: storage full")

var monthDay = regexp.MustCompile(`^(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])$`)

// normalize validates in and fills defaults.
func normalize(in *Input) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return invalid("name", "a name is required")
	}
	if len([]rune(in.Name)) > maxNameLen {
		return invalid("name", "keep the name under %d characters", maxNameLen)
	}
	r := &in.Rules
	var err error
	if r.Kinds, err = cleanSet("rules.kinds", r.Kinds, map[string]bool{"movie": true, "episode": true}); err != nil {
		return err
	}
	if len(r.Kinds) == 0 {
		r.Kinds = []string{"movie", "episode"}
	}
	if r.BingeGapMinutes < 0 || r.BingeGapMinutes > maxGapMins {
		return invalid("rules.bingeGapMinutes", "use 0 to %d minutes", maxGapMins)
	}
	if r.Genres, err = cleanList("rules.genres", r.Genres); err != nil {
		return err
	}
	if r.ExcludeGenres, err = cleanList("rules.excludeGenres", r.ExcludeGenres); err != nil {
		return err
	}
	if r.Collections, err = cleanList("rules.collections", r.Collections); err != nil {
		return err
	}
	r.MaxContentRating = strings.TrimSpace(r.MaxContentRating)
	r.DateFrom, r.DateTo = strings.TrimSpace(r.DateFrom), strings.TrimSpace(r.DateTo)
	if (r.DateFrom == "") != (r.DateTo == "") {
		return invalid("rules.dateFrom", "set both dates or neither")
	}
	for field, v := range map[string]string{"rules.dateFrom": r.DateFrom, "rules.dateTo": r.DateTo} {
		if v != "" && !monthDay.MatchString(v) {
			return invalid(field, "use MM-DD")
		}
	}
	switch r.Audience.Mode {
	case "":
		r.Audience.Mode = "everyone"
	case "everyone", "only", "except":
	default:
		return invalid("rules.audience.mode", "use everyone, only or except")
	}
	if len(r.Audience.Viewers) > 1000 {
		return invalid("rules.audience.viewers", "too many viewers")
	}
	seen := map[int64]bool{}
	viewers := make([]int64, 0, len(r.Audience.Viewers))
	for _, id := range r.Audience.Viewers {
		if id > 0 && !seen[id] {
			seen[id] = true
			viewers = append(viewers, id)
		}
	}
	r.Audience.Viewers = viewers
	if r.Audience.Mode == "everyone" {
		r.Audience.Viewers = []int64{}
	}
	switch r.Network {
	case "":
		r.Network = "any"
	case "any", "home", "away":
	default:
		return invalid("rules.network", "use any, home or away")
	}
	if r.Devices, err = cleanSet("rules.devices", r.Devices, map[string]bool{core.DeviceTV: true, core.DeviceMobile: true, core.DeviceDesktop: true, core.DeviceWeb: true}); err != nil {
		return err
	}
	f := &in.Frequency
	switch f.Mode {
	case "":
		f.Mode = FrequencyAlways
	case FrequencyAlways, FrequencyDaily, FrequencyTitle:
	case FrequencyHours:
		if f.Hours < 1 || f.Hours > maxHours {
			return invalid("frequency.hours", "use 1 to %d hours", maxHours)
		}
	default:
		return invalid("frequency.mode", "use always, daily, hours or title")
	}
	if f.Mode != FrequencyHours {
		f.Hours = 0
	}
	if in.Priority < -1000 || in.Priority > 1000 {
		return invalid("priority", "use -1000 to 1000")
	}
	switch in.Order {
	case "":
		in.Order = core.PrerollBeforeTrailers
	case core.PrerollBeforeTrailers, core.PrerollAfterTrailers:
	default:
		return invalid("order", "use before_trailers or after_trailers")
	}
	return nil
}

func cleanList(field string, in []string) ([]string, error) {
	if len(in) > maxListLen {
		return nil, invalid(field, "use at most %d values", maxListLen)
	}
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		k := strings.ToLower(v)
		if v == "" || seen[k] {
			continue
		}
		if len(v) > 200 {
			return nil, invalid(field, "a value is too long")
		}
		seen[k] = true
		out = append(out, v)
	}
	return out, nil
}

func cleanSet(field string, in []string, allowed map[string]bool) ([]string, error) {
	out, err := cleanList(field, in)
	if err != nil {
		return nil, err
	}
	for i, v := range out {
		v = strings.ToLower(v)
		if !allowed[v] {
			return nil, invalid(field, "unknown value %q", v)
		}
		out[i] = v
	}
	return out, nil
}

// window is how long frequency needs a viewer's plays.
func (f Frequency) window() time.Duration {
	switch f.Mode {
	case FrequencyDaily:
		return 25 * time.Hour // the calendar day in any timezone offset
	case FrequencyHours:
		return time.Duration(f.Hours) * time.Hour
	case FrequencyTitle:
		return MaxRetention
	}
	return 0
}
