import { Badge, Skeleton, cn } from "@braedonsaunders/appkit-ui";
import { useEffect, useMemo, useState } from "react";
import { Shelf } from "../../components/Poster";
import { PanelHead } from "../../components/workspace";
import { t } from "../../i18n";
import { api, apiFetch } from "../../lib/api";
import type { CollectionSpec, TitleCard } from "../../lib/contract";
import { blankSpec } from "../../lib/rules";

export const ROW_NAME = "4K nobody has watched";

export const KIDS_EXCLUDE: CollectionSpec["exclude"] = [
  { field: "genre", op: "in", value: ["Family", "Animation"] },
  { field: "contentRating", op: "in", value: ["G", "PG", "TV-Y", "TV-Y7", "TV-G"] },
];

/** The collection the prototype plugin builds, before and after "skip kids' titles". */
export function studioSpec(skipKids: boolean): CollectionSpec {
  const spec = blankSpec("mixed");
  spec.name = ROW_NAME;
  spec.description = t("Built in Studio");
  spec.rules = [
    { field: "watched", op: "is", value: false },
    { field: "rating", op: "gte", value: 7 },
  ];
  spec.exclude = skipKids ? KIDS_EXCLUDE : [];
  spec.sort = "rating";
  spec.sortDesc = true;
  spec.limit = 18;
  spec.targets = { ...spec.targets, homeRow: true, collectionsTab: true };
  return spec;
}

export interface Result {
  total: number;
  items: TitleCard[];
}

/** Runs a spec through the collection preview, retrying while the server warms up. */
export function usePreview(spec: CollectionSpec): Result | null {
  const [result, setResult] = useState<Result | null>(null);
  const key = JSON.stringify(spec);
  useEffect(() => {
    const ctl = new AbortController();
    const load = (tries: number) =>
      api
        .previewCollection(spec, ctl.signal)
        .then((r) => setResult({ total: r.total, items: r.items }))
        .catch(() => {
          if (!ctl.signal.aborted && tries > 0) window.setTimeout(() => void load(tries - 1), 300);
        });
    void load(5);
    return () => ctl.abort();
    // The spec's content, not its identity, decides a reload.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);
  return result;
}

/** The Home rows around the new one, for context only. */
export function useContextTitles(): TitleCard[] | null {
  const [titles, setTitles] = useState<TitleCard[] | null>(null);
  useEffect(() => {
    const ctl = new AbortController();
    const load = (tries: number) =>
      apiFetch<{ items: TitleCard[] }>("/catalog/browse?sort=popularity&limit=40", {
        signal: ctl.signal,
      })
        .then((r) => setTitles(r.items))
        .catch(() => {
          if (!ctl.signal.aborted && tries > 0) window.setTimeout(() => void load(tries - 1), 300);
        });
    void load(5);
    return () => ctl.abort();
  }, []);
  return titles;
}

/** Why each title is in the row, shown when the owner hovers a poster. */
export function ruleTrace(items: TitleCard[], skipKids: boolean): Record<string, string> {
  const out: Record<string, string> = {};
  for (const it of items) {
    const parts = [
      t("No viewer has started it"),
      t("Rated {rating}, at least 7", { rating: (it.rating ?? 0).toFixed(1) }),
    ];
    if (skipKids) parts.push(t("Not a kids' title"));
    out[it.key] = parts.join(" · ");
  }
  return out;
}

export function HomePreview({
  ready,
  result,
  titles,
  skipKids,
  reloaded,
}: {
  ready: boolean;
  result: Result | null;
  titles: TitleCard[] | null;
  skipKids: boolean;
  /** Shown briefly after a save rebuilt the preview. */
  reloaded?: boolean;
}) {
  const previewReady = ready && result !== null;
  const reasons = useMemo(
    () => (result ? ruleTrace(result.items, skipKids) : {}),
    [result, skipKids],
  );
  return (
    <div className="flex min-h-full flex-col">
      <PanelHead className="pb-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-sm font-semibold text-fg">{t("Home as Alex sees it")}</h2>
          {previewReady ? (
            <div className="flex flex-wrap gap-1.5">
              {reloaded ? <Badge variant="success">{t("Reloaded")}</Badge> : null}
              <Badge variant="secondary">{t("{count} titles", { count: result.total })}</Badge>
              <Badge variant="secondary">{t("Rebuilt nightly")}</Badge>
            </div>
          ) : null}
        </div>
      </PanelHead>
      <div className="space-y-7 p-4 sm:p-5">
        <HomeRowPreview
          title={t("Continue watching")}
          items={titles?.slice(0, 10) ?? null}
          dim
        />
        <div
          className={cn(
            "-mx-2 rounded-xl p-2 transition",
            previewReady && "bg-primary/5 ring-1 ring-primary/30",
          )}
        >
          {previewReady ? (
            <HomeRowPreview
              title={ROW_NAME}
              items={result.items}
              badge={t("New")}
              reasons={reasons}
            />
          ) : (
            <ShelfSkeleton />
          )}
        </div>
        <HomeRowPreview
          title={t("Trending on your server")}
          items={titles?.slice(30, 42) ?? null}
          dim
        />
      </div>
    </div>
  );
}

export function HomeRowPreview({
  title,
  items,
  dim,
  badge,
  reasons,
}: {
  title: string;
  items: TitleCard[] | null;
  dim?: boolean;
  badge?: string;
  reasons?: Record<string, string>;
}) {
  return (
    <section aria-label={title} className={cn("space-y-2.5", dim && "opacity-45")}>
      <h3 className="flex items-center gap-2 text-sm font-semibold text-fg">
        {title}
        {badge ? <Badge variant="info">{badge}</Badge> : null}
      </h3>
      {items ? (
        <Shelf items={items} reasons={reasons} ariaLabel={title} posterWidth="w-24 sm:w-28" />
      ) : (
        <ShelfSkeleton />
      )}
    </section>
  );
}

export function ShelfSkeleton() {
  return (
    <div className="space-y-2.5">
      <Skeleton className="h-4 w-44" />
      <div className="flex gap-3 overflow-hidden">
        {Array.from({ length: 8 }, (_, i) => (
          <Skeleton key={i} className="aspect-[2/3] w-24 shrink-0 rounded-md sm:w-28" />
        ))}
      </div>
    </div>
  );
}

/** One recipe supplies every Home-demo count, including the installed collection. */
export function recipeSpec(step: "input" | "unstarted" | "rated" | "output", skipKids = false): CollectionSpec {
  const spec = studioSpec(skipKids);
  if (step === "input") spec.rules = [];
  if (step === "unstarted") spec.rules = spec.rules.slice(0, 1);
  if (step !== "output") { spec.limit = 0; spec.exclude = []; }
  return spec;
}
export function useHomeFacts(skipKids = false) {
  const input = usePreview(recipeSpec("input"));
  const unstarted = usePreview(recipeSpec("unstarted"));
  const rated = usePreview(recipeSpec("rated"));
  const output = usePreview(recipeSpec("output", skipKids));
  return { input, unstarted, rated, output };
}
export function homeSummary(facts: ReturnType<typeof useHomeFacts>): string[] {
  const count = (result: Result | null) => result ? String(result.total) : "…";
  return [
    t("{count} titles in the 4K demo catalogue", { count: count(facts.input) }),
    t("{count} titles nobody has started", { count: count(facts.unstarted) }),
    t("{count} titles rated 7 or more", { count: count(facts.rated) }),
    t("{count} titles in the sample row", { count: count(facts.output) }),
  ];
}
