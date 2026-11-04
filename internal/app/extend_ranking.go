package app

import (
	"fmt"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/releasepolicy"
)

// registerRankingDescriptors registers the built-in ranking steps and rule
// fields (WS1a). Copy is the console's existing English, declared with
// extend.T so the same translations carry over. The resolver gets the
// registry here because it is built before the extensibility runtime.
func (a *App) registerRankingDescriptors(r *extend.Registry) error {
	if err := r.Register(builtinRankingDescriptors()...); err != nil {
		return err
	}
	if a.resolver != nil {
		a.resolver.SetExtendRegistry(r)
	}
	return nil
}

// builtinRankingDescriptors builds every rankingStep and ruleField
// descriptor Ether ships, in current console order. Step ids and default
// positions come from core.DefaultRankingSteps; rule fields come from
// releasepolicy.BuiltinFields, so the registry cannot drift from the code
// that validates and runs them.
func builtinRankingDescriptors() []extend.Descriptor {
	ds := rankingStepDescriptors()
	ds = append(ds, ruleFieldDescriptors()...)
	return ds
}

func rankingStepDescriptors() []extend.Descriptor {
	type stepCopy struct {
		configKey         string
		hint              extend.Text
		editor            extend.RankingEditorKind
		options           []extend.DescriptorOption
		required, movable bool
	}
	copy := map[string]stepCopy{
		core.RankLanguage: {
			editor:    extend.RankingEditorOrderedList,
			configKey: "preferredLanguages", movable: true,
		},
		core.RankConnection: {
			editor: extend.RankingEditorNone,
			hint:   extend.T("Fits the viewer's bandwidth"), required: true,
		},
		core.RankResolution: {
			editor:    extend.RankingEditorRange,
			configKey: "heightRange", movable: true,
			options: extend.Options("480", "720", "1080", "2160"),
		},
		core.RankReleaseType: {
			editor:    extend.RankingEditorOrderedList,
			configKey: "preferredSources", movable: true,
			options: extend.Options("remux", "bluray", "webdl", "webrip", "hdtv", "dvd", "screener", "cam"),
		},
		core.RankHDR: {
			editor:    extend.RankingEditorChoice,
			configKey: "hdr", movable: true,
			options: extend.Options("dv", "hdr10", "sdr"),
		},
		core.RankCustomScore: {
			editor:    extend.RankingEditorNone,
			configKey: "customFormatScores", movable: true,
			hint: extend.T("Higher total score first, including your custom format scores"),
		},
		core.RankVerified: {
			editor: extend.RankingEditorNone,
			hint:   extend.T("Verified streams rank ahead of untested ones"), required: true,
		},
		core.RankVideoCodec: {
			editor:    extend.RankingEditorOrderedList,
			configKey: "preferredCodecs", movable: true,
			options: extend.Options("av1", "hevc", "h264", "vp9", "mpeg2"),
		},
		core.RankAudioFormat: {
			editor:    extend.RankingEditorOrderedList,
			configKey: "preferredAudio", movable: true,
			options: extend.Options("truehd", "dtshd", "eac3", "ac3", "dts", "aac", "flac", "opus"),
		},
		core.RankSize: {
			editor:    extend.RankingEditorToggle,
			configKey: "preferLargest", movable: true,
		},
	}
	// Labels must be extend.T string literals for the i18n extractor, so
	// they are spelled out per step rather than read from the map.
	labels := map[string]extend.Text{
		core.RankLanguage:    extend.T("Audio language"),
		core.RankConnection:  extend.T("Connection fit"),
		core.RankResolution:  extend.T("Resolution"),
		core.RankReleaseType: extend.T("Release type"),
		core.RankHDR:         extend.T("HDR"),
		core.RankCustomScore: extend.T("Custom score"),
		core.RankVerified:    extend.T("Played fine recently"),
		core.RankVideoCodec:  extend.T("Video codec"),
		core.RankAudioFormat: extend.T("Audio format"),
		core.RankSize:        extend.T("Larger file"),
	}
	ds := make([]extend.Descriptor, 0, len(core.DefaultRankingSteps))
	for i, id := range core.DefaultRankingSteps {
		c, ok := copy[id]
		if !ok {
			panic(fmt.Sprintf("extend ranking: no descriptor copy for step %q", id))
		}
		d := extend.Descriptor{
			Kind: extend.KindRankingStep, ID: id, Owner: extend.Builtin,
			Label: labels[id], Hint: c.hint, Order: i,
		}.WithMeta(extend.RankingStepMeta{
			Editor:          c.editor,
			Options:         c.options,
			Required:        c.required,
			CanMove:         c.movable,
			DefaultPosition: i,
			ConfigKey:       c.configKey,
		})
		ds = append(ds, d)
	}
	return ds
}

