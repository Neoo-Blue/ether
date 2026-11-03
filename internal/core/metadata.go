package core

import (
	"context"
	"errors"
)

// MetadataRequest identifies a record within a provider and, when known, its
// expected canonical IDs. These are per-caller assertions, never identity hints
// that let a provider silently rebind a resource. Provider IDs are opaque; they are never used as TMDB IDs.
type MetadataRequest struct {
	ID   string    `json:"id"`
	Type MediaType `json:"type"`
	IDs  IDs       `json:"ids,omitempty"`
}

// MetadataProvider resolves catalogue records during explicit materialization.
// The catalogue owns identity validation, persistence and visibility policy.
// No ordinary browse or cached detail read invokes this interface.
type MetadataProvider interface {
	ResolveMetadata(context.Context, MetadataRequest) (*Title, error)
}

var ErrMetadataIdentity = errors.New("metadata has no supported catalogue identity")
var ErrMetadataFiltered = errors.New("metadata is hidden by catalogue visibility rules")
