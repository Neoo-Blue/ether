package app

import (
	"github.com/braedonsaunders/ether/internal/config/schema"
	"github.com/braedonsaunders/ether/internal/extend"
)

// registerSettingsDescriptors serves Ether's own settings forms as
// settingsSchema descriptors (WS1f). The console renders them with
// SchemaForm; there is no TypeScript copy.
func (a *App) registerSettingsDescriptors(r *extend.Registry) error {
	ds, err := schema.BuiltinDescriptors()
	if err != nil {
		return err
	}
	return r.Register(ds...)
}
