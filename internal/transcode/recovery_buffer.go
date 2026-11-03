package transcode

import (
	"context"
	"errors"

	"github.com/braedonsaunders/ether/internal/core"
)

var (
	_ core.RecoveryBufferSource    = (*Session)(nil)
	_ core.BufferedInputSwapper    = (*Session)(nil)
	_ core.ReplacementOutputWaiter = (*Session)(nil)
	_ core.InputSwapState          = (*Session)(nil)
)

func (s *Session) CurrentSwapInput() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.input
}

// WaitForReplacementOutput waits for the first segment at the handoff and
// for the published sequence from the current playhead to reach past it.
// This is server-side publication evidence, not client delivery or decode.
func (s *Session) WaitForReplacementOutput(ctx context.Context, input string) error {
	for {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return errors.New("transcode: session closed")
		}
		if s.input != input || s.recoverySeekAt != s.swapSeekAt {
			s.mu.Unlock()
			return core.ErrReplacementSuperseded
		}
		if s.inputFailed {
			s.mu.Unlock()
			return errors.New("transcode: replacement input failed")
		}
		b := s.recoveryBufferLocked()
		if b.ReadyUntil > max(b.Position, s.segStart(s.swapBoundary)) {
			s.mu.Unlock()
			return nil
		}
		changed := s.changed
		s.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (s *Session) RecoveryBuffer() core.RecoveryBuffer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recoveryBufferLocked()
}

func (s *Session) recoveryBufferLocked() core.RecoveryBuffer {
	start := max(0, s.lastReq)
	b := core.RecoveryBuffer{Position: s.segStart(start), ReadyUntil: s.segStart(start), SeekAt: s.recoverySeekAt}
	if s.closed || len(s.done) == 0 {
		return b
	}
	for n := start; n < start+512; n++ {
		if s.total > 0 && n >= s.total {
			break
		}
		track := min(max(0, s.wantTrack), len(s.done)-1)
		ready := s.done[track][n]
		// Inactive video representations are not playback dependencies.
		if audio := s.audioTrackLocked(); audio >= 0 && audio < len(s.done) {
			ready = ready && s.done[audio][n]
		}
		if !ready {
			break
		}
		b.ReadyUntil = s.segStart(n + 1)
	}
	track := min(max(0, s.wantTrack), len(s.done)-1)
	for n := start; n < start+512 && (s.total <= 0 || n < s.total); n++ {
		if !s.completedDeliveries[track][n] {
			break
		}
		if audio := s.audioTrackLocked(); audio >= 0 && audio < len(s.done) && !s.completedDeliveries[audio][n] {
			break
		}
		b.DeliveredUntil = s.segStart(n + 1)
	}
	return b
}
