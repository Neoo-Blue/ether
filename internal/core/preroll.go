package core

import (
	"context"
	"time"
)

// Pre-rolls are owner videos played before a feature (GitHub #298). They
// are served to clients as extras, so their rating keys are KindExtra keys
// with payloads in a range the extras registry never assigns.
const (
	prerollPayloadBase int64 = 999_999_000_000_000
	// MaxPrerollID is the largest pre-roll id a key can carry.
	MaxPrerollID int64 = 999_999_999
)

// PrerollKey is the rating key of pre-roll id (1..MaxPrerollID).
func PrerollKey(id int64) RatingKey { return ExtraKey(prerollPayloadBase + id) }

// PrerollID returns the pre-roll id of a pre-roll key, else 0.
func (k RatingKey) PrerollID() int64 {
	if k.Kind() != KindExtra {
		return 0
	}
	if p := k.payload(); p > prerollPayloadBase {
		return p - prerollPayloadBase
	}
	return 0
}

// IsPreroll reports whether k addresses a pre-roll.
func (k RatingKey) IsPreroll() bool { return k.PrerollID() > 0 }

// Pre-roll placement relative to Cinema Trailers.
const (
	PrerollBeforeTrailers = "before_trailers"
	PrerollAfterTrailers  = "after_trailers"
)

// PrerollRequest describes one play a pre-roll may precede.
type PrerollRequest struct {
	Viewer *User
	// Device is the form factor: DeviceTV, DeviceMobile, DeviceDesktop or
	// DeviceWeb.
	Device string
	// Away is true when the client is not on the server's local network.
	Away bool
	// Feature is the title about to play (a movie or an episode).
	Feature *Title
	// Resume is true when the play continues from a saved position.
	Resume bool
	// AutoPlay is true when the play is an automatic continuation, such
	// as the next episode starting by itself.
	AutoPlay bool
	// Client names the app ("Plex for Apple TV").
	Client string
	At     time.Time
}

// PrerollClip is a ready pre-roll video on this node.
type PrerollClip struct {
	Key         RatingKey
	Name        string
	Path        string // converted MP4 (H.264/AAC, faststart)
	Duration    time.Duration
	Width       int
	Height      int
	BitrateKbps int
	Size        int64
	// Order is PrerollBeforeTrailers or PrerollAfterTrailers.
	Order     string
	UpdatedAt time.Time
}

// PrerollPlay records that a pre-roll actually started for a viewer.
type PrerollPlay struct {
	Viewer  *User
	Key     RatingKey
	Feature RatingKey
	// FeatureTitle is the display title of the feature, when known.
	FeatureTitle string
	Client       string
}

// Prerolls selects and serves owner pre-rolls (internal/prerolls).
type Prerolls interface {
	// Active reports, from memory, whether any pre-roll could play. It
	// lets callers skip all selection work when none is configured.
	Active(ctx context.Context) bool
	// Pick returns the pre-roll for a play, or false for none.
	Pick(ctx context.Context, req PrerollRequest) (PrerollClip, bool)
	// Clip returns a ready pre-roll by key.
	Clip(ctx context.Context, key RatingKey) (PrerollClip, bool)
	// Thumb returns the pre-roll's JPEG thumbnail path.
	Thumb(ctx context.Context, key RatingKey) (string, bool)
	// Started records a play once per viewer, pre-roll and feature.
	Started(ctx context.Context, play PrerollPlay)
}
