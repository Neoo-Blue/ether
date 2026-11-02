package schema

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/braedonsaunders/ether/internal/extend"
)

// Field error messages. They are shown beside fields in the console, so they
// are declared with extend.T and translated like any built-in copy.
var (
	errRequired    = extend.T("This is required.").Source
	errText        = extend.T("Must be text.").Source
	errNumber      = extend.T("Must be a number.").Source
	errBool        = extend.T("Must be on or off.").Source
	errList        = extend.T("Must be a list.").Source
	errGroup       = extend.T("Must be a group of settings.").Source
	errChoice      = extend.T("Is not one of the choices.").Source
	errDuplicate   = extend.T("The same choice appears twice.").Source
	errURL         = extend.T("Must be a web address.").Source
	errLanguage    = extend.T("Must be a language code such as en or en-US.").Source
	errDuration    = extend.T("Must be a duration such as 24h or 30m.").Source
	errTooSmallFmt = extend.T("Must be at least %v.").Source
	errTooLargeFmt = extend.T("Must be at most %v.").Source
)

var languagePattern = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)

// ValidateValues checks values against s and returns field errors keyed by
// JSON pointer ("/port", "/rtxVideo/mode"). Missing optional fields are fine;
// keys the schema does not name are ignored so newer servers can add fields
// without breaking older stored settings. Plugin settings use this before
// their own /ether/configure check.
func ValidateValues(s Schema, values map[string]any) map[string]string {
	errs := map[string]string{}
	for _, g := range s.Groups {
		validateFields(g.Fields, values, "", errs)
		validateFields(g.More, values, "", errs)
	}
	return errs
}

func pointer(prefix, key string) string {
	escaped := strings.NewReplacer("~", "~0", "/", "~1").Replace(key)
	return prefix + "/" + escaped
}

func validateFields(fields []Field, values map[string]any, prefix string, errs map[string]string) {
	for _, f := range fields {
		at := pointer(prefix, f.Key)
		value, present := values[f.Key]
		if f.Kind == KindGroup {
			if present && value != nil {
				nested, ok := value.(map[string]any)
				if !ok {
					errs[at] = errGroup
					continue
				}
				validateFields(f.Fields, nested, at, errs)
			}
			continue
		}
		if f.Kind == KindHidden || f.Kind == KindReadonly || f.Kind == KindJSON {
			continue
		}
		if !present || value == nil {
			if f.Required {
				errs[at] = errRequired
			}
			continue
		}
		switch f.Kind {
		case KindText, KindSecret, KindFolder:
			str, ok := value.(string)
			switch {
			case !ok:
				errs[at] = errText
			case f.Required && strings.TrimSpace(str) == "":
				errs[at] = errRequired
			}
		case KindDuration:
			str, ok := value.(string)
			switch {
			case !ok:
				errs[at] = errText
			case str == "":
				if f.Required {
					errs[at] = errRequired
				}
			default:
				if _, err := time.ParseDuration(str); err != nil {
					errs[at] = errDuration
				}
			}
		case KindURL:
			str, ok := value.(string)
			switch {
			case !ok:
				errs[at] = errText
			case str == "":
				if f.Required {
					errs[at] = errRequired
				}
			default:
				u, err := url.Parse(str)
				if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
					errs[at] = errURL
				}
			}
		case KindLanguage:
			str, ok := value.(string)
			switch {
			case !ok:
				errs[at] = errText
			case str == "":
				if f.Required {
					errs[at] = errRequired
				}
			case !languagePattern.MatchString(str):
				errs[at] = errLanguage
			}
		case KindViewer, KindCollection, KindTitle:
			// Pickers store an id; numeric ids and string keys are both in use.
			switch v := value.(type) {
			case string:
				if f.Required && v == "" {
					errs[at] = errRequired
				}
			default:
				if _, ok := numberOf(value); !ok {
					errs[at] = errText
				}
			}
		case KindNumber, KindBytes:
			n, ok := numberOf(value)
			switch {
			case !ok || math.IsNaN(n) || math.IsInf(n, 0):
				errs[at] = errNumber
			case f.Min != nil && n < *f.Min:
				errs[at] = fmt.Sprintf(errTooSmallFmt, trimNumber(*f.Min))
			case f.Max != nil && n > *f.Max:
				errs[at] = fmt.Sprintf(errTooLargeFmt, trimNumber(*f.Max))
			}
		case KindBool:
			if _, ok := value.(bool); !ok {
				errs[at] = errBool
			}
		case KindSelect:
			if len(f.Options) > 0 && !isChoice(f, value) {
				errs[at] = errChoice
			}
		case KindMultiSelect, KindOrderedList, KindList:
			items, ok := value.([]any)
			if !ok {
				errs[at] = errList
				continue
			}
			if f.Required && len(items) == 0 {
				errs[at] = errRequired
				continue
			}
			seen := map[string]bool{}
			for _, item := range items {
				if f.Kind != KindList && len(f.Options) > 0 && !isChoice(f, item) {
					errs[at] = errChoice
					break
				}
				switch item.(type) {
				case string:
				default:
					if _, isNum := numberOf(item); !isNum || !f.Numeric {
						errs[at] = errText
					}
				}
				if errs[at] != "" {
					break
				}
				if f.Kind == KindOrderedList {
					key := fmt.Sprintf("%v", item)
					if seen[key] {
						errs[at] = errDuplicate
						break
					}
					seen[key] = true
				}
			}
		}
	}
}

