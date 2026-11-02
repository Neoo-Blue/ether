/**
 * Server › Plugins: Installed, Catalogue and Develop (docs/extend/SPEC.md
 * WS10a, WS11). Owned by WS10a.
 */
import {
  Alert,
  AlertDescription,
  AlertTitle,
  Badge,
  Button,
  EmptyState,
  Switch,
} from "@braedonsaunders/appkit-ui";
import { AlertTriangle, ArrowUpCircle, Eye, Plus, Puzzle, ShieldCheck } from "lucide-react";
import { lazy, Suspense, useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Artwork } from "../../components/Artwork";
import { DomainPage, Section, useRouteTab, type PageTab } from "../../components/layout";
import { LEVEL_LABEL, StatusDot } from "../../components/status";
import { t, tn } from "../../i18n";
import { apiFetch, errorMessage } from "../../lib/api";
import { invalidateRegistry } from "../../lib/extend";
import { useStreamEvent } from "../../lib/stream";
import { CatalogueTab } from "./CatalogueTab";
import { InstallDrawer } from "./InstallDrawer";
import { PluginDrawer } from "./PluginDrawer";
import { kindIcon, kindLabel, pluginHealthLevel, type PluginSummary } from "./types";

const Develop = lazy(() => import("./develop/Develop"));

type PluginsTabKey = "installed" | "catalogue" | "develop";

