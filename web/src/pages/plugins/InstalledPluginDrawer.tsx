import {
  Alert,
  AlertDescription,
  AlertTitle,
  Badge,
  Button,
  SettingsSection,
  SubtabNav,
  Switch,
  confirmDialog,
  toast,
} from "@braedonsaunders/appkit-ui";
import {
  AlertTriangle,
  ArrowUpRight,
  Check,
  Copy,
  ExternalLink,
  RefreshCw,
  RotateCcw,
  ShieldAlert,
  ShieldCheck,
  Trash2,
  Undo2,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { Artwork } from "../../components/Artwork";
import { Drawer } from "../../components/Drawer";
import { ErrorPanel, LoadingRows, MutationError } from "../../components/feedback";
import { TestButton, type TestState } from "../../components/fields";
import { Section } from "../../components/layout";
import { useViewers } from "../../components/pickers";
import { SettingsRow } from "../../components/SettingsRow";
import { LevelBadge } from "../../components/status";
import { t, tn } from "../../i18n";
import { ApiError, apiFetch, errorMessage } from "../../lib/api";
import { invalidateRegistry, useRegistry } from "../../lib/extend";
import { formatBytes, formatDateTime, formatNumber } from "../../lib/format";
import { SchemaGroups } from "../settings/SchemaForm";
import { FolderGrantsPicker, ViewerGrantsPicker } from "./PermissionSheet";
import { AddonResources } from "./AddonResources";
import { SidecarPairing } from "./SidecarPairing";
import {
  buildGrantItems,
  buildPlacements,
  journalOpLabel,
  kindIcon,
  kindLabel,
  mapApiFieldErrors,
  pluginHealthLevel,
  schemaToFieldGroups,
  selectFieldsNeedingOptions,
  type JournalEntry,
  type PluginDetail,
  type PluginGrants,
  type PluginSummary,
} from "./types";

export type DrawerTab =
  | "overview"
  | "resources"
  | "settings"
  | "permissions"
  | "health"
  | "activity"
  | "logs"
  | "updates"
  | "remove";

export function InstalledPluginDrawer({
  pluginId,
  initialTab = "overview",
  logRevision = 0,
  liveLogLines = [],
  onClose,
  onChanged,
  onRemoved,
}: {
  pluginId: string | null;
  initialTab?: DrawerTab;
  logRevision?: number;
  liveLogLines?: string[];
  onClose: () => void;
  onChanged: () => void;
  onRemoved: () => void;
}) {
  const [tab, setTab] = useState<DrawerTab>(initialTab);
  const [detail, setDetail] = useState<PluginDetail | null>(null);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<unknown>(null);
  const [mutationError, setMutationError] = useState<unknown>(null);

  // Settings tab state
  const [settingsDraft, setSettingsDraft] = useState<Record<string, unknown>>({});
  const [settingsSchema, setSettingsSchema] = useState<Record<string, unknown>>({});
  const [dynamicOptions, setDynamicOptions] = useState<
    Record<string, { value: string; label: string }[]>
  >({});
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [savingSettings, setSavingSettings] = useState(false);
  const [testState, setTestState] = useState<TestState>({ status: "idle" });

  // Activity tab state
  const [journal, setJournal] = useState<JournalEntry[]>([]);
  const [shadowReport, setShadowReport] = useState<JournalEntry[]>([]);
  const [busyActivity, setBusyActivity] = useState(false);

  // Logs tab state
  const [logs, setLogs] = useState<string[]>([]);
  const [copiedLogs, setCopiedLogs] = useState(false);

  // Updates tab state
  const [checkingUpdate, setCheckingUpdate] = useState(false);
  const [applyingUpdate, setApplyingUpdate] = useState(false);
  const [rollingBack, setRollingBack] = useState(false);
  const [pendingAddedPerms, setPendingAddedPerms] = useState<string[]>([]);

  // Remove tab state
  const [undoBeforeRemove, setUndoBeforeRemove] = useState(true);
  const [removing, setRemoving] = useState(false);

  const { descriptors } = useRegistry();
  const { viewers } = useViewers();

  useEffect(() => {
    setTab(initialTab);
  }, [pluginId, initialTab]);

  const loadDetail = useCallback(async (id: string, quiet = false) => {
    if (!quiet) setLoading(true);
    setLoadError(null);
    try {
      const d = await apiFetch<PluginDetail>(`/plugins/${encodeURIComponent(id)}`);
      setDetail(d);
      setPendingAddedPerms(d.addedPermissions ?? []);
    } catch (e) {
      setLoadError(e);
    } finally {
      if (!quiet) setLoading(false);
    }
  }, []);

  const loadSettings = useCallback(async (id: string) => {
    try {
      const res = await apiFetch<{
        settings: Record<string, unknown>;
        schema: Record<string, unknown>;
      }>(`/plugins/${encodeURIComponent(id)}/settings`);
      setSettingsDraft(res.settings ?? {});
      const sch = res.schema ?? {};
      setSettingsSchema(sch);
      const optionFields = selectFieldsNeedingOptions(sch);
      if (optionFields.length > 0) {
        const entries = await Promise.all(
          optionFields.map(async (field) => {
            try {
              const optRes = await apiFetch<{ options: { value: string; label: string }[] }>(
                `/plugins/${encodeURIComponent(id)}/options/${encodeURIComponent(field)}`,
              );
              return [field, optRes.options ?? []] as const;
            } catch {
              return [field, []] as const;
            }
          }),
        );
        setDynamicOptions(Object.fromEntries(entries));
      } else {
        setDynamicOptions({});
      }
    } catch {
      // Keep empty settings if unreachable
    }
  }, []);

  const loadActivity = useCallback(async (id: string) => {
    try {
      const [jRes, sRes] = await Promise.all([
        apiFetch<{ entries: JournalEntry[] }>(`/plugins/${encodeURIComponent(id)}/journal`),
        apiFetch<{ entries: JournalEntry[]; count: number }>(
          `/plugins/${encodeURIComponent(id)}/shadow`,
        ),
      ]);
      setJournal(jRes.entries ?? []);
      setShadowReport(sRes.entries ?? []);
    } catch {
      // Ignore secondary load error
    }
  }, []);

  const loadLogs = useCallback(async (id: string) => {
    try {
      const lRes = await apiFetch<{ lines: string[] }>(
        `/plugins/${encodeURIComponent(id)}/logs?limit=200`,
      );
      setLogs((lRes.lines ?? []).slice(-200));
    } catch {
      // Ignore secondary load error
    }
  }, []);

  useEffect(() => {
    if (!pluginId) {
      setDetail(null);
      return;
    }
    setMutationError(null);
    setFieldErrors({});
    setTestState({ status: "idle" });
    void loadDetail(pluginId);
    void loadSettings(pluginId);
    void loadActivity(pluginId);
    void loadLogs(pluginId);
  }, [pluginId, loadDetail, loadSettings, loadActivity, loadLogs]);

  useEffect(() => {
    if (!pluginId || logRevision === 0) return;
    void loadDetail(pluginId, true);
    void loadLogs(pluginId);
  }, [pluginId, logRevision, loadDetail, loadLogs]);

  const toggleEnabled = async (next: boolean) => {
    if (!detail) return;
    setMutationError(null);
    try {
      await apiFetch(`/plugins/${encodeURIComponent(detail.id)}/${next ? "enable" : "disable"}`, {
        method: "POST",
      });
      invalidateRegistry();
      await loadDetail(detail.id, true);
      onChanged();
    } catch (e) {
      setMutationError(e);
    }
  };

  const saveSettings = async () => {
    if (!detail) return;
    setSavingSettings(true);
    setMutationError(null);
    setFieldErrors({});
    try {
      await apiFetch(`/plugins/${encodeURIComponent(detail.id)}/settings`, {
        method: "PUT",
        body: JSON.stringify({ settings: settingsDraft }),
      });
      toast.success(t("Settings saved"));
      await loadSettings(detail.id);
      onChanged();
    } catch (e) {
      setMutationError(e);
      if (e instanceof ApiError && e.fields) {
        setFieldErrors(mapApiFieldErrors(e.fields, "settings"));
      }
    } finally {
      setSavingSettings(false);
    }
  };

  const runPluginTest = async () => {
    if (!detail) return;
    setTestState({ status: "running" });
    const started = Date.now();
    try {
      const res = await apiFetch<{ ok: boolean; message?: string; details?: string[] }>(
        `/plugins/${encodeURIComponent(detail.id)}/test`,
        { method: "POST" },
      );
      if (res.ok) {
        setTestState({ status: "ok", durationMs: Date.now() - started });
      } else {
        setTestState({ status: "fail", message: res.message || t("Test failed") });
      }
    } catch (e) {
      setTestState({ status: "fail", message: errorMessage(e) });
    }
  };

  const updateGrants = async (nextGrants: PluginGrants) => {
    if (!detail) return;
    setMutationError(null);
    try {
      await apiFetch(`/plugins/${encodeURIComponent(detail.id)}/grants`, {
        method: "PUT",
        body: JSON.stringify(nextGrants),
      });
      await loadDetail(detail.id, true);
      toast.success(t("Permissions updated"));
      onChanged();
    } catch (e) {
      setMutationError(e);
    }
  };

  const toggleGrantRevoked = async (key: string) => {
    if (!detail) return;
    const current = detail.permissions.grants ?? {};
    const revoked = new Set(current.revoked ?? []);
    if (revoked.has(key)) revoked.delete(key);
    else revoked.add(key);
    await updateGrants({
      ...current,
      revoked: [...revoked],
    });
  };

  const toggleShadow = async (next: boolean) => {
    if (!detail) return;
    setBusyActivity(true);
    setMutationError(null);
    try {
      await apiFetch(`/plugins/${encodeURIComponent(detail.id)}/shadow`, {
        method: "POST",
        body: JSON.stringify({ shadow: next }),
      });
      await loadDetail(detail.id, true);
      await loadActivity(detail.id);
      onChanged();
    } catch (e) {
      setMutationError(e);
    } finally {
      setBusyActivity(false);
    }
  };

  const undoAll = async () => {
    if (!detail) return;
    const ok = await confirmDialog({
      title: t("Undo everything this plugin did?"),
      message: t("This reverses every collection and watch-state change recorded for {name}.", {
        name: detail.name,
      }),
      confirmLabel: t("Undo changes"),
      tone: "danger",
    });
    if (!ok) return;
    setBusyActivity(true);
    setMutationError(null);
    try {
      const res = await apiFetch<{ undone: number }>(
        `/plugins/${encodeURIComponent(detail.id)}/undo`,
        { method: "POST" },
      );
      toast.success(tn(res.undone, "Undid {count} change", "Undid {count} changes"));
      await loadActivity(detail.id);
      await loadDetail(detail.id, true);
      onChanged();
    } catch (e) {
      setMutationError(e);
    } finally {
      setBusyActivity(false);
    }
  };

  const checkForUpdates = async () => {
    if (!detail) return;
    setCheckingUpdate(true);
    setMutationError(null);
    try {
      const res = await apiFetch<{
        updateAvailable: boolean;
        version: string;
        addedPermissions?: string[];
      }>(`/plugins/${encodeURIComponent(detail.id)}/update/check`, { method: "POST" });
      if (res.addedPermissions) setPendingAddedPerms(res.addedPermissions);
      await loadDetail(detail.id, true);
      if (res.updateAvailable) {
        toast.success(t("Version {version} is available", { version: res.version }));
      } else {
        toast.success(t("{name} is up to date", { name: detail.name }));
      }
    } catch (e) {
      setMutationError(e);
    } finally {
      setCheckingUpdate(false);
    }
  };

  const applyUpdate = async (acceptAddedPermissions: boolean) => {
    if (!detail) return;
    setApplyingUpdate(true);
    setMutationError(null);
    try {
      await apiFetch<PluginSummary>(`/plugins/${encodeURIComponent(detail.id)}/update`, {
        method: "POST",
        body: JSON.stringify({ acceptAddedPermissions }),
      });
      setPendingAddedPerms([]);
      invalidateRegistry();
      await loadDetail(detail.id, true);
      onChanged();
      toast.success(t("Updated {name}", { name: detail.name }));
    } catch (e) {
      if (e instanceof ApiError && e.code === "permission_increase" && e.fields) {
        setPendingAddedPerms(e.fields.map((f) => f.message));
      } else {
        setMutationError(e);
      }
    } finally {
      setApplyingUpdate(false);
    }
  };

  const toggleAutoUpdate = async (next: boolean) => {
    if (!detail) return;
    setMutationError(null);
    try {
      await apiFetch(`/plugins/${encodeURIComponent(detail.id)}/auto-update`, {
        method: "PUT",
        body: JSON.stringify({ autoUpdate: next }),
      });
      await loadDetail(detail.id, true);
      onChanged();
    } catch (e) {
      setMutationError(e);
    }
  };

  const rollbackVersion = async () => {
    if (!detail) return;
    setRollingBack(true);
    setMutationError(null);
    try {
      await apiFetch<PluginSummary>(`/plugins/${encodeURIComponent(detail.id)}/rollback`, {
        method: "POST",
      });
      invalidateRegistry();
      await loadDetail(detail.id, true);
      onChanged();
      toast.success(t("Rolled back {name}", { name: detail.name }));
    } catch (e) {
      setMutationError(e);
    } finally {
      setRollingBack(false);
    }
  };

  const removePlugin = async () => {
    if (!detail) return;
    const ok = await confirmDialog({
      title: t("Remove {name}?", { name: detail.name }),
      message: undoBeforeRemove
        ? t("This undoes changes made by {name}, then deletes its settings, storage and logs.", {
            name: detail.name,
          })
        : t("This deletes {name}, including its settings, storage and logs.", {
            name: detail.name,
          }),
      confirmLabel: t("Remove plugin"),
      tone: "danger",
    });
    if (!ok) return;
    setRemoving(true);
    setMutationError(null);
    try {
      const query = undoBeforeRemove ? "?undo=1" : "";
      await apiFetch(`/plugins/${encodeURIComponent(detail.id)}${query}`, {
        method: "DELETE",
      });
      invalidateRegistry();
      toast.success(t("Removed {name}", { name: detail.name }));
      onRemoved();
    } catch (e) {
      setMutationError(e);
    } finally {
      setRemoving(false);
    }
  };

  const allLogs = [...logs, ...liveLogLines].slice(-200);
  const copyLogs = async () => {
    const text = allLogs.join("\n");
    try {
      await navigator.clipboard?.writeText(text);
    } catch {
      // Fallback when clipboard is restricted
    }
    setCopiedLogs(true);
    toast.success(t("Copied logs"));
    setTimeout(() => setCopiedLogs(false), 1500);
  };

  const tabs: { key: DrawerTab; label: string }[] = [
    { key: "overview", label: t("Overview") },
    ...(detail?.stremioOnly ? [{ key: "resources" as const, label: t("Resources") }] : []),
    { key: "settings", label: t("Settings") },
    { key: "permissions", label: t("Permissions") },
    { key: "health", label: t("Health") },
    { key: "activity", label: t("Activity") },
    { key: "logs", label: t("Logs") },
    { key: "updates", label: t("Updates") },
    { key: "remove", label: t("Remove") },
  ];

  const placements = detail ? buildPlacements(descriptors, detail.id) : [];
  const grantItems = detail ? buildGrantItems(detail) : [];
  const revokedSet = new Set(detail?.permissions.grants?.revoked ?? []);
  const fieldGroups = schemaToFieldGroups(settingsSchema, settingsDraft, dynamicOptions, viewers);
  const hasSettings = fieldGroups.length > 0 || Object.keys(settingsDraft).length > 0;
  const activeJournalCount = journal.filter((j) => !j.undone).length;

  return (
    <Drawer
      open={Boolean(pluginId)}
      onClose={onClose}
      title={detail?.name ?? t("Plugin")}
      description={
        detail ? `${kindLabel(detail.kind, detail.stremioOnly)} · v${detail.version}` : undefined
      }
      size="xl"
    >
      {loading && !detail ? (
        <LoadingRows rows={5} />
      ) : loadError ? (
        <ErrorPanel error={loadError} onRetry={() => pluginId && void loadDetail(pluginId)} />
      ) : detail ? (
        <div className="space-y-5">
          {/* Header summary bar */}
          <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border bg-bg-subtle p-4">
            <div className="flex min-w-0 items-center gap-3.5">
              {detail.icon || detail.manifest.ether?.icon || detail.manifest.logo ? (
                <Artwork
                  src={detail.icon ?? detail.manifest.ether?.icon ?? detail.manifest.logo}
                  alt={detail.name}
                  icon={kindIcon(detail.kind, 20)}
                  className="size-12 shrink-0 rounded-lg border border-border bg-bg-subtle"
                  imgClassName="object-contain p-1"
                />
              ) : (
                <span
                  aria-hidden
                  className="flex size-12 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary"
                >
                  {kindIcon(detail.kind, 22)}
                </span>
              )}
              <div className="min-w-0 space-y-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="truncate text-base font-semibold text-fg">{detail.name}</span>
                  <Badge variant="secondary">{kindLabel(detail.kind, detail.stremioOnly)}</Badge>
                  <LevelBadge level={pluginHealthLevel(detail.health, detail.enabled)} />
                  {!detail.verified ? (
                    <Badge variant="warning">
                      <AlertTriangle size={12} aria-hidden /> {t("Unverified")}
                    </Badge>
                  ) : (
                    <Badge variant="success">
                      <ShieldCheck size={12} aria-hidden /> {t("Verified")}
                    </Badge>
                  )}
                  {detail.shadow ? <Badge variant="secondary">{t("Shadow mode")}</Badge> : null}
                  {detail.updateVersion ? (
                    <Badge variant="warning">
                      {t("Update v{version}", { version: detail.updateVersion })}
                    </Badge>
                  ) : null}
                </div>
                <p className="text-xs text-fg-muted">
                  {detail.summary ||
                    detail.manifest.ether?.summary ||
                    detail.manifest.description ||
                    detail.id}
                </p>
              </div>
            </div>
            <div className="flex items-center gap-2">
              <label htmlFor="drawer-plugin-enabled" className="text-xs font-medium text-fg-muted">
                {detail.enabled ? t("Enabled") : t("Disabled")}
              </label>
              <Switch
                id="drawer-plugin-enabled"
                checked={detail.enabled}
                onChange={(e) => void toggleEnabled(e.currentTarget.checked)}
                aria-label={t("Enable {name}", { name: detail.name })}
              />
            </div>
          </div>

          <div className="border-b border-border">
            <SubtabNav
              ariaLabel={t("Plugin sections")}
              active={tab}
              onSelect={(k) => {
                setMutationError(null);
                setTab(k as DrawerTab);
              }}
              tabs={tabs}
            />
          </div>

          <MutationError error={mutationError} />

          {tab === "resources" ? <AddonResources detail={detail} /> : null}

          {/* TAB 1: OVERVIEW */}
          {tab === "overview" ? (
            <div className="space-y-5">
              {detail.manifest.ether?.runtime?.kind === "remote" ? (
                <SidecarPairing key={detail.id} id={detail.id} />
              ) : null}
              <Section title={t("About")}>
                <p className="text-sm text-fg">
                  {detail.summary ||
                    detail.manifest.ether?.summary ||
                    detail.manifest.description ||
                    t("No summary provided.")}
                </p>
                <dl className="grid gap-3 pt-2 text-xs sm:grid-cols-2">
                  <div>
                    <dt className="text-fg-muted">{t("Plugin ID")}</dt>
                    <dd className="mt-0.5 font-mono text-fg">{detail.id}</dd>
                  </div>
                  <div>
                    <dt className="text-fg-muted">{t("Kind")}</dt>
                    <dd className="mt-0.5 text-fg">{kindLabel(detail.kind, detail.stremioOnly)}</dd>
                  </div>
                  <div>
                    <dt className="text-fg-muted">{t("Author")}</dt>
                    <dd className="mt-0.5 text-fg">
                      {detail.manifest.ether?.author?.name ? (
                        detail.manifest.ether.author.url ? (
                          <a
                            href={detail.manifest.ether.author.url}
                            target="_blank"
                            rel="noreferrer"
                            className="inline-flex items-center gap-1 text-primary hover:underline"
                          >
                            {detail.manifest.ether.author.name}
                            <ExternalLink size={12} aria-hidden />
                          </a>
                        ) : (
                          detail.manifest.ether.author.name
                        )
                      ) : (
                        t("Community author")
                      )}
                    </dd>
                  </div>
                  {detail.manifest.ether?.repo ? (
                    <div>
                      <dt className="text-fg-muted">{t("Links")}</dt>
                      <dd className="mt-0.5">
                        <a
                          href={detail.manifest.ether.repo}
                          target="_blank"
                          rel="noreferrer"
                          className="inline-flex items-center gap-1 text-primary hover:underline"
                        >
                          {t("Source repository")}
                          <ExternalLink size={12} aria-hidden />
                        </a>
                      </dd>
                    </div>
                  ) : null}
                  {detail.manifest.ether?.license ? (
                    <div>
                      <dt className="text-fg-muted">{t("Licence")}</dt>
                      <dd className="mt-0.5 text-fg">{detail.manifest.ether.license}</dd>
                    </div>
                  ) : null}
                </dl>
              </Section>

              <Section
                title={t("Where it appears")}
                description={t("Pages, pickers and console surfaces this plugin contributes.")}
              >
                {placements.length > 0 ? (
                  <ul className="divide-y divide-border" aria-label={t("Where it appears")}>
                    {placements.map((p) => (
                      <li
                        key={p.id}
                        className="flex items-center justify-between gap-3 py-2.5 first:pt-0 last:pb-0"
                      >
                        <div className="min-w-0">
                          <p className="truncate text-sm font-medium text-fg">{p.label}</p>
                          <p className="text-xs text-fg-muted">{p.area}</p>
                        </div>
                        <Link
                          to={p.href}
                          onClick={onClose}
                          className="inline-flex items-center gap-1 text-xs font-medium text-primary hover:underline"
                        >
                          {t("Open")} <ArrowUpRight size={13} aria-hidden />
                        </Link>
                      </li>
                    ))}
                  </ul>
                ) : (
                  <p className="text-sm text-fg-muted">
                    {detail.enabled
                      ? t("This plugin runs in the background without adding console pages.")
                      : t("Turn this plugin on to see its pages and pickers in the console.")}
                  </p>
                )}
              </Section>
            </div>
          ) : null}

          {/* TAB 2: SETTINGS */}
          {tab === "settings" ? (
            <div className="space-y-5">
              {hasSettings ? (
                <SchemaGroups
                  groups={fieldGroups}
                  value={settingsDraft}
                  onChange={setSettingsDraft}
                  errors={fieldErrors}
                  prefix="settings"
                  footer={
                    <div className="flex w-full flex-wrap items-center justify-between gap-2">
                      <TestButton state={testState} onTest={() => void runPluginTest()} />
                      <Button
                        type="button"
                        size="sm"
                        disabled={savingSettings}
                        onClick={() => void saveSettings()}
                      >
                        {savingSettings ? t("Saving…") : t("Save settings")}
                      </Button>
                    </div>
                  }
                />
              ) : (
                <Section
                  title={t("Settings")}
                  footer={<TestButton state={testState} onTest={() => void runPluginTest()} />}
                >
                  <p className="text-sm text-fg-muted">
                    {t("This plugin has no configurable settings.")}
                  </p>
                </Section>
              )}
            </div>
          ) : null}

          {/* TAB 3: PERMISSIONS */}
          {tab === "permissions" ? (
            <div className="space-y-5">
              <SettingsSection
                title={t("Granted access")}
                description={t("Revoke any permission to block those calls immediately.")}
              >
                {grantItems.map((item) => {
                  const isRevoked = revokedSet.has(item.key);
                  return (
                    <SettingsRow
                      key={item.key}
                      title={item.label}
                      description={
                        item.revocable
                          ? isRevoked
                            ? t("Revoked. Calls needing this access are denied.")
                            : t("Allowed")
                          : undefined
                      }
                      control={
                        item.revocable ? (
                          <Button
                            type="button"
                            variant={isRevoked ? "outline" : "secondary"}
                            size="sm"
                            aria-label={
                              isRevoked
                                ? t("Restore {permission}", { permission: item.label })
                                : t("Revoke {permission}", { permission: item.label })
                            }
                            onClick={() => void toggleGrantRevoked(item.key)}
                          >
                            {isRevoked ? t("Restore") : t("Revoke")}
                          </Button>
                        ) : (
                          <Badge variant="secondary">{t("No access needed")}</Badge>
                        )
                      }
                    />
                  );
                })}
              </SettingsSection>

              {detail.permissions.needsViewers ? (
                <ViewerGrantsPicker
                  value={detail.permissions.grants?.viewers ?? []}
                  onChange={(viewersList) =>
                    void updateGrants({
                      ...detail.permissions.grants,
                      viewers: viewersList,
                    })
                  }
                />
              ) : null}

              {detail.permissions.needsFolders ? (
                <FolderGrantsPicker
                  value={detail.permissions.grants?.folders ?? []}
                  onChange={(foldersList) =>
                    void updateGrants({
                      ...detail.permissions.grants,
                      folders: foldersList,
                    })
                  }
                />
              ) : null}
            </div>
          ) : null}

          {/* TAB 4: HEALTH */}
          {tab === "health" ? (
            <div className="space-y-5">
              <Section
                title={t("Health status")}
                actions={
                  <LevelBadge
                    level={pluginHealthLevel(detail.healthDetail.status, detail.enabled)}
                  />
                }
              >
                {detail.healthDetail.reason ? (
                  <p className="text-sm text-fg">{detail.healthDetail.reason}</p>
                ) : (
                  <p className="text-sm text-fg-muted">
                    {t("Responding normally within its time and memory budgets.")}
                  </p>
                )}
                {detail.healthDetail.lastError ? (
                  <div className="rounded-lg border border-danger/30 bg-danger/5 px-3 py-2 text-xs text-danger">
                    <span className="font-semibold">{t("Last error:")}</span>{" "}
                    <span className="font-mono">{detail.healthDetail.lastError}</span>
                  </div>
                ) : null}
                <dl className="grid grid-cols-3 gap-3 pt-2 text-xs">
                  <div className="rounded-lg border border-border bg-bg-subtle p-3">
                    <dt className="text-fg-muted">{t("Slow calls")}</dt>
                    <dd className="mt-1 text-base font-semibold tabular-nums text-fg">
                      {formatNumber(detail.healthDetail.slow, 0)}
                    </dd>
                  </div>
                  <div className="rounded-lg border border-border bg-bg-subtle p-3">
                    <dt className="text-fg-muted">{t("Restarts")}</dt>
                    <dd className="mt-1 text-base font-semibold tabular-nums text-fg">
                      {formatNumber(detail.healthDetail.restarts, 0)}
                    </dd>
                  </div>
                  <div className="rounded-lg border border-border bg-bg-subtle p-3">
                    <dt className="text-fg-muted">{t("Event drops")}</dt>
                    <dd className="mt-1 text-base font-semibold tabular-nums text-fg">
                      {formatNumber(detail.healthDetail.drops, 0)}
                    </dd>
                  </div>
                </dl>
              </Section>

              <Section
                title={t("Privacy label")}
                description={t("Declared network hosts compared with actual outbound connections.")}
              >
                {detail.privacy.undeclared && detail.privacy.undeclared.length > 0 ? (
                  <Alert variant="destructive">
                    <AlertTitle className="flex items-center gap-2">
                      <ShieldAlert size={14} aria-hidden />
                      {t("Undeclared hosts blocked")}
                    </AlertTitle>
                    <AlertDescription>{detail.privacy.undeclared.join(", ")}</AlertDescription>
                  </Alert>
                ) : null}
                <div className="grid gap-4 sm:grid-cols-2">
                  <div>
                    <h4 className="mb-2 text-xs font-semibold uppercase tracking-wider text-fg-muted">
                      {t("Declared hosts")}
                    </h4>
                    {detail.privacy.declaredHosts.length > 0 ? (
                      <ul className="space-y-1 text-xs font-mono text-fg">
                        {detail.privacy.declaredHosts.map((h) => (
                          <li key={h} className="rounded bg-bg-subtle px-2 py-1">
                            {h}
                          </li>
                        ))}
                      </ul>
                    ) : (
                      <p className="text-xs text-fg-muted">{t("None declared")}</p>
                    )}
                  </div>
                  <div>
                    <h4 className="mb-2 text-xs font-semibold uppercase tracking-wider text-fg-muted">
                      {t("Observed hosts")}
                    </h4>
                    {Object.keys(detail.privacy.observedHosts ?? {}).length > 0 ? (
                      <ul className="space-y-1 text-xs">
                        {Object.entries(detail.privacy.observedHosts).map(([host, count]) => (
                          <li
                            key={host}
                            className="flex items-center justify-between rounded bg-bg-subtle px-2 py-1"
                          >
                            <span className="font-mono text-fg">{host}</span>
                            <span className="tabular-nums text-fg-muted">
                              {tn(count, "{count} call", "{count} calls")}
                            </span>
                          </li>
                        ))}
                      </ul>
                    ) : (
                      <p className="text-xs text-fg-muted">{t("No outbound calls observed")}</p>
                    )}
                  </div>
                </div>
              </Section>

              <Section
                title={t("Impact")}
                description={t("Resource usage and response times on this server.")}
              >
                <dl className="grid grid-cols-2 gap-3 text-xs sm:grid-cols-3">
                  <div className="rounded-lg border border-border bg-bg-subtle p-3">
                    <dt className="text-fg-muted">{t("Calls")}</dt>
                    <dd className="mt-1 text-base font-semibold tabular-nums text-fg">
                      {formatNumber(detail.impact.calls, 0)}
                    </dd>
                  </div>
                  <div className="rounded-lg border border-border bg-bg-subtle p-3">
                    <dt className="text-fg-muted">{t("Latency (p50 / p95)")}</dt>
                    <dd className="mt-1 text-base font-semibold tabular-nums text-fg">
                      {t("{p50} ms / {p95} ms", {
                        p50: detail.impact.p50Ms,
                        p95: detail.impact.p95Ms,
                      })}
                    </dd>
                  </div>
                  <div className="rounded-lg border border-border bg-bg-subtle p-3">
                    <dt className="text-fg-muted">{t("Errors")}</dt>
                    <dd className="mt-1 text-base font-semibold tabular-nums text-fg">
                      {formatNumber(detail.impact.errors, 0)}
                    </dd>
                  </div>
                  <div className="rounded-lg border border-border bg-bg-subtle p-3">
                    <dt className="text-fg-muted">{t("Outbound data")}</dt>
                    <dd className="mt-1 text-base font-semibold tabular-nums text-fg">
                      {formatBytes(detail.impact.egressBytes)}
                    </dd>
                  </div>
                  <div className="rounded-lg border border-border bg-bg-subtle p-3">
                    <dt className="text-fg-muted">{t("Plugin storage")}</dt>
                    <dd className="mt-1 text-base font-semibold tabular-nums text-fg">
                      {formatBytes(detail.impact.kvBytes)}
                    </dd>
                  </div>
                </dl>
              </Section>
            </div>
          ) : null}

          {/* TAB 5: ACTIVITY */}
          {tab === "activity" ? (
            <div className="space-y-5">
              <SettingsSection
                title={t("Shadow mode")}
                description={t(
                  "When shadow mode is on, writes are recorded in the shadow report without changing your collections or watch state.",
                )}
              >
                <SettingsRow
                  title={<label htmlFor="plugin-shadow-switch">{t("Shadow mode")}</label>}
                  description={
                    detail.shadow
                      ? t("Recording writes without applying them.")
                      : t("Applying writes normally and recording them in the journal.")
                  }
                  control={
                    <Switch
                      id="plugin-shadow-switch"
                      checked={detail.shadow}
                      disabled={busyActivity}
                      onChange={(e) => void toggleShadow(e.currentTarget.checked)}
                      aria-label={t("Shadow mode")}
                    />
                  }
                />
              </SettingsSection>

              <Section
                title={t("Shadow report")}
                description={t("Actions this plugin would have taken while in shadow mode.")}
              >
                {shadowReport.length > 0 ? (
                  <ul className="divide-y divide-border text-sm">
                    {shadowReport.map((entry) => (
                      <li
                        key={entry.id}
                        className="flex flex-wrap items-center justify-between gap-2 py-2.5 first:pt-0 last:pb-0"
                      >
                        <div>
                          <p className="font-medium text-fg">{journalOpLabel(entry.op)}</p>
                          {entry.detail ? (
                            <p className="text-xs text-fg-muted">{entry.detail}</p>
                          ) : null}
                        </div>
                        <span className="text-xs text-fg-subtle">
                          {formatDateTime(entry.created)}
                        </span>
                      </li>
                    ))}
                  </ul>
                ) : (
                  <p className="text-sm text-fg-muted">{t("No shadow actions recorded.")}</p>
                )}
              </Section>

              <Section
                title={t("Activity journal")}
                description={t(
                  "Every collection or watch-state change made by this plugin can be reversed.",
                )}
                actions={
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={busyActivity || activeJournalCount === 0}
                    onClick={() => void undoAll()}
                  >
                    <Undo2 size={14} /> {t("Undo everything this plugin did")}
                  </Button>
                }
              >
                {journal.length > 0 ? (
                  <ul className="divide-y divide-border text-sm">
                    {journal.map((entry) => (
                      <li
                        key={entry.id}
                        className="flex flex-wrap items-center justify-between gap-2 py-2.5 first:pt-0 last:pb-0"
                      >
                        <div>
                          <div className="flex items-center gap-2">
                            <span className="font-medium text-fg">{journalOpLabel(entry.op)}</span>
                            {entry.undone ? (
                              <Badge variant="secondary">{t("Undone")}</Badge>
                            ) : (
                              <Badge variant="success">{t("Applied")}</Badge>
                            )}
                          </div>
                          {entry.detail ? (
                            <p className="text-xs text-fg-muted">{entry.detail}</p>
                          ) : null}
                        </div>
                        <span className="text-xs text-fg-subtle">
                          {formatDateTime(entry.created)}
                        </span>
                      </li>
                    ))}
                  </ul>
                ) : (
                  <p className="text-sm text-fg-muted">
                    {t("This plugin has not made any reversible changes yet.")}
                  </p>
                )}
              </Section>
            </div>
          ) : null}

          {/* TAB 6: LOGS */}
          {tab === "logs" ? (
            <Section
              title={t("Plugin logs")}
              description={t("Last 200 log lines with secrets automatically redacted.")}
              actions={
                <div className="flex items-center gap-2">
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => void loadLogs(detail.id)}
                  >
                    <RefreshCw size={13} /> {t("Refresh")}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={allLogs.length === 0}
                    onClick={() => void copyLogs()}
                  >
                    {copiedLogs ? <Check size={13} /> : <Copy size={13} />}{" "}
                    {copiedLogs ? t("Copied") : t("Copy")}
                  </Button>
                </div>
              }
            >
              {allLogs.length > 0 ? (
                <pre
                  aria-label={t("Log output")}
                  className="max-h-96 overflow-auto rounded-lg border border-border bg-bg-subtle p-3.5 font-mono text-xs leading-relaxed text-fg"
                >
                  {allLogs.join("\n")}
                </pre>
              ) : (
                <p className="text-sm text-fg-muted">{t("No log lines recorded yet.")}</p>
              )}
            </Section>
          ) : null}

          {/* TAB 7: UPDATES */}
          {tab === "updates" ? (
            <div className="space-y-5">
              <Section
                title={t("Version")}
                actions={
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={checkingUpdate}
                    onClick={() => void checkForUpdates()}
                  >
                    <RefreshCw size={13} />{" "}
                    {checkingUpdate ? t("Checking…") : t("Check for updates")}
                  </Button>
                }
              >
                <dl className="grid gap-3 text-xs sm:grid-cols-2">
                  <div className="rounded-lg border border-border bg-bg-subtle p-3">
                    <dt className="text-fg-muted">{t("Installed version")}</dt>
                    <dd className="mt-1 font-mono text-base font-semibold text-fg">
                      v{detail.version}
                    </dd>
                  </div>
                  <div className="rounded-lg border border-border bg-bg-subtle p-3">
                    <dt className="text-fg-muted">{t("Available version")}</dt>
                    <dd className="mt-1 font-mono text-base font-semibold text-fg">
                      {detail.updateVersion ? `v${detail.updateVersion}` : t("Up to date")}
                    </dd>
                  </div>
                </dl>

                {detail.changelog ? (
                  <div className="rounded-lg border border-border bg-bg-subtle p-3">
                    <h4 className="text-xs font-semibold text-fg">{t("What changed")}</h4>
                    <p className="mt-1 text-xs text-fg-muted">{detail.changelog}</p>
                  </div>
                ) : null}

                {detail.updateVersion ? (
                  <div className="space-y-3 pt-2">
                    {pendingAddedPerms.length > 0 ? (
                      <Alert variant="warning">
                        <AlertTitle>
                          {t("Version {version} asks for new permissions", {
                            version: detail.updateVersion,
                          })}
                        </AlertTitle>
                        <AlertDescription>
                          <p className="mb-2 text-xs">
                            {t(
                              "Auto-update paused because this version requests additional access. Approve the new permissions to continue.",
                            )}
                          </p>
                          <ul className="list-disc space-y-1 pl-4 text-xs font-medium">
                            {pendingAddedPerms.map((perm) => (
                              <li key={perm}>{t(perm)}</li>
                            ))}
                          </ul>
                        </AlertDescription>
                      </Alert>
                    ) : null}

                    <div className="flex flex-wrap items-center gap-2">
                      {pendingAddedPerms.length > 0 ? (
                        <Button
                          type="button"
                          size="sm"
                          disabled={applyingUpdate}
                          onClick={() => void applyUpdate(true)}
                        >
                          {applyingUpdate ? t("Updating…") : t("Approve permissions and update")}
                        </Button>
                      ) : (
                        <Button
                          type="button"
                          size="sm"
                          disabled={applyingUpdate}
                          onClick={() => void applyUpdate(false)}
                        >
                          {applyingUpdate
                            ? t("Updating…")
                            : t("Update to v{version}", { version: detail.updateVersion })}
                        </Button>
                      )}
                    </div>
                  </div>
                ) : null}
              </Section>

              <SettingsSection title={t("Update preferences")}>
                <SettingsRow
                  title={<label htmlFor="plugin-auto-update">{t("Auto-update")}</label>}
                  description={t(
                    "Automatically install new versions when they do not ask for new permissions.",
                  )}
                  control={
                    <Switch
                      id="plugin-auto-update"
                      checked={detail.autoUpdate}
                      onChange={(e) => void toggleAutoUpdate(e.currentTarget.checked)}
                      aria-label={t("Auto-update")}
                    />
                  }
                />
                <SettingsRow
                  title={t("Roll back")}
                  description={
                    detail.prevVersion
                      ? t("Return to earlier version v{version}.", {
                          version: detail.prevVersion,
                        })
                      : t("No earlier version is stored on this server.")
                  }
                  control={
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      disabled={!detail.prevVersion || rollingBack}
                      onClick={() => void rollbackVersion()}
                    >
                      <RotateCcw size={13} /> {rollingBack ? t("Rolling back…") : t("Roll back")}
                    </Button>
                  }
                />
              </SettingsSection>
            </div>
          ) : null}

          {/* TAB 8: REMOVE */}
          {tab === "remove" ? (
            <Section
              title={t("Remove plugin")}
              description={t(
                "Removing this plugin deletes its package files, settings, sealed secrets, storage and logs.",
              )}
              footer={
                <Button
                  type="button"
                  variant="destructive"
                  size="sm"
                  disabled={removing}
                  onClick={() => void removePlugin()}
                >
                  <Trash2 size={14} /> {removing ? t("Removing…") : t("Remove plugin")}
                </Button>
              }
            >
              <SettingsRow
                title={<label htmlFor="remove-undo-first">{t("Undo its changes first")}</label>}
                description={t(
                  "Reverse any collections or watch-state changes recorded in the activity journal before removing the plugin.",
                )}
                control={
                  <Switch
                    id="remove-undo-first"
                    checked={undoBeforeRemove}
                    onChange={(e) => setUndoBeforeRemove(e.currentTarget.checked)}
                    aria-label={t("Undo its changes first")}
                  />
                }
              />
            </Section>
          ) : null}
        </div>
      ) : null}
    </Drawer>
  );
}
