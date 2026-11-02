package app

import (
	"context"
	"strings"
)

// syncLanguages keeps auth_users.audio_languages and subtitle_languages
// aligned with the viewer's ordered lists in trackprefs (Audio & subtitles).
// Track selection and every protocol read the trackprefs lists; the account
// columns stay the same facts for callers that already load them from the
// user row (release language ranking, Plex accounts, failover).
func (a *App) syncLanguages(ctx context.Context, user int64, audio, subtitles []string) error {
	if user <= 0 || a.users == nil {
		return nil
	}
	current, err := a.users.ActiveUser(ctx, user)
	if err != nil {
		return err
	}
	if sameLanguages(current.AudioLanguages, audio) && sameLanguages(current.SubtitleLanguages, subtitles) {
		return nil
	}
	next := *current
	next.AudioLanguages = append([]string(nil), audio...)
	next.SubtitleLanguages = append([]string(nil), subtitles...)
	return a.users.UpdateUser(ctx, next)
}

func sameLanguages(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !strings.EqualFold(strings.TrimSpace(a[i]), strings.TrimSpace(b[i])) {
			return false
		}
	}
	return true
}
