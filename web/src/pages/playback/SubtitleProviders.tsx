import { Button } from "@braedonsaunders/appkit-ui";
import { Plus } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { SubtitleCheck } from "./SubtitleCheck";
import { AddAddonDrawer } from "../../components/addons/AddAddonDrawer";
import { ErrorPanel, LoadingRows } from "../../components/feedback";
import { t } from "../../i18n";
import { api, errorMessage } from "../../lib/api";
import { textOf, useRegistry } from "../../lib/extend";
import { useAsync } from "../../lib/hooks";
import type { EtherConfig } from "../../lib/types";

type Provider = { id: string; kind: string; name?: string; enabled: boolean };

export function SubtitleProviders() {
  const config = useAsync(() => api.getConfig(), []);
  const registry = useRegistry("subtitleProvider");
  const [checking, setChecking] = useState<{ id: string; name: string } | null>(null);
  const [adding, setAdding] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const configured: Provider[] = config.data?.subtitles?.providers ?? [
    { id: "opensubtitles", kind: "opensubtitles", enabled: true },
  ];
  const providers: Provider[] = [
    ...configured,
    ...registry.descriptors
      .filter((d) => d.owner !== "builtin" && !configured.some((p) => p.kind === d.id))
      .map((d) => ({ id: d.id, kind: d.id, enabled: true })),
  ];
  const save = async (next: Provider[]) => {
    setBusy(true);
    setError("");
    try {
      const live = await api.getConfig();
      await api.putConfig({ ...live, subtitles: { providers: next } } as EtherConfig);
      await config.reload();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  const move = (index: number, step: number) => {
    const next = [...providers];
    const target = index + step;
    if (target < 0 || target >= next.length) return;
    [next[index], next[target]] = [next[target]!, next[index]!];
    void save(next);
  };
  return (
    <section className="max-w-3xl space-y-3">
      <div>
        <h2 className="text-lg font-semibold">{t("Subtitle providers")}</h2>
        <p className="text-sm text-fg-muted">
          {t("Searches use this order when results are equally suitable.")}
        </p>
      </div>
      {config.error ? (
        <ErrorPanel error={config.error} onRetry={() => void config.reload()} />
      ) : !config.data ? (
        <LoadingRows rows={2} />
      ) : (
        <div className="space-y-2">
          {providers.map((p, i) => (
            <div
              key={p.id}
              className="flex items-center gap-2 rounded-xl border border-border bg-surface p-3"
            >
              <span className="min-w-0 flex-1 truncate">
                {p.name ||
                  textOf(registry.descriptors.find((d) => d.id === p.kind)?.label) ||
                  p.kind}
              </span>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                disabled={busy || i === 0}
                aria-label={t("Move up")}
                onClick={() => move(i, -1)}
              >
                ↑
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                disabled={busy || i === providers.length - 1}
                aria-label={t("Move down")}
                onClick={() => move(i, 1)}
              >
                ↓
              </Button>
              {registry.descriptors.some((d) => d.id === p.kind && d.owner !== "builtin") ? (
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  onClick={() => {
                    const descriptor = registry.descriptors.find((d) => d.id === p.kind)!;
                    setChecking({ id: descriptor.owner, name: textOf(descriptor.label) });
                  }}
                >
                  {t("Check")}
                </Button>
              ) : null}
            </div>
          ))}
        </div>
      )}
      <div className="flex flex-wrap items-center gap-3">
        <Button type="button" variant="outline" size="sm" onClick={() => setAdding(true)}>
          <Plus size={14} aria-hidden /> {t("Add a Stremio add-on")}
        </Button>
        <Link className="text-sm text-primary underline" to="/plugins">
          {t("Add a subtitle provider in Plugins")}
        </Link>
      </div>
      {checking ? <SubtitleCheck provider={checking} onClose={() => setChecking(null)} /> : null}
      {error ? (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      ) : null}
      <AddAddonDrawer
        open={adding}
        role="subtitles"
        onClose={() => setAdding(false)}
        onAdded={() => void config.reload()}
      />
    </section>
  );
}
