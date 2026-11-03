package prerolls

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
)

// startedTTL deduplicates the many requests one pre-roll play makes
// (decision, ranges, restarts) into one play-log row.
const (
	startedTTL  = 30 * time.Minute
	pendingTTL  = 30 * time.Minute
	maxInMemory = 8192
)

// Pick implements core.Prerolls: it chooses at most one pre-roll for a
// play. Nothing is recorded until Started.
func (s *Service) Pick(ctx context.Context, req core.PrerollRequest) (core.PrerollClip, bool) {
	if req.Feature == nil || req.Resume {
		return core.PrerollClip{}, false
	}
	all, selection, err := s.cachedWithSelection(ctx)
	if err != nil {
		s.log.Debug("pre-roll list", "err", err)
		return core.PrerollClip{}, false
	}
	live := all[:0:0]
	for _, p := range all {
		if p.Enabled && p.Video.State == StateReady {
			live = append(live, p)
		}
	}
	if len(live) == 0 {
		return core.PrerollClip{}, false
	}
	if req.At.IsZero() {
		req.At = s.now()
	}
	dec := s.evaluate(ctx, req, live, selection, false)
	if dec.winner == nil {
		return core.PrerollClip{}, false
	}
	clip := s.clipOf(*dec.winner)
	if req.Viewer != nil {
		s.mu.Lock()
		s.prune(req.At)
		s.pending[pendingKey(req.Viewer.ID, dec.winner.seq)] = pendingPlay{feature: req.Feature.Key, title: featureTitle(req.Feature), at: req.At}
		s.mu.Unlock()
	}
	return clip, true
}

func featureTitle(t *core.Title) string {
	if t == nil {
		return ""
	}
	if t.Type == core.TypeEpisode && t.ShowTitle != "" {
		return t.ShowTitle + " · S" + strconv.Itoa(t.Season) + "E" + strconv.Itoa(t.Episode) + " · " + t.Title
	}
	return t.Title
}

func (s *Service) clipOf(p Preroll) core.PrerollClip {
	return core.PrerollClip{
		Key: p.Key(), Name: p.Name, Path: s.path(p.seq, ".mp4"),
		Duration: time.Duration(p.Video.DurationMs) * time.Millisecond,
		Width:    p.Video.Width, Height: p.Video.Height, BitrateKbps: p.bitrate, Size: p.sizeBytes,
		Order: p.Order, UpdatedAt: p.UpdatedAt,
	}
}

func (s *Service) readyBySeq(ctx context.Context, seq int64) (Preroll, bool) {
	all, err := s.cached(ctx)
	if err != nil {
		return Preroll{}, false
	}
	for _, p := range all {
		if p.seq == seq {
			if p.Video.State != StateReady || !fileExists(s.path(seq, ".mp4")) {
				return Preroll{}, false
			}
			return p, true
		}
	}
	return Preroll{}, false
}

// Clip implements core.Prerolls. A disabled pre-roll already queued for a
// play still plays; a deleted or reconverting one does not.
func (s *Service) Clip(ctx context.Context, key core.RatingKey) (core.PrerollClip, bool) {
	p, ok := s.readyBySeq(ctx, key.PrerollID())
	if !ok {
		return core.PrerollClip{}, false
	}
	return s.clipOf(p), true
}

// Thumb implements core.Prerolls.
func (s *Service) Thumb(ctx context.Context, key core.RatingKey) (string, bool) {
	seq := key.PrerollID()
	if seq <= 0 {
		return "", false
	}
	path := s.path(seq, ".jpg")
	return path, fileExists(path)
}

