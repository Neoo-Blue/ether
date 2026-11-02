import { Badge, Button, Switch } from "@braedonsaunders/appkit-ui";
import { RotateCcw, Shield } from "lucide-react";
import { Section } from "../../../components/layout";
import { t } from "../../../i18n";
import type { StudioJournalEntry } from "./types";

function describeEntry(entry: StudioJournalEntry): string {
  switch (entry.op) {
    case "collections.upsert":
      return entry.shadow
        ? t("Would create or update collection {name}", { name: entry.detail })
        : t("Updated collection {name}", { name: entry.detail });
    case "viewers.setWatchState":
      return entry.shadow
        ? t("Would update watch state on {title}", { title: entry.detail })
        : t("Updated watch state on {title}", { title: entry.detail });
    case "notify.owner":
      return entry.shadow
        ? t("Would send owner notice: {text}", { text: entry.detail })
        : t("Sent owner notice: {text}", { text: entry.detail });
    default:
      return `${entry.op}: ${entry.detail}`;
  }
}

export function ShadowUndoPanel({
  shadow,
  entries,
  busy,
  onToggleShadow,
  onUndo,
}: {
  shadow: boolean;
  entries: StudioJournalEntry[];
  busy: boolean;
  onToggleShadow: (next: boolean) => void;
  onUndo: () => void;
}) {
  const activeEntries = entries.filter((e) => !e.undone);
  const shadowEntries = entries.filter((e) => e.shadow && !e.undone);

  return (
    <div className="space-y-4" data-testid="studio-shadow-undo-panel">
      <Section
        title={t("Shadow mode")}
        description={t(
          "Run the plugin normally while intercepting collection, watch-state and notification writes into a dry-run report.",
        )}
        actions={
          <div className="flex items-center gap-2">
            <Switch
              checked={shadow}
              onChange={(e) => onToggleShadow(e.currentTarget.checked)}
              aria-label={t("Shadow mode")}
              data-testid="shadow-mode-switch"
            />
            <Badge variant={shadow ? "info" : "secondary"}>
              {shadow ? t("Shadow mode on") : t("Live writes on")}
            </Badge>
          </div>
        }
      >
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border bg-bg-subtle px-4 py-3 text-sm">
          <div className="flex items-center gap-2.5">
            <Shield className="h-4 w-4 text-primary" />
            <span className="text-fg">
              {shadow
                ? t(
                    "Writes are intercepted and recorded below without changing live collections or watch history.",
                  )
                : t("Writes apply to the sandbox state and record an inverse in the undo journal.")}
            </span>
          </div>
          {shadowEntries.length > 0 ? (
            <Badge variant="info">
              {t("{count} shadow writes recorded", { count: shadowEntries.length })}
            </Badge>
          ) : null}
        </div>
      </Section>

      <Section
        title={t("Write journal and undo")}
        description={t(
          "Every collection, watch-state or notice operation records an inverse so you can revert plugin writes in one click.",
        )}
        actions={
          <Button
            size="sm"
            variant="secondary"
            disabled={busy || activeEntries.length === 0}
            onClick={onUndo}
            data-testid="studio-undo-button"
          >
            <RotateCcw className="mr-1.5 h-3.5 w-3.5" />
            {t("Undo plugin writes")}
          </Button>
        }
      >
        {entries.length === 0 ? (
          <p className="text-sm text-fg-muted">
            {t(
              "No writes recorded yet. Trigger a sync or action route to see what this plugin writes.",
            )}
          </p>
        ) : (
          <div className="space-y-2">
            {entries.map((entry) => (
              <div
                key={entry.id}
                data-testid={`journal-entry-${entry.id}`}
                className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-border bg-bg-subtle px-3.5 py-2.5 text-xs"
              >
                <div className="flex items-center gap-2">
                  <Badge variant={entry.undone ? "secondary" : entry.shadow ? "info" : "success"}>
                    {entry.undone ? t("Undone") : entry.shadow ? t("Shadow") : t("Applied")}
                  </Badge>
                  <code className="rounded bg-surface px-1.5 py-0.5 font-mono text-fg-muted">
                    {entry.op}
                  </code>
                  <span className="text-sm text-fg">{describeEntry(entry)}</span>
                </div>
              </div>
            ))}
          </div>
        )}
      </Section>
    </div>
  );
}
