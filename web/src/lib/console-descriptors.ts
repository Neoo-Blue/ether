/**
 * Meta types for the console descriptor kinds (mirror of
 * internal/extend/meta_console.go) and the small pure helpers the shell,
 * command search and Ask Ether share.
 */
import { BUILTIN, textOf, type Descriptor, type ExtendText } from "./extend";
import { searchTerms } from "../i18n";

export interface PageSectionMeta {
  key: string;
  label: ExtendText;
  keywords?: ExtendText;
}

export interface PageMeta {
  route?: string;
  navGroup?: string;
  inNav?: boolean;
  mobile?: boolean;
  exact?: boolean;
  keywords?: ExtendText;
  sections?: PageSectionMeta[];
}

export interface NavItemMeta {
  route: string;
  page?: string;
  mobile?: boolean;
  exact?: boolean;
}

export interface CommandMeta {
  route?: string;
  action?: string;
  keywords?: ExtendText;
  agentSuggestion?: boolean;
  routePatterns?: string[];
}

export interface IntegrationSwitchMeta {
  key: string;
  label: ExtendText;
  hint?: ExtendText;
}

export interface IntegrationMeta {
  category: string;
  logo?: string;
  adds?: ExtendText[];
  connect?: string;
  tab?: string;
  configKey?: string;
  endpoint?: string;
  test?: string;
  flag?: string;
  requiredField?: string;
  readyField?: string;
  publicFallback?: boolean;
  form?: string;
  switches?: IntegrationSwitchMeta[];
}

/**
 * Search aliases for a descriptor text: the active language's terms plus the
 * English ones, as command search has always matched. Plugin keywords arrive
 * resolved for the active locale.
 */
export function searchTextOf(text?: ExtendText): string | undefined {
  if (!text) return undefined;
  if (text.source) return searchTerms(text.source);
  const resolved = textOf(text);
  return resolved || undefined;
}

/** The console route a page descriptor opens. */
export function pageRoute(d: Descriptor<PageMeta>): string {
  if (d.meta?.route) return d.meta.route;
  if (d.owner !== BUILTIN) return pluginPageRoute(d.owner, pluginLocalId(d));
  return "/";
}

/** A plugin page's local id: the part after "plugin:<owner>/". */
export function pluginLocalId(d: Descriptor): string {
  const prefix = `plugin:${d.owner}/`;
  return d.id.startsWith(prefix) ? d.id.slice(prefix.length) : d.id;
}

export function pluginPageRoute(plugin: string, page: string): string {
  return `/p/${encodeURIComponent(plugin)}/${page.split("/").map(encodeURIComponent).join("/")}`;
}

/** Whether a route pattern (exact, or a prefix ending in "/*") matches. */
export function routeMatches(pattern: string, route: string): boolean {
  if (pattern.endsWith("/*")) return route.startsWith(pattern.slice(0, -1));
  return pattern === route;
}

/** Ask Ether suggestions for a console route, in registry order. */
export function agentSuggestionsFor(
  commands: Descriptor<CommandMeta>[],
  route: string,
): Descriptor<CommandMeta>[] {
  return commands.filter(
    (c) =>
      c.meta?.agentSuggestion &&
      (c.meta.routePatterns ?? []).some((p) => routeMatches(p, route)),
  );
}