export default function Plugins() {
  const navigate = useNavigate();

  const [plugins, setPlugins] = useState<PluginSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);
  const [togglingId, setTogglingId] = useState<string | null>(null);

  const [selectedPluginId, setSelectedPluginId] = useState<string | null>(null);
  const [installOpen, setInstallOpen] = useState(false);

  const [logRevision, setLogRevision] = useState(0);
  const [liveLogLines, setLiveLogLines] = useState<Record<string, string[]>>({});

  const loadPlugins = useCallback(async (silent = false) => {
    if (!silent) setLoading(true);
    try {
      const res = await apiFetch<{ plugins?: PluginSummary[] }>("/plugins");
      setPlugins(Array.isArray(res?.plugins) ? res.plugins : []);
      setErr(null);
    } catch (e) {
      setErr(errorMessage(e));
    } finally {
      if (!silent) setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadPlugins(false);
  }, [loadPlugins]);

  useStreamEvent("plugins.changed", () => {
    void loadPlugins(true);
    invalidateRegistry();
    setLogRevision((n) => n + 1);
  });

  useStreamEvent<{ plugin?: string; id?: string; line?: string }>("plugins.log", (ev) => {
    const payload = ev.data;
    const pid = payload?.plugin ?? payload?.id;
    const line = payload?.line;
    if (!pid || !line) return;
    setLiveLogLines((prev) => ({
      ...prev,
      [pid]: [...(prev[pid] ?? []), line],
    }));
    setLogRevision((n) => n + 1);
  });

  const tabs: readonly (PageTab & { key: PluginsTabKey })[] = useMemo(
    () => [
      {
        key: "installed",
        label: t("Installed"),
        count: plugins.length > 0 ? plugins.length : undefined,
      },
      {
        key: "catalogue",
        label: t("Catalogue"),
      },
      {
        key: "develop",
        label: t("Develop"),
      },
    ],
    [plugins.length],
  );

  const activeTab = useRouteTab(tabs, "installed");

  const installedIds = useMemo(() => new Set(plugins.map((p) => p.id)), [plugins]);

  const handleToggleEnabled = async (plugin: PluginSummary, nextEnabled: boolean) => {
    setTogglingId(plugin.id);
    setErr(null);
    setPlugins((prev) =>
      prev.map((p) => (p.id === plugin.id ? { ...p, enabled: nextEnabled } : p)),
    );
    try {
      const action = nextEnabled ? "enable" : "disable";
      await apiFetch(`/plugins/${encodeURIComponent(plugin.id)}/${action}`, {
        method: "POST",
        body: JSON.stringify({}),
      });
      await loadPlugins(true);
      invalidateRegistry();
    } catch (e) {
      setErr(errorMessage(e));
      await loadPlugins(true);
    } finally {
      setTogglingId(null);
    }
  };

  const handleInstalled = async (pluginId: string) => {
    await loadPlugins(true);
    if (activeTab !== "installed") {
      navigate("/plugins/installed");
    }
    setSelectedPluginId(pluginId);
  };

  const handleRemoved = async () => {
    setSelectedPluginId(null);
    await loadPlugins(true);
  };

  return (
    <DomainPage
      title={t("Plugins")}
      description={t("Sources, integrations and extensions that add to Ether.")}
      base="/plugins"
      tabs={tabs}
      active={activeTab}
      actions={
        <Button size="sm" onClick={() => setInstallOpen(true)}>
          <Plus size={14} /> {t("Install from a link")}
        </Button>
      }
    >
      {err ? (
        <Alert variant="destructive">
          <AlertTitle>{t("Plugin error")}</AlertTitle>
          <AlertDescription>{err}</AlertDescription>
        </Alert>
      ) : null}

      {activeTab === "installed" ? (
        <Section
          title={t("Installed plugins")}
          description={
            plugins.length > 0
              ? tn(plugins.length, "{count} plugin installed", "{count} plugins installed")
              : undefined
          }
        >
          {loading ? (
            <p className="text-xs text-fg-muted">{t("Loading plugins…")}</p>
          ) : plugins.length === 0 ? (
            <EmptyState
              icon={<Puzzle size={18} />}
              title={t("No plugins installed")}
              description={t(
                "Plugins add sources, channels, subtitle providers and custom pages to Ether.",
              )}
              action={
                <Button size="sm" onClick={() => setInstallOpen(true)}>
                  <Plus size={14} /> {t("Install from a link")}
                </Button>
              }
            />
          ) : (
            <div className="divide-y divide-border rounded-xl border border-border bg-surface">
              {plugins.map((plugin) => {
                const level = pluginHealthLevel(plugin.health, plugin.enabled);
                const healthText = LEVEL_LABEL[level];
                return (
                  <div
                    key={plugin.id}
                    className="flex items-center justify-between gap-4 px-4 py-3.5 transition-colors hover:bg-surface-hover"
                  >
                    <button
                      type="button"
                      onClick={() => setSelectedPluginId(plugin.id)}
                      className="flex min-w-0 flex-1 items-center gap-3.5 text-left"
                    >
                      <Artwork
                        src={plugin.icon}
                        alt={plugin.name}
                        className="h-11 w-11 shrink-0 rounded-lg"
                        icon={kindIcon(plugin.kind, 18)}
                      />
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="truncate text-sm font-semibold text-fg">
                            {plugin.name}
                          </span>
                          <Badge>{kindLabel(plugin.kind)}</Badge>
                          <span className="inline-flex items-center gap-1.5 text-xs text-fg-muted">
                            <StatusDot level={level} />
                            <span>{healthText}</span>
                          </span>
                          <span className="font-mono text-xs text-fg-subtle">
                            v{plugin.version}
                          </span>
                          {plugin.updateVersion ? (
                            <Badge variant="info">
                              <ArrowUpCircle size={11} aria-hidden />{" "}
                              {t("Update v{version}", { version: plugin.updateVersion })}
                            </Badge>
                          ) : null}
                          {!plugin.verified ? (
                            <Badge variant="warning">
                              <AlertTriangle size={11} aria-hidden /> {t("Unverified")}
                            </Badge>
                          ) : (
                            <Badge variant="success">
                              <ShieldCheck size={11} aria-hidden /> {t("Verified")}
                            </Badge>
                          )}
                          {plugin.shadow ? (
                            <Badge variant="info">
                              <Eye size={11} aria-hidden /> {t("Shadow mode")}
                            </Badge>
                          ) : null}
                        </div>
                        {plugin.summary ? (
                          <p className="mt-1 truncate text-xs text-fg-muted">{plugin.summary}</p>
                        ) : null}
                      </div>
                    </button>

                    <div className="flex shrink-0 items-center gap-3">
                      <Switch
                        aria-label={t("Enable {name}", { name: plugin.name })}
                        checked={plugin.enabled}
                        disabled={togglingId === plugin.id}
                        onChange={(e) => handleToggleEnabled(plugin, e.currentTarget.checked)}
                      />
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </Section>
      ) : null}

      {activeTab === "catalogue" ? (
        <CatalogueTab
          installedIds={installedIds}
          onInstalled={(id) => void handleInstalled(id)}
          onSelectInstalled={(id) => {
            navigate("/plugins/installed");
            setSelectedPluginId(id);
          }}
          onOpenInstall={() => setInstallOpen(true)}
        />
      ) : null}

      {activeTab === "develop" ? (
        <Suspense fallback={<p className="text-xs text-fg-muted">{t("Loading studio…")}</p>}>
          <Develop />
        </Suspense>
      ) : null}

      <PluginDrawer
        pluginId={selectedPluginId}
        onClose={() => setSelectedPluginId(null)}
        onChanged={() => void loadPlugins(true)}
        onRemoved={() => void handleRemoved()}
        logRevision={logRevision}
        liveLogLines={selectedPluginId ? (liveLogLines[selectedPluginId] ?? []) : []}
      />

      <InstallDrawer
        open={installOpen}
        onClose={() => setInstallOpen(false)}
        onInstalled={(id) => void handleInstalled(id)}
      />
    </DomainPage>
  );
}