// Started implements core.Prerolls: it records one play per viewer,
// pre-roll and feature, however many requests the client makes.
func (s *Service) Started(ctx context.Context, play core.PrerollPlay) {
	seq := play.Key.PrerollID()
	if seq <= 0 || play.Viewer == nil {
		return
	}
	now := s.now()
	s.mu.Lock()
	s.prune(now)
	pk := pendingKey(play.Viewer.ID, seq)
	if pend, ok := s.pending[pk]; ok {
		if play.Feature == 0 || play.Feature == pend.feature {
			play.Feature = pend.feature
			if play.FeatureTitle == "" {
				play.FeatureTitle = pend.title
			}
		}
	}
	sk := pk + "|" + play.Feature.String()
	if _, dup := s.started[sk]; dup {
		s.mu.Unlock()
		return
	}
	s.started[sk] = now
	s.mu.Unlock()
	name := play.Viewer.Title
	if name == "" {
		name = play.Viewer.Username
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO prerolls_plays(preroll_seq,viewer_id,viewer_name,rating_key,title,client,at) VALUES(?,?,?,?,?,?,?)`,
		seq, play.Viewer.ID, clip(name, 120), int64(play.Feature), clip(play.FeatureTitle, 300), clip(play.Client, 120), now.UnixNano()); err != nil {
		s.log.Warn("record pre-roll play", "preroll", seq, "err", err)
		s.mu.Lock()
		delete(s.started, sk)
		s.mu.Unlock()
		return
	}
	s.mu.Lock()
	due := now.Sub(s.lastPrune) > time.Hour
	if due {
		s.lastPrune = now
	}
	s.mu.Unlock()
	if due {
		_ = s.Prune(ctx)
	}
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

func pendingKey(viewer, seq int64) string {
	return strconv.FormatInt(viewer, 10) + "|" + strconv.FormatInt(seq, 10)
}

// prune bounds the in-memory maps; s.mu is held.
func (s *Service) prune(now time.Time) {
	for k, at := range s.started {
		if now.Sub(at) > startedTTL {
			delete(s.started, k)
		}
	}
	for k, p := range s.pending {
		if now.Sub(p.at) > pendingTTL {
			delete(s.pending, k)
		}
	}
	for k := range s.started {
		if len(s.started) <= maxInMemory {
			break
		}
		delete(s.started, k)
	}
	for k := range s.pending {
		if len(s.pending) <= maxInMemory {
			break
		}
		delete(s.pending, k)
	}
}

// SimulateInput is the simulator request.
type SimulateInput struct {
	ViewerID  int64  `json:"viewerId"`
	RatingKey string `json:"ratingKey"`
	Device    string `json:"device"`
	Away      bool   `json:"away"`
	Resume    bool   `json:"resume"`
	At        string `json:"at"`
	// Client optionally names the app, to explain unsupported apps.
	Client string `json:"client,omitempty"`
}

// Simulate explains which pre-roll a play would get, through the same
// rules as playback, without recording anything.
func (s *Service) Simulate(ctx context.Context, in SimulateInput) (Decision, error) {
	key, err := core.ParseRatingKey(strings.TrimSpace(in.RatingKey))
	if err != nil || (key.Kind() != core.KindMovie && key.Kind() != core.KindEpisode) {
		return Decision{}, invalid("ratingKey", "choose a movie or an episode")
	}
	if s.title == nil {
		return Decision{}, invalid("ratingKey", "the catalogue is unavailable")
	}
	feature, err := s.title(ctx, key)
	if err != nil || feature == nil {
		return Decision{}, invalid("ratingKey", "that title was not found")
	}
	var viewer *core.User
	if in.ViewerID > 0 {
		if s.viewer == nil {
			return Decision{}, invalid("viewerId", "viewers are unavailable")
		}
		if viewer, err = s.viewer(ctx, in.ViewerID); err != nil || viewer == nil {
			return Decision{}, invalid("viewerId", "that viewer was not found")
		}
	}
	at := s.now()
	if v := strings.TrimSpace(in.At); v != "" {
		if at, err = time.Parse(time.RFC3339, v); err != nil {
			return Decision{}, invalid("at", "use an RFC 3339 time")
		}
	}
	switch in.Device {
	case "", core.DeviceTV, core.DeviceMobile, core.DeviceDesktop, core.DeviceWeb:
	default:
		return Decision{}, invalid("device", "use tv, mobile, desktop or web")
	}
	all, err := s.loadAll(ctx)
	if err != nil {
		return Decision{}, err
	}
	return s.evaluate(ctx, core.PrerollRequest{
		Viewer: viewer, Device: in.Device, Away: in.Away, Feature: feature, Resume: in.Resume, Client: in.Client, At: at,
	}, all, s.Selection(ctx), true), nil
}
