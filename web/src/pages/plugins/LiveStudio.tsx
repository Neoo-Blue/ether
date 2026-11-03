import { AgentPanel, type AgentMessage } from "@braedonsaunders/appkit-ai/react";
import { Badge, Button, Dialog, SearchSelect, Textarea, toast } from "@braedonsaunders/appkit-ui";
import { useCallback, useEffect, useRef, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { Workspace } from "../../components/workspace";
import {
  decideAgentApproval,
  fetchAgentMessages,
  fetchAgentStatus,
} from "../../components/agent/agentApi";
import { approvalRequestLabels } from "../../components/agent/approvalLabels";
import { apiFetch, apiStream, errorMessage } from "../../lib/api";
import { PluginView } from "../../viewspec/PluginView";
import { formatDateTime } from "../../lib/format";
import { invalidateRegistry } from "../../lib/extend";
import { PermissionSheet } from "./PermissionSheet";
import type { InstallPreview } from "./types";
import { t } from "../../i18n";
import { searchSelectLabels } from "../../i18n/appkit-labels";
import type { StudioProject, StudioProjectSummary, StudioCompileResult } from "./develop/types";

/** Real runtime checkpoint. The mock design remains available in mock mode. */
export default function LiveStudio() {
  const [messages, setMessages] = useState<AgentMessage[]>([]);
  const thread = useRef<string | undefined>(
    sessionStorage.getItem("ether-studio-thread") || undefined,
  );
  const [projects, setProjects] = useState<StudioProjectSummary[]>([]);
  const [project, setProject] = useState<StudioProject | null>(null);
  const [file, setFile] = useState("src/plugin.ts");
  const [source, setSource] = useState("");
  const [dirty, setDirty] = useState(false);
  const [enabled, setEnabled] = useState(false);
  const [model, setModel] = useState("");
  const [error, setError] = useState("");
  const [view, setView] = useState<"preview" | "code">("preview");
  const [epoch, setEpoch] = useState(0);
  const [saving, setSaving] = useState(false);
  const chosen = useRef<string | undefined>(undefined);
  const stamps = useRef<Record<string, string>>({});
  const dirtyRef = useRef(false);
  dirtyRef.current = dirty;
  const loadProject = useCallback(async (id: string) => {
    const r = await apiFetch<{ project: StudioProject }>(
      `/plugins/studio/projects/${encodeURIComponent(id)}`,
    );
    setProject(r.project);
    chosen.current = id;
    if (!dirtyRef.current) {
      setFile("src/plugin.ts");
      setSource(r.project.files["src/plugin.ts"] ?? "");
    }
    setEpoch((e) => e + 1);
  }, []);
  const refresh = useCallback(async () => {
    if (thread.current) {
      const r = await fetchAgentMessages(thread.current);
      setMessages(r.messages);
    }
    const r = await apiFetch<{ projects: StudioProjectSummary[] }>("/plugins/studio/projects");
    setProjects(r.projects);
    // Follow the draft the agent just created or changed; otherwise keep the
    // owner's pick. First load opens the most recently edited draft.
    const latest = [...r.projects].sort(
      (a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt),
    );
    const changed = latest.find((p) => stamps.current[p.id] !== p.updatedAt);
    const first = Object.keys(stamps.current).length === 0;
    stamps.current = Object.fromEntries(r.projects.map((p) => [p.id, p.updatedAt]));
    const follow = !dirtyRef.current && (first ? latest[0]?.id : changed?.id);
    const id = follow || chosen.current || latest[0]?.id;
    if (id) await loadProject(id);
  }, [loadProject]);
  useEffect(() => {
    void fetchAgentStatus()
      .then((s) => {
        setEnabled(s.enabled);
        setModel(s.model || "");
      })
      .catch((e) => setError(errorMessage(e)));
    void refresh().catch((e) => setError(errorMessage(e)));
  }, [refresh]);
  const send = async (prompt: string, signal: AbortSignal) => {
    const response = await apiStream("/agent/turn", {
      method: "POST",
      signal,
      body: JSON.stringify({
        threadId: thread.current,
        prompt,
        context: {
          route: "/plugins/studio",
          record: project ? { id: project.id, type: "studio_project" } : undefined,
        },
      }),
    });
    const id = response.headers.get("X-Ether-Thread-Id");
    if (id) {
      thread.current = id;
      sessionStorage.setItem("ether-studio-thread", id);
    }
    if (!response.ok || !response.body) return response;
    const reader = response.body.getReader();
    return new Response(
      new ReadableStream<Uint8Array>({
        async pull(controller) {
          try {
            const r = await reader.read();
            if (r.done) {
              controller.close();
              void refresh().catch((e) => setError(errorMessage(e)));
            } else controller.enqueue(r.value);
          } catch (e) {
            controller.error(e);
          }
        },
        cancel(reason) {
          return reader.cancel(reason);
        },
      }),
      { status: response.status, headers: response.headers },
    );
  };
  const navigate = useNavigate();
  const [installing, setInstalling] = useState<InstallPreview | null>(null);
  const [grantViewers, setGrantViewers] = useState<number[]>([]);
  const [grantFolders, setGrantFolders] = useState<string[]>([]);
  const [installBusy, setInstallBusy] = useState(false);
  const [installError, setInstallError] = useState("");
  const openInstall = async () => {
    if (!project) return;
    setInstallError("");
    setGrantViewers([]);
    setGrantFolders([]);
    try {
      const r = await apiFetch<{ preview: InstallPreview }>(
        `/plugins/studio/projects/${encodeURIComponent(project.id)}/install/preview`,
        { method: "POST", body: "{}" },
      );
      setInstalling(r.preview);
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  const install = async () => {
    if (!project || !installing) return;
    setInstallBusy(true);
    setInstallError("");
    try {
      const r = await apiFetch<{ plugin: { id: string; name: string } }>(
        `/plugins/studio/projects/${encodeURIComponent(project.id)}/install`,
        {
          method: "POST",
          body: JSON.stringify({
            grants: { viewers: grantViewers, folders: grantFolders },
            acceptAddedPermissions: true,
          }),
        },
      );
      setInstalling(null);
      invalidateRegistry();
      toast.success(t("{name} is installed", { name: r.plugin.name }));
      navigate("/plugins/installed");
    } catch (e) {
      setInstallError(errorMessage(e));
    } finally {
      setInstallBusy(false);
    }
  };
  const save = async () => {
    if (!project) return;
    setSaving(true);
    setError("");
    try {
      const r = await apiFetch<{ project: StudioProject; compile: StudioCompileResult }>(
        `/plugins/studio/projects/${encodeURIComponent(project.id)}/files`,
        { method: "PUT", body: JSON.stringify({ files: { [file]: source } }) },
      );
      setProject(r.project);
      setDirty(false);
      setEpoch((e) => e + 1);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setSaving(false);
    }
  };
  return (
    <Workspace title={t("Studio")} actions={<Link to="/plugins">{t("Plugins")}</Link>}>
      <div className="grid min-h-0 flex-1 grid-cols-1 lg:grid-cols-[minmax(320px,0.85fr)_minmax(0,1.15fr)]">
        <section
          className="order-2 flex min-h-[500px] flex-col border-t border-border lg:order-1 lg:border-r lg:border-t-0"
          aria-label={t("Studio conversation")}
        >
          <div className="flex items-center gap-2 border-b border-border p-3">
            <Badge>{t("Live model")}</Badge>
            <span className="text-xs text-fg-muted">{model || t("Model unavailable")}</span>
          </div>
          <AgentPanel
            enabled={enabled}
            initialMessages={messages}
            send={send}
            suggestions={[
              t("Add free live channels to Live TV, with a programme guide"),
              t("Find free, legal copies of classic films for movies in my catalogue"),
              t("Make a Home row from an external list of films"),
            ]}
            labels={{
              title: t("Build with Ether"),
              welcomeTitle: t("Describe your plugin"),
              welcomeDescription: t(
                "Real model responses and saved drafts. Review source changes before applying them.",
              ),
              disabledTitle: t("Connect a model"),
            }}
            approvalRequestLabels={approvalRequestLabels()}
            onDecideApprovalRequest={async (id, decision, note) => {
              await decideAgentApproval(id, decision, note, {
                route: "/plugins/studio",
                record: project ? { id: project.id, type: "studio_project" } : undefined,
              });
              await refresh();
            }}
          />
        </section>
        <section
          className="order-1 min-h-[420px] overflow-auto p-5 lg:order-2"
          aria-label={t("Draft workspace")}
        >
          {error ? (
            <p role="alert" className="mb-4 text-danger">
              {error}
            </p>
          ) : null}
          <div className="mb-4 flex flex-wrap items-center gap-2">
            <SearchSelect
              value={project?.id || ""}
              onChange={(id) => {
                if (!dirty) void loadProject(id).catch((e) => setError(errorMessage(e)));
              }}
              options={projects.map((p) => ({ value: p.id, label: p.name }))}
              ariaLabel={t("Saved project")}
              disabled={dirty}
              {...searchSelectLabels()}
            />
            <Button
              variant={view === "preview" ? "default" : "outline"}
              onClick={() => setView("preview")}
            >
              {t("Preview")}
            </Button>
            <Button
              variant={view === "code" ? "default" : "outline"}
              onClick={() => setView("code")}
            >
              {t("Code")}
            </Button>
            {project ? (
              <Button
                className="ml-auto"
                disabled={dirty || (project.diagnostics || []).length > 0}
                onClick={() => void openInstall()}
              >
                {t("Install")}
              </Button>
            ) : null}
          </div>
          {project ? (
            <>
              <h2 className="text-lg font-semibold">{project.name}</h2>
              <p className="mb-4 text-xs text-fg-muted">
                {dirty ? t("Unsaved draft") : t("Saved on server")} ·{" "}
                {formatDateTime(project.updatedAt)}
              </p>
              {(project.diagnostics || []).map((d, i) => (
                <p key={i} className="mb-2 text-sm text-danger">
                  {d.file}:{d.line} {d.text}
                </p>
              ))}
              {view === "code" ? (
                <div className="space-y-3">
                  <SearchSelect
                    value={file}
                    onChange={(f) => {
                      setFile(f);
                      setSource(project.files[f] || "");
                    }}
                    disabled={dirty}
                    options={Object.keys(project.files).map((f) => ({ value: f, label: f }))}
                    ariaLabel={t("Source file")}
                    {...searchSelectLabels()}
                  />
                  <Textarea
                    aria-label={t("Plugin source")}
                    className="min-h-[480px] font-mono text-xs"
                    value={source}
                    onChange={(e) => {
                      setSource(e.currentTarget.value);
                      setDirty(true);
                    }}
                  />
                  <Button disabled={!dirty || saving} onClick={() => void save()}>
                    {t("Save and compile")}
                  </Button>
                </div>
              ) : project.defaultPage ? (
                <div key={`${project.id}:${epoch}`} className="rounded-xl border border-border p-4">
                  <PluginView plugin={`studio:${project.id}`} page={project.defaultPage} />
                </div>
              ) : (
                <p>
                  {t(
                    "This draft has no page preview. Open Code to inspect its capability implementation.",
                  )}
                </p>
              )}
              <p className="mt-4 text-xs text-fg-muted">
                {t(
                  "Preview runs the saved draft in the Studio sandbox. It does not install the plugin.",
                )}
              </p>
            </>
          ) : (
            <div className="rounded-xl border border-dashed border-border p-8">
              <h2 className="font-medium">{t("Your plugin will appear here")}</h2>
              <p className="mt-2 text-sm text-fg-muted">
                {t(
                  "Start with a description. Approved draft changes are saved and compiled by Ether.",
                )}
              </p>
            </div>
          )}
        </section>
      </div>
      {installing ? (
        <Dialog
          open
          onClose={() => setInstalling(null)}
          title={t("Install {name}", { name: installing.manifest.name })}
          footer={
            <>
              <Button variant="ghost" onClick={() => setInstalling(null)}>
                {t("Cancel")}
              </Button>
              <Button disabled={installBusy} onClick={() => void install()}>
                {installBusy
                  ? t("Installing…")
                  : installing.alreadyInstalled
                    ? t("Update")
                    : t("Install")}
              </Button>
            </>
          }
        >
          <div className="space-y-4">
            <p className="text-sm text-fg-muted">
              {installing.alreadyInstalled
                ? t("This replaces version {version} with the saved draft.", {
                    version: installing.installedVersion ?? "",
                  })
                : t("The saved draft becomes a real plugin on this server.")}
            </p>
            <PermissionSheet
              permissions={installing.permissions}
              verified={installing.verified}
              needsViewers={installing.needsViewers}
              needsFolders={installing.needsFolders}
              viewers={grantViewers}
              onChangeViewers={setGrantViewers}
              folders={grantFolders}
              onChangeFolders={setGrantFolders}
              addedPermissions={installing.addedPermissions}
              ownDraft
            />
            {installError ? (
              <p role="alert" className="text-sm text-danger">
                {installError}
              </p>
            ) : null}
          </div>
        </Dialog>
      ) : null}
    </Workspace>
  );
}