func numberOf(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

func isChoice(f Field, value any) bool {
	got, ok := choiceString(f, value)
	if !ok {
		return false
	}
	for _, o := range f.Options {
		if o.Value == got {
			return true
		}
	}
	return false
}

func choiceString(f Field, value any) (string, bool) {
	if str, ok := value.(string); ok {
		return str, true
	}
	if n, ok := numberOf(value); ok && f.Numeric {
		return trimNumber(n), true
	}
	return "", false
}

func trimNumber(n float64) string {
	if n == math.Trunc(n) && !math.IsInf(n, 0) {
		return fmt.Sprintf("%d", int64(n))
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", n), "0"), ".")
}

// ValidateSchema checks a schema itself. Plugin schemas (forPlugin) may not
// use the json kind and must follow the copy rules of SPEC §5.5.
func ValidateSchema(s Schema, forPlugin bool) error {
	seen := map[string]bool{}
	for _, g := range s.Groups {
		for _, fields := range [][]Field{g.Fields, g.More} {
			if err := validateSchemaFields(fields, "", seen, forPlugin); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateSchemaFields(fields []Field, prefix string, seen map[string]bool, forPlugin bool) error {
	for _, f := range fields {
		at := pointer(prefix, f.Key)
		switch {
		case f.Key == "":
			return fmt.Errorf("%s: key is required", prefix)
		case f.Label.Empty():
			return fmt.Errorf("%s: label is required", at)
		case !Known(f.Kind):
			return fmt.Errorf("%s: unknown kind %q", at, f.Kind)
		case forPlugin && f.Kind == KindJSON:
			return fmt.Errorf("%s: json fields are not allowed in plugin settings", at)
		case seen[at]:
			// The same key may appear in fields and more as two halves of one
			// group (TRANSCODE's rtxVideo); only leaf duplicates are an error.
			if f.Kind != KindGroup {
				return fmt.Errorf("%s: key is used twice", at)
			}
		}
		seen[at] = true
		if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
			return fmt.Errorf("%s: min is above max", at)
		}
		switch f.Kind {
		case KindSelect, KindMultiSelect, KindOrderedList:
			if len(f.Options) == 0 && f.OptionsFrom == "" {
				return fmt.Errorf("%s: %s needs options", at, f.Kind)
			}
		case KindBytes:
			if u := f.Unit.Source; u != "" && u != "GiB" && u != "MiB" {
				return fmt.Errorf("%s: bytes unit must be GiB or MiB, not %q", at, u)
			}
		case KindGroup:
			if err := validateSchemaFields(f.Fields, at, seen, forPlugin); err != nil {
				return err
			}
		}
		if forPlugin {
			if err := copyLint(at, "label", f.Label, 40); err != nil {
				return err
			}
			if !f.Help.Empty() {
				if err := copyLint(at, "hint", f.Help, 120); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

var bannedWords = regexp.MustCompile(`(?i)\b(simply|easily|just)\b`)

func copyLint(at, what string, text extend.Text, maxRunes int) error {
	source := text.Source
	if source == "" {
		source = text.Resolved
	}
	if source == "" { // Plugin $t copy is linted from its strings at install.
		return nil
	}
	switch {
	case utf8.RuneCountInString(source) > maxRunes:
		return fmt.Errorf("%s: %s is over %d characters", at, what, maxRunes)
	case strings.Contains(source, "—"):
		return fmt.Errorf("%s: %s contains an em dash", at, what)
	case strings.Contains(source, "!"):
		return fmt.Errorf("%s: %s contains an exclamation mark", at, what)
	case bannedWords.MatchString(source):
		return fmt.Errorf("%s: %s uses a word the copy rules ban (simply, just, easily)", at, what)
	case what == "hint" && strings.Count(strings.TrimRight(source, "."), ". ") > 0:
		return fmt.Errorf("%s: hint must be one sentence", at)
	}
	return nil
}
