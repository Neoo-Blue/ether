/**
 * The bridge between a rendered plugin view and Ether: who the plugin is,
 * how actions post back, how the view refreshes, and how view parameters
 * (search text, page, sort, tab) re-run the plugin's loader. PluginView
 * provides it; blocks and widgets consume it. Standalone renders (fixtures,
 * tests) get inert defaults so a spec can render without a server.
 */
import { createContext, useContext } from "react";
import {
  ROOT_SCOPE_KEY,
  isFieldRef,
  resolvePath,
  type FieldRef,
  type Value,
} from "@braedonsaunders/appkit-viewspec";

/** An approval card returned by a plugin action (same idea as Ask Ether's). */
export interface PluginProposal {
  id: string;
  title: string;
  description?: string;
  /** Pre-formatted lines the plugin's server wrote for the owner. */
  lines?: string[];
}

/** Where an action result wants the owner taken. */
export interface PluginOpen {
  /** Another page of the same plugin. */
  page?: string;
  /** A plugin page shown in a drawer instead of navigating. */
  drawer?: string;
  /** A console route (must stay inside the console). */
  route?: string;
  params?: Record<string, string>;
}

export interface PluginActionResult {
  toast?: string | { text: string; kind?: "success" | "error" | "info" };
  refresh?: boolean;
  open?: PluginOpen;
  proposal?: PluginProposal;
}

export interface PluginViewContextValue {
  plugin: string;
  /** Current view parameters (search, page, sort, tab, slot params). */
  params: Record<string, string>;
  /** Merge new parameters and re-fetch the view from the plugin loader. */
  setParams: (patch: Record<string, string | undefined>) => void;
  /** Re-fetch the current view. */
  refresh: () => void;
  /** Post an action to the plugin and apply its result. */
  postAction: (
    action: string,
    params?: Record<string, unknown>,
  ) => Promise<PluginActionResult | null>;
  /** Open a plugin page or console route (drawer opens in place). */
  open: (target: PluginOpen) => void;
}

const inert: PluginViewContextValue = {
  plugin: "",
  params: {},
  setParams: () => {},
  refresh: () => {},
  postAction: async () => null,
  open: () => {},
};

export const PluginViewContext = createContext<PluginViewContextValue>(inert);

export function usePluginView(): PluginViewContextValue {
  return useContext(PluginViewContext);
}

/**
 * Resolve a field reference against a scope, honoring `$root` as the page
 * scope at any depth. Validation already refused prototype paths, but a
 * resolution failure must never take the page down: it reads as missing.
 */
export function resolveIn(scope: unknown, root: unknown, ref: FieldRef): unknown {
  try {
    if (ref.$ === ROOT_SCOPE_KEY) return root;
    if (ref.$.startsWith(`${ROOT_SCOPE_KEY}.`)) {
      return resolvePath(root, ref.$.slice(ROOT_SCOPE_KEY.length + 1));
    }
    return resolvePath(scope, ref.$);
  } catch {
    return undefined;
  }
}

/** Resolve a literal-or-reference value. */
export function valueIn<T>(
  value: Value<T> | undefined,
  scope: unknown,
  root: unknown,
): T | undefined {
  if (value === undefined) return undefined;
  if (isFieldRef(value)) return resolveIn(scope, root, value) as T | undefined;
  return value;
}

/** Resolve to display text: missing reads as empty, never "undefined". */
export function textIn(value: Value | undefined, scope: unknown, root: unknown): string {
  const resolved = valueIn(value, scope, root);
  return resolved === undefined || resolved === null ? "" : String(resolved);
}

/**
 * Resolve widget props against a scope. Field references become their data
 * values anywhere inside the props object (bounded depth); everything else
 * passes through. This is still only field lookup, never computation.
 */
export function resolveProps(
  props: Record<string, unknown> | undefined,
  scope: unknown,
  root: unknown,
): Record<string, unknown> {
  if (!props) return {};
  const walk = (value: unknown, depth: number): unknown => {
    if (isFieldRef(value)) return resolveIn(scope, root, value);
    if (depth >= 4 || value === null || typeof value !== "object") return value;
    if (Array.isArray(value)) return value.map((item) => walk(item, depth + 1));
    const out: Record<string, unknown> = {};
    for (const [key, child] of Object.entries(value)) {
      out[key] = walk(child, depth + 1);
    }
    return out;
  };
  return walk(props, 0) as Record<string, unknown>;
}

/** Rows for tables and repeats: missing reads as empty, non-arrays as empty. */
export function rowsIn(ref: FieldRef, scope: unknown, root: unknown): unknown[] {
  const value = resolveIn(scope, root, ref);
  return Array.isArray(value) ? value : [];
}

/** A finite number for pagination counters; anything else reads as 0. */
export function numberIn(ref: FieldRef, scope: unknown, root: unknown): number {
  const value = resolveIn(scope, root, ref);
  const n = typeof value === "number" ? value : Number(value);
  return Number.isFinite(n) ? n : 0;
}

/** Presence flag (`when`): only an explicit reference gates a block. */
export function visible(when: FieldRef | undefined, scope: unknown, root: unknown): boolean {
  if (!when) return true;
  return Boolean(resolveIn(scope, root, when));
}
