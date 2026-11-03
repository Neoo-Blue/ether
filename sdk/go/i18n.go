package etherplugin

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	bannedCopyRE = regexp.MustCompile(`(?i)\b(simply|easily|just)\b`)
	interpRE     = regexp.MustCompile(`\{([A-Za-z0-9_]+)\}`)
)

// TRef formats a "$t:<key>" localization reference for manifests and ViewSpecs.
func TRef(key string) string {
	return "$t:" + key
}

// LintCopy validates a user-visible string against Ether's copy rules.
// It returns an empty string when valid, or the plain reason when invalid.
func LintCopy(raw string, maxRunes int) string {
	s := strings.TrimSpace(raw)
	if maxRunes > 0 && utf8.RuneCountInString(s) > maxRunes {
		return fmt.Sprintf("is longer than %d characters", maxRunes)
	}
	if strings.ContainsAny(s, "—–") {
		return "must not contain em or en dashes"
	}
	if strings.Contains(s, "!") {
		return "must not contain exclamation marks"
	}
	if bannedCopyRE.MatchString(s) {
		return "must not use the words simply, just or easily"
	}
	for _, r := range s {
		if unicode.IsLetter(r) {
			if unicode.IsLower(r) {
				return "must use sentence case"
			}
			break
		}
	}
	words := strings.Fields(s)
	if len(words) >= 3 {
		caps := 0
		for _, w := range words {
			first, _ := utf8.DecodeRuneInString(w)
			if unicode.IsLetter(first) && unicode.IsUpper(first) {
				caps++
			}
		}
		if caps*3 > len(words)*2 {
			return "must use sentence case"
		}
	}
	return ""
}

// StringsCatalog maps locale codes (e.g. "en", "fr-CA") to key-value tables.
type StringsCatalog map[string]map[string]string

// I18n resolves localized strings with fallback to base language and "en".
type I18n struct {
	catalog       StringsCatalog
	defaultLocale string
}

// NewI18n creates a localization helper backed by a StringsCatalog.
func NewI18n(catalog StringsCatalog, defaultLocale string) *I18n {
	if defaultLocale == "" {
		defaultLocale = "en"
	}
	if catalog == nil {
		catalog = StringsCatalog{}
	}
	return &I18n{catalog: catalog, defaultLocale: defaultLocale}
}

// T resolves a key for the given locale and interpolates {param} placeholders.
func (i *I18n) T(rawKey string, params map[string]any, locale string) string {
	key := strings.TrimPrefix(rawKey, "$t:")
	if locale == "" {
		locale = i.defaultLocale
	}
	base := locale
	if idx := strings.IndexByte(locale, '-'); idx > 0 {
		base = locale[:idx]
	}
	var tmpl string
	found := false
	for _, cand := range []string{locale, base, "en"} {
		if cand == "" {
			continue
		}
		if tbl, ok := i.catalog[cand]; ok {
			if v, ok := tbl[key]; ok {
				tmpl = v
				found = true
				break
			}
		}
	}
	if !found {
		tmpl = key
	}
	if len(params) == 0 {
		return tmpl
	}
	return interpRE.ReplaceAllStringFunc(tmpl, func(match string) string {
		name := match[1 : len(match)-1]
		if v, ok := params[name]; ok && v != nil {
			return fmt.Sprint(v)
		}
		return match
	})
}

// Catalog returns the underlying locale catalog.
func (i *I18n) Catalog() StringsCatalog {
	return i.catalog
}
