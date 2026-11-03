package app

import "github.com/braedonsaunders/ether/internal/extend"

// BuiltinSubtitleDescriptors returns the built-in subtitle provider
// descriptors. Stremio subtitle add-ons are installed add-ons: each
// publishes its own descriptor (plugin:<id>/subtitles).
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
	}
}

// registerSubtitleDescriptors registers built-in subtitle providers (WS3).
func (a *App) registerSubtitleDescriptors(r *extend.Registry) error {
	return r.Register(BuiltinSubtitleDescriptors()...)
}
