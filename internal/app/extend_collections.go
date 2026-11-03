package app

import (
	"github.com/braedonsaunders/ether/internal/collections"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/personalize"
)

// registerCollectionDescriptors registers the built-in collection source
// kinds and Home Designer row source kinds (WS1b). The descriptors come
// from the same kind registries that validate and run the kinds, so the
// console can never offer a kind the server cannot run. Plugin and Stremio
// add-on list sources (WS9) register their kinds on the collections and
// personalize registries and publish descriptors the same way, with their
// plugin id as Owner.
func (a *App) registerCollectionDescriptors(r *extend.Registry) error {
	if err := r.Register(collections.BuiltinDescriptors()...); err != nil {
		return err
	}
	if a.personalize == nil {
		return r.Register(personalize.BuiltinRowDescriptors()...)
	}
	return r.Register(a.personalize.RowKindRegistry().Descriptors()...)
}