func ruleFieldDescriptors() []extend.Descriptor {
	// Presentation per field: label and hint copy, suggested choices, unit.
	// Validation shape (class, operators, single-value) comes from
	// releasepolicy so compile and the picker read the same spec.
	type fieldCopy struct {
		label   extend.Text
		hint    extend.Text
		choices []extend.DescriptorOption
		unit    string
	}
	languageCodes := extend.Options(
		"en", "es", "es-419", "es-ES", "pt", "pt-BR", "pt-PT", "fr", "de", "it",
		"nl", "sv", "da", "no", "nb", "nn", "fi", "is", "ja", "ko", "zh", "pl",
		"cs", "hu", "ro", "tr", "ru", "uk", "ar", "he", "hi", "th", "vi", "id",
	)
	copy := map[string]fieldCopy{
		"language":  {label: extend.T("Declared audio language"), choices: languageCodes},
		"subtitles": {label: extend.T("Declared subtitle language"), choices: languageCodes},
		"audio_matches_original": {
			label: extend.T("Original-language audio"),
			hint:  extend.T("Declared audio must match this title's original language. Subtitles do not count, and a filename alone cannot make the comparison."),
		},
		"name":  {label: extend.T("Release name")},
		"group": {label: extend.T("Release group")},
		"source": {
			label:   extend.T("Release type"),
			choices: extend.Options("webdl", "webrip", "bluray", "remux", "hdtv", "dvd", "cam"),
		},
		"video": {
			label:   extend.T("Video codec"),
			choices: extend.Options("h264", "hevc", "av1", "mpeg2", "xvid"),
		},
		"audio": {
			label:   extend.T("Audio codec"),
			choices: extend.Options("aac", "ac3", "eac3", "truehd", "dts", "dtshd", "opus"),
		},
		"height":   {label: extend.T("Resolution"), unit: "p"},
		"size_gib": {label: extend.T("Size (GiB)"), unit: "GiB"},
		"hdr": {
			label:   extend.T("HDR"),
			choices: extend.Options("hdr10", "dv"),
		},
		"edition":  {label: extend.T("Edition")},
		"indexer":  {label: extend.T("Indexer")},
		"protocol": {label: extend.T("Protocol"), choices: extend.Options("usenet", "torrent", "http")},
	}
	fields := releasepolicy.BuiltinFields()
	ds := make([]extend.Descriptor, 0, len(fields))
	for i, f := range fields {
		c, ok := copy[f.ID]
		if !ok {
			panic(fmt.Sprintf("extend ranking: no descriptor copy for rule field %q", f.ID))
		}
		d := extend.Descriptor{
			Kind: extend.KindRuleField, ID: f.ID, Owner: extend.Builtin,
			Label: c.label, Hint: c.hint, Order: i,
		}.WithMeta(extend.RuleFieldMeta{
			ValueClass:      f.ValueClass,
			Operators:       f.Operators,
			DefaultOperator: f.DefaultOperator,
			Choices:         c.choices,
			Unit:            c.unit,
			SingleValue:     f.SingleValue,
		})
		ds = append(ds, d)
	}
	return ds
}
