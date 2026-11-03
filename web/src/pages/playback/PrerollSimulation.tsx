import { useEffect, useState } from "react";
import { TitleSearch } from "../../components/pickers";
import { t, tx } from "../../i18n";
import { api } from "../../lib/api";
import type { TitleCard, Viewer } from "../../lib/contract";
import { prerollsApi, type DeviceKind, type Simulation } from "./prerolls-api";

function reason(code: string, ok: boolean) {
  switch (code) {
    case "disabled":
      return t("This pre-roll is turned off.");
    case "not_ready":
      return t("The video is not ready.");
    case "kind":
      return ok ? t("The title type matches.") : t("The title type does not match.");
    case "first_episode":
      return t("This is not the first episode of the session.");
    case "binge_gap":
      return t("The session gap has not elapsed.");
    case "genre":
      return t("The title does not match the included genres.");
    case "excluded_genre":
      return t("The title has an excluded genre.");
    case "collection":
      return t("The title is not in a selected collection.");
    case "rating":
      return t("The title exceeds the content rating limit.");
    case "date_window":
      return t("The date is outside the selected window.");
    case "audience":
      return t("This viewer is not included.");
    case "network":
      return t("The home or away setting does not match.");
    case "device":
      return t("This device kind is not included.");
    case "frequency":
      return t("This viewer has seen it too recently.");
    case "resumed":
      return t("The title is being resumed.");
    case "lower_priority":
      return t("Another pre-roll has a higher priority.");
    case "shuffled_out":
      return t("Another pre-roll was chosen by shuffle.");
    case "unsupported_app":
      return t("This app does not support pre-rolls.");
    default:
      return t("The server returned an unknown reason.");
  }
}
export function PrerollSimulation() {
  const [viewers, setViewers] = useState<Viewer[]>([]);
  const [viewer, setViewer] = useState("");
  const [title, setTitle] = useState<TitleCard>();
  const [device, setDevice] = useState<DeviceKind>("tv");
  const [away, setAway] = useState(false);
  const [date, setDate] = useState("");
  const [result, setResult] = useState<Simulation>();
  const [error, setError] = useState(false);
  useEffect(() => {
    void api
      .viewers()
      .then(setViewers)
      .catch(() => setError(true));
  }, []);
  async function simulate() {
    try {
      setResult(
        await prerollsApi.simulate({
          viewerId: Number(viewer),
          ratingKey: title!.key,
          device,
          away,
          resume: false,
          ...(date ? { at: new Date(date).toISOString() } : {}),
        }),
      );
      setError(false);
    } catch {
      setError(true);
    }
  }
  return (
    <section className="space-y-3 rounded border border-border p-4">
      <h3 className="font-semibold">{t("What would play?")}</h3>
      <p className="text-sm text-fg-muted">
        {t("Try a viewer and title to see which pre-roll wins and why.")}
      </p>
      <label>
        {t("Viewer")}{" "}
        <select value={viewer} onChange={(e) => setViewer(e.currentTarget.value)}>
          <option value="">{t("Choose a viewer")}</option>
          {viewers.map((v) => (
            <option key={v.id} value={v.id}>
              {v.title || v.username}
            </option>
          ))}
        </select>
      </label>
      <TitleSearch onPick={setTitle} />
      {title && <p>{tx("Selected: {title}", { title: title.title })}</p>}
      <details>
        <summary>{t("More options")}</summary>
        <label>
          {t("Device")}{" "}
          <select value={device} onChange={(e) => setDevice(e.currentTarget.value as DeviceKind)}>
            <option value="tv">{t("TV")}</option>
            <option value="mobile">{t("Mobile")}</option>
            <option value="desktop">{t("Desktop")}</option>
            <option value="web">{t("Web")}</option>
          </select>
        </label>
        <label>
          <input
            type="checkbox"
            checked={away}
            onChange={(e) => setAway(e.currentTarget.checked)}
          />{" "}
          {t("Away from home")}
        </label>
        <label>
          {t("Date and time")}{" "}
          <input
            type="datetime-local"
            value={date}
            onChange={(e) => setDate(e.currentTarget.value)}
          />
        </label>
      </details>
      <button disabled={!viewer || !title} onClick={() => void simulate()}>
        {t("See what plays")}
      </button>
      {error && <p role="alert">{t("Could not check pre-rolls.")}</p>}
      {result && (
        <div>
          <p>
            {result.winner
              ? tx("Winner: {name}", { name: result.winner.name })
              : t("No pre-roll would play.")}
          </p>
          <ul>
            {result.candidates.map((c) => (
              <li key={c.id}>
                <strong>{c.name}</strong>
                <ul>
                  {c.reasons.map((r, i) => (
                    <li key={i}>{reason(r.code, r.ok)}</li>
                  ))}
                </ul>
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  );
}
