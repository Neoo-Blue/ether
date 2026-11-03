import { Badge, Button, EmptyState, Switch, buttonVariants } from "@braedonsaunders/appkit-ui";
import { ArrowUpCircle, Eye, FolderSearch, Link2, Puzzle, Sparkles } from "lucide-react";
import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import { Link, useNavigate } from "react-router-dom";
import { Artwork } from "../../components/Artwork";
import { ErrorPanel } from "../../components/feedback";
import { DomainPage, useRouteTab, type PageTab } from "../../components/layout";
import { LevelBadge } from "../../components/status";
import { t } from "../../i18n";
import { useLocale } from "../../i18n/react";
import { apiFetch } from "../../lib/api";
import { invalidateRegistry } from "../../lib/extend";
import { useStreamEvent } from "../../lib/stream";
import { CatalogueTab } from "./CatalogueTab";
import { DirectoryBrowser } from "./DirectoryBrowser";
import { InstallDrawer } from "./InstallDrawer";
import { InstalledPluginDrawer } from "./InstalledPluginDrawer";
import { kindIcon, kindLabel, pluginHealthLevel, type PluginSummary } from "./types";

type TabKey = "installed" | "marketplace";

/** Installed plugins, the marketplace and imports, on the real plugin API. */
export default function LivePlugins() {
  const navigate = useNavigate();
  const { locale } = useLocale();
  const [plugins, setPlugins] = useState<PluginSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<unknown>(null);
  const [toggling, setToggling] = useState<string | null>(null);
  const [open, setOpen] = useState<string | null>(null);
  const [installUrl, setInstallUrl] = useState<string | null>(null);
  const [browsing, setBrowsing] = useState(false);
  const [logRevision, setLogRevision] = useState(0);
  const [liveLogs, setLiveLogs] = useState<Record<string, string[]>>({});

  const load = useCallback(async (quiet = false) => {
    if (!quiet) setLoading(true);
    try {
      const res = await apiFetch<{ plugins?: PluginSummary[] }>("/plugins");
      setPlugins(Array.isArray(res?.plugins) ? res.plugins : []);
      setLoadError(null);
    } catch (e) {
      setLoadError(e);
    } finally {
      if (!quiet) setLoading(false);
    }
  }, []);
  useEffect(() => {
    void load();
  }, [load]);
  useStreamEvent("plugins.changed", () => {
    void load(true);
    invalidateRegistry();
    setLogRevision((n) => n + 1);
  });
  useStreamEvent<{ plugin?: string; id?: string; line?: string }>("plugins.log", (ev) => {
    const id = ev.data?.plugin ?? ev.data?.id;
    const line = ev.data?.line;
    if (!id || !line) return;
    setLiveLogs((prev) => ({ ...prev, [id]: [...(prev[id] ?? []), line].slice(-200) }));
    setLogRevision((n) => n + 1);
  });

  const tabs: readonly (PageTab & { key: TabKey })[] = useMemo(
    () => [
      {
        key: "installed",
        label: t("Installed"),
        count: plugins.length > 0 ? plugins.length : undefined,
      },
      { key: "marketplace", label: t("Marketplace") },
    ],
    [plugins.length, locale],
  );
  const active = useRouteTab(tabs, "installed");
  const installedIds = useMemo(() => new Set(plugins.map((p) => p.id)), [plugins]);

  const toggle = async (p: PluginSummary, on: boolean) => {
    setToggling(p.id);
    setPlugins((list) => list.map((x) => (x.id === p.id ? { ...x, enabled: on } : x)));
    try {
      await apiFetch(`/plugins/${encodeURIComponent(p.id)}/${on ? "enable" : "disable"}`, {
        method: "POST",
        body: JSON.stringify({}),
      });
      invalidateRegistry();
    } catch (e) {
      setLoadError(e);
    } finally {
      await load(true);
      setToggling(null);
    }
  };
  const installed = async (id: string) => {
    setInstallUrl(null);
    await load(true);
    invalidateRegistry();
    if (active !== "installed") navigate("/plugins/installed");
    setOpen(id);
  };

  const on = plugins.filter((p) => p.enabled).length;
  const trying = plugins.filter((p) => p.enabled && p.shadow).length;
  const attention = plugins.filter((p) => {
    const level = pluginHealthLevel(p.health, p.enabled);
    return level === "warn" || level === "error";
  }).length;

  return (
    <DomainPage
      title={t("Plugins")}
      base="/plugins"
      tabs={tabs}
      active={active}
      actions={
        <>
          <Button variant="outline" onClick={() => setBrowsing(true)}>
            <FolderSearch size={15} /> {t("Browse directories")}
          </Button>
          <Button variant="outline" onClick={() => setInstallUrl("")}>
            <Link2 size={15} /> {t("Install from a link")}
          </Button>
          <Link to="/plugins/studio" className={buttonVariants()}>
            <Sparkles size={15} /> {t("Build a plugin")}
          </Link>
        </>
      }
    >
      {active === "installed" ? (
        <>
          <div className="grid grid-cols-3 gap-2 sm:gap-3">
            <Tile label={t("Plugins on")} value={String(on)} />
            <Tile label={t("Trying out")} value={String(trying)} />
            <Tile
              label={t("Health")}
              value={
                attention ? (
                  <LevelBadge
                    level="warn"
                    label={t("{count} need attention", { count: attention })}
                  />
                ) : (
                  <LevelBadge level="ok" label={t("All working")} />
                )
              }
            />
          </div>
          {loadError ? <ErrorPanel error={loadError} onRetry={() => void load()} /> : null}
          {loading ? (
            <p className="text-xs text-fg-muted">{t("Loading plugins…")}</p>
          ) : plugins.length === 0 && !loadError ? (
            <EmptyState
              icon={<Puzzle size={18} />}
              title={t("No plugins installed")}
              description={t(
                "Plugins add sources, channels, subtitle providers and custom pages to Ether.",
              )}
              action={
                <div className="flex flex-wrap justify-center gap-2">
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => navigate("/plugins/marketplace")}
                  >
                    {t("Open the marketplace")}
                  </Button>
                  <Link to="/plugins/studio" className={buttonVariants({ size: "sm" })}>
                    <Sparkles size={14} /> {t("Build a plugin")}
                  </Link>
                </div>
              }
            />
          ) : (
            <ul
              className="grid gap-4 md:grid-cols-2 xl:grid-cols-3"
              aria-label={t("Installed plugins")}
            >
              {plugins.map((p) => (
                <li key={p.id}>
                  <PluginCard
                    plugin={p}
                    busy={toggling === p.id}
                    onOpen={() => setOpen(p.id)}
                    onToggle={(next) => void toggle(p, next)}
                  />
                </li>
              ))}
            </ul>
          )}
        </>
      ) : (
        <CatalogueTab
          installedIds={installedIds}
          onInstalled={(id) => void installed(id)}
          onSelectInstalled={(id) => {
            navigate("/plugins/installed");
            setOpen(id);
          }}
          onOpenInstall={() => setInstallUrl("")}
        />
      )}

      <InstalledPluginDrawer
        pluginId={open}
        onClose={() => setOpen(null)}
        onChanged={() => void load(true)}
        onRemoved={() => {
          setOpen(null);
          void load(true);
        }}
        logRevision={logRevision}
        liveLogLines={open ? (liveLogs[open] ?? []) : []}
      />
      <InstallDrawer
        open={installUrl !== null}
        initialUrl={installUrl ?? ""}
        onClose={() => setInstallUrl(null)}
        onInstalled={(id) => void installed(id)}
      />
      {browsing ? (
        <DirectoryBrowser
          onClose={() => setBrowsing(false)}
          onImport={(address) => {
            setBrowsing(false);
            setInstallUrl(address);
          }}
        />
      ) : null}
    </DomainPage>
  );
}

