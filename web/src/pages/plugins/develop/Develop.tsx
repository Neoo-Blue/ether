import {
  Badge,
  Button,
  Input,
  Select,
  Spinner,
  Switch,
  cn,
  toast,
} from "@braedonsaunders/appkit-ui";
import {
  AlertTriangle,
  CheckCircle2,
  Code2,
  Download,
  FileCode2,
  FolderPlus,
  Layers,
  Play,
  Plus,
  ShieldCheck,
  Sparkles,
} from "lucide-react";
import { lazy, Suspense, useCallback, useEffect, useMemo, useState } from "react";
import { Drawer } from "../../../components/Drawer";
import { Section } from "../../../components/layout";
import {
  SLOT_CATALOG,
  SlotXrayOverlay,
  SlotXrayToggleButton,
  useSlotXray,
  type SlotSpec,
} from "../../../components/SlotXray";
import { t } from "../../../i18n";
import { apiFetch } from "../../../lib/api";
import { invalidateRegistry } from "../../../lib/extend";
import { useStreamEvent } from "../../../lib/stream";
import { PluginView } from "../../../viewspec/PluginView";
import { InspectorPanel } from "./InspectorPanel";
import { ShadowUndoPanel } from "./ShadowUndoPanel";
import type {
  StudioCallRecord,
  StudioCheckReport,
  StudioCompileResult,
  StudioDiagnostic,
  StudioInstallPreview,
  StudioJournalEntry,
  StudioPreviewResponse,
  StudioProject,
  StudioProjectSummary,
  StudioRunResponse,
  StudioTemplate,
  StudioValidationHint,
} from "./types";

const LazyCodeEditor = lazy(() => import("./CodeEditor"));

type WorkspacePane = "preview" | "inspector" | "shadow" | "check";

const getJSON = <T,>(path: string) => apiFetch<T>(path);
const postJSON = <T,>(path: string, body?: unknown) =>
  apiFetch<T>(path, {
    method: "POST",
    body: JSON.stringify(body ?? {}),
  });
const putJSON = <T,>(path: string, body: unknown) =>
  apiFetch<T>(path, {
    method: "PUT",
    body: JSON.stringify(body),
  });

