import { Button, Skeleton, Textarea, cn } from "@braedonsaunders/appkit-ui";
import { Check, FileCode2, FileJson, Languages, Settings2, Sparkles, Wand2, X } from "lucide-react";
import { Suspense, lazy, useEffect, useState, type ReactNode } from "react";
import { t } from "../../i18n";
import { MANIFEST, SETTINGS, STRINGS, pluginSource } from "./code/sdk";

const MonacoPane = lazy(() => import("./code/MonacoPane"));
const CodeMirrorPane = lazy(() => import("./code/CodeMirrorPane"));

type FileKey = "plugin.ts" | "manifest.json" | "strings.json" | "settings.schema.json";

const FILES: { key: FileKey; icon: ReactNode; label: string }[] = [
  { key: "plugin.ts", icon: <FileCode2 size={14} />, label: "plugin.ts" },
  { key: "manifest.json", icon: <FileJson size={14} />, label: "manifest.json" },
  { key: "strings.json", icon: <Languages size={14} />, label: "strings.json" },
  { key: "settings.schema.json", icon: <Settings2 size={14} />, label: "settings.schema.json" },
];

export type Engine = "monaco" | "codemirror";

/** Session-owned drafts survive switching between Describe, Blocks and Code. */
export function useCodeDraft(skipKids: boolean) {
  const [docs, setDocs] = useState<Record<FileKey, string>>(() => ({
    "plugin.ts": pluginSource(skipKids), "manifest.json": MANIFEST,
    "strings.json": STRINGS, "settings.schema.json": SETTINGS,
  }));
  const [dirty, setDirty] = useState(false);
  useEffect(() => {
    setDocs(d => d["plugin.ts"] === pluginSource(!skipKids)
      ? { ...d, "plugin.ts": pluginSource(skipKids) } : d);
  }, [skipKids]);
  return { docs, setDocs, dirty, setDirty };
}

