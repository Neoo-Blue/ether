import { Badge, Button, Dialog, Input, cn, toast } from "@braedonsaunders/appkit-ui";
import {
  ArrowLeft,
  ArrowRight,
  Check,
  Copy,
  Download,
  GitFork,
  MoreHorizontal,
  Search,
  Sparkles,
  Star,
} from "lucide-react";
import { useMemo, useState } from "react";
import { StatusDot } from "../../components/status";
import { t } from "../../i18n";
import { formatNumber } from "../../lib/format";
import { CategoryIcon, PermissionRows, PluginMark, VerifiedBadge } from "./marks";
import { CATEGORIES, categoryLabel, listings, type Category, type Listing } from "./proto";

type View = { kind: "browse" } | { kind: "detail"; id: string } | { kind: "review"; id: string };

/** The plugin library: search, categories, suggestions, and a detail view, in one modal. */
export function Marketplace({
  open,
  onClose,
  onInstall,
  onRemix,
  initial,
}: {
  open: boolean;
  onClose: () => void;
  onInstall: (listing: Listing) => void;
  onRemix: (listing: Listing) => void;
  initial?: string;
}) {
  const [view, setView] = useState<View>(
    initial ? { kind: "detail", id: initial } : { kind: "browse" },
  );
  const all = useMemo(listings, []);
  const current = view.kind === "browse" ? undefined : all.find((l) => l.id === view.id);
  return (
    <Dialog
      open={open}
      onClose={onClose}
      size="xl"
      fullHeight
      title={t("Marketplace")}
      closeLabel={t("Close")}
    >
      {view.kind === "browse" || !current ? (
        <Browse
          all={all}
          onOpen={(id) => setView({ kind: "detail", id })}
          onInstall={(id) => setView({ kind: "review", id })}
        />
      ) : view.kind === "detail" ? (
        <Detail
          listing={current}
          onBack={() => setView({ kind: "browse" })}
          onInstall={() => setView({ kind: "review", id: current.id })}
          onRemix={() => onRemix(current)}
        />
      ) : (
        <Review
          listing={current}
          onBack={() => setView({ kind: "detail", id: current.id })}
          onConfirm={() => onInstall(current)}
        />
      )}
    </Dialog>
  );
}

function Browse({
  all,
  onOpen,
  onInstall,
}: {
  all: Listing[];
  onOpen: (id: string) => void;
  onInstall: (id: string) => void;
}) {
  const [query, setQuery] = useState("");
  const [category, setCategory] = useState<Category | "all">("all");
  const needle = query.trim().toLowerCase();
  const shown = all.filter(
    (l) =>
      (category === "all" || l.category === category) &&
      (!needle || `${l.name} ${l.summary}`.toLowerCase().includes(needle)),
  );
  const forYou = all.filter((l) => l.because && !l.installed);
  const filtering = needle !== "" || category !== "all";
  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col">
      <div className="min-w-0 space-y-3 border-b border-border px-5 py-3">
        <div className="flex items-center gap-2">
          <div className="relative flex-1">
            <Search
              size={15}
              aria-hidden
              className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-fg-subtle"
            />
            <Input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t("Search plugins and Stremio add-ons")}
              aria-label={t("Search plugins")}
              className="pl-9"
            />
          </div>
          <Button variant="ghost" size="icon" aria-label={t("Catalogue sources")}>
            <MoreHorizontal size={16} />
          </Button>
        </div>
        <div
          role="group"
          aria-label={t("Categories")}
          className="flex gap-1.5 overflow-x-auto pb-0.5"
        >
          <Chip active={category === "all"} onClick={() => setCategory("all")}>
            {t("All")}
          </Chip>
          {CATEGORIES.map((c) => (
            <Chip key={c} active={category === c} onClick={() => setCategory(c)}>
              <CategoryIcon category={c} size={13} /> {categoryLabel(c)}
            </Chip>
          ))}
        </div>
      </div>
      <div className="app-scroll min-h-0 min-w-0 flex-1 space-y-7 overflow-y-auto overflow-x-hidden px-5 py-5">
        {!filtering && forYou.length ? (
          <section aria-label={t("For your server")} className="space-y-3">
            <h3 className="flex items-center gap-2 text-sm font-semibold text-fg">
              <Sparkles size={15} className="text-primary" aria-hidden /> {t("For your server")}
            </h3>
            <div className="grid gap-3 md:grid-cols-2">
              {forYou.map((l) => (
                <button
                  key={l.id}
                  type="button"
                  onClick={() => onOpen(l.id)}
                  className="group flex min-w-0 items-center gap-4 rounded-xl border border-border bg-surface p-4 text-left transition hover:border-border-strong hover:shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
                >
                  <PluginMark category={l.category} />
                  <div className="min-w-0 flex-1">
                    <p className="font-semibold text-fg">{l.name}</p>
                    <p className="mt-0.5 line-clamp-2 text-sm text-fg-muted">{l.because}</p>
                  </div>
                  <ArrowRight
                    size={16}
                    className="shrink-0 text-fg-subtle transition group-hover:translate-x-0.5 group-hover:text-fg"
                    aria-hidden
                  />
                </button>
              ))}
            </div>
          </section>
        ) : null}
        <section aria-label={t("Plugins")} className="space-y-3">
          <h3 className="text-sm font-semibold text-fg">
            {filtering ? t("{count} plugins", { count: shown.length }) : t("Popular")}
          </h3>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {shown
              .slice()
              .sort((a, b) => b.installs - a.installs)
              .map((l) => (
                <ListingCard
                  key={l.id}
                  listing={l}
                  onOpen={() => onOpen(l.id)}
                  onInstall={() => onInstall(l.id)}
                />
              ))}
          </div>
        </section>
      </div>
    </div>
  );
}

