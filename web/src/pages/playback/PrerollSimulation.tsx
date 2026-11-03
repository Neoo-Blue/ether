import { Badge, Button, Input, SearchSelect, Switch, cn } from "@braedonsaunders/appkit-ui";
import { CheckCircle2, Clapperboard, PlayCircle, XCircle } from "lucide-react";
import { useState } from "react";
import { Artwork } from "../../components/Artwork";
import { Field, Segmented } from "../../components/fields";
import { Section } from "../../components/layout";
import { TitleSearch, useViewers, viewerName } from "../../components/pickers";
import { titleSubtitle } from "../../components/Poster";
import { t, tn } from "../../i18n";
import { searchSelectLabels } from "../../i18n/appkit-labels";
import type { TitleCard } from "../../lib/contract";
import { Disclosure, ErrorNote, PrerollThumb } from "./PrerollParts";
import { prerollsApi, type Candidate, type DeviceKind, type Preroll, type Simulation } from "./prerolls-api";
import { DEVICES, plainError, reasonSentence, type PlainError } from "./prerolls-copy";

/**
 * "What would play?": runs the server's own rules for one viewer and title
 * and explains, for every pre-roll, each condition that passed or failed.
 */
export function PrerollSimulation({ prerolls }: { prerolls: Preroll[] }) {
  const { viewers } = useViewers();
  const [viewer, setViewer] = useState("");
  const [title, setTitle] = useState<TitleCard | null>(null);
  const [device, setDevice] = useState<DeviceKind>("tv");
  const [away, setAway] = useState(false);
  const [resume, setResume] = useState(false);
  const [at, setAt] = useState("");
  const [result, setResult] = useState<{ sim: Simulation; title: TitleCard } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<PlainError | null>(null);
  // Changing a question clears the old answer, so a result always matches what's shown.
  const change =
    <T,>(setter: (v: T) => void) =>
    (v: T) => {
      setter(v);
      setResult(null);
    };

  async function run() {
    if (!viewer || !title) return;
    setBusy(true);
    setError(null);
    try {
      const when = at ? new Date(at) : null;
      const sim = await prerollsApi.simulate({
        viewerId: Number(viewer),
        ratingKey: title.key,
        device,
        away,
        resume,
        ...(when && !Number.isNaN(when.getTime()) ? { at: when.toISOString() } : {}),
      });
      setResult({ sim, title });
    } catch (e) {
      setError(plainError(e, t("Ether couldn't check which pre-roll would play.")));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Section
      title={t("What would play?")}
      description={t("Pick a viewer and a title to see which pre-roll plays, and why the others don't.")}
    >
      <div className="grid gap-4 md:grid-cols-2">
        <Field label={t("Viewer")}>
          <SearchSelect
            {...searchSelectLabels()}
            ariaLabel={t("Viewer")}
            placeholder={viewers ? t("Choose a viewer") : t("Loading viewers…")}
            value={viewer}
            onChange={change(setViewer)}
            options={(viewers ?? []).map((v) => ({
              value: String(v.id),
              label: viewerName(v),
              hint: v.isOwner ? t("Owner") : undefined,
            }))}
          />
        </Field>
        <Field label={t("Title")}>
          {title ? (
            <div className="flex items-center gap-3 rounded-md border border-border bg-surface p-1.5 pr-2">
              <Artwork src={title.poster} alt="" title={title.title} className="h-12 w-8 shrink-0 rounded" />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm font-medium text-fg">{title.title}</span>
                <span className="block truncate text-xs text-fg-muted">{titleSubtitle(title)}</span>
              </span>
              <Button type="button" variant="ghost" size="sm" onClick={() => change(setTitle)(null)}>
                {t("Change")}
              </Button>
            </div>
          ) : (
            <TitleSearch onPick={change(setTitle)} label={t("Title")} />
          )}
        </Field>
      </div>
      <Disclosure label={t("More options")}>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={t("Device")}>
            <Segmented
              ariaLabel={t("Device")}
              size="sm"
              value={device}
              onChange={change(setDevice)}
              options={DEVICES}
            />
          </Field>
          <Field label={t("Where")}>
            <Segmented
              ariaLabel={t("Where")}
              size="sm"
              value={away ? "away" : "home"}
              onChange={(v) => change(setAway)(v === "away")}
              options={[
                { value: "home", label: t("At home") },
                { value: "away", label: t("Away from home") },
              ]}
            />
          </Field>
          <Field
            label={t("Date and time")}
            htmlFor="preroll-sim-at"
            hint={t("Leave empty to check right now.")}
          >
            <Input
              id="preroll-sim-at"
              type="datetime-local"
              value={at}
              onChange={(e) => change(setAt)(e.currentTarget.value)}
            />
          </Field>
          <label className="flex items-center gap-3 self-end rounded-lg border border-border bg-surface p-3">
            <Switch
              checked={resume}
              onChange={(e) => change(setResume)(e.currentTarget.checked)}
              aria-label={t("Resuming the title")}
            />
            <span className="text-sm text-fg">{t("Resuming the title")}</span>
          </label>
        </div>
      </Disclosure>
      <div className="flex flex-wrap items-center gap-3">
        <Button type="button" disabled={!viewer || !title || busy} onClick={() => void run()}>
          <PlayCircle size={16} aria-hidden /> {busy ? t("Checking…") : t("See what plays")}
        </Button>
        {!viewer || !title ? (
          <span className="text-xs text-fg-muted">{t("Choose a viewer and a title first.")}</span>
        ) : null}
      </div>
      <ErrorNote error={error} />
      {result ? <SimulationResult result={result} prerolls={prerolls} /> : null}
    </Section>
  );
}

function SimulationResult({
  result: { sim, title },
  prerolls,
}: {
  result: { sim: Simulation; title: TitleCard };
  prerolls: Preroll[];
}) {
  const winner = sim.winner ? prerolls.find((p) => p.id === sim.winner!.id) : undefined;
  // The winner leads, then the others in the order the server checked them.
  const ordered = [
    ...sim.candidates.filter((c) => c.id === sim.winner?.id),
    ...sim.candidates.filter((c) => c.id !== sim.winner?.id),
  ];
  return (
    <div className="space-y-4" aria-live="polite">
      {sim.winner ? (
        <div className="flex items-center gap-4 rounded-xl border border-primary/30 bg-primary-subtle p-4">
          {winner ? (
            <PrerollThumb preroll={winner} className="w-32" eager />
          ) : (
            <span className="grid size-12 place-items-center rounded-lg bg-surface text-primary">
              <Clapperboard size={20} aria-hidden />
            </span>
          )}
          <div className="min-w-0">
            <p className="text-xs font-medium uppercase tracking-wide text-primary">
              {t("Plays")}
            </p>
            <p className="truncate text-lg font-semibold text-fg">{sim.winner.name}</p>
            <p className="text-sm text-fg-muted">
              {t("Before {title}", { title: title.title })}
            </p>
          </div>
        </div>
      ) : (
        <div className="flex items-center gap-4 rounded-xl border border-border bg-bg-subtle p-4">
          <span className="grid size-12 place-items-center rounded-lg bg-surface text-fg-subtle ring-1 ring-border">
            <Clapperboard size={20} aria-hidden />
          </span>
          <div>
            <p className="text-base font-semibold text-fg">{t("No pre-roll plays")}</p>
            <p className="text-sm text-fg-muted">
              {t("{title} starts straight away.", { title: title.title })}
            </p>
          </div>
        </div>
      )}
      <ul className="divide-y divide-border rounded-xl border border-border" aria-label={t("Every pre-roll")}>
        {ordered.map((c) => (
          <CandidateRow key={c.id} candidate={c} />
        ))}
      </ul>
    </div>
  );
}

function CandidateRow({ candidate: c }: { candidate: Candidate }) {
  // What stopped it comes first; the checks it passed stay folded away
  // unless it is the one that plays.
  const failed = c.reasons.filter((r) => !r.ok);
  const passed = c.reasons.filter((r) => r.ok);
  const [showPassed, setShowPassed] = useState(false);
  const shown = c.plays || showPassed ? [...failed, ...passed] : failed;
  return (
    <li className="space-y-2 p-4">
      <div className="flex items-center gap-2">
        {c.plays ? (
          <CheckCircle2 size={18} className="shrink-0 text-success" aria-hidden />
        ) : (
          <XCircle size={18} className="shrink-0 text-danger" aria-hidden />
        )}
        <span className="min-w-0 flex-1 truncate font-medium text-fg">{c.name}</span>
        <Badge variant={c.plays ? "success" : "secondary"}>
          {c.plays ? t("Plays") : t("Doesn't play")}
        </Badge>
      </div>
      <ul className="space-y-1 pl-7">
        {shown.map((r, i) => (
          <li
            key={`${r.code}-${i}`}
            className={cn("flex items-start gap-2 text-sm", r.ok ? "text-fg-muted" : "text-fg")}
          >
            {r.ok ? (
              <CheckCircle2
                size={14}
                className="mt-0.5 shrink-0 text-success"
                aria-label={t("Passed")}
              />
            ) : (
              <XCircle size={14} className="mt-0.5 shrink-0 text-danger" aria-label={t("Failed")} />
            )}
            <span>{reasonSentence(r.code, r.ok)}</span>
          </li>
        ))}
      </ul>
      {!c.plays && passed.length ? (
        <button
          type="button"
          className="ml-7 text-xs font-medium text-primary hover:underline"
          aria-expanded={showPassed}
          onClick={() => setShowPassed((v) => !v)}
        >
          {showPassed
            ? t("Hide the checks it passed")
            : tn(passed.length, "Show the {count} check it passed", "Show the {count} checks it passed")}
        </button>
      ) : null}
    </li>
  );
}
