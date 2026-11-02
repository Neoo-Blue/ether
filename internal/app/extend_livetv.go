package app

import (
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/livetv"
)

// registerLiveTVDescriptors registers the built-in Live TV source kinds
// (WS1b). Stremio tv add-ons (WS3) and plugin channel sources (WS9)
// register kinds on the livetv registry and publish descriptors the same
// way, with their owner set.
func (a *App) registerLiveTVDescriptors(r *extend.Registry) error {
	return r.Register(livetv.BuiltinDescriptors()...)
}
