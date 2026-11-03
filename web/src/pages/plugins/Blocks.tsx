import { Badge, Button, Skeleton, cn } from "@braedonsaunders/appkit-ui";
import { useLocale } from "../../i18n/react";
import {
  ArrowDown,
  ArrowDownWideNarrow,
  Bell,
  Braces,
  CalendarClock,
  Code2,
  Filter,
  Globe,
  GripVertical,
  LayoutTemplate,
  Layers,
  Library,
  ListOrdered,
  PanelsTopLeft,
  Plus,
  Scissors,
  Shuffle,
  Tv,
  Webhook,
  Zap,
  type LucideIcon,
} from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";
import { Artwork } from "../../components/Artwork";
import { PosterCard } from "../../components/Poster";
import { t } from "../../i18n";
import type { CollectionSpec, Rule, TitleCard } from "../../lib/contract";
import { RuleRow } from "../collections/RuleRow";
import {
  KIDS_EXCLUDE,
  ROW_NAME,
  recipeSpec,
  ruleTrace,
  studioSpec,
  usePreview,
  type Result,
} from "./preview";

type Kind = "input" | "step" | "output";

interface Block {
  id: string;
  kind: Kind;
  icon: LucideIcon;
  label: string;
  summary: string;
  code: string;
  /** The spec as it stands after this block, so every block shows its own live data. */
  spec?: CollectionSpec;
  rules?: Rule[];
}

function stage(rules: Rule[], exclude: Rule[], limit = 0): CollectionSpec {
  const s = studioSpec(false);
  s.rules = rules;
  s.exclude = exclude;
  s.limit = limit;
  return s;
}

function blocksFor(skipKids: boolean): Block[] {
  const notStarted: Rule = { field: "watched", op: "is", value: false };
  const rated: Rule = { field: "rating", op: "gte", value: 7 };
  const kids = skipKids ? KIDS_EXCLUDE : [];
  const list: Block[] = [
    {
      id: "input",
      kind: "input",
      icon: Library,
      label: t("4K demo catalogue"),
      summary: t("Sample catalogue titles"),
      code: `const titles = await ether.catalog.search({ resolution: "2160p" });`,
      spec: recipeSpec("input"),
    },
    {
      id: "not-started",
      kind: "step",
      icon: Filter,
      label: t("Nobody has started it"),
      summary: t("Filter"),
      code: `.filter((title) => !started.has(title.id))`,
      spec: recipeSpec("unstarted"),
      rules: [notStarted],
    },
    {
      id: "rated",
      kind: "step",
      icon: Filter,
      label: t("Rated 7 or more"),
      summary: t("Filter"),
      code: `.filter((title) => (title.rating ?? 0) >= 7)`,
      spec: recipeSpec("rated"),
      rules: [rated],
    },
  ];
  if (skipKids)
    list.push({
      id: "kids",
      kind: "step",
      icon: Filter,
      label: t("Skip kids' titles"),
      summary: t("Filter"),
      code: `.filter((title) => !title.isForKids())`,
      spec: stage([notStarted, rated], kids),
      rules: kids,
    });
  list.push(
    {
      id: "sort",
      kind: "step",
      icon: ArrowDownWideNarrow,
      label: t("Highest rated first"),
      summary: t("Sort"),
      code: `.sortBy("rating")`,
    },
    {
      id: "limit",
      kind: "step",
      icon: Scissors,
      label: t("Keep the top 18"),
      summary: t("Limit"),
      code: `limit: 18,`,
    },
    {
      id: "output",
      kind: "output",
      icon: LayoutTemplate,
      label: ROW_NAME,
      summary: t("Home row for everyone, rebuilt nightly"),
      code: `collections: {\n  "unwatched-4k": { title: t("4K nobody has watched"), refresh: "nightly" },\n}`,
      spec: recipeSpec("output", skipKids),
    },
  );
  return list;
}

const PALETTE: { group: string; items: { icon: LucideIcon; label: string }[] }[] = [
  {
    group: "inputs",
    items: [
      { icon: Library, label: "Catalogue titles" },
      { icon: Tv, label: "Live TV channels" },
      { icon: Zap, label: "An event" },
      { icon: CalendarClock, label: "A schedule" },
      { icon: Webhook, label: "A webhook" },
      { icon: Globe, label: "A web page or API" },
    ],
  },
  {
    group: "steps",
    items: [
      { icon: Filter, label: "Filter" },
      { icon: ArrowDownWideNarrow, label: "Sort" },
      { icon: Scissors, label: "Limit" },
      { icon: Layers, label: "Match to titles" },
      { icon: Shuffle, label: "Map fields" },
      { icon: Braces, label: "Code" },
    ],
  },
  {
    group: "outputs",
    items: [
      { icon: LayoutTemplate, label: "Home row" },
      { icon: Layers, label: "Collection" },
      { icon: Tv, label: "Channels" },
      { icon: Bell, label: "Notification" },
      { icon: ListOrdered, label: "Ranking step" },
      { icon: PanelsTopLeft, label: "Page" },
    ],
  },
];