export function Develop() {
  const [templates, setTemplates] = useState<StudioTemplate[]>([]);
  const [projects, setProjects] = useState<StudioProjectSummary[]>([]);
  const [activeProject, setActiveProject] = useState<StudioProject | null>(null);
  const [activeFile, setActiveFile] = useState<string>("src/plugin.ts");
  const [draftFiles, setDraftFiles] = useState<Record<string, string>>({});
  const [diagnostics, setDiagnostics] = useState<StudioDiagnostic[]>([]);
  const [pane, setPane] = useState<WorkspacePane>("preview");
  const [previewKey, setPreviewKey] = useState(0);
  const [routePreview, setRoutePreview] = useState<StudioPreviewResponse | null>(null);
  const [runMethod, setRunMethod] = useState("GET");
  const [runPath, setRunPath] = useState("");
  const [runBody, setRunBody] = useState("{}");
  const [runResult, setRunResult] = useState<StudioRunResponse | null>(null);
  const [calls, setCalls] = useState<StudioCallRecord[]>([]);
  const [hints, setHints] = useState<StudioValidationHint[]>([]);
  const [logs, setLogs] = useState<string[]>([]);
  const [journal, setJournal] = useState<StudioJournalEntry[]>([]);
  const [checkReport, setCheckReport] = useState<StudioCheckReport | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  // New plugin drawer state
  const [newOpen, setNewOpen] = useState(false);
  const [selectedTemplateId, setSelectedTemplateId] = useState("channels");
  const [newName, setNewName] = useState("");
  const [newPluginId, setNewPluginId] = useState("");
  const [newSlot, setNewSlot] = useState("");
  const [stremioUrl, setStremioUrl] = useState("");
  const [stremioMode, setStremioMode] = useState<"remix" | "direct">("remix");

  // Install permission sheet drawer state
  const [installOpen, setInstallOpen] = useState(false);
  const [installPreview, setInstallPreview] = useState<StudioInstallPreview | null>(null);
  const [installShadow, setInstallShadow] = useState(false);
  const [installing, setInstalling] = useState(false);

  const { selectedSlot, selectSlot } = useSlotXray();

  const refreshProjectTelemetry = useCallback(async (projectId: string) => {
    try {
      const [callsRes, journalRes, prevRes] = await Promise.all([
        getJSON<{ calls: StudioCallRecord[]; hints: StudioValidationHint[] }>(
          `/plugins/studio/projects/${encodeURIComponent(projectId)}/calls`,
        ),
        getJSON<{ entries: StudioJournalEntry[] }>(
          `/plugins/studio/projects/${encodeURIComponent(projectId)}/journal`,
        ),
        getJSON<StudioPreviewResponse>(
          `/plugins/studio/projects/${encodeURIComponent(projectId)}/preview`,
        ),
      ]);
      setCalls(callsRes.calls ?? []);
      setHints(callsRes.hints ?? []);
      setJournal(journalRes.entries ?? []);
      setRoutePreview(prevRes);
      setPreviewKey((k) => k + 1);
    } catch {
      // Ignore transient errors while editing
    }
  }, []);

  const selectProjectState = useCallback(
    (proj: StudioProject) => {
      setActiveProject(proj);
      setDraftFiles({ ...proj.files });
      setDiagnostics(proj.diagnostics ?? []);
      const files = Object.keys(proj.files);
      if (proj.files["src/plugin.ts"]) {
        setActiveFile("src/plugin.ts");
      } else if (files.length > 0) {
        setActiveFile(files[0]!);
      }
      setRunPath(proj.defaultRoute || "/ether/health");
      void refreshProjectTelemetry(proj.id);
    },
    [refreshProjectTelemetry],
  );

  const loadStudioIndex = useCallback(async () => {
    setLoading(true);
    try {
      const [tplRes, projRes] = await Promise.all([
        getJSON<{ templates: StudioTemplate[] }>("/plugins/studio/templates"),
        getJSON<{ projects: StudioProjectSummary[] }>("/plugins/studio/projects"),
      ]);
      const tpls = tplRes.templates ?? [];
      const projs = projRes.projects ?? [];
      setTemplates(tpls);
      setProjects(projs);
      if (projs.length > 0 && !activeProject) {
        const detail = await getJSON<{ project: StudioProject }>(
          `/plugins/studio/projects/${encodeURIComponent(projs[0]!.id)}`,
        );
        selectProjectState(detail.project);
      }
    } catch {
      // Handled gracefully when starting fresh
    } finally {
      setLoading(false);
    }
  }, [activeProject, selectProjectState]);

  useEffect(() => {
    void loadStudioIndex();
  }, [loadStudioIndex]);

  function openNewDialogForTemplate(tplId: string, slot = "") {
    const tpl = templates.find((item) => item.id === tplId) ?? templates[0];
    setSelectedTemplateId(tpl?.id ?? tplId);
    setNewName(tpl?.defaultName ?? "");
    setNewPluginId(tpl?.defaultPluginId ?? "");
    setNewSlot(slot);
    setStremioUrl("");
    setStremioMode("remix");
    setNewOpen(true);
  }

  // Open New plugin drawer when a slot is clicked in Slot X-ray
  useEffect(() => {
    if (selectedSlot && templates.length) {
      const spec = SLOT_CATALOG.find((s) => s.slot === selectedSlot);
      openNewDialogForTemplate(spec?.template ?? "page", selectedSlot);
      selectSlot(null);
    }
  }, [selectedSlot, selectSlot, templates]);

  // Hot reload preview & diagnostics when studio.reload fires over /api/stream
  useStreamEvent<{ projectId?: string; pluginId?: string; ok?: boolean }>("studio.reload", (ev) => {
    if (!activeProject) return;
    if (ev.data?.projectId && ev.data.projectId !== activeProject.id) return;
    void refreshProjectTelemetry(activeProject.id);
  });

  async function handleCreateProject() {
    if (selectedTemplateId === "stremio_remix" && stremioMode === "direct" && stremioUrl.trim()) {
      try {
        const prev = await postJSON<StudioInstallPreview>("/plugins/install/preview", {
          kind: "stremio",
          url: stremioUrl.trim(),
        });
        setNewOpen(false);
        setInstallPreview(prev);
        setInstallOpen(true);
        return;
      } catch (e) {
        toast.error(e instanceof Error ? e.message : t("Could not preview Stremio add-on."));
        return;
      }
    }

    try {
      const res = await postJSON<{ project: StudioProject }>("/plugins/studio/projects", {
        template: selectedTemplateId,
        name: newName.trim() || undefined,
        pluginId: newPluginId.trim() || undefined,
        slot: newSlot.trim() || undefined,
        stremioUrl: stremioUrl.trim() || undefined,
      });
      setNewOpen(false);
      selectProjectState(res.project);
      const listRes = await getJSON<{ projects: StudioProjectSummary[] }>(
        "/plugins/studio/projects",
      );
      setProjects(listRes.projects ?? []);
      toast.success(t("Created plugin project."));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("Could not create plugin project."));
    }
  }

  async function handleSaveAndCompile() {
    if (!activeProject) return;
    setSaving(true);
    try {
      const res = await putJSON<{ project: StudioProject; compile: StudioCompileResult }>(
        `/plugins/studio/projects/${encodeURIComponent(activeProject.id)}/files`,
        { files: draftFiles },
      );
      setActiveProject(res.project);
      setDiagnostics(res.compile.diagnostics ?? res.project.diagnostics ?? []);
      await refreshProjectTelemetry(res.project.id);
      if (res.compile.ok) {
        toast.success(t("Saved and reloaded preview."));
      } else {
        toast.error(t("Saved with compile diagnostics."));
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("Could not save files."));
    } finally {
      setSaving(false);
    }
  }

  async function handleRunRequest() {
    if (!activeProject) return;
    setSaving(true);
    try {
      await putJSON(`/plugins/studio/projects/${encodeURIComponent(activeProject.id)}/files`, {
        files: draftFiles,
      });
      let parsedBody: unknown = undefined;
      if (runMethod !== "GET" && runBody.trim()) {
        try {
          parsedBody = JSON.parse(runBody);
        } catch {
          parsedBody = runBody;
        }
      }
      const isHotRank = runPath === "hot:rank_key";
      const res = await postJSON<StudioRunResponse>(
        `/plugins/studio/projects/${encodeURIComponent(activeProject.id)}/run`,
        isHotRank
          ? { hotPath: "rank" }
          : {
              method: runMethod,
              path: runPath || activeProject.defaultRoute || "/ether/health",
              body: parsedBody,
            },
      );
      setRunResult(res);
      setLogs(res.logs ?? []);
      setJournal(res.journal ?? []);
      await refreshProjectTelemetry(activeProject.id);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("Request execution failed."));
    } finally {
      setSaving(false);
    }
  }

  async function handleRunCheck() {
    if (!activeProject) return;
    setSaving(true);
    try {
      const rep = await postJSON<StudioCheckReport>(
        `/plugins/studio/projects/${encodeURIComponent(activeProject.id)}/check`,
        { files: draftFiles },
      );
      setCheckReport(rep);
      setDiagnostics(rep.diagnostics ?? []);
      setPane("check");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("Conformance check failed."));
    } finally {
      setSaving(false);
    }
  }

  async function handleToggleShadow(next: boolean) {
    if (!activeProject) return;
    try {
      const res = await postJSON<{ project: StudioProject; shadow: boolean }>(
        `/plugins/studio/projects/${encodeURIComponent(activeProject.id)}/shadow`,
        { shadow: next },
      );
      setActiveProject(res.project);
      toast.success(next ? t("Shadow mode enabled.") : t("Shadow mode disabled."));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("Could not toggle shadow mode."));
    }
  }

  async function handleUndoWrites() {
    if (!activeProject) return;
    try {
      const res = await postJSON<{ undone: number; entries: StudioJournalEntry[] }>(
        `/plugins/studio/projects/${encodeURIComponent(activeProject.id)}/undo`,
        {},
      );
      setJournal(res.entries ?? []);
      toast.success(t("Reverted {count} plugin writes.", { count: res.undone }));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("Could not undo plugin writes."));
    }
  }

  async function handleOpenInstallSheet() {
    if (!activeProject) return;
    setSaving(true);
    try {
      await putJSON(`/plugins/studio/projects/${encodeURIComponent(activeProject.id)}/files`, {
        files: draftFiles,
      });
      const res = await postJSON<{ preview: StudioInstallPreview }>(
        `/plugins/studio/projects/${encodeURIComponent(activeProject.id)}/install/preview`,
        {},
      );
      setInstallPreview(res.preview);
      setInstallShadow(activeProject.shadow);
      setInstallOpen(true);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("Fix compile errors before installing."));
    } finally {
      setSaving(false);
    }
  }

  async function handleConfirmInstall() {
    if (!activeProject && !installPreview) return;
    setInstalling(true);
    try {
      if (activeProject) {
        await postJSON(`/plugins/studio/projects/${encodeURIComponent(activeProject.id)}/install`, {
          shadow: installShadow,
        });
      } else if (stremioUrl.trim()) {
        await postJSON("/plugins/install", {
          kind: "stremio",
          url: stremioUrl.trim(),
          shadow: installShadow,
        });
      }
      invalidateRegistry();
      setInstallOpen(false);
      toast.success(t("Installed plugin on this server."));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("Could not install plugin."));
    } finally {
      setInstalling(false);
    }
  }

  const fileList = useMemo(() => Object.keys(draftFiles).sort(), [draftFiles]);
  const errorCount = useMemo(
    () => diagnostics.filter((d) => d.severity === "error").length,
    [diagnostics],
  );
  const hasViewPage = Boolean(activeProject?.defaultPage || routePreview?.kind === "view");
  const viewPageKey = activeProject?.defaultPage || routePreview?.page || "overview";

  return (
    <div className="space-y-6" data-testid="plugins-develop-tab">
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border bg-surface px-5 py-4">
        <div className="space-y-1">
          <div className="flex items-center gap-2">
            <Code2 className="h-4 w-4 text-primary" />
            <h2 className="text-sm font-semibold text-fg">{t("Plugin Studio")}</h2>
            {activeProject ? <Badge variant="secondary">{activeProject.pluginId}</Badge> : null}
            {activeProject?.slot ? (
              <Badge variant="info">{t("Slot: {slot}", { slot: activeProject.slot })}</Badge>
            ) : null}
          </div>
          <p className="text-sm text-fg-muted">
            {t(
              "Build, preview, inspect and install plugins in the console without external tooling.",
            )}
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {projects.length > 1 ? (
            <Select
              aria-label={t("Active project")}
              value={activeProject?.id ?? ""}
              onChange={async (e) => {
                const nextId = e.currentTarget.value;
                const res = await getJSON<{ project: StudioProject }>(
                  `/plugins/studio/projects/${encodeURIComponent(nextId)}`,
                );
                selectProjectState(res.project);
              }}
            >
              {projects.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} ({p.pluginId})
                </option>
              ))}
            </Select>
          ) : null}
          <SlotXrayToggleButton />
          <Button
            size="sm"
            variant="secondary"
            onClick={() => openNewDialogForTemplate("channels")}
            data-testid="studio-new-plugin-button"
          >
            <Plus className="mr-1.5 h-4 w-4" />
            {t("New plugin")}
          </Button>
          {activeProject ? (
            <>
              <Button
                size="sm"
                variant="secondary"
                disabled={saving}
                onClick={() => void handleRunCheck()}
                data-testid="studio-check-button"
              >
                <ShieldCheck className="mr-1.5 h-4 w-4" />
                {t("Check")}
              </Button>
              <Button
                size="sm"
                variant="secondary"
                disabled={saving}
                onClick={() => void handleSaveAndCompile()}
                data-testid="studio-save-button"
              >
                <Play className="mr-1.5 h-4 w-4" />
                {t("Save and reload")}
              </Button>
              <Button
                size="sm"
                variant="default"
                disabled={saving || errorCount > 0}
                onClick={() => void handleOpenInstallSheet()}
                data-testid="studio-install-button"
              >
                <Download className="mr-1.5 h-4 w-4" />
                {t("Install")}
              </Button>
            </>
          ) : null}
        </div>
      </div>

      <SlotXrayOverlay
        onSelectSlot={(slot: SlotSpec) => {
          openNewDialogForTemplate(slot.template, slot.slot);
        }}
      />

      {loading ? (
        <div className="grid min-h-48 place-items-center rounded-xl border border-border bg-surface p-8">
          <Spinner size={20} label={t("Loading Plugin Studio")} />
        </div>
      ) : null}

      {!loading && !activeProject ? (
        <Section
          title={t("Start from a template")}
          description={t(
            "Pick one of the eight starter templates to scaffold a working plugin with manifest, TypeScript entry and live preview.",
          )}
        >
          <div
            className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4"
            data-testid="studio-template-grid"
          >
            {templates.map((tpl) => (
              <button
                key={tpl.id}
                type="button"
                data-testid={`studio-template-card-${tpl.id}`}
                onClick={() => openNewDialogForTemplate(tpl.id)}
                className="flex flex-col justify-between rounded-xl border border-border bg-bg-subtle p-4 text-left transition hover:border-primary hover:bg-surface focus:outline-none focus:ring-2 focus:ring-primary"
              >
                <div className="space-y-1.5">
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-sm font-semibold text-fg">{t(tpl.label)}</span>
                    <Badge variant="secondary">{tpl.kind}</Badge>
                  </div>
                  <p className="text-xs text-fg-muted">{t(tpl.summary)}</p>
                </div>
                <div className="mt-4 flex items-center text-xs font-medium text-primary">
                  <FolderPlus className="mr-1.5 h-3.5 w-3.5" />
                  {t("Use template")}
                </div>
              </button>
            ))}
          </div>
        </Section>
      ) : null}

      {activeProject ? (
        <div className="grid gap-6 xl:grid-cols-12">
          {/* Left pane: File list + CodeMirror editor */}
          <div className="space-y-4 xl:col-span-6">
            <div className="overflow-hidden rounded-xl border border-border bg-surface">
              <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border bg-bg-subtle px-3 py-2">
                <div className="flex flex-wrap items-center gap-1" role="tablist">
                  {fileList.map((file) => {
                    const fileErrs = diagnostics.filter(
                      (d) => d.file === file || d.file.endsWith(`/${file}`),
                    );
                    const active = activeFile === file;
                    return (
                      <button
                        key={file}
                        type="button"
                        role="tab"
                        aria-selected={active}
                        data-testid={`studio-file-tab-${file}`}
                        onClick={() => setActiveFile(file)}
                        className={cn(
                          "flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 font-mono text-xs transition",
                          active
                            ? "bg-surface font-semibold text-fg shadow-xs"
                            : "text-fg-muted hover:text-fg",
                        )}
                      >
                        <FileCode2 className="h-3.5 w-3.5" />
                        <span>{file}</span>
                        {fileErrs.length > 0 ? (
                          <Badge
                            variant={
                              fileErrs.some((e) => e.severity === "error")
                                ? "destructive"
                                : "warning"
                            }
                          >
                            {fileErrs.length}
                          </Badge>
                        ) : null}
                      </button>
                    );
                  })}
                </div>
                <div className="flex items-center gap-2 text-xs">
                  {errorCount > 0 ? (
                    <Badge variant="destructive">
                      {t("{count} errors", { count: errorCount })}
                    </Badge>
                  ) : (
                    <Badge variant="success">{t("Compiled cleanly")}</Badge>
                  )}
                </div>
              </div>

              <Suspense
                fallback={
                  <div className="grid min-h-80 place-items-center p-6">
                    <Spinner size={18} label={t("Loading code editor")} />
                  </div>
                }
              >
                <LazyCodeEditor
                  file={activeFile}
                  value={draftFiles[activeFile] ?? ""}
                  diagnostics={diagnostics}
                  onChange={(next) =>
                    setDraftFiles((prev) => ({
                      ...prev,
                      [activeFile]: next,
                    }))
                  }
                />
              </Suspense>
            </div>
          </div>

          {/* Right pane: Live Preview / Inspector / Shadow & Undo / Check */}
          <div className="space-y-4 xl:col-span-6">
            <div className="flex flex-wrap items-center justify-between gap-2 rounded-xl border border-border bg-surface px-3 py-2">
              <div className="flex flex-wrap items-center gap-1" role="tablist">
                {(
                  [
                    { key: "preview", label: t("Live preview") },
                    { key: "inspector", label: t("Inspector") },
                    { key: "shadow", label: t("Shadow and undo") },
                    { key: "check", label: t("Checks") },
                  ] as const
                ).map((tab) => (
                  <button
                    key={tab.key}
                    type="button"
                    role="tab"
                    aria-selected={pane === tab.key}
                    data-testid={`studio-pane-tab-${tab.key}`}
                    onClick={() => setPane(tab.key)}
                    className={cn(
                      "rounded-lg px-3 py-1.5 text-xs font-medium transition",
                      pane === tab.key
                        ? "bg-primary/10 text-primary"
                        : "text-fg-muted hover:text-fg",
                    )}
                  >
                    {tab.label}
                  </button>
                ))}
              </div>
              <Badge variant={activeProject.shadow ? "info" : "secondary"}>
                {activeProject.shadow ? t("Shadow mode") : t("Live mode")}
              </Badge>
            </div>

            {pane === "preview" ? (
              <div className="space-y-4" data-testid="studio-preview-pane">
                {hasViewPage ? (
                  <Section
                    title={t("ViewSpec v1 page preview")}
                    description={t(
                      "Rendered with the real PluginView component against your sandboxed dev instance.",
                    )}
                  >
                    <div
                      key={`${activeProject.id}:${previewKey}`}
                      data-testid="studio-plugin-view-container"
                      className="rounded-lg border border-border bg-bg-subtle p-4"
                    >
                      <PluginView plugin={`studio:${activeProject.id}`} page={viewPageKey} />
                    </div>
                  </Section>
                ) : null}

                <Section
                  title={t("Route and capability runner")}
                  description={t(
                    "Invoke any route or hot-path function on the sandboxed dev instance and inspect its response.",
                  )}
                  actions={
                    <Button
                      size="sm"
                      variant="default"
                      disabled={saving}
                      onClick={() => void handleRunRequest()}
                      data-testid="studio-run-route-button"
                    >
                      <Play className="mr-1.5 h-3.5 w-3.5" />
                      {t("Run route")}
                    </Button>
                  }
                >
                  <div className="grid gap-3 sm:grid-cols-12">
                    <div className="sm:col-span-3">
                      <Select
                        aria-label={t("HTTP method")}
                        value={runMethod}
                        onChange={(e) => setRunMethod(e.currentTarget.value)}
                      >
                        <option value="GET">GET</option>
                        <option value="POST">POST</option>
                      </Select>
                    </div>
                    <div className="sm:col-span-9">
                      <Input
                        aria-label={t("Route path")}
                        value={runPath}
                        onChange={(e) => setRunPath(e.target.value)}
                        placeholder="/ether/channels.json"
                        data-testid="studio-run-path-input"
                      />
                    </div>
                  </div>

                  {runMethod === "POST" && runPath !== "hot:rank_key" ? (
                    <div className="space-y-1">
                      <label className="text-xs font-medium text-fg-muted">
                        {t("Request JSON body")}
                      </label>
                      <Input
                        aria-label={t("Request JSON body")}
                        value={runBody}
                        onChange={(e) => setRunBody(e.target.value)}
                      />
                    </div>
                  ) : null}

                  {runResult || routePreview ? (
                    <div className="space-y-2">
                      <div className="flex items-center justify-between text-xs text-fg-muted">
                        <span>
                          {t("Route: {route}", {
                            route: runResult?.call.route ?? routePreview?.route ?? "",
                          })}
                        </span>
                        <span>
                          {t("{ms} ms", {
                            ms: runResult?.ms ?? routePreview?.ms ?? 0,
                          })}
                        </span>
                      </div>
                      <pre
                        data-testid="studio-preview-json"
                        className="max-h-64 overflow-auto rounded-lg border border-border bg-bg-subtle p-3 font-mono text-xs text-fg"
                      >
                        {JSON.stringify(runResult?.body ?? routePreview?.body ?? {}, null, 2)}
                      </pre>
                    </div>
                  ) : null}
                </Section>
              </div>
            ) : null}

            {pane === "inspector" ? (
              <InspectorPanel
                calls={calls}
                hints={hints}
                logs={logs}
                onRefresh={() => void refreshProjectTelemetry(activeProject.id)}
              />
            ) : null}

            {pane === "shadow" ? (
              <ShadowUndoPanel
                shadow={activeProject.shadow}
                entries={journal}
                busy={saving}
                onToggleShadow={(next) => void handleToggleShadow(next)}
                onUndo={() => void handleUndoWrites()}
              />
            ) : null}

            {pane === "check" ? (
              <Section
                title={t("Conformance and copy checks")}
                description={t(
                  "Runs the same manifest, copy, compile, runtime and ViewSpec checks as ether-plugin check.",
                )}
              >
                {!checkReport ? (
                  <p className="text-sm text-fg-muted">
                    {t("Click Check in the toolbar to validate this project.")}
                  </p>
                ) : (
                  <div className="space-y-2.5" data-testid="studio-check-results">
                    {checkReport.checks.map((item) => (
                      <div
                        key={item.id}
                        className="rounded-lg border border-border bg-bg-subtle p-3.5"
                      >
                        <div className="flex items-center justify-between gap-2">
                          <div className="flex items-center gap-2">
                            {item.status === "pass" ? (
                              <CheckCircle2 className="h-4 w-4 text-success" />
                            ) : (
                              <AlertTriangle className="h-4 w-4 text-danger" />
                            )}
                            <span className="text-sm font-medium text-fg">{item.label}</span>
                          </div>
                          <Badge variant={item.status === "pass" ? "success" : "destructive"}>
                            {item.status === "pass" ? t("Passed") : t("Failed")}
                          </Badge>
                        </div>
                        {item.message ? (
                          <p className="mt-1 text-xs text-fg-muted">{item.message}</p>
                        ) : null}
                        {item.fix ? (
                          <p className="mt-1 text-xs text-fg">
                            <span className="font-medium">{t("Fix:")}</span> {item.fix}
                          </p>
                        ) : null}
                      </div>
                    ))}
                  </div>
                )}
              </Section>
            ) : null}
          </div>
        </div>
      ) : null}

      {/* New plugin drawer */}
      <Drawer open={newOpen} onClose={() => setNewOpen(false)} title={t("New plugin")}>
        <div className="space-y-4" data-testid="studio-new-plugin-drawer">
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-fg">{t("Template")}</label>
            <Select
              aria-label={t("Template")}
              value={selectedTemplateId}
              onChange={(e) => {
                const val = e.currentTarget.value;
                setSelectedTemplateId(val);
                const found = templates.find((item) => item.id === val);
                if (found) {
                  setNewName(found.defaultName);
                  setNewPluginId(found.defaultPluginId);
                }
              }}
            >
              {templates.map((tpl) => (
                <option key={tpl.id} value={tpl.id}>
                  {tpl.label}
                </option>
              ))}
            </Select>
          </div>

          {selectedTemplateId === "stremio_remix" ? (
            <div className="space-y-3 rounded-lg border border-border bg-bg-subtle p-3.5">
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-fg">{t("Stremio manifest link")}</label>
                <Input
                  aria-label={t("Stremio manifest link")}
                  value={stremioUrl}
                  onChange={(e) => setStremioUrl(e.target.value)}
                  placeholder="https://v3-cinemeta.strem.io/manifest.json"
                  data-testid="studio-stremio-url-input"
                />
              </div>
              <div className="flex flex-wrap gap-2">
                <Button
                  size="sm"
                  variant={stremioMode === "remix" ? "default" : "secondary"}
                  onClick={() => setStremioMode("remix")}
                >
                  {t("Create a remix with settings or a page")}
                </Button>
                <Button
                  size="sm"
                  variant={stremioMode === "direct" ? "default" : "secondary"}
                  onClick={() => setStremioMode("direct")}
                >
                  {t("Install as-is")}
                </Button>
              </div>
            </div>
          ) : null}

          <div className="space-y-1.5">
            <label className="text-xs font-medium text-fg">{t("Plugin name")}</label>
            <Input
              aria-label={t("Plugin name")}
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              placeholder={t("Community channels")}
              data-testid="studio-new-name-input"
            />
          </div>

          <div className="space-y-1.5">
            <label className="text-xs font-medium text-fg">{t("Plugin ID")}</label>
            <Input
              aria-label={t("Plugin ID")}
              value={newPluginId}
              onChange={(e) => setNewPluginId(e.target.value)}
              placeholder="community.studio.channels"
              data-testid="studio-new-id-input"
            />
          </div>

          {newSlot ? (
            <div className="flex items-center justify-between rounded-lg border border-primary/40 bg-primary/5 px-3 py-2 text-xs">
              <div className="flex items-center gap-2">
                <Sparkles className="h-3.5 w-3.5 text-primary" />
                <span className="text-fg">
                  {t("Pre-wired for console slot {slot}", { slot: newSlot })}
                </span>
              </div>
              <Button size="sm" variant="ghost" onClick={() => setNewSlot("")}>
                {t("Clear")}
              </Button>
            </div>
          ) : null}

          <div className="flex justify-end gap-2 pt-3">
            <Button variant="secondary" onClick={() => setNewOpen(false)}>
              {t("Cancel")}
            </Button>
            <Button
              variant="default"
              onClick={() => void handleCreateProject()}
              data-testid="studio-create-confirm-button"
            >
              {selectedTemplateId === "stremio_remix" && stremioMode === "direct"
                ? t("Preview install")
                : t("Create project")}
            </Button>
          </div>
        </div>
      </Drawer>

      {/* Permission sheet install drawer */}
      <Drawer
        open={installOpen}
        onClose={() => setInstallOpen(false)}
        title={t("Review permissions and install")}
      >
        {installPreview ? (
          <div className="space-y-4" data-testid="studio-permission-sheet">
            <div className="rounded-lg border border-border bg-bg-subtle p-4">
              <div className="flex items-center justify-between gap-2">
                <div>
                  <div className="text-sm font-semibold text-fg">
                    {installPreview.manifest.name}
                  </div>
                  <div className="font-mono text-xs text-fg-muted">
                    {installPreview.manifest.id} v{installPreview.manifest.version}
                  </div>
                </div>
                <Badge variant="info">{installPreview.manifest.ether?.kind ?? t("stremio")}</Badge>
              </div>
              {installPreview.manifest.description ? (
                <p className="mt-2 text-xs text-fg-muted">{installPreview.manifest.description}</p>
              ) : null}
            </div>

            <div className="space-y-2">
              <div className="text-xs font-semibold text-fg">{t("Requested permissions")}</div>
              {installPreview.permissionLines.length === 0 ? (
                <p className="rounded-lg border border-border bg-bg-subtle px-3.5 py-2.5 text-xs text-fg-muted">
                  {t("Runs in the local sandbox with no network or catalogue permissions.")}
                </p>
              ) : (
                <ul className="space-y-1.5">
                  {installPreview.permissionLines.map((line, i) => (
                    <li
                      key={i}
                      className="flex items-center gap-2 rounded-lg border border-border bg-bg-subtle px-3.5 py-2 text-xs text-fg"
                    >
                      <Layers className="h-3.5 w-3.5 text-primary" />
                      <span>{line}</span>
                    </li>
                  ))}
                </ul>
              )}
            </div>

            <div className="flex items-center justify-between rounded-lg border border-border bg-bg-subtle px-3.5 py-2.5">
              <div>
                <div className="text-xs font-medium text-fg">{t("Start in shadow mode")}</div>
                <div className="text-xs text-fg-muted">
                  {t("Intercept writes until you turn live mode on.")}
                </div>
              </div>
              <Switch
                checked={installShadow}
                onChange={(e) => setInstallShadow(e.currentTarget.checked)}
                aria-label={t("Start in shadow mode")}
              />
            </div>

            <div className="flex justify-end gap-2 pt-3">
              <Button variant="secondary" onClick={() => setInstallOpen(false)}>
                {t("Cancel")}
              </Button>
              <Button
                variant="default"
                disabled={installing}
                onClick={() => void handleConfirmInstall()}
                data-testid="studio-confirm-install-button"
              >
                {t("Confirm and install")}
              </Button>
            </div>
          </div>
        ) : null}
      </Drawer>
    </div>
  );
}

export default Develop;
