/**
 * i18n and copy-rule helpers for Ether plugins (SPEC §2.7, §2.8, §5.1.1, §5.6).
 */

export type LocaleStrings = Record<string, string>;
export type StringsCatalog = Record<string, LocaleStrings>;

export interface I18nHelper {
  /** Resolves a key for the given locale with fallback to base language, then "en", then the key itself. */
  t(key: string, params?: Record<string, string | number>, locale?: string): string;
  /** Returns a "$t:<key>" reference for manifest and ViewSpec fields. */
  ref(key: string): `$t:${string}`;
  /** Returns the full catalog of locale strings. */
  catalog(): StringsCatalog;
}

/** Returns a `$t:key` reference for manifest and ViewSpec fields. */
export function tRef<K extends string>(key: K): `$t:${K}` {
  return `$t:${key}`;
}

/** Creates a locale resolver backed by an inline strings catalog. */
export function createI18n(
  strings: StringsCatalog = {},
  defaultLocale = "en",
): I18nHelper {
  const resolveKey = (
    rawKey: string,
    params?: Record<string, string | number>,
    locale = defaultLocale,
  ): string => {
    const key = rawKey.startsWith("$t:") ? rawKey.slice(3) : rawKey;
    const base = locale ? locale.split("-")[0] ?? "" : "";
    const candidates = [locale, base, "en"];
    let template: string | undefined;
    for (const loc of candidates) {
      if (!loc) continue;
      const table = strings[loc];
      if (table && Object.prototype.hasOwnProperty.call(table, key)) {
        template = table[key];
        break;
      }
    }
    if (template === undefined) {
      template = key;
    }
    if (!params) return template;
    return template.replace(/\{([A-Za-z0-9_]+)\}/g, (match, name: string) => {
      const val = params[name];
      return val !== undefined ? String(val) : match;
    });
  };

  return {
    t: resolveKey,
    ref: tRef,
    catalog: () => strings,
  };
}

const BANNED_WORDS = /\b(simply|easily|just)\b/i;

/**
 * Checks a user-visible string against Ether's copy rules (internal/plugins/manifest.go LintCopy):
 * - max characters (when max > 0)
 * - no em or en dashes
 * - no exclamation marks
 * - no "simply", "just" or "easily"
 * - sentence case (starts with an uppercase letter, not Title Case)
 * Returns an empty string when valid, or the plain reason when invalid.
 */
export function lintCopy(raw: string, max = 0): string {
  const s = raw.trim();
  if (max > 0 && Array.from(s).length > max) {
    return `is longer than ${max} characters`;
  }
  if (s.includes("—") || s.includes("–")) {
    return "must not contain em or en dashes";
  }
  if (s.includes("!")) {
    return "must not contain exclamation marks";
  }
  if (BANNED_WORDS.test(s)) {
    return "must not use the words simply, just or easily";
  }
  for (const ch of s) {
    const lower = ch.toLowerCase();
    const upper = ch.toUpperCase();
    if (lower !== upper) {
      if (ch === lower) {
        return "must use sentence case";
      }
      break;
    }
  }
  const words = s.split(/\s+/).filter(Boolean);
  if (words.length >= 3) {
    let caps = 0;
    for (const w of words) {
      const first = Array.from(w)[0];
      if (first && first.toLowerCase() !== first.toUpperCase() && first === first.toUpperCase()) {
        caps++;
      }
    }
    if (caps * 3 >= words.length * 2) {
      return "must use sentence case";
    }
  }
  return "";
}
