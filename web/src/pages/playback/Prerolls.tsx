import { useCallback, useEffect, useState } from "react";
import { PrerollDrawer } from "./PrerollDrawer";
import { PrerollSimulation } from "./PrerollSimulation";
import { t, tx } from "../../i18n";
import { prerollsApi, type Preroll, type Selection } from "./prerolls-api";

export function Prerolls() {
  const [importing, setImporting] = useState(false);
  const [paths, setPaths] = useState("");
  const [editing, setEditing] = useState<Preroll | "new" | null>(null);
  const [items, setItems] = useState<Preroll[]>([]);
  const [selection, setSelection] = useState<Selection>("priority");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const reload = useCallback(async () => {
    try {
      const result = await prerollsApi.list();
      setItems(result.prerolls ?? []);
      setSelection(result.selection);
      setError("");
    } catch {
      setError(t("Could not load pre-rolls."));
    } finally {
      setLoading(false);
    }
  }, []);
  useEffect(() => {
    void reload();
  }, [reload]);
  async function toggle(p: Preroll) {
    try {
      await prerollsApi.update(p.id, { ...p, enabled: !p.enabled });
      await reload();
    } catch {
      setError(t("Could not save changes."));
    }
  }
  return (
    <section className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-xl font-semibold">{t("Pre-rolls")}</h2>
          <p className="text-fg-muted">{t("Play your own videos before the feature.")}</p>
        </div>
        <button onClick={() => setImporting(true)}>{t("Import from Plex Media Server")}</button>
        <button
          onClick={() => setEditing("new")}
          className="rounded bg-primary px-4 py-2 text-white"
        >
          {t("Add pre-roll")}
        </button>
      </div>
      {importing && (
        <div className="rounded border border-border p-4 space-y-3">
          <label className="block">
            {t("Paste the Movie pre-roll video value from Plex Media Server")}{" "}
            <input value={paths} onChange={(e) => setPaths(e.currentTarget.value)} />
          </label>
          <button
            disabled={!paths.trim()}
            onClick={async () => {
              try {
                await prerollsApi.importPms(paths);
                setImporting(false);
                setPaths("");
                await reload();
              } catch {
                setError(t("Could not import pre-rolls."));
              }
            }}
          >
            {t("Import")}
          </button>{" "}
          <button onClick={() => setImporting(false)}>{t("Cancel")}</button>
        </div>
      )}
      <PrerollSimulation />
      {error && (
        <p role="alert" className="text-danger">
          {error}
        </p>
      )}
      <label className="flex items-center gap-3">
        {t("When several match")}{" "}
        <select
          value={selection}
          onChange={async (e) => {
            const value = e.currentTarget.value as Selection;
            try {
              await prerollsApi.selection(value);
              setSelection(value);
            } catch {
              setError(t("Could not save changes."));
            }
          }}
        >
          <option value="priority">{t("Highest priority wins")}</option>
          <option value="shuffle">{t("Shuffle")}</option>
        </select>
      </label>
      {loading ? (
        <p>{t("Loading…")}</p>
      ) : items.length === 0 ? (
        <div className="rounded border border-border p-8 text-center">
          <p>{t("No pre-rolls yet. Add a video to welcome viewers before their feature.")}</p>
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead>
              <tr>
                <th>{t("Video")}</th>
                <th>{t("Plays before")}</th>
                <th>{t("Who")}</th>
                <th>{t("How often")}</th>
                <th>{t("Last 30 days")}</th>
                <th>{t("On")}</th>
              </tr>
            </thead>
            <tbody>
              {items.map((p) => (
                <tr key={p.id} className="border-t border-border">
                  <td className="py-3">
                    <button className="font-medium" onClick={() => setEditing(p)}>
                      {p.name}
                    </button>
                    <div>
                      {tx("{seconds} seconds", { seconds: Math.round(p.video.durationMs / 1000) })}
                    </div>
                    {p.video.state === "processing"
                      ? t("Processing")
                      : p.video.state === "failed"
                        ? t("Failed")
                        : null}
                  </td>
                  <td>
                    {p.rules.kinds.length === 2
                      ? t("Movies and episodes")
                      : p.rules.kinds.includes("movie")
                        ? t("Movies")
                        : t("Episodes")}
                  </td>
                  <td>
                    {p.rules.audience.mode === "everyone"
                      ? t("Everyone")
                      : p.rules.audience.mode === "only"
                        ? t("Only selected viewers")
                        : t("Everyone except selected viewers")}
                  </td>
                  <td>
                    {p.frequency.mode === "always"
                      ? t("Every time")
                      : p.frequency.mode === "daily"
                        ? t("Once a day")
                        : p.frequency.mode === "title"
                          ? t("Once per title")
                          : tx("Once every {hours} hours", { hours: p.frequency.hours })}
                  </td>
                  <td>{p.plays30d}</td>
                  <td>
                    <input
                      type="checkbox"
                      aria-label={t("Enable {name}", { name: p.name })}
                      checked={p.enabled}
                      onChange={() => void toggle(p)}
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {editing && (
        <PrerollDrawer
          entry={editing === "new" ? undefined : editing}
          onClose={() => setEditing(null)}
          onSaved={() => void reload()}
        />
      )}
    </section>
  );
}
