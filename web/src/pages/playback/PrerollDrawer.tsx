import { useEffect, useState } from "react";
import { Drawer } from "../../components/Drawer";
import { t, tx } from "../../i18n";
import { api, API_BASE, sessionXhrSetup } from "../../lib/api";
import type { Viewer } from "../../lib/contract";
import {
  prerollsApi,
  type DeviceKind,
  type Preroll,
  type PrerollInput,
  type Play,
} from "./prerolls-api";

const defaults = (): PrerollInput => ({
  name: "",
  enabled: true,
  rules: {
    kinds: ["movie"],
    firstEpisodeOnly: false,
    bingeGapMinutes: 30,
    genres: [],
    excludeGenres: [],
    collections: [],
    maxContentRating: "",
    dateFrom: "",
    dateTo: "",
    audience: { mode: "everyone", viewers: [] },
    network: "any",
    devices: [],
  },
  frequency: { mode: "always", hours: 6 },
  priority: 0,
  order: "before_trailers",
});
const split = (s: string) =>
  s
    .split(",")
    .map((x) => x.trim())
    .filter(Boolean);
export function PrerollDrawer({
  entry,
  onClose,
  onSaved,
}: {
  entry?: Preroll;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [draft, setDraft] = useState<PrerollInput>(() =>
    entry ? structuredClone(entry) : defaults(),
  );
  const [video, setVideo] = useState(entry?.video);
  const [id, setId] = useState(entry?.id);
  const [viewers, setViewers] = useState<Viewer[]>([]);
  const [plays, setPlays] = useState<Play[]>([]);
  const [file, setFile] = useState<File>();
  const [path, setPath] = useState("");
  const [progress, setProgress] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    void api
      .viewers()
      .then(setViewers)
      .catch(() => setError(t("Could not load viewers.")));
    if (id)
      void prerollsApi
        .plays(id)
        .then((x) => setPlays(x.plays ?? []))
        .catch(() => setError(t("Could not load activity.")));
  }, [id]);
  const rules = (r: Partial<PrerollInput["rules"]>) =>
    setDraft((d) => ({ ...d, rules: { ...d.rules, ...r } }));
  const upload = (id: string, file: File) =>
    new Promise<Preroll>((resolve, reject) => {
      const form = new FormData();
      form.append("file", file);
      if (import.meta.env.MODE === "mock") {
        void fetch(`${API_BASE}/admin/prerolls/${encodeURIComponent(id)}/video`, {
          method: "POST",
          body: form,
        })
          .then((r) => r.json())
          .then(resolve, reject);
        return;
      }
      const xhr = new XMLHttpRequest();
      const url = `${API_BASE}/admin/prerolls/${encodeURIComponent(id)}/video`;
      xhr.open("POST", url);
      xhr.setRequestHeader("X-Ether-Request", "1");
      sessionXhrSetup(xhr, url);
      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable) setProgress(Math.round((e.loaded / e.total) * 100));
      };
      xhr.onerror = () => reject(new Error("upload"));
      xhr.onload = () => {
        try {
          if (xhr.status < 200 || xhr.status >= 300) throw new Error("upload");
          resolve(JSON.parse(xhr.responseText));
        } catch (e) {
          reject(e);
        }
      };
      xhr.send(form);
    });
  async function save() {
    setBusy(true);
    setError("");
    try {
      const p = id ? await prerollsApi.update(id, draft) : await prerollsApi.create(draft);
      setId(p.id);
      let updated = p;
      if (file) updated = await upload(p.id, file);
      else if (path.trim()) updated = await prerollsApi.path(p.id, path.trim());
      setVideo(updated.video);
      setFile(undefined);
      setPath("");
      onSaved();
      onClose();
    } catch {
      setError(t("Could not save pre-roll."));
    } finally {
      setBusy(false);
    }
  }
  async function remove() {
    if (!id || !window.confirm(t("Delete this pre-roll?"))) return;
    setBusy(true);
    try {
      await prerollsApi.remove(id);
      onSaved();
      onClose();
    } catch {
      setError(t("Could not delete pre-roll."));
      setBusy(false);
    }
  }
  return (
    <Drawer open onClose={onClose} title={entry ? t("Edit pre-roll") : t("Add pre-roll")}>
      <div className="space-y-5">
        <label className="block">
          {t("Name")}{" "}
          <input
            value={draft.name}
            onChange={(e) => setDraft({ ...draft, name: e.currentTarget.value })}
          />
        </label>
        <section className="space-y-2">
          <h3 className="font-semibold">{t("Video")}</h3>
          <label
            className="block rounded border border-dashed border-border p-4"
            onDragOver={(e) => e.preventDefault()}
            onDrop={(e) => {
              e.preventDefault();
              setFile(e.dataTransfer.files[0]);
            }}
          >
            {t("Drop a video here or choose a file")}{" "}
            <input
              type="file"
              accept="video/*"
              onChange={(e) => setFile(e.currentTarget.files?.[0])}
            />
          </label>
          {file && <p>{file.name}</p>}
          <label className="block">
            {t("Use a file on the server")}{" "}
            <input value={path} onChange={(e) => setPath(e.currentTarget.value)} />
          </label>
          {busy && file && <progress value={progress} max={100} />}
          {video && (
            <>
              <p>
                {tx("Length: {seconds} seconds", { seconds: Math.round(video.durationMs / 1000) })}
              </p>
              {video.previewUrl && <video controls src={video.previewUrl} />}
            </>
          )}
        </section>
        <section className="space-y-2">
          <h3 className="font-semibold">{t("Plays before")}</h3>
          {(["movie", "episode"] as const).map((k) => (
            <label key={k} className="block">
              <input
                type="checkbox"
                checked={draft.rules.kinds.includes(k)}
                onChange={(e) =>
                  rules({
                    kinds: e.currentTarget.checked
                      ? [...draft.rules.kinds, k]
                      : draft.rules.kinds.filter((x) => x !== k),
                  })
                }
              />{" "}
              {k === "movie" ? t("Movies") : t("Episodes")}
            </label>
          ))}
          <label className="block">
            <input
              type="checkbox"
              checked={draft.rules.firstEpisodeOnly}
              onChange={(e) => rules({ firstEpisodeOnly: e.currentTarget.checked })}
            />{" "}
            {t("Only the first episode of a session")}
          </label>
          <details>
            <summary>{t("Advanced")}</summary>
            <label>
              {t("Session gap (minutes)")}{" "}
              <input
                type="number"
                min="1"
                value={draft.rules.bingeGapMinutes}
                onChange={(e) => rules({ bingeGapMinutes: Number(e.currentTarget.value) })}
              />
            </label>
          </details>
          {(
            [
              ["genres", t("Include genres")],
              ["excludeGenres", t("Exclude genres")],
              ["collections", t("Collections")],
            ] as const
          ).map(([key, label]) => (
            <label className="block" key={key}>
              {label}{" "}
              <input
                value={draft.rules[key].join(", ")}
                onChange={(e) => rules({ [key]: split(e.currentTarget.value) })}
              />
            </label>
          ))}
          <label className="block">
            {t("Content rating ceiling")}{" "}
            <input
              value={draft.rules.maxContentRating}
              onChange={(e) => rules({ maxContentRating: e.currentTarget.value })}
            />
          </label>
          <label>
            {t("From (MM-DD)")}{" "}
            <input
              value={draft.rules.dateFrom}
              onChange={(e) => rules({ dateFrom: e.currentTarget.value })}
            />
          </label>
          <label>
            {t("To (MM-DD)")}{" "}
            <input
              value={draft.rules.dateTo}
              onChange={(e) => rules({ dateTo: e.currentTarget.value })}
            />
          </label>
        </section>
        <section className="space-y-2">
          <h3 className="font-semibold">{t("Who")}</h3>
          <select
            value={draft.rules.audience.mode}
            onChange={(e) =>
              rules({
                audience: {
                  ...draft.rules.audience,
                  mode: e.currentTarget.value as "everyone" | "only" | "except",
                },
              })
            }
          >
            <option value="everyone">{t("Everyone")}</option>
            <option value="only">{t("Only these viewers")}</option>
            <option value="except">{t("Everyone except")}</option>
          </select>
          {draft.rules.audience.mode !== "everyone" &&
            viewers.map((v) => (
              <label className="block" key={v.id}>
                <input
                  type="checkbox"
                  checked={draft.rules.audience.viewers.includes(v.id)}
                  onChange={(e) =>
                    rules({
                      audience: {
                        ...draft.rules.audience,
                        viewers: e.currentTarget.checked
                          ? [...draft.rules.audience.viewers, v.id]
                          : draft.rules.audience.viewers.filter((x) => x !== v.id),
                      },
                    })
                  }
                />{" "}
                {v.title || v.username}
              </label>
            ))}
          <select
            value={draft.rules.network}
            onChange={(e) => rules({ network: e.currentTarget.value as "any" | "home" | "away" })}
          >
            <option value="any">{t("Home and away")}</option>
            <option value="home">{t("Home only")}</option>
            <option value="away">{t("Away only")}</option>
          </select>
          {(
            [
              ["tv", t("TV")],
              ["mobile", t("Mobile")],
              ["desktop", t("Desktop")],
              ["web", t("Web")],
            ] as [DeviceKind, string][]
          ).map(([v, label]) => (
            <label className="block" key={v}>
              <input
                type="checkbox"
                checked={draft.rules.devices.includes(v)}
                onChange={(e) =>
                  rules({
                    devices: e.currentTarget.checked
                      ? [...draft.rules.devices, v]
                      : draft.rules.devices.filter((x) => x !== v),
                  })
                }
              />{" "}
              {label}
            </label>
          ))}
        </section>
        <section>
          <h3 className="font-semibold">{t("How often")}</h3>
          <select
            value={draft.frequency.mode}
            onChange={(e) =>
              setDraft({
                ...draft,
                frequency: {
                  ...draft.frequency,
                  mode: e.currentTarget.value as PrerollInput["frequency"]["mode"],
                },
              })
            }
          >
            <option value="always">{t("Every time")}</option>
            <option value="daily">{t("Once a day")}</option>
            <option value="hours">{t("Once every N hours")}</option>
            <option value="title">{t("Once per title")}</option>
          </select>
          {draft.frequency.mode === "hours" && (
            <input
              type="number"
              min="1"
              aria-label={t("Hours")}
              value={draft.frequency.hours}
              onChange={(e) =>
                setDraft({
                  ...draft,
                  frequency: { ...draft.frequency, hours: Number(e.currentTarget.value) },
                })
              }
            />
          )}
        </section>
        <label className="block">
          {t("Priority")}{" "}
          <input
            type="number"
            value={draft.priority}
            onChange={(e) => setDraft({ ...draft, priority: Number(e.currentTarget.value) })}
          />
        </label>
        <label className="block">
          {t("Cinema trailers")}{" "}
          <select
            value={draft.order}
            onChange={(e) =>
              setDraft({ ...draft, order: e.currentTarget.value as PrerollInput["order"] })
            }
          >
            <option value="before_trailers">{t("Before cinema trailers")}</option>
            <option value="after_trailers">{t("After cinema trailers")}</option>
          </select>
        </label>
        <section>
          <h3>{t("Activity")}</h3>
          {plays.length ? (
            plays.map((p, i) => (
              <p key={i}>
                {p.viewerName} · {p.title} · {p.at}
              </p>
            ))
          ) : (
            <p>{t("No recent plays.")}</p>
          )}
        </section>
        <p>
          {t(
            "Where it shows: Plex apps, Jellyfin web and Android TV, and Emby apps. Pre-rolls do not show in Infuse, Swiftfin, Silo, or Stremio.",
          )}
        </p>
        {error && <p role="alert">{error}</p>}
        <button disabled={busy || !draft.name.trim()} onClick={() => void save()}>
          {t("Save")}
        </button>
        {id && (
          <button disabled={busy} onClick={() => void remove()}>
            {t("Delete")}
          </button>
        )}{" "}
        <button onClick={onClose}>{t("Cancel")}</button>
      </div>
    </Drawer>
  );
}
