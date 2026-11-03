import Editor, { DiffEditor, loader, type Monaco } from "@monaco-editor/react";
import * as monaco from "monaco-editor";
import * as ts from "monaco-editor/languages/features/typescript/register.js";
import EditorWorker from "monaco-editor/editor/editor.worker?worker";
import JsonWorker from "monaco-editor/language/json/json.worker?worker";
import TsWorker from "monaco-editor/language/typescript/ts.worker?worker";
import { SDK_DTS } from "./sdk";

// Bundled locally: no CDN, works offline, lazy-loaded with the editor route.
self.MonacoEnvironment = {
  getWorker(_id: string, label: string) {
    if (label === "typescript" || label === "javascript") return new TsWorker();
    if (label === "json") return new JsonWorker();
    return new EditorWorker();
  },
};
loader.config({ monaco });

function token(name: string): string {
  const raw = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  const parts = raw
    .split(/[\s,/]+/)
    .map(Number)
    .slice(0, 3);
  if (parts.length !== 3 || parts.some(Number.isNaN)) return "#888888";
  return `#${parts.map((n) => n.toString(16).padStart(2, "0")).join("")}`;
}

/** Ether's tokens as a Monaco theme, read from the live page so light and dark match. */
function defineEtherTheme(m: Monaco): string {
  const dark = document.documentElement.classList.contains("dark");
  const name = dark ? "ether-dark" : "ether-light";
  const c = (n: string) => token(n).slice(1);
  m.editor.defineTheme(name, {
    base: dark ? "vs-dark" : "vs",
    inherit: true,
    rules: [
      { token: "keyword", foreground: c("--ch-accent-violet") },
      { token: "string", foreground: c("--ch-accent-emerald") },
      { token: "number", foreground: c("--ch-accent-amber") },
      { token: "type", foreground: c("--ch-accent-rose") },
      { token: "identifier", foreground: c("--ch-fg") },
      { token: "comment", foreground: c("--ch-fg-subtle"), fontStyle: "italic" },
      { token: "delimiter", foreground: c("--ch-fg-muted") },
    ],
    colors: {
      "editor.background": token("--ch-surface"),
      "editor.foreground": token("--ch-fg"),
      "editorLineNumber.foreground": token("--ch-fg-subtle"),
      "editorLineNumber.activeForeground": token("--ch-fg-muted"),
      "editor.lineHighlightBackground": token("--ch-bg-subtle"),
      "editor.lineHighlightBorder": token("--ch-bg-subtle"),
      "editorCursor.foreground": token("--ch-primary"),
      "editor.selectionBackground": `${token("--ch-primary")}33`,
      "editorWidget.background": token("--ch-elevated"),
      "editorWidget.border": token("--ch-border"),
      "editorSuggestWidget.background": token("--ch-elevated"),
      "editorSuggestWidget.border": token("--ch-border"),
      "editorSuggestWidget.selectedBackground": `${token("--ch-primary")}26`,
      "editorHoverWidget.background": token("--ch-elevated"),
      "editorHoverWidget.border": token("--ch-border"),
      "editorError.foreground": token("--ch-danger"),
      "diffEditor.insertedTextBackground": `${token("--ch-success")}26`,
      "diffEditor.removedTextBackground": `${token("--ch-danger")}26`,
      "diffEditor.insertedLineBackground": `${token("--ch-success")}14`,
      "diffEditor.removedLineBackground": `${token("--ch-danger")}14`,
    },
  });
  return name;
}

let sdkLoaded = false;
function prepare(m: Monaco) {
  if (!sdkLoaded) {
    ts.typescriptDefaults.setCompilerOptions({
      target: ts.ScriptTarget.ES2020,
      module: ts.ModuleKind.ESNext,
      moduleResolution: ts.ModuleResolutionKind.NodeJs,
      strict: true,
      allowNonTsExtensions: true,
    });
    ts.typescriptDefaults.addExtraLib(SDK_DTS, "file:///node_modules/ether/index.d.ts");
    sdkLoaded = true;
  }
  return defineEtherTheme(m);
}

const OPTIONS = {
  fontSize: 13,
  lineHeight: 21,
  fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
  minimap: { enabled: false },
  scrollBeyondLastLine: false,
  renderLineHighlight: "line" as const,
  padding: { top: 12, bottom: 12 },
  overviewRulerBorder: false,
  hideCursorInOverviewRuler: true,
  automaticLayout: true,
  tabSize: 2,
  quickSuggestions: true,
  scrollbar: { verticalScrollbarSize: 10, horizontalScrollbarSize: 10 },
};

export default function MonacoPane({
  file,
  value,
  onChange,
  diff,
}: {
  file: string;
  value: string;
  onChange: (value: string) => void;
  /** When set, shows the agent's proposed change as a diff. */
  diff?: string;
}) {
  const language = file.endsWith(".ts") ? "typescript" : "json";
  const path = `file:///${file}`;
  if (diff !== undefined)
    return (
      <DiffEditor
        original={value}
        modified={diff}
        language={language}
        originalModelPath={`${path}.before`}
        modifiedModelPath={`${path}.after`}
        beforeMount={(m) => void prepare(m)}
        onMount={(_e, m) => m.editor.setTheme(prepare(m))}
        options={{ ...OPTIONS, renderSideBySide: false, readOnly: true }}
      />
    );
  return (
    <Editor
      path={path}
      value={value}
      language={language}
      onChange={(v) => onChange(v ?? "")}
      beforeMount={(m) => void prepare(m)}
      onMount={(_e, m) => m.editor.setTheme(prepare(m))}
      options={OPTIONS}
    />
  );
}
