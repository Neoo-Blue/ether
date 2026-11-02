package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/notify"
)

type notificationField struct {
	Key      string `json:"key"`
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	Hint     string `json:"hint"`
	Required bool   `json:"required"`
	Secret   bool   `json:"secret"`
	Default  string `json:"default"`
	Options  []struct {
		Value string `json:"value"`
		Label string `json:"label"`
	} `json:"options"`
}

func notificationFields(id string, raw json.RawMessage) []extend.SettingsField {
	var fields []notificationField
	if json.Unmarshal(raw, &fields) != nil {
		var doc struct {
			Fields []notificationField `json:"fields"`
		}
		_ = json.Unmarshal(raw, &doc)
		fields = doc.Fields
	}
	out := make([]extend.SettingsField, 0, len(fields))
	for _, f := range fields {
		field := extend.SettingsField{Key: f.Key, Kind: f.Kind, Label: textOf(id, f.Label), Hint: textOf(id, f.Hint), Required: f.Required, Secret: f.Secret || f.Kind == "secret", Default: f.Default}
		for _, opt := range f.Options {
			field.Options = append(field.Options, extend.FieldOption{Value: opt.Value, Label: textOf(id, opt.Label)})
		}
		out = append(out, field)
	}
	return out
}

func (m *Manager) syncNotificationKind(r Record) {
	s := m.deps.Notifications
	if s == nil {
		return
	}
	id := extend.PluginID(r.ID, "notify")
	s.Kinds().RemovePlugin(id, r.ID)
	if !r.Enabled || r.Manifest.Ether == nil || !r.Manifest.HasCapability(CapNotify) || r.Manifest.Ether.Contributes.Notification == nil {
		return
	}
	meta := r.Manifest.Ether.Contributes.Notification
	fields := notificationFields(r.ID, meta.Fields)
	k := notify.ChannelKind{ID: id, Owner: r.ID, Label: textOf(r.ID, r.Name), Fields: fields, SendTest: meta.SendTest, Selectable: true, Formats: meta.Formats, MaxLength: meta.MaxLength}
	k.Validate = func(c notify.Channel) error {
		for _, f := range fields {
			v := c.Setting(f.Key)
			if f.Secret {
				v = c.Secret(f.Key)
			}
			if f.Required && v == "" {
				return fmt.Errorf("%s is required", f.Key)
			}
			if v != "" && f.Kind == extend.FieldSelect && len(f.Options) > 0 {
				valid := false
				for _, option := range f.Options {
					if option.Value == v {
						valid = true
						break
					}
				}
				if !valid {
					return fmt.Errorf("%s has an invalid value", f.Key)
				}
			}
		}
		return nil
	}
	k.Deliver = func(ctx context.Context, _ *notify.Service, c notify.Channel, event notify.Event) error {
		rec, err := m.store.record(ctx, r.ID)
		if err != nil {
			return err
		}
		if rec.Shadow {
			_, err := m.store.journal(ctx, JournalEntry{Plugin: r.ID, Op: "notify", Detail: event.Title + ": " + event.Message, Shadow: true})
			return err
		}
		// Configure and deliver as one serialized operation: channel credentials
		// are sent only on the configure route, including for remote runtimes.
		lock := m.notificationLock(r.ID)
		lock.Lock()
		defer lock.Unlock()
		settings, err := m.mergedSettings(ctx, r.ID)
		if err != nil {
			return errors.New("plugin notification configuration failed")
		}
		for key, value := range c.Settings {
			settings[key] = value
		}
		for key, value := range c.Secrets {
			settings[key] = value
		}
		configBody, err := json.Marshal(settings)
		if err != nil {
			return errors.New("plugin notification configuration failed")
		}
		configured, err := m.Call(ctx, r.ID, Request{Method: "POST", Path: "/ether/configure", Body: configBody})
		if err != nil || configured.Status < 200 || configured.Status >= 300 {
			return errors.New("plugin notification configuration failed")
		}
		var configuredResult struct {
			Errors map[string]string `json:"errors"`
		}
		if len(configured.Body) > 0 && (json.Unmarshal(configured.Body, &configuredResult) != nil || len(configuredResult.Errors) > 0) {
			return errors.New("plugin notification configuration failed")
		}
		if meta.MaxLength > 0 {
			runes := []rune(event.Message)
			if len(runes) > meta.MaxLength {
				event.Message = string(runes[:meta.MaxLength])
			}
		}
		body, err := json.Marshal(map[string]any{"event": event})
		if err != nil {
			return err
		}
		resp, err := m.Call(ctx, r.ID, Request{Method: "POST", Path: "/ether/notify", Body: body})
		if err != nil || resp.Status < 200 || resp.Status >= 300 {
			return errors.New("plugin notification delivery failed")
		}
		return nil
	}
	if err := s.RegisterChannelKind(k); err != nil {
		m.log.Warn("plugin notification kind registration failed", "plugin", r.ID, "error", err)
	}
}
