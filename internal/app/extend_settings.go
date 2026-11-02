package app

import (
	"fmt"

	"github.com/braedonsaunders/ether/internal/config/schema"
	"github.com/braedonsaunders/ether/internal/extend"
)

// registerSettingsDescriptors serves Ether's own settings forms as
// settingsSchema descriptors (WS1f). The console renders them with
// SchemaForm; there is no TypeScript copy.
func (a *App) registerSettingsDescriptors(r *extend.Registry) error {
	builtins := schema.Builtins()
	ds := make([]extend.Descriptor, 0, len(builtins))
	for i, b := range builtins {
		if err := schema.ValidateSchema(b.Schema, false); err != nil {
			return fmt.Errorf("settings schema %s: %w", b.ID, err)
		}
		ds = append(ds, extend.Descriptor{
			Kind:  extend.KindSettingsSchema,
			ID:    b.ID,
			Owner: extend.Builtin,
			Label: b.Label,
			Order: i,
		}.WithMeta(b.Schema.Meta(b.ConfigKey)))
	}
	return r.Register(ds...)
}
