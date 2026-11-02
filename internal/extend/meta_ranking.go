package extend

// Ranking step and rule field descriptor metadata (SPEC §5.9, WS1a).
// Mirrored as TypeScript types next to the console consumer in
// web/src/pages/releases/ReleasePreferences.tsx.

// DescriptorOption is one selectable value in a descriptor editor. Label is
// optional: when empty the console shows Value.
type DescriptorOption struct {
	Value string `json:"value"`
	Label Text   `json:"label,omitempty"`
}

// Options builds DescriptorOptions from plain values whose display text is
// the value itself (codec names, source kinds, language codes).
func Options(values ...string) []DescriptorOption {
	out := make([]DescriptorOption, len(values))
	for i, v := range values {
		out[i] = DescriptorOption{Value: v}
	}
	return out
}

// RankingEditorKind names the inline editor a ranking step offers in the
// "What matters most" ladder.
type RankingEditorKind string

const (
	RankingEditorNone        RankingEditorKind = "none"
	RankingEditorOrderedList RankingEditorKind = "ordered_list"
	RankingEditorChoice      RankingEditorKind = "choice"
	RankingEditorRange       RankingEditorKind = "range"
	RankingEditorToggle      RankingEditorKind = "toggle"
)

// RankingStepMeta is the Meta of a KindRankingStep descriptor.
type RankingStepMeta struct {
	// Editor is the inline editor kind for this step's own setting.
	Editor RankingEditorKind `json:"editor"`
	// Options are the values the editor offers: the ordered choices for
	// ordered_list and choice editors, the selectable bounds for range.
	Options []DescriptorOption `json:"options,omitempty"`
	// Required steps must stay in every ladder (connection, verified).
	Required bool `json:"required,omitempty"`
	// CanMove is false for steps the ladder renders as Automatic: the owner
	// cannot reorder them.
	CanMove bool `json:"canMove"`
	// DefaultPosition is the step's 0-based place in the default ladder.
	// "Add a step" inserts a step ahead of the first ladder step with a
	// larger DefaultPosition. Plugin steps are never auto-inserted; this
	// only says where one lands when the owner adds it.
	DefaultPosition int `json:"defaultPosition"`
	// ConfigKey names the QualityProfile field the editor edits for
	// built-in steps (preferredLanguages, preferredSources,
	// preferredCodecs, preferredAudio, preferLargest, heightRange, hdr).
	// Plugin steps leave it empty; their settings arrive with WS9h.
	ConfigKey string `json:"configKey,omitempty"`
}

// Rule value classes: how a condition value is edited and validated.
const (
	RuleValueText     = "text"     // free text with equals/contains/regex
	RuleValueChoice   = "choice"   // free text with suggested choices
	RuleValueLanguage = "language" // exact declared-language match
	RuleValueNumber   = "number"   // a min/max range
	RuleValueNone     = "none"     // takes no value at all
)

// RuleFieldMeta is the Meta of a KindRuleField descriptor.
type RuleFieldMeta struct {
	// ValueClass is one of the RuleValue* constants.
	ValueClass string `json:"valueClass"`
	// Operators are the comparisons the field accepts, in display order
	// (equals, contains, regex, range).
	Operators []string `json:"operators,omitempty"`
	// DefaultOperator is preselected when a condition is created or
	// retargeted to this field.
	DefaultOperator string `json:"defaultOperator,omitempty"`
	// Choices are suggested values for choice and language fields. Choice
	// fields still accept free text; language choices are ISO codes the
	// console labels through Intl.DisplayNames.
	Choices []DescriptorOption `json:"choices,omitempty"`
	// Unit is the value's unit for number fields ("GiB", "p").
	Unit string `json:"unit,omitempty"`
	// SingleValue fields can hold one value per release, so two required
	// rules demanding different values conflict.
	SingleValue bool `json:"singleValue,omitempty"`
}
