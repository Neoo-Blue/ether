package playback

import (
	"context"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
)

type encoderStatusSource interface{ EncoderStatus() *core.EncoderStatus }

// checkEncoderSpeed uses fresh authenticated viewer telemetry, published
// contiguous output and two distinct ffmpeg progress reports. Low speed
// suppresses provider blame even before recovery is necessary.
func (m *Manager) checkEncoderSpeed(s *session, g *generation, hls core.HLSSession, now time.Time) bool {
	source, ok := hls.(encoderStatusSource)
	if !ok {
		return false
	}
	status := source.EncoderStatus()
	if status == nil || status.Decision.VideoCopy || status.Phase != "running" || status.Speed <= 0 || status.UpdatedAt.IsZero() || now.Sub(status.UpdatedAt) > 8*time.Second || now.Before(status.UpdatedAt) || now.Sub(status.StartedAt) < 6*time.Second {
		return false
	}
	slow := status.Speed < 0.95
	s.mu.Lock()
	viewerAt, position, state := s.viewerAt, s.viewerPosition, s.viewerState
	s.mu.Unlock()
	buffer, ok := hls.(core.RecoveryBufferSource)
	if !ok || state != "playing" || viewerAt.IsZero() || now.Before(viewerAt) || now.Sub(viewerAt) > 30*time.Second {
		return slow
	}
	b := buffer.RecoveryBuffer()
	if !viewerAt.After(b.SeekAt) || b.ReadyUntil <= 0 {
		return slow
	}
	position += now.Sub(viewerAt)
	lead := max(time.Duration(0), b.ReadyUntil-position)
	runway := time.Duration(0)
	if slow {
		runway = time.Duration(float64(lead) / (1 - status.Speed))
	}
	s.speedMu.Lock()
	if s.encoderRunAt != status.StartedAt {
		s.encoderRunAt, s.encoderSampleAt = status.StartedAt, time.Time{}
		s.encoderSlowChecks = 0
	}
	if status.UpdatedAt.After(s.encoderSampleAt) {
		s.encoderSampleAt = status.UpdatedAt
		if slow {
			s.encoderSlowChecks++
		} else {
			s.encoderSlowChecks = 0
		}
	}
	trigger := slow && s.encoderSlowChecks >= 2 && runway <= 30*time.Second && !now.Before(s.encoderRetryAt)
	if trigger {
		s.encoderSlowChecks = 0
		s.encoderRetryAt = now.Add(30 * time.Second)
	}
	s.speedMu.Unlock()
	s.mu.Lock()
	if slow {
		s.encoderRecoveryReason = "encoder below real time"
		r := runway
		s.encoderRunway = &r
		if trigger {
			s.encoderRecoveryReason = "searching for a compatible video copy"
		}
	} else {
		s.encoderRecoveryReason, s.encoderRunway = "", nil
	}
	s.mu.Unlock()
	if trigger {
		m.backgroundEncoderFailover(s, g, position)
	}
	return slow
}

func (m *Manager) backgroundEncoderFailover(s *session, g *generation, at time.Duration) {
	if !g.backgroundFailover.CompareAndSwap(false, true) {
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		g.backgroundFailover.Store(false)
		return
	}
	m.wg.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.wg.Done()
		defer g.backgroundFailover.Store(false)
		ctx, cancel := context.WithTimeout(m.ctx, m.opts.FailoverTimeout)
		defer cancel()
		if s.stopCtx != nil {
			stop := context.AfterFunc(s.stopCtx, cancel)
			defer stop()
		}
		stillSlow := func() bool {
			if s.current() != g {
				return false
			}
			s.mu.Lock()
			playing := s.viewerState == "playing"
			s.mu.Unlock()
			if !playing {
				return false
			}
			hls, ok := m.deps.HLSLookup(s.id)
			if !ok || hls == nil {
				return false
			}
			source, ok := hls.(encoderStatusSource)
			if !ok {
				return false
			}
			status := source.EncoderStatus()
			return status != nil && !status.Decision.VideoCopy && status.Phase == "running" && status.Speed > 0 && status.Speed < 0.95 && !status.UpdatedAt.IsZero() && time.Since(status.UpdatedAt) <= 8*time.Second
		}
		_, err := m.failoverWith(ctx, s, g, at, failoverOpts{encoder: true, reason: "encoder output slower than playback", followPlayer: true, preserveBuffer: true, quietFailure: true, stillNeeded: stillSlow})
		if err != nil && ctx.Err() == nil {
			s.mu.Lock()
			if s.cur == g {
				s.encoderRecoveryReason = "encoder below real time; no compatible video copy available"
			}
			s.mu.Unlock()
			m.log.Info("no compatible copy available for slow encoder", "session", s.id, "err", err)
		}
	}()
}