function Chip({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        "inline-flex h-8 shrink-0 items-center gap-1.5 rounded-md border px-2.5 text-sm transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50",
        active
          ? "border-primary/40 bg-primary/10 font-medium text-primary"
          : "border-border bg-surface text-fg-muted hover:border-border-strong hover:text-fg",
      )}
    >
      {children}
    </button>
  );
}

function Counts({ listing }: { listing: Listing }) {
  return (
    <span className="flex items-center gap-3 text-xs tabular-nums text-fg-muted">
      <span className="inline-flex items-center gap-1" title={t("Installs")}>
        <Download size={12} aria-hidden /> {formatNumber(listing.installs)}
      </span>
      <span className="inline-flex items-center gap-1" title={t("Stars")}>
        <Star size={12} aria-hidden /> {formatNumber(listing.stars)}
      </span>
    </span>
  );
}

function InstallButton({ listing, onInstall }: { listing: Listing; onInstall: () => void }) {
  if (listing.installed === "current")
    return (
      <Button size="sm" variant="ghost" disabled>
        <Check size={14} /> {t("Installed")}
      </Button>
    );
  return (
    <Button
      size="sm"
      variant={listing.installed === "update" ? "default" : "outline"}
      onClick={(e) => {
        e.stopPropagation();
        onInstall();
      }}
    >
      {listing.installed === "update" ? t("Update") : t("Install")}
    </Button>
  );
}

function ListingCard({
  listing,
  onOpen,
  onInstall,
}: {
  listing: Listing;
  onOpen: () => void;
  onInstall: () => void;
}) {
  return (
    <article
      aria-label={listing.name}
      className="flex h-full flex-col rounded-xl border border-border bg-surface p-5 transition hover:border-border-strong hover:shadow-sm"
    >
      <button
        type="button"
        onClick={onOpen}
        className="flex flex-1 flex-col text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
      >
        <div className="flex items-start gap-3">
          <PluginMark category={listing.category} />
          <div className="min-w-0 flex-1 space-y-1">
            <h4 className="truncate font-semibold text-fg">{listing.name}</h4>
            <div className="flex flex-wrap gap-1">
              <VerifiedBadge verified={listing.verified} source={listing.source} />
              {listing.stremio ? <Badge variant="secondary">{t("Stremio add-on")}</Badge> : null}
            </div>
          </div>
        </div>
        <p className="mt-3 flex-1 text-sm text-fg-muted">{listing.summary}</p>
      </button>
      <div className="mt-4 flex items-center justify-between gap-2">
        <Counts listing={listing} />
        <InstallButton listing={listing} onInstall={onInstall} />
      </div>
    </article>
  );
}

