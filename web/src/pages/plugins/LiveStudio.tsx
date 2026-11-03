import { AgentPanel, type AgentMessage } from "@braedonsaunders/appkit-ai/react";
import { Badge, Button, SearchSelect, Textarea } from "@braedonsaunders/appkit-ui";
import { useCallback, useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { Workspace } from "../../components/workspace";
import {
  decideAgentApproval,
  fetchAgentMessages,
  fetchAgentStatus,
} from "../../components/agent/agentApi";
import { approvalRequestLabels } from "../../components/agent/approvalLabels";
import { apiFetch, apiStream, errorMessage } from "../../lib/api";
import { PluginView } from "../../viewspec/PluginView";
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
              t("A page listing this Friday's three screenings with times and screens"),
              t("A shelf of my unwatched movies that are under 100 minutes"),
              t("Send me a notification when a new episode of a show I follow is available"),
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
          </div>
          {project ? (
            <>
              <h2 className="text-lg font-semibold">{project.name}</h2>
              <p className="mb-4 text-xs text-fg-muted">
                {dirty ? t("Unsaved draft") : t("Saved on server")} · {project.updatedAt}
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
    </Workspace>
  );
}
