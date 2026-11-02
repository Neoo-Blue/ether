import { Badge, Button, cn } from "@braedonsaunders/appkit-ui";
import { Activity, AlertTriangle, CheckCircle2, Info, RefreshCw } from "lucide-react";
import { useState } from "react";
import { Section } from "../../../components/layout";
import { t } from "../../../i18n";
import type { StudioCallRecord, StudioValidationHint } from "./types";

function formatJSON(raw?: string): string {
  if (!raw) return "{}";
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
}

export function InspectorPanel({
  calls,
  hints,
  logs,
  onRefresh,
}: {
  calls: StudioCallRecord[];
  hints: StudioValidationHint[];
  logs: string[];
  onRefresh: () => void;
}) {
  const [selectedCallId, setSelectedCallId] = useState<number | null>(null);
  const activeCall =
    calls.find((c) => c.id === selectedCallId) ?? (calls.length > 0 ? calls[0]! : null);

  return (
    <div className="space-y-4" data-testid="studio-inspector-panel">
      <Section
        title={t("Validation hints")}
        description={t(
          "Plain-language checks on the payloads your plugin returned, with suggested fixes.",
        )}
        actions={
          <Button size="sm" variant="secondary" onClick={onRefresh}>
            <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
            {t("Refresh inspector")}
          </Button>
        }
      >
        {hints.length === 0 ? (
          <div className="flex items-center gap-2 rounded-lg border border-border bg-bg-subtle px-4 py-3 text-sm text-fg-muted">
            <CheckCircle2 className="h-4 w-4 text-success" />
            <span>
              {t("No validation warnings on recent calls. Payloads match Ether contracts.")}
            </span>
          </div>
        ) : (
          <div className="space-y-2.5">
            {hints.map((h, idx) => (
              <div
                key={`${h.route}:${h.field ?? idx}:${idx}`}
                data-testid="inspector-validation-hint"
                className="rounded-lg border border-border bg-bg-subtle p-3.5"
              >
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="flex items-center gap-2">
                    {h.severity === "error" ? (
                      <AlertTriangle className="h-4 w-4 text-danger" />
                    ) : h.severity === "warning" ? (
                      <AlertTriangle className="h-4 w-4 text-warning" />
                    ) : (
                      <Info className="h-4 w-4 text-primary" />
                    )}
                    <span className="text-sm font-medium text-fg">{h.message}</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    {h.field ? (
                      <code className="rounded bg-surface px-1.5 py-0.5 font-mono text-xs text-fg-muted">
                        {h.field}
                      </code>
                    ) : null}
                    <Badge
                      variant={
                        h.severity === "error"
                          ? "destructive"
                          : h.severity === "warning"
                            ? "warning"
                            : "info"
                      }
                    >
                      {h.route}
                    </Badge>
                  </div>
                </div>
                <p className="mt-1.5 text-xs text-fg-muted">
                  <span className="font-medium text-fg">{t("Suggested fix:")}</span>{" "}
                  {h.suggestedFix}
                </p>
              </div>
            ))}
          </div>
        )}
      </Section>

      <Section
        title={t("Recorded calls")}
        description={t(
          "Every request Ether made to this plugin, with timing, HTTP status and redacted JSON payloads.",
        )}
      >
        {calls.length === 0 ? (
          <p className="text-sm text-fg-muted">
            {t("No calls recorded yet. Run a request or open the preview pane to record a call.")}
          </p>
        ) : (
          <div className="grid gap-4 lg:grid-cols-12">
            <div className="space-y-1.5 lg:col-span-5">
              {calls.map((c) => {
                const isSelected = activeCall?.id === c.id;
                const ok = c.status >= 200 && c.status < 300 && !c.error;
                return (
                  <button
                    key={c.id}
                    type="button"
                    data-testid={`inspector-call-row-${c.id}`}
                    onClick={() => setSelectedCallId(c.id)}
                    className={cn(
                      "flex w-full items-center justify-between gap-2 rounded-lg border px-3 py-2.5 text-left text-xs transition",
                      isSelected
                        ? "border-primary bg-primary/5"
                        : "border-border bg-bg-subtle hover:bg-surface",
                    )}
                  >
                    <div className="min-w-0">
                      <div className="flex items-center gap-1.5">
                        <Badge variant={ok ? "success" : "destructive"}>{c.status}</Badge>
                        <span className="font-mono font-semibold text-fg">{c.method}</span>
                        <span className="truncate font-mono text-fg">{c.route}</span>
                      </div>
                      {c.error ? <div className="mt-1 truncate text-danger">{c.error}</div> : null}
                    </div>
                    <span className="shrink-0 font-mono text-fg-muted">
                      {t("{ms} ms", { ms: c.ms })}
                    </span>
                  </button>
                );
              })}
            </div>

            {activeCall ? (
              <div className="space-y-3 lg:col-span-7">
                <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-border bg-bg-subtle px-3.5 py-2 text-xs">
                  <div className="flex items-center gap-2">
                    <Activity className="h-3.5 w-3.5 text-primary" />
                    <span className="font-mono font-semibold text-fg">
                      {activeCall.method} {activeCall.route}
                    </span>
                  </div>
                  <div className="flex items-center gap-2 text-fg-muted">
                    <span>{t("Status {code}", { code: activeCall.status })}</span>
                    <span>•</span>
                    <span>{t("{ms} ms", { ms: activeCall.ms })}</span>
                  </div>
                </div>

                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="space-y-1">
                    <div className="text-xs font-medium text-fg-muted">{t("Redacted request")}</div>
                    <pre
                      data-testid="inspector-request-json"
                      className="max-h-60 overflow-auto rounded-lg border border-border bg-bg-subtle p-3 font-mono text-xs text-fg"
                    >
                      {formatJSON(activeCall.request)}
                    </pre>
                  </div>
                  <div className="space-y-1">
                    <div className="text-xs font-medium text-fg-muted">
                      {t("Redacted response")}
                    </div>
                    <pre
                      data-testid="inspector-response-json"
                      className="max-h-60 overflow-auto rounded-lg border border-border bg-bg-subtle p-3 font-mono text-xs text-fg"
                    >
                      {formatJSON(activeCall.response)}
                    </pre>
                  </div>
                </div>
              </div>
            ) : null}
          </div>
        )}
      </Section>

      {logs.length > 0 ? (
        <Section
          title={t("Sandbox log output")}
          description={t("Structured log lines emitted via ether.log during recent calls.")}
        >
          <pre className="max-h-48 overflow-auto rounded-lg border border-border bg-bg-subtle p-3 font-mono text-xs text-fg">
            {logs.join("\n")}
          </pre>
        </Section>
      ) : null}
    </div>
  );
}