function Detail({
  listing,
  onBack,
  onInstall,
  onRemix,
}: {
  listing: Listing;
  onBack: () => void;
  onInstall: () => void;
  onRemix: () => void;
}) {
  return (
    <div className="app-scroll min-h-0 flex-1 overflow-y-auto">
      <div className="border-b border-border px-5 py-3">
        <Button variant="ghost" size="sm" onClick={onBack}>
          <ArrowLeft size={14} /> {t("All plugins")}
        </Button>
      </div>
      <div className="space-y-6 px-5 py-5">
        <header className="flex flex-wrap items-start gap-4">
          <PluginMark category={listing.category} size="lg" />
          <div className="min-w-0 flex-1 space-y-1.5">
            <div className="flex flex-wrap items-center gap-2">
              <h3 className="text-xl font-semibold tracking-tight text-fg">{listing.name}</h3>
              <VerifiedBadge verified={listing.verified} source={listing.source} />
              {listing.stremio ? <Badge variant="secondary">{t("Stremio add-on")}</Badge> : null}
            </div>
            <p className="text-sm text-fg-muted">{listing.summary}</p>
            <div className="flex flex-wrap items-center gap-3 text-xs text-fg-muted">
              <span>{t("By {author}", { author: listing.author })}</span>
              <Counts listing={listing} />
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <Button variant="outline" onClick={onRemix}>
              <GitFork size={15} /> {t("Make it mine")}
            </Button>
            {listing.installed === "current" ? (
              <Button variant="ghost" disabled>
                <Check size={15} /> {t("Installed")}
              </Button>
            ) : (
              <Button onClick={onInstall}>
                {listing.installed === "update" ? t("Update") : t("Install")}
              </Button>
            )}
          </div>
        </header>

        <Screenshots listing={listing} />

        <div className="grid gap-5 lg:grid-cols-3">
          <div className="space-y-5 lg:col-span-2">
            <Block title={t("What it adds")}>
              <ul className="divide-y divide-border">
                {listing.adds.map((a) => (
                  <li
                    key={a.label}
                    className="flex items-center justify-between gap-3 py-2.5 first:pt-0 last:pb-0"
                  >
                    <span className="text-sm text-fg">{a.label}</span>
                    <span className="shrink-0 rounded-md bg-bg-subtle px-2 py-0.5 text-xs text-fg-muted">
                      {a.where}
                    </span>
                  </li>
                ))}
              </ul>
            </Block>
            <Block title={t("It can")}>
              <PermissionRows items={listing.permissions} />
            </Block>
            <Block title={t("Limits")}>
              <ul className="space-y-2">
                {listing.limits.map((l) => (
                  <li key={l} className="flex items-center gap-2 text-sm text-fg">
                    <Check size={14} className="shrink-0 text-success" aria-hidden /> {l}
                  </li>
                ))}
              </ul>
            </Block>
          </div>
          <div className="space-y-5">
            <Block title={t("Version {version}", { version: listing.version })}>
              <ol className="space-y-2.5">
                {listing.changelog.map((c) => (
                  <li key={c.version} className="text-sm">
                    <span className="font-medium tabular-nums text-fg">{c.version}</span>
                    <span className="text-fg-muted"> · {c.note}</span>
                  </li>
                ))}
              </ol>
              <p className="mt-3 text-xs text-fg-subtle">
                {t("Updated {when}", { when: listing.updated })}
              </p>
            </Block>
            <Block title={t("Source")}>
              <SourceChip address={listing.repo} />
            </Block>
          </div>
        </div>
      </div>
    </div>
  );
}

