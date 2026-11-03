package extend

// Settings field kinds (SPEC §5.5). The console renders a field from its
// kind; Secret says the value is stored sealed and never returned.
const (
	FieldText   = "text"
	FieldSecret = "secret"
	FieldURL    = "url"
	FieldSelect = "select"
	FieldHidden = "hidden"
)

// FieldOption is one choice of a select field.
type FieldOption struct {
	Value string `json:"value"`
	Label Text   `json:"label"`
}

// SettingsField is one field of a kind's settings schema.
type SettingsField struct {
	Key      string        `json:"key"`
	Kind     string        `json:"kind"`
	Label    Text          `json:"label"`
	Hint     Text          `json:"hint,omitempty"`
	Required bool          `json:"required,omitempty"`
	Secret   bool          `json:"secret,omitempty"`
	Default  string        `json:"default,omitempty"`
	Options  []FieldOption `json:"options,omitempty"`
	Wide     bool          `json:"wide,omitempty"`
}

// ChannelKindMeta is the Meta of a channelKind descriptor: the schema the
// console renders the channel drawer from.
type ChannelKindMeta struct {
	Fields []SettingsField `json:"fields"`
	// SendTest reports the kind supports the Send test button.
	SendTest bool `json:"sendTest"`
	// Selectable is false for kinds created another way (browser push is
	// created by connecting a device, not picked in the drawer).
	Selectable bool `json:"selectable"`
	// Formats lists payload formats: "text", and "html" for email.
	Formats []string `json:"formats,omitempty"`
	// MaxLength caps the delivered message text, in runes (0 = no cap).
	MaxLength int `json:"maxLength,omitempty"`
}

// EventKindMeta is the Meta of an eventKind descriptor. The label and hint
// carry the event's name and description.
type EventKindMeta struct {
	// Automatic reports Ether produces the event itself, as opposed to the
	// owner API only.
	Automatic bool `json:"automatic"`
	// Critical marks the events in the Critical alerts preset.
	Critical bool `json:"critical,omitempty"`
	// ChannelKinds, when set, restricts the event to these channel kinds
	// (the weekly newsletter goes by email only).
	ChannelKinds []string `json:"channelKinds,omitempty"`
}