/** Edits are retained locally; this prototype never compiles or executes them. */
export function CodeView({ engine, onSkipKids, preview, draft }: {
  engine: Engine;
  onSkipKids: (v: boolean) => void;
  preview: ReactNode;
  draft: ReturnType<typeof useCodeDraft>;
}) {
  const [file, setFile] = useState<FileKey>("plugin.ts");
  const { docs, setDocs, dirty, setDirty } = draft;
  const [ask, setAsk] = useState<"idle" | "change" | "explain" | "diff">("idle");
  const [change, setChange] = useState("");

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "s") e.preventDefault();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const proposed = ask === "diff" ? pluginSource(true) : undefined;

  return (
    <div className="app-scroll min-h-0 flex-1 overflow-y-auto lg:grid lg:grid-cols-5 lg:overflow-hidden">
      <section
        aria-label={t("Code")}
        className="flex min-h-[32rem] flex-col border-b border-border bg-surface lg:col-span-3 lg:min-h-0 lg:border-b-0 lg:border-r"
      >
        <div className="flex shrink-0 flex-wrap items-center gap-1 border-b border-border bg-bg-subtle px-2">
          {FILES.map((f) => (
            <button
              key={f.key}
              type="button"
              onClick={() => setFile(f.key)}
              aria-current={file === f.key ? "page" : undefined}
              className={cn(
                "-mb-px inline-flex h-10 shrink-0 items-center gap-1.5 border-b-2 px-3 font-mono text-xs transition",
                file === f.key
                  ? "border-primary bg-surface text-fg"
                  : "border-transparent text-fg-muted hover:text-fg",
              )}
            >
              {f.icon} {f.label}
              {f.key === "plugin.ts" && dirty ? (
                <span className="size-1.5 rounded-full bg-primary" aria-label={t("Not saved")} />
              ) : null}
            </button>
          ))}
          <div className="ml-auto flex shrink-0 items-center gap-1 pl-2">
            <Button variant="ghost" size="sm" onClick={() => setAsk("explain")}>
              <Sparkles size={14} /> {t("Explain")}
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setAsk("change")}>
              <Wand2 size={14} /> {t("Change this…")}
            </Button>
          </div>
        </div>

        {ask === "explain" ? (
          <AgentNote onClose={() => setAsk("idle")}>
            {t(
              "Finds 4K titles rated 7 or more, drops any a viewer has started, then sorts the rest by rating.",
            )}
          </AgentNote>
        ) : null}
        {ask === "change" ? (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              if (change.trim()) setAsk("diff");
            }}
            className="flex shrink-0 items-center gap-2 border-b border-border bg-primary/5 px-3 py-2"
          >
            <Wand2 size={15} className="shrink-0 text-primary" aria-hidden />
            <Textarea
              autoFocus
              rows={1}
              value={change}
              onChange={(e) => setChange(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.shiftKey) {
                  e.preventDefault();
                  if (change.trim()) setAsk("diff");
                }
              }}
              aria-label={t("Change this")}
              placeholder={t("Describe the change to the selected code")}
              className="min-h-8 flex-1 resize-none border-0 bg-transparent py-1 shadow-none focus-visible:ring-0"
            />
            <Button size="sm" type="submit" disabled={!change.trim()}>
              {t("Change")}
            </Button>
          </form>
        ) : null}
        {ask === "diff" ? (
          <div className="flex shrink-0 items-center gap-2 border-b border-border bg-primary/5 px-3 py-2 text-sm">
            <Wand2 size={15} className="shrink-0 text-primary" aria-hidden />
            <span className="flex-1 text-fg">{t("Skips kids' titles with one filter")}</span>
            <Button size="sm" variant="ghost" onClick={() => setAsk("idle")}>
              <X size={14} /> {t("Reject")}
            </Button>
            <Button
              size="sm"
              onClick={() => {
                onSkipKids(true);
                setAsk("idle");
                setDocs(d => ({ ...d, "plugin.ts": pluginSource(true) }));
                setDirty(true);
              }}
            >
              <Check size={14} /> {t("Accept")}
            </Button>
          </div>
        ) : null}

        <div className="min-h-0 flex-1">
          <Suspense fallback={<EditorSkeleton />}>
            {engine === "monaco" ? (
              <MonacoPane
                file={file}
                value={docs[file]}
                diff={file === "plugin.ts" ? proposed : undefined}
                onChange={(v) => {
                  setDocs((d) => ({ ...d, [file]: v }));
                  setDirty(true);
                }}
              />
            ) : (
              <CodeMirrorPane
                key={`${file}-${proposed ? "diff" : "edit"}`}
                file={file}
                value={file === "plugin.ts" && proposed ? proposed : docs[file]}
                original={file === "plugin.ts" && proposed ? docs[file] : undefined}
                onChange={(v) => {
                  setDocs((d) => ({ ...d, [file]: v }));
                  setDirty(true);
                }}
              />
            )}
          </Suspense>
        </div>
        <div className="flex shrink-0 items-center justify-between gap-2 border-t border-border bg-bg-subtle px-3 py-1.5 text-xs text-fg-muted">
          <span>{dirty ? t("Unsaved draft · kept in this Studio session · not executed") : t("Sample code · not executed")}</span>
          <span className="font-mono">TypeScript · {engine === "monaco" ? "Monaco" : "CodeMirror"}</span>
        </div>
      </section>
      <section aria-label={t("Preview")} className="min-w-0 bg-bg lg:col-span-2 lg:min-h-0 lg:overflow-y-auto">
        <p className="border-b border-border px-4 py-2 text-xs text-fg-muted">{t("Sample preview · independent of code edits")}</p>
        {preview}
      </section>
    </div>
  );
}

function AgentNote({ children, onClose }: { children: ReactNode; onClose: () => void }) {
  return (
    <div className="flex shrink-0 items-start gap-2 border-b border-border bg-primary/5 px-3 py-2.5 text-sm text-fg">
      <Sparkles size={15} className="mt-0.5 shrink-0 text-primary" aria-hidden />
      <p className="flex-1">{children}</p>
      <Button variant="ghost" size="icon" aria-label={t("Close")} onClick={onClose} className="size-7">
        <X size={14} />
      </Button>
    </div>
  );
}

function EditorSkeleton() {
  return (
    <div className="space-y-2.5 p-4">
      {[72, 40, 88, 64, 52, 80, 36].map((w, i) => (
        <Skeleton key={i} className="h-3.5" style={{ width: `${w}%` }} />
      ))}
    </div>
  );
}
