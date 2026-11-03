package livetv

import "time"

// warmWatchedWindow is how long after a channel was last watched its
// upcoming items keep being pre-warmed, so a viewer flicking back finds the
// next programme ready while an unwatched channel costs no indexer searches.
const warmWatchedWindow = 2 * time.Hour

// noteWatchedLocked records that channelID is being watched now; s.mu held.
func (s *Service) noteWatchedLocked(channelID string) {
	if s.watched == nil {
		s.watched = map[string]time.Time{}
	}
	s.watched[channelID] = s.o.Now()
}

// recentlyWatched is the set of channels playing now or watched within
// warmWatchedWindow; older entries are forgotten.
func (s *Service) recentlyWatched() map[string]bool {
	now := s.o.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ss := range s.sessions {
		s.noteWatchedLocked(ss.info.Channel.ID)
	}
	out := map[string]bool{}
	for id, at := range s.watched {
		if now.Sub(at) > warmWatchedWindow {
			delete(s.watched, id)
			continue
		}
		out[id] = true
	}
	return out
}
