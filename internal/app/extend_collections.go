package app

import (
	"github.com/braedonsaunders/ether/internal/collections"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/personalize"
)

// registerCollectionDescriptors registers the built-in collection source
// kinds and Home Designer row source kinds (WS1b). The descriptors come
// from the same kind registries that validate and run the kinds, so the
// console can never offer a kind the server cannot run. Plugin list
// sources (WS9) and Stremio catalogs (WS3) register their kinds on the
// collections and personalize registries and publish descriptors the same
// way, with their plugin id as Owner.
func (a *App) registerCollectionDescriptors(r *extend.Registry) error {
	if err := r.Register(collections.BuiltinDescriptors()...); err != nil {
		return err
	}
	if a.personalize == nil {
		return r.Register(personalize.BuiltinRowDescriptors()...)
	}
	if err := a.personalize.RegisterRowSourceKind(stremioHomeKind{}); err != nil {
		return err
	}
	descriptors := a.personalize.RowKindRegistry().Descriptors()
	for i := range descriptors {
		if descriptors[i].ID == "stremioCatalog" {
			descriptors[i].Label = extend.T("Stremio catalog")
			descriptors[i].Hint = extend.T("A Home row from a Stremio catalog")
		}
	}
	return r.Register(descriptors...)
}

type stremioHomeKind struct{}

func (stremioHomeKind) Kind() string                 { return "stremioCatalog" }
func (stremioHomeKind) CollectionSourceKind() string { return "stremioCatalog" }
func (stremioHomeKind) Descriptor() core.RowDescriptor {
	return core.RowDescriptor{HubID: "collection:*", Kind: "stremioCatalog", Title: "Stremio catalog", Description: "A Home row from a Stremio catalog", Group: "collections", Sections: []string{"home", "movies", "shows"}}
}
