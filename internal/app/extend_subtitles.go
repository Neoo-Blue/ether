package app

import "github.com/braedonsaunders/ether/internal/extend"

// BuiltinSubtitleDescriptors returns the built-in subtitle provider
// descriptors (OpenSubtitles and Stremio subtitle add-ons).
func BuiltinSubtitleDescriptors() []extend.Descriptor {
	return []extend.Descriptor{
		{
			Kind:  extend.KindSubtitleProvider,
			ID:    "opensubtitles",
			Owner: extend.Builtin,
			Label: extend.T("OpenSubtitles"),
			Hint:  extend.T("Search OpenSubtitles by IMDb, TMDB and release hash."),
			Icon:  "Subtitles",
			Order: 10,
		},
		{
			Kind:  extend.KindSubtitleProvider,
			ID:    "stremio",
			Owner: extend.Builtin,
			Label: extend.T("Stremio subtitle add-on"),
			Hint:  extend.T("Search a Stremio add-on whose manifest provides subtitles (such as SubSense)."),
			Icon:  "Puzzle",
			Order: 20,
		},
	}
}

// registerSubtitleDescriptors registers built-in subtitle providers (WS3).
func (a *App) registerSubtitleDescriptors(r *extend.Registry) error {
	return r.Register(BuiltinSubtitleDescriptors()...)
}