function groupLabel(g: string): string {
  return g === "inputs" ? t("Inputs") : g === "steps" ? t("Steps") : t("Outputs");
}

/**
 * The visual builder: the plugin as a flow of blocks, each showing the data
 * as it stands at that point, in the same pattern as Catalog › Rules.
 */
export function BlocksView({
  skipKids,
  onSkipKids,
}: {
  skipKids: boolean;
  onSkipKids: (v: boolean) => void;
}) {
  const { locale } = useLocale();
  const blocks = useMemo(() => blocksFor(skipKids), [skipKids, locale]);
  const [selected, setSelected] = useState("rated");
  const [codeOpen, setCodeOpen] = useState<string | null>(null);
  const [day, setDay] = useState(6);
  const current = blocks.find((b) => b.id === selected) ?? blocks[0]!;
  return (
    <div className="app-scroll min-h-0 flex-1 overflow-y-auto lg:grid lg:grid-cols-[14rem_minmax(0,1fr)_20rem] lg:overflow-hidden">
      <aside
        aria-label={t("Blocks")}
        className="app-scroll hidden border-r border-border bg-bg-subtle p-3 lg:block lg:min-h-0 lg:overflow-y-auto"
      >
        {PALETTE.map((g) => (
          <div key={g.group} className="mb-4">
            <p className="mb-1.5 px-1 text-[11px] font-semibold uppercase tracking-wide text-fg-subtle">
              {groupLabel(g.group)}
            </p>
            <ul className="space-y-1">
              {g.items.map((it) => (
                <li key={it.label}>
                  <button
                    type="button"
                    draggable
                    className="group flex w-full items-center gap-2 rounded-lg border border-transparent px-2 py-1.5 text-left text-sm text-fg-muted transition hover:border-border hover:bg-surface hover:text-fg"
                  >
                    <it.icon size={15} aria-hidden className="shrink-0" />
                    <span className="flex-1 truncate">{t(it.label)}</span>
                    <GripVertical
                      size={13}
                      aria-hidden
                      className="shrink-0 opacity-0 transition group-hover:opacity-60"
                    />
                  </button>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </aside>

      <main
        aria-label={t("Flow")}
        className="app-scroll min-w-0 bg-bg lg:min-h-0 lg:overflow-y-auto"
      >
        <div className="sticky top-0 z-10 flex flex-wrap items-center gap-3 border-b border-border bg-bg/95 px-5 py-2.5 backdrop-blur">
          <span className="text-sm text-fg-muted">{t("Run as of")}</span>
          <input
            type="range"
            min={0}
            max={6}
            value={day}
            onChange={(e) => setDay(Number(e.target.value))}
            aria-label={t("Run as of")}
            className="w-40 accent-[rgb(var(--ch-primary))]"
          />
          <span className="text-sm font-medium text-fg">{dayLabel(day)}</span>
          <span className="ml-auto text-xs text-fg-subtle">
            {t("Hover a poster to see why it's in")}
          </span>
        </div>
        <ol className="mx-auto max-w-3xl space-y-0 px-5 py-5">
          {blocks.map((b, i) => (
            <li key={b.id}>
              {i > 0 ? <Connector /> : null}
              <BlockCard
                block={b}
                selected={selected === b.id}
                onSelect={() => setSelected(b.id)}
                codeOpen={codeOpen === b.id}
                onCode={() => setCodeOpen((c) => (c === b.id ? null : b.id))}
                skipKids={skipKids}
              />
            </li>
          ))}
        </ol>
      </main>

      <aside
        aria-label={t("Block settings")}
        className="app-scroll border-t border-border bg-bg-subtle p-4 lg:min-h-0 lg:overflow-y-auto lg:border-l lg:border-t-0"
      >
        <Inspector block={current} skipKids={skipKids} onSkipKids={onSkipKids} />
      </aside>
    </div>
  );
}

function dayLabel(day: number): string {
  if (day === 6) return t("Now");
  if (day === 5) return t("Yesterday");
  const d = new Date(Date.now() - (6 - day) * 86_400_000);
  return d.toLocaleDateString(undefined, { weekday: "long" });
}

function Connector() {
  return (
    <div className="group relative flex h-9 items-center justify-center" aria-hidden>
      <span className="absolute inset-y-0 left-1/2 w-px -translate-x-1/2 bg-border" />
      <span className="relative flex size-6 items-center justify-center rounded-full border border-border bg-surface text-fg-subtle transition group-hover:border-primary group-hover:text-primary">
        <ArrowDown size={13} className="group-hover:hidden" />
        <Plus size={13} className="hidden group-hover:block" />
      </span>
    </div>
  );
}

const KIND_TILE: Record<Kind, string> = {
  input: "bg-info/10 text-info",
  step: "bg-bg-subtle text-fg-muted",
  output: "bg-primary/10 text-primary",
};

function BlockCard({
  block,
  selected,
  onSelect,
  codeOpen,
  onCode,
  skipKids,
}: {
  block: Block;
  selected: boolean;
  onSelect: () => void;
  codeOpen: boolean;
  onCode: () => void;
  skipKids: boolean;
}) {
  const live = usePreview(block.spec ?? studioSpec(skipKids));
  const showData = Boolean(block.spec);
  return (
    <article
      aria-label={block.label}
      className={cn(
        "rounded-xl border bg-surface transition",
        selected
          ? "border-primary/60 shadow-sm ring-1 ring-primary/25"
          : "border-border hover:border-border-strong",
      )}
    >
      <button
        type="button"
        onClick={onSelect}
        className="flex w-full items-center gap-3 px-4 py-3 text-left focus-visible:outline-none"
      >
        <span
          className={cn(
            "flex size-9 shrink-0 items-center justify-center rounded-lg",
            KIND_TILE[block.kind],
          )}
        >
          <block.icon size={18} aria-hidden />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate text-sm font-semibold text-fg">{block.label}</span>
          <span className="block truncate text-xs text-fg-muted">{block.summary}</span>
        </span>
        {showData ? (
          live ? (
            <span className="shrink-0 text-right">
              <span className="block text-lg font-semibold tabular-nums leading-tight text-fg">
                {live.total}
              </span>
              <span className="block text-[11px] text-fg-subtle">{t("titles")}</span>
            </span>
          ) : (
            <Skeleton className="h-8 w-10" />
          )
        ) : null}
      </button>
      {showData ? (
        <div className="px-4 pb-3">
          {block.kind === "output" ? (
            <OutputStrip result={live} skipKids={skipKids} />
          ) : (
            <MiniStrip result={live} />
          )}
        </div>
      ) : null}
      <div className="flex items-center justify-end border-t border-border-subtle px-2 py-1">
        <Button variant="ghost" size="sm" onClick={onCode} className="h-7 text-xs">
          <Code2 size={13} /> {codeOpen ? t("Hide code") : t("View code")}
        </Button>
      </div>
      {codeOpen ? (
        <pre className="overflow-x-auto border-t border-border-subtle bg-bg-subtle px-4 py-3 font-mono text-xs leading-relaxed text-fg">
          {block.code}
        </pre>
      ) : null}
    </article>
  );
}

function MiniStrip({ result }: { result: Result | null }) {
  if (!result)
    return (
      <div className="flex gap-1.5">
        {Array.from({ length: 12 }, (_, i) => (
          <Skeleton key={i} className="aspect-[2/3] w-8 rounded" />
        ))}
      </div>
    );
  return (
    <div className="flex gap-1.5 overflow-hidden" aria-hidden>
      {result.items.slice(0, 14).map((it) => (
        <Artwork
          key={it.key}
          src={it.poster}
          alt=""
          title={it.title}
          className="aspect-[2/3] w-8 shrink-0 overflow-hidden rounded ring-1 ring-border-subtle"
        />
      ))}
    </div>
  );
}

function OutputStrip({ result, skipKids }: { result: Result | null; skipKids: boolean }) {
  const { locale } = useLocale();
  const reasons = useMemo(
    () => (result ? ruleTrace(result.items, skipKids) : {}),
    [result, skipKids, locale],
  );
  if (!result) return <MiniStrip result={null} />;
  return (
    <div className="grid grid-cols-6 gap-2">
      {result.items.slice(0, 12).map((it: TitleCard) => (
        <PosterCard key={it.key} item={it} reason={reasons[it.key]} showMeta={false} />
      ))}
    </div>
  );
}

function Inspector({
  block,
  skipKids,
  onSkipKids,
}: {
  block: Block;
  skipKids: boolean;
  onSkipKids: (v: boolean) => void;
}) {
  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <block.icon size={16} className="text-fg-muted" aria-hidden />
        <h2 className="text-sm font-semibold text-fg">{block.label}</h2>
        <Badge variant="secondary" className="ml-auto">
          {block.summary}
        </Badge>
      </div>
      {block.rules ? (
        <div className="space-y-2">
          {block.rules.map((r, i) => (
            <RuleRow
              key={`${r.field}-${i}`}
              rule={r}
              index={i}
              kind={block.id === "kids" ? "exclude" : "include"}
              mediaType="mixed"
              region="US"
              onChange={() => undefined}
              onRemove={() => (block.id === "kids" ? onSkipKids(false) : undefined)}
            />
          ))}
        </div>
      ) : (
        <InspectorNote>{inspectorLine(block)}</InspectorNote>
      )}
      {!skipKids && block.id === "rated" ? (
        <Button variant="outline" size="sm" onClick={() => onSkipKids(true)}>
          <Plus size={14} /> {t("Add a filter after this")}
        </Button>
      ) : null}
    </div>
  );
}

function inspectorLine(block: Block): string {
  switch (block.id) {
    case "input":
      return t(
        "Every title with a 4K release. Change the input to use channels, events or a web page.",
      );
    case "sort":
      return t("Highest rated first. Ties keep their catalogue order.");
    case "limit":
      return t("Keeps the first 18 after sorting.");
    default:
      return t("Shown on Home for every viewer, rebuilt each night.");
  }
}

function InspectorNote({ children }: { children: ReactNode }) {
  return <p className="text-sm text-fg-muted">{children}</p>;
}