function Review({
  listing,
  onBack,
  onConfirm,
}: {
  listing: Listing;
  onBack: () => void;
  onConfirm: () => void;
}) {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="app-scroll min-h-0 flex-1 overflow-y-auto px-5 py-6">
        <div className="mx-auto max-w-lg space-y-6">
          <div className="flex items-center gap-4">
            <PluginMark category={listing.category} size="lg" />
            <div className="min-w-0">
              <h3 className="text-lg font-semibold text-fg">
                {t("Install {name}", { name: listing.name })}
              </h3>
              <VerifiedBadge verified={listing.verified} source={listing.source} />
            </div>
          </div>
          <Block title={t("It can")}>
            <PermissionRows items={listing.permissions} />
          </Block>
          <Block title={t("It appears in")}>
            <ul className="space-y-1.5">
              {listing.adds.map((a) => (
                <li key={a.label} className="text-sm text-fg">
                  {a.where}
                </li>
              ))}
            </ul>
          </Block>
        </div>
      </div>
      <div className="flex items-center justify-end gap-2 border-t border-border bg-bg-subtle px-5 py-3">
        <Button variant="ghost" onClick={onBack}>
          {t("Back")}
        </Button>
        <Button onClick={onConfirm}>{t("Install")}</Button>
      </div>
    </div>
  );
}

function Block({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section aria-label={title} className="rounded-xl border border-border bg-surface p-4">
      <h4 className="mb-3 text-xs font-semibold uppercase tracking-wide text-fg-subtle">{title}</h4>
      {children}
    </section>
  );
}

/** A screenshot of the plugin's result, as the kit generates it, in the current theme. */
function Screenshots({ listing }: { listing: Listing }) {
  return (
    <figure
      aria-label={t("Screenshot")}
      className="overflow-hidden rounded-xl border border-border bg-bg p-4 text-fg shadow-sm"
    >
      <ScreenshotBody listing={listing} />
    </figure>
  );
}

function ScreenshotBody({ listing }: { listing: Listing }) {
  if (listing.category === "channels") {
    const rows = [
      { n: "101", name: t("News Now"), ok: true },
      { n: "102", name: t("Classic Movies"), ok: false },
      { n: "103", name: t("Comedy Central Live"), ok: true },
      { n: "104", name: t("Nature HD"), ok: false },
    ];
    return (
      <div className="space-y-2">
        <p className="text-xs font-semibold text-fg">{t("Live TV")}</p>
        {rows.map((r) => (
          <div
            key={r.n}
            className="flex items-center gap-3 rounded-lg border border-border bg-surface px-3 py-2 text-sm"
          >
            <span className="w-8 tabular-nums text-fg-muted">{r.n}</span>
            <span className="flex-1 truncate">{r.name}</span>
            <StatusDot level={r.ok ? "ok" : "error"} />
            <span className="w-16 text-right text-xs text-fg-muted">
              {r.ok ? t("Live") : t("Hidden")}
            </span>
          </div>
        ))}
      </div>
    );
  }
  return (
    <div className="space-y-2">
      <p className="text-xs font-semibold text-fg">{listing.adds[0]?.where}</p>
      <div className="grid grid-cols-5 gap-2">
        {Array.from({ length: 5 }, (_, i) => (
          <div key={i} className="aspect-[2/3] rounded-md bg-bg-subtle ring-1 ring-border-subtle" />
        ))}
      </div>
      <p className="text-xs text-fg-muted">{listing.adds[0]?.label}</p>
    </div>
  );
}

/** The listing's repository on one line, with copy. */
function SourceChip({ address }: { address: string }) {
  return (
    <div className="flex min-w-0 items-center gap-1 rounded-lg border border-border bg-bg-subtle py-1 pl-3 pr-1">
      <a
        href={`https://${address}`}
        target="_blank"
        rel="noreferrer"
        title={address}
        className="min-w-0 flex-1 truncate font-mono text-sm text-primary"
      >
        {address}
      </a>
      <Button
        variant="ghost"
        size="sm"
        aria-label={t("Copy address")}
        onClick={() => {
          void navigator.clipboard
            .writeText(address)
            .then(() => toast.success(t("Address copied")))
            .catch(() => toast.error(t("Couldn't copy address")));
        }}
      >
        <Copy size={15} />
      </Button>
    </div>
  );
}