function Tile({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="rounded-xl border border-border bg-surface px-3 py-2.5 sm:px-4 sm:py-3.5">
      <p className="text-xs text-fg-muted">{label}</p>
      <div className="mt-1 text-sm font-semibold tabular-nums text-fg sm:text-2xl">{value}</div>
    </div>
  );
}

function PluginCard({
  plugin,
  busy,
  onOpen,
  onToggle,
}: {
  plugin: PluginSummary;
  busy: boolean;
  onOpen: () => void;
  onToggle: (on: boolean) => void;
}) {
  const level = pluginHealthLevel(plugin.health, plugin.enabled);
  return (
    <article
      aria-label={plugin.name}
      className="flex h-full flex-col rounded-xl border border-border bg-surface p-5"
    >
      <button
        type="button"
        onClick={onOpen}
        className="flex items-start gap-3 rounded-lg text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
      >
        {plugin.icon ? (
          <Artwork
            src={plugin.icon}
            alt={plugin.name}
            icon={kindIcon(plugin.kind, 20)}
            className="size-11 shrink-0 rounded-lg"
          />
        ) : (
          <span
            aria-hidden
            className="flex size-11 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary"
          >
            {kindIcon(plugin.kind, 22)}
          </span>
        )}
        <div className="min-w-0 flex-1 space-y-1">
          <h3 className="truncate font-semibold text-fg">{plugin.name}</h3>
          <div className="flex flex-wrap items-center gap-1.5">
            {!plugin.enabled ? (
              <Badge variant="secondary">{t("Off")}</Badge>
            ) : plugin.shadow ? (
              <Badge variant="info">
                <Eye size={12} aria-hidden /> {t("Trying out")}
              </Badge>
            ) : (
              <LevelBadge level={level} />
            )}
            <Badge variant="secondary">{kindLabel(plugin.kind, plugin.stremioOnly)}</Badge>
            {plugin.updateVersion ? (
              <Badge variant="info">
                <ArrowUpCircle size={11} aria-hidden />{" "}
                {t("Update v{version}", { version: plugin.updateVersion })}
              </Badge>
            ) : null}
            {!plugin.verified ? <Badge variant="warning">{t("Unverified")}</Badge> : null}
          </div>
        </div>
      </button>
      <p className="mt-3 line-clamp-2 min-h-10 text-sm text-fg-muted">{plugin.summary}</p>
      <div className="mt-auto flex items-center justify-between gap-3 pt-4">
        <span className="font-mono text-xs text-fg-subtle">v{plugin.version}</span>
        <Switch
          aria-label={t("Enable {name}", { name: plugin.name })}
          checked={plugin.enabled}
          disabled={busy}
          onChange={(e) => onToggle(e.currentTarget.checked)}
        />
      </div>
    </article>
  );
}
