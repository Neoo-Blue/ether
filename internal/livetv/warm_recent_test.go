package livetv

import (
	"testing"
	"time"
)

// Only channels playing now or watched in the last two hours are
// pre-warmed; an unwatched channel costs no searches.
func TestRecentlyWatchedChannels(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	s := &Service{o: Options{Now: func() time.Time { return now }}, sessions: map[string]*session{}}
	s.mu.Lock()
	s.noteWatchedLocked("news")
	s.mu.Unlock()
	playing := &session{}
	playing.info.Channel.ID = "movies"
	s.sessions["one"] = playing
	if got := s.recentlyWatched(); !got["news"] || !got["movies"] || got["sports"] {
		t.Fatalf("recent = %v, want news and movies", got)
	}
	now = now.Add(3 * time.Hour)
	if got := s.recentlyWatched(); got["news"] || !got["movies"] {
		t.Fatalf("recent after 3h = %v, want only the channel still playing", got)
	}
	delete(s.sessions, "one")
	now = now.Add(3 * time.Hour)
	if got := s.recentlyWatched(); len(got) != 0 {
		t.Fatalf("recent after everyone left = %v, want none", got)
	}
}
