import CodeMirror from "@uiw/react-codemirror";
import { javascript } from "@codemirror/lang-javascript";
import { json } from "@codemirror/lang-json";
import { useMemo } from "react";
import { t } from "../../../i18n";
import type { StudioDiagnostic } from "./types";

export default function CodeEditor({
  file,
  value,
  diagnostics,
  onChange,
}: {
  file: string;
  value: string;
  diagnostics: StudioDiagnostic[];
  onChange: (next: string) => void;
}) {
  const extensions = useMemo(() => {
    if (file.endsWith(".json")) {
      return [json()];
    }
    return [javascript({ typescript: true })];
  }, [file]);

  const fileDiagnostics = useMemo(
    () => diagnostics.filter((d) => d.file === file || d.file.endsWith(`/${file}`)),
    [diagnostics, file],
  );

  return (
    <div className="flex flex-col" data-testid="studio-code-editor">
      <div className="overflow-hidden rounded-b-lg border-t border-border text-sm">
        <CodeMirror
          value={value}
          height="420px"
          extensions={extensions}
          onChange={onChange}
          basicSetup={{
            lineNumbers: true,
            foldGutter: true,
            highlightActiveLine: true,
          }}
        />
      </div>
      {fileDiagnostics.length > 0 ? (
        <div
          role="region"
          aria-label={t("File diagnostics")}
          className="space-y-1.5 border-t border-border bg-bg-subtle px-4 py-2.5"
        >
          {fileDiagnostics.map((d, idx) => (
            <div
              key={`${d.file}:${d.line}:${d.column}:${idx}`}
              className="flex items-start gap-2 font-mono text-xs"
            >
              <span
                className={
                  d.severity === "error"
                    ? "font-semibold text-danger"
                    : "font-semibold text-warning"
                }
              >
                {t("Line {line}:{col}", { line: d.line, col: d.column })}
              </span>
              <span className="text-fg">{d.text}</span>
            </div>
          ))}
        </div>
      ) : null}
    </div>
  );
}
