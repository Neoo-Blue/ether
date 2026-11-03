import { Button, Input } from "@braedonsaunders/appkit-ui";
import { useState } from "react";
import { Link } from "react-router-dom";
import { SubtitleCheck } from "./SubtitleCheck";
import { Drawer } from "../../components/Drawer";
import { Field, SecretInput } from "../../components/fields";
import { ErrorPanel, LoadingRows } from "../../components/feedback";
import { t } from "../../i18n";
import { api, errorMessage } from "../../lib/api";
import { textOf, useRegistry } from "../../lib/extend";
import { useAsync } from "../../lib/hooks";
import type { EtherConfig } from "../../lib/types";

type Provider = { id: string; kind: string; name?: string; url?: string; enabled: boolean };

export function SubtitleProviders() {
  const config = useAsync(() => api.getConfig(), []);
  const registry = useRegistry("subtitleProvider");
  const [checking, setChecking] = useState<{ id: string; name: string } | null>(null);
  const [editing, setEditing] = useState<Provider | null>(null);
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
      setEditing(null);
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
              {p.kind === "stremio" ? (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => setEditing({ ...p })}
                >
                  {t("Edit")}
                </Button>
              ) : null}
            </div>
          ))}
        </div>
      )}
      <Link className="text-sm text-primary underline" to="/plugins">
        {t("Add a subtitle provider in Plugins")}
      </Link>
      {checking ? <SubtitleCheck provider={checking} onClose={() => setChecking(null)} /> : null}
      {error && !editing ? (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      ) : null}
      <Drawer
        open={editing !== null}
        onClose={() => setEditing(null)}
        title={t("Stremio subtitle add-on")}
      >
        {editing ? (
          <div className="space-y-4">
            <Field label={t("Name")} htmlFor="subtitle-provider-name">
              <Input
                id="subtitle-provider-name"
                value={editing.name ?? ""}
                onChange={(e) => setEditing({ ...editing, name: e.currentTarget.value })}
              />
            </Field>
            <Field
              htmlFor="subtitle-provider-url"
              label={t("Add-on URL")}
              hint={t("Paste the configured manifest URL from your add-on.")}
            >
              <SecretInput
                id="subtitle-provider-url"
                value={editing.url ?? ""}
                onChange={(url) => setEditing({ ...editing, url })}
              />
            </Field>
            {error ? (
              <p role="alert" className="text-sm text-danger">
                {error}
              </p>
            ) : null}
            <div className="flex gap-2">
              <Button
                disabled={busy || !editing.url?.trim()}
                onClick={() =>
                  void save(
                    providers.some((p) => p.id === editing.id)
                      ? providers.map((p) => (p.id === editing.id ? editing : p))
                      : [...providers, editing],
                  )
                }
              >
                {t("Save")}
              </Button>
              {providers.some((p) => p.id === editing.id) ? (
                <Button
                  variant="outline"
                  disabled={busy}
                  onClick={() => void save(providers.filter((p) => p.id !== editing.id))}
                >
                  {t("Remove")}
                </Button>
              ) : null}
            </div>
          </div>
        ) : null}
      </Drawer>
    </section>
  );
}
