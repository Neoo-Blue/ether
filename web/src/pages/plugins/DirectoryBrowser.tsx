import { Badge, Button, SearchSelect } from "@braedonsaunders/appkit-ui";
import { useEffect, useRef, useState } from "react";
import { Drawer } from "../../components/Drawer";
import { t } from "../../i18n";
import { searchSelectLabels } from "../../i18n/appkit-labels";
import { apiFetch, errorMessage } from "../../lib/api";
import { useRegistry } from "../../lib/extend";

type Entry = {
  id: string;
  name: string;
  description?: string;
  version?: string;
  manifestURL?: string;
  unsupported?: string;
};
/** Shared discovery UI for every provider of the pluginDirectory contract. */
export function DirectoryBrowser({
  onClose,
  onImport,
}: {
  onClose: () => void;
  onImport: (url: string) => void;
}) {
  const registry = useRegistry<{ key: string }>("pluginDirectory");
  const [source, setSource] = useState("");
  const [entries, setEntries] = useState<Entry[] | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const generation = useRef(0);
  const selected = registry.descriptors.find((d) => d.id === source);
  useEffect(() => {
    generation.current++;
    setEntries(null);
    setError("");
    setBusy(false);
    return () => {
      generation.current++;
    };
  }, [source, selected]);
  const browse = async () => {
    if (!selected?.meta?.key) return;
    const current = ++generation.current;
    setBusy(true);
    setError("");
    setEntries(null);
    try {
      const result = await apiFetch<{ entries: Entry[] }>(
        `/plugins/${encodeURIComponent(selected.owner)}/directories/${encodeURIComponent(selected.meta.key)}`,
      );
      if (current === generation.current) setEntries(result.entries);
    } catch (e) {
      if (current === generation.current) setError(errorMessage(e));
    } finally {
      if (current === generation.current) setBusy(false);
    }
  };
  return (
    <Drawer open onClose={onClose} title={t("Browse plugins")} size="lg">
      <div className="space-y-4">
        <p className="text-sm text-fg-muted">
          {t(
            "Choose a directory, then review a plugin’s fetched manifest before installing it. Directory listings are recommendations from their provider.",
          )}
        </p>
        <SearchSelect
          value={source}
          onChange={setSource}
          options={registry.descriptors.map((d) => ({
            value: d.id,
            label: `${d.group || d.owner} · ${d.label}`,
          }))}
          ariaLabel={t("Plugin directory")}
          searchable
          {...searchSelectLabels()}
        />
        <Button disabled={!selected || busy} onClick={() => void browse()}>
          {busy ? t("Loading…") : t("Browse directory")}
        </Button>
        {!registry.loading && !registry.descriptors.length ? (
          <p>{t("No enabled plugin provides a directory yet.")}</p>
        ) : null}
        {error || registry.error ? (
          <p role="alert" className="text-danger">
            {error || registry.error?.message}
          </p>
        ) : null}
        {entries?.length === 0 ? <p>{t("This directory returned no plugins.")}</p> : null}
        <ul className="space-y-3">
          {entries?.map((e, i) => (
            <li key={`${e.id}:${i}`} className="rounded-xl border border-border p-4">
              <div className="flex items-center justify-between gap-3">
                <h3 className="font-medium">{e.name || e.id}</h3>
                <Badge variant="secondary">{e.version || t("Version not supplied")}</Badge>
              </div>
              <p className="mt-2 text-sm text-fg-muted">{e.description}</p>
              <p className="mt-2 break-all text-xs text-fg-subtle">{e.id}</p>
              {e.unsupported ? (
                <p className="mt-2 text-sm text-warning">{e.unsupported}</p>
              ) : (
                <Button
                  variant="outline"
                  className="mt-3"
                  disabled={!e.manifestURL}
                  onClick={() => onImport(e.manifestURL!)}
                >
                  {t("Review manifest")}
                </Button>
              )}
            </li>
          ))}
        </ul>
      </div>
    </Drawer>
  );
}
