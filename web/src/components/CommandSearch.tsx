import {
  GlobalSearch,
  type GlobalSearchGroup,
  type GlobalSearchResult,
} from "@braedonsaunders/appkit-ui";
import { useCallback, useRef } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../lib/api";
import { t, tn } from "../i18n";
import { textOf, useRegistry, type Descriptor } from "../lib/extend";
import {
  searchTextOf,
  type CommandMeta,
  type PageMeta,
} from "../lib/console-descriptors";
import { destinationsFrom } from "../lib/sitemap";
import { loadViewers, viewerName } from "./pickers";

function matches(q: string, ...fields: (string | undefined)[]): boolean {
  const hay = fields.filter(Boolean).join(" ").toLowerCase();
  return q
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean)
    .every((w) => hay.includes(w));
}

/**
 * ⌘K: jump to any page, section, viewer or collection, or start a common
 * task. Destinations and actions come from the descriptor registry (WS1d):
 * page descriptors (built-in and plugin) fill "Go to", command descriptors
 * with a route fill "Actions". Agent suggestions are not ⌘K actions.
 */
export function CommandSearch() {
  const navigate = useNavigate();
  const { descriptors } = useRegistry("page", "command");
  const live = useRef<Descriptor[]>([]);
  live.current = descriptors;
  const search = useCallback(
    async (q: string, signal: AbortSignal): Promise<GlobalSearchResult> => {
      const all = live.current;
      const pages = all.filter((d) => d.kind === "page") as Descriptor<PageMeta>[];
      const commands = all.filter((d) => d.kind === "command") as Descriptor<CommandMeta>[];
      const groups: GlobalSearchGroup[] = [];
      const places = destinationsFrom(pages)
        .filter((d) => matches(q, d.title, d.page, d.keywords))
        .slice(0, 8)
        .map((d) => ({
          id: d.href,
          type: "page",
          title: d.title,
          subtitle: d.isPage ? undefined : d.page,
          href: d.href,
        }));
      if (places.length) groups.push({ id: "places", label: t("Go to"), hits: places });
      const actions = commands
        .filter(
          (c) =>
            c.meta?.route &&
            !c.meta.agentSuggestion &&
            matches(q, textOf(c.label), searchTextOf(c.meta.keywords)),
        )
        .map((c) => ({
          id: c.id,
          type: "action",
          title: textOf(c.label),
          href: c.meta!.route!,
        }));
      if (actions.length) groups.push({ id: "actions", label: t("Actions"), hits: actions });
      const [viewers, collections] = await Promise.all([
        loadViewers().catch(() => []),
        api.collections().catch(() => []),
      ]);
      if (signal.aborted) return { groups: [], total: 0 };
      const people = viewers
        .filter((v) => matches(q, viewerName(v), v.username))
        .slice(0, 5)
        .map((v) => ({
          id: String(v.id),
          type: "viewer",
          title: viewerName(v),
          subtitle: v.isOwner ? t("Owner") : t("Viewer"),
          href: `/viewers/${v.id}`,
        }));
      if (people.length) groups.push({ id: "viewers", label: t("Viewers"), hits: people });
      const cols = (Array.isArray(collections) ? collections : [])
        .filter((c) => c.id && matches(q, c.name))
        .slice(0, 5)
        .map((c) => ({
          id: c.id!,
          type: "collection",
          title: c.name,
          subtitle: t("Collection"),
          href: `/collections/${encodeURIComponent(c.id!)}`,
        }));
      if (cols.length) groups.push({ id: "collections", label: t("Collections"), hits: cols });
      return { groups, total: groups.reduce((n, g) => n + g.hits.length, 0) };
    },
    [],
  );
  return (
    <GlobalSearch
      className="w-full max-w-md"
      search={search}
      minimumQueryLength={1}
      onNavigate={(hit) => navigate(hit.href)}
      labels={{
        placeholder: t("Search or jump to…"),
        ariaLabel: t("Search Ether"),
        clear: t("Clear search"),
        searching: t("Searching…"),
        noMatches: (query) => t("Nothing matches “{query}”", { query }),
        navigate: t("navigate"),
        open: t("open"),
        close: t("close"),
        resultCount: (count) => tn(count, "{count} result", "{count} results"),
      }}
    />
  );
}
