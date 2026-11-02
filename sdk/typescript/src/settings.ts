/**
 * Settings schema decoding and validation (SPEC §5.5).
 * Mirrors internal/plugins/manifest.go and internal/plugins/install.go.
 */

import type { SettingField, SettingsSchema } from "./types.ts";
import { lintCopy } from "./i18n.ts";

const ALLOWED_KINDS = new Set([
  "group",
  "text",
  "secret",
  "duration",
  "readonly",
  "number",
  "bool",
  "select",
  "list",
  "bytes",
  "hidden",
  "multiSelect",
  "orderedList",
  "language",
  "viewer",
  "collection",
  "folder",
  "url",
  "title",
]);

export interface DecodedSettings<S> {
  settings: S;
  errors: Record<string, string>;
}

/** Collects all leaf setting fields from a SettingsSchema. */
export function collectSettingFields(
  schema: SettingsSchema | Record<string, unknown> | undefined,
): SettingField[] {
  if (!schema || typeof schema !== "object") return [];
  const out: SettingField[] = [];
  const walk = (node: unknown): void => {
    if (!node || typeof node !== "object") return;
    if (Array.isArray(node)) {
      for (const item of node) walk(item);
      return;
    }
    const obj = node as Record<string, unknown>;
    const key =
      typeof obj.key === "string"
        ? obj.key
        : typeof obj.name === "string"
          ? obj.name
          : typeof obj.id === "string"
            ? obj.id
            : typeof obj.field === "string"
              ? obj.field
              : "";
    const kind = typeof obj.kind === "string" ? obj.kind : "";
    if (key && kind && kind !== "group") {
      out.push({
        key,
        kind: kind as SettingField["kind"],
        label: typeof obj.label === "string" ? obj.label : undefined,
        hint: typeof obj.hint === "string" ? obj.hint : undefined,
        required: obj.required === true,
        default: obj.default,
        min: typeof obj.min === "number" ? obj.min : undefined,
        max: typeof obj.max === "number" ? obj.max : undefined,
      });
    }
    for (const v of Object.values(obj)) {
      if (v && typeof v === "object") walk(v);
    }
  };
  walk(schema);
  return out;
}

/** Lints a settings schema for forbidden kinds and copy-rule violations (SPEC §5.5). */
export function lintSettingsSchema(
  schema: SettingsSchema | Record<string, unknown> | undefined,
): Array<{ field: string; message: string }> {
  if (!schema || typeof schema !== "object") return [];
  const problems: Array<{ field: string; message: string }> = [];
  const walk = (node: unknown, path: string): void => {
    if (!node || typeof node !== "object") return;
    if (Array.isArray(node)) {
      node.forEach((item, i) => walk(item, `${path}[${i}]`));
      return;
    }
    const obj = node as Record<string, unknown>;
    if (typeof obj.kind === "string") {
      if (obj.kind === "json") {
        problems.push({
          field: `${path}.kind`,
          message: "the json field kind is not allowed in plugin settings",
        });
      } else if (!ALLOWED_KINDS.has(obj.kind)) {
        problems.push({
          field: `${path}.kind`,
          message: `unknown field kind ${JSON.stringify(obj.kind)}`,
        });
      }
    }
    if (typeof obj.label === "string" && !obj.label.startsWith("$t:")) {
      const msg = lintCopy(obj.label, 40);
      if (msg) problems.push({ field: `${path}.label`, message: msg });
    }
    if (typeof obj.hint === "string" && !obj.hint.startsWith("$t:")) {
      const msg = lintCopy(obj.hint, 120);
      if (msg) problems.push({ field: `${path}.hint`, message: msg });
      const trimmed = obj.hint.replace(/\.+$/, "");
      if (trimmed.includes(".")) {
        problems.push({ field: `${path}.hint`, message: "must be one sentence" });
      }
    }
    for (const [k, v] of Object.entries(obj)) {
      walk(v, `${path}.${k}`);
    }
  };
  walk(schema, "ether.settings");
  return problems;
}

/**
 * Decodes and validates incoming settings against a plugin's settings schema,
 * applying field defaults and returning JSON-pointer keyed errors for `/ether/configure`.
 */
export function decodeSettings<S = Record<string, unknown>>(
  schema: SettingsSchema | Record<string, unknown> | undefined,
  incoming: Record<string, unknown> | undefined,
  defaults?: Partial<S>,
): DecodedSettings<S> {
  const fields = collectSettingFields(schema);
  const result: Record<string, unknown> = { ...(defaults as Record<string, unknown> | undefined) };
  const errors: Record<string, string> = {};

  for (const field of fields) {
    if (field.default !== undefined && result[field.key] === undefined) {
      result[field.key] = field.default;
    }
  }

  if (incoming && typeof incoming === "object") {
    for (const [k, v] of Object.entries(incoming)) {
      result[k] = v;
    }
  }

  for (const field of fields) {
    const val = result[field.key];
    const ptr = `/${field.key}`;
    if (field.required && (val === undefined || val === null || val === "")) {
      errors[ptr] = "is required";
      continue;
    }
    if (val === undefined || val === null || val === "") {
      continue;
    }
    switch (field.kind) {
      case "number":
      case "bytes":
      case "viewer": {
        const num = typeof val === "number" ? val : Number(val);
        if (!Number.isFinite(num)) {
          errors[ptr] = "must be a number";
        } else {
          if (field.min !== undefined && num < field.min) {
            errors[ptr] = `must be at least ${field.min}`;
          }
          if (field.max !== undefined && num > field.max) {
            errors[ptr] = `must be at most ${field.max}`;
          }
          result[field.key] = num;
        }
        break;
      }
      case "bool": {
        if (typeof val !== "boolean") {
          errors[ptr] = "must be true or false";
        }
        break;
      }
      case "list":
      case "multiSelect":
      case "orderedList": {
        if (!Array.isArray(val)) {
          errors[ptr] = "must be a list";
        }
        break;
      }
      case "url": {
        if (typeof val !== "string" || (!val.startsWith("http://") && !val.startsWith("https://"))) {
          errors[ptr] = "must be an http or https URL";
        }
        break;
      }
      default:
        break;
    }
  }

  return {
    settings: result as S,
    errors,
  };
}
