/**
 * The Studio stage: every agent tool call and result becomes a live moment,
 * drawn from a closed set of stage widgets. In the real build each moment is
 * a ViewSpec part on the agent's stream, bound to the tool result; here a
 * fixed script stands in for the agent so the motion can be reviewed.
 */
import { useLocale } from "../../i18n/react";
import { Badge, Skeleton, cn } from "@braedonsaunders/appkit-ui";
import {
  Check,
  Globe,
  Library,
  ListChecks,
  Radio,
  Search,
  Sparkles,
  Tv,
  Wrench,
  type LucideIcon,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Artwork } from "../../components/Artwork";
import { StatusDot } from "../../components/status";
import { t } from "../../i18n";
import { studioSpec, usePreview, useHomeFacts, homeSummary } from "./preview";
import "./stage.css";

export type Moment =
  | { kind: "intent"; text: string; tool: string }
  | {
      kind: "sweep";
      tool: string;
      title: string;
      sources: { name: string; found: string; icon: LucideIcon }[];
    }
  | {
      kind: "found";
      tool: string;
      title: string;
      cards: { name: string; url: string; via: string }[];
    }
  | { kind: "streams"; tool: string; title: string; tiles: StreamTile[] }
  | { kind: "repair"; text: string }
  | { kind: "guide"; tool: string; title: string; channels: GuideChannel[] }
  | { kind: "posters"; tool: string; title: string }
  | { kind: "checks"; title: string; items: string[] }
  | { kind: "done"; text: string };

export interface StreamTile {
  name: string;
  ok: boolean;
  /** Milliseconds until the first frame, or until the test gives up. */
  ms: number;
  score?: string;
}

export interface GuideChannel {
  number: number;
  name: string;
  now: string;
  next: string;
  progress: number;
}

export interface Script {
  key: "channels" | "home";
  moments: { at: number; moment: Moment }[];
  /** When the stage settles into the editor. */
  end: number;
}

export function scriptFor(prompt: string): Script {
  return /channel|tv|sport|live/i.test(prompt) ? channelsScript() : homeScript();
}

const CHANNELS: GuideChannel[] = [
  {
    number: 801,
    name: "Kigali Sport 1",
    now: "Rwanda Premier League",
    next: "Sports news",
    progress: 0.62,
  },
  {
    number: 802,
    name: "Amahoro Sports",
    now: "Basketball Africa League",
    next: "Cycling: Tour du Rwanda",
    progress: 0.35,
  },
  {
    number: 803,
    name: "Rwanda Sports Live",
    now: "Volleyball final",
    next: "Match of the week",
    progress: 0.81,
  },
];

function channelsScript(): Script {
  return {
    key: "channels",
    end: 17_500,
    moments: [
      {
        at: 0,
        moment: {
          kind: "intent",
          tool: "understand",
          text: t("Sports channels from Rwanda, in Live TV with a guide"),
        },
      },
      {
        at: 1_000,
        moment: {
          kind: "sweep",
          tool: "web_search",
          title: t("Searching for streams"),
          sources: [
            { name: t("Free channel directories"), found: t("2 found"), icon: Globe },
            { name: t("Public IPTV lists"), found: t("3 found"), icon: Radio },
            { name: t("Stremio TV add-ons on this server"), found: t("1 found"), icon: Tv },
            { name: t("Broadcaster websites"), found: t("1 found"), icon: Globe },
          ],
        },
      },
      {
        at: 4_200,
        moment: {
          kind: "found",
          tool: "fetch",
          title: t("4 channels after removing duplicates"),
          cards: [
            {
              name: "Kigali Sport 1",
              url: "kigalisport.example/live/1.m3u8",
              via: t("Public IPTV lists"),
            },
            {
              name: "Amahoro Sports",
              url: "amahoro.example/hls/sport.m3u8",
              via: t("Free channel directories"),
            },
            {
              name: "Rwanda Sports Live",
              url: "rsl.example/stream/index.m3u8",
              via: t("Broadcaster websites"),
            },
            {
              name: "Inkuru TV Sport",
              url: "inkuru.example/tv/sport.m3u8",
              via: t("Public IPTV lists"),
            },
          ],
        },
      },
      {
        at: 6_400,
        moment: {
          kind: "streams",
          tool: "test_play",
          title: t("Testing each stream through Ether"),
          tiles: [
            { name: "Kigali Sport 1", ok: true, ms: 1400, score: "APR 2 – 1 RAY" },
            { name: "Amahoro Sports", ok: true, ms: 1900, score: "REG 64 – 58 PAT" },
            { name: "Rwanda Sports Live", ok: true, ms: 1600, score: "Set 3 · 21 – 19" },
            { name: "Inkuru TV Sport", ok: false, ms: 3200 },
          ],
        },
      },
      {
        at: 11_000,
        moment: { kind: "repair", text: t("Dropped Inkuru TV Sport: no picture after 3.2 s") },
      },
      {
        at: 11_900,
        moment: {
          kind: "guide",
          tool: "test_probe",
          title: t("Building the guide"),
          channels: CHANNELS,
        },
      },
      {
        at: 14_600,
        moment: {
          kind: "checks",
          title: t("Checked on your server"),
          items: [
            t("3 of 3 channels have a guide"),
            t("Each stream starts in under 2 s"),
            t("No viewer's stream was used for testing"),
          ],
        },
      },
      {
        at: 16_400,
        moment: { kind: "done", text: t("3 sports channels, ready to add to Live TV") },
      },
    ],
  };
}

function homeScript(facts?: ReturnType<typeof useHomeFacts>): Script {
  const lines = facts
    ? homeSummary(facts)
    : [
        t("Reading the sample catalogue"),
        t("Checking watch history"),
        t("Applying the rating filter"),
        t("Building the sample row"),
      ];
  return {
    key: "home",
    end: 10_500,
    moments: [
      {
        at: 0,
        moment: {
          kind: "intent",
          tool: "understand",
          text: t("A Home row of 4K titles that no viewer has started"),
        },
      },
      {
        at: 900,
        moment: {
          kind: "sweep",
          tool: "catalog_search",
          title: t("Reading your server"),
          sources: [
            { name: t("4K demo catalogue"), found: lines[0]!, icon: Library },
            { name: t("Nobody has started it"), found: lines[1]!, icon: ListChecks },
            { name: t("Rated 7 or more"), found: lines[2]!, icon: Search },
          ],
        },
      },
      {
        at: 3_300,
        moment: { kind: "posters", tool: "collection_preview", title: t("Building the row") },
      },
      {
        at: 7_000,
        moment: { kind: "repair", text: t("Sorted by rating and applied the row limit") },
      },
      {
        at: 7_800,
        moment: {
          kind: "checks",
          title: t("Checked on your server"),
          items: [lines[3]!, t("Sample preview uses the same recipe as installation")],
        },
      },
      { at: 9_400, moment: { kind: "done", text: lines[3]! } },
    ],
  };
}

/** Plays a script: moments arrive on schedule, newest in focus, older ones settled above. */
export function StageRunner({ script, onDone }: { script: Script; onDone: () => void }) {
  const [shown, setShown] = useState(1);
  const facts = useHomeFacts();
  const moments = script.key === "home" ? homeScript(facts).moments : script.moments;
  useEffect(() => {
    setShown(1);
    const timers = script.moments.map((m, i) => window.setTimeout(() => setShown(i + 1), m.at));
    timers.push(window.setTimeout(onDone, script.end));
    return () => timers.forEach(window.clearTimeout);
  }, [script, onDone]);
  const arrived = moments.slice(0, shown);
  const current = arrived[arrived.length - 1]!.moment;
  const result = [...arrived]
    .reverse()
    .find(({ moment }) => moment.kind === "posters" || moment.kind === "guide");
  return (
    <div className="mx-auto w-full max-w-4xl space-y-4 px-4 py-6 sm:px-6">
      <p className="text-xs text-fg-muted">{t("Prototype · sample results")}</p>
      <div className="stage-rise" key={shown}>
        <MomentView moment={current} />
      </div>
      {result && result.moment !== current ? <MomentView moment={result.moment} /> : null}
      <details className="rounded-xl border border-border bg-surface p-4">
        <summary className="cursor-pointer text-sm text-fg-muted">
          {t("Completed steps ({count})", { count: Math.max(0, shown - 1) })}
        </summary>
        <ul className="mt-3 space-y-2 text-sm text-fg-muted">
          {arrived.slice(0, -1).map(({ moment }, i) => (
            <li key={i}>{"title" in moment ? moment.title : moment.text}</li>
          ))}
        </ul>
      </details>
      <details className="rounded-xl border border-border bg-surface p-4">
        <summary className="cursor-pointer text-sm text-fg-muted">{t("Inspector")}</summary>
        <pre className="mt-3 overflow-auto text-xs text-fg-muted">
          {JSON.stringify(
            arrived.map(({ moment }) => moment),
            null,
            2,
          )}
        </pre>
      </details>
    </div>
  );
}

function MomentView({ moment }: { moment: Moment }) {
  switch (moment.kind) {
    case "intent":
      return (
        <div className="flex items-center gap-3">
          <span className="flex size-9 items-center justify-center rounded-full bg-primary text-primary-fg">
            <Sparkles size={17} aria-hidden />
          </span>
          <p className="text-lg font-semibold tracking-tight text-fg">{moment.text}</p>
        </div>
      );
    case "sweep":
      return <Sweep title={moment.title} tool={moment.tool} sources={moment.sources} />;
    case "found":
      return (
        <Panel title={moment.title} tool={moment.tool}>
          <div className="grid gap-2.5 sm:grid-cols-2">
            {moment.cards.map((c, i) => (
              <div
                key={c.url}
                className="stage-pop flex items-center gap-3 rounded-lg border border-border bg-bg-subtle px-3 py-2.5"
                style={{ animationDelay: `${i * 140}ms` }}
              >
                <Monogram name={c.name} />
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium text-fg">{c.name}</p>
                  <p className="truncate font-mono text-xs text-fg-muted">{c.url}</p>
                </div>
              </div>
            ))}
          </div>
        </Panel>
      );
    case "streams":
      return (
        <Panel title={moment.title} tool={moment.tool}>
          <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
            {moment.tiles.map((tile, i) => (
              <StreamTileView key={tile.name} tile={tile} delay={i * 350} />
            ))}
          </div>
        </Panel>
      );
    case "repair":
      return (
        <p className="flex items-center gap-2 pl-1 text-sm text-fg-muted">
          <Wrench size={14} className="shrink-0 text-primary" aria-hidden /> {moment.text}
        </p>
      );
    case "guide":
      return (
        <Panel title={moment.title} tool={moment.tool}>
          <GuideStrip channels={moment.channels} animate />
        </Panel>
      );
    case "posters":
      return <PosterFlow title={moment.title} tool={moment.tool} />;
    case "checks":
      return (
        <Panel title={moment.title}>
          <ul className="space-y-2">
            {moment.items.map((it, i) => (
              <li
                key={it}
                className="stage-snap flex items-center gap-2.5 text-sm text-fg"
                style={{ animationDelay: `${i * 260}ms` }}
              >
                <span className="flex size-5 items-center justify-center rounded-full bg-success/15 text-success">
                  <Check size={12} strokeWidth={3} aria-hidden />
                </span>
                {it}
              </li>
            ))}
          </ul>
        </Panel>
      );
    case "done":
      return (
        <div className="stage-pop flex items-center gap-3 rounded-xl border border-primary/30 bg-primary/5 px-4 py-3.5">
          <span className="flex size-8 items-center justify-center rounded-full bg-primary text-primary-fg">
            <Check size={16} strokeWidth={3} aria-hidden />
          </span>
          <p className="font-semibold text-fg">{moment.text}</p>
        </div>
      );
  }
}

function Panel({ title, children }: { title: string; tool?: string; children: ReactNode }) {
  return (
    <section
      aria-label={title}
      className="rounded-xl border border-border bg-surface p-4 shadow-sm"
    >
      <div className="mb-3 flex items-center justify-between gap-2">
        <h3 className="text-sm font-semibold text-fg">{title}</h3>
      </div>
      {children}
    </section>
  );
}

function Sweep({
  title,
  tool,
  sources,
}: {
  title: string;
  tool: string;
  sources: { name: string; found: string; icon: LucideIcon }[];
}) {
  const [done, setDone] = useState(0);
  useEffect(() => {
    const timers = sources.map((_, i) => window.setTimeout(() => setDone(i + 1), 550 * (i + 1)));
    return () => timers.forEach((id) => window.clearTimeout(id));
  }, [sources]);
  return (
    <Panel title={title} tool={tool}>
      <ul className="space-y-2">
        {sources.map((s, i) => {
          const scanning = i === done;
          const finished = i < done;
          return (
            <li
              key={s.name}
              className={cn(
                "stage-snap relative flex items-center gap-3 overflow-hidden rounded-lg border px-3 py-2 text-sm",
                finished ? "border-border bg-bg-subtle" : "border-border-subtle",
                scanning && "stage-sweep border-primary/40",
              )}
              style={{ animationDelay: `${i * 120}ms` }}
            >
              <s.icon size={15} className="shrink-0 text-fg-muted" aria-hidden />
              <span className={cn("flex-1", finished || scanning ? "text-fg" : "text-fg-subtle")}>
                {s.name}
              </span>
              {finished ? (
                <span className="stage-pop text-xs font-medium tabular-nums text-success">
                  {s.found}
                </span>
              ) : scanning ? (
                <span className="text-xs text-fg-muted">{t("Searching")}</span>
              ) : null}
            </li>
          );
        })}
      </ul>
    </Panel>
  );
}

function Monogram({ name }: { name: string }) {
  const letters = name
    .split(/\s+/)
    .slice(0, 2)
    .map((w) => w[0])
    .join("");
  return (
    <span className="flex size-9 shrink-0 items-center justify-center rounded-md bg-primary/10 text-xs font-bold text-primary">
      {letters}
    </span>
  );
}

/** A real playing test tile. The prototype draws a test card; the build plays the stream through Ether. */
function StreamTileView({ tile, delay }: { tile: StreamTile; delay: number }) {
  const [phase, setPhase] = useState<"tuning" | "playing" | "failed">("tuning");
  useEffect(() => {
    const id = window.setTimeout(() => setPhase(tile.ok ? "playing" : "failed"), delay + tile.ms);
    return () => window.clearTimeout(id);
  }, [tile, delay]);
  return (
    <figure className="stage-pop space-y-1.5" style={{ animationDelay: `${delay}ms` }}>
      <div className="relative aspect-video overflow-hidden rounded-lg bg-overlay ring-1 ring-border">
        {phase === "playing" ? (
          <TestCard name={tile.name} score={tile.score ?? ""} />
        ) : phase === "failed" ? (
          <div className="flex size-full items-center justify-center text-xs text-on-scrim/80">
            {t("No picture")}
          </div>
        ) : (
          <Skeleton className="size-full rounded-none" />
        )}
        {phase === "playing" ? (
          <span className="absolute left-1.5 top-1.5 rounded bg-danger px-1 text-[9px] font-bold uppercase tracking-wide text-white">
            {t("Live")}
          </span>
        ) : null}
      </div>
      <figcaption className="flex items-center gap-1.5 text-xs">
        <StatusDot
          level={phase === "playing" ? "ok" : phase === "failed" ? "error" : "unknown"}
          pulse={phase === "failed"}
        />
        <span className="min-w-0 flex-1 truncate text-fg">{tile.name}</span>
        <span className="shrink-0 tabular-nums text-fg-muted">
          {phase === "playing"
            ? t("{seconds} s", { seconds: (tile.ms / 1000).toFixed(1) })
            : phase === "failed"
              ? t("Failed")
              : ""}
        </span>
      </figcaption>
    </figure>
  );
}

function cssColor(name: string): string {
  const raw = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return raw
    ? `rgb(${raw
        .split(/[\s,]+/)
        .slice(0, 3)
        .join(" ")})`
    : "#0f766e";
}

/** An animated test card on a real <video>, from a canvas stream. */
function TestCard({ name, score }: { name: string; score: string }) {
  const ref = useRef<HTMLVideoElement>(null);
  useEffect(() => {
    const canvas = document.createElement("canvas");
    canvas.width = 320;
    canvas.height = 180;
    const g = canvas.getContext("2d")!;
    const a = cssColor("--ch-primary");
    const b = cssColor("--ch-accent-indigo");
    let frame = 0;
    let raf = 0;
    const draw = () => {
      frame++;
      const grad = g.createLinearGradient(0, 0, 320, 180);
      grad.addColorStop(0, a);
      grad.addColorStop(1, b);
      g.fillStyle = grad;
      g.fillRect(0, 0, 320, 180);
      g.strokeStyle = "rgba(255,255,255,0.35)";
      g.lineWidth = 2;
      g.strokeRect(24, 30, 272, 120);
      g.beginPath();
      g.moveTo(160, 30);
      g.lineTo(160, 150);
      g.stroke();
      g.beginPath();
      g.arc(160, 90, 22, 0, Math.PI * 2);
      g.stroke();
      const x = 40 + ((Math.sin(frame / 25) + 1) / 2) * 240;
      const y = 50 + ((Math.cos(frame / 31) + 1) / 2) * 80;
      g.fillStyle = "white";
      g.beginPath();
      g.arc(x, y, 5, 0, Math.PI * 2);
      g.fill();
      g.fillStyle = "rgba(0,0,0,0.55)";
      g.fillRect(0, 150, 320, 30);
      g.fillStyle = "white";
      g.font = "600 13px system-ui, sans-serif";
      g.fillText(score, 10, 170);
      const secs = Math.floor(frame / 30);
      g.fillText(`${67 + Math.floor(secs / 60)}:${String(secs % 60).padStart(2, "0")}`, 270, 170);
      raf = requestAnimationFrame(draw);
    };
    draw();
    const stream = canvas.captureStream(30);
    const v = ref.current;
    if (v) {
      v.srcObject = stream;
      void v.play().catch(() => undefined);
    }
    return () => {
      cancelAnimationFrame(raf);
      stream.getTracks().forEach((tr) => tr.stop());
    };
  }, [name, score]);
  return <video ref={ref} muted playsInline aria-label={name} className="size-full object-cover" />;
}

export function GuideStrip({ channels, animate }: { channels: GuideChannel[]; animate?: boolean }) {
  return (
    <div className="overflow-hidden rounded-lg border border-border">
      <div className="grid grid-cols-[3.5rem_10rem_1fr_1fr] border-b border-border bg-bg-subtle px-3 py-1.5 text-[11px] font-medium text-fg-subtle">
        <span />
        <span />
        <span>{t("Now")}</span>
        <span>{t("Next")}</span>
      </div>
      {channels.map((c, i) => (
        <div
          key={c.number}
          className={cn(
            "grid grid-cols-[3.5rem_10rem_1fr_1fr] items-center gap-2 border-b border-border-subtle px-3 py-2 last:border-b-0",
            animate && "stage-snap",
          )}
          style={animate ? { animationDelay: `${i * 320}ms` } : undefined}
        >
          <span className="font-semibold tabular-nums text-fg">{c.number}</span>
          <span className="flex min-w-0 items-center gap-2">
            <Monogram name={c.name} />
            <span className="truncate text-sm text-fg">{c.name}</span>
          </span>
          <span className="min-w-0 rounded-md bg-primary/10 px-2 py-1.5">
            <span className="block truncate text-xs font-medium text-fg">{c.now}</span>
            <span className="mt-1 block h-1 overflow-hidden rounded-full bg-primary/15">
              <span
                className="block h-full rounded-full bg-primary"
                style={{ width: `${c.progress * 100}%` }}
              />
            </span>
          </span>
          <span className="min-w-0 truncate rounded-md bg-bg-subtle px-2 py-1.5 text-xs text-fg-muted">
            {c.next}
          </span>
        </div>
      ))}
    </div>
  );
}

function PosterFlow({ title, tool }: { title: string; tool: string }) {
  const { locale } = useLocale();
  const spec = useMemo(() => studioSpec(false), [locale]);
  const result = usePreview(spec);
  return (
    <Panel title={title} tool={tool}>
      {result ? (
        <>
          <div className="grid grid-cols-6 gap-2.5 sm:grid-cols-9">
            {result.items.slice(0, 18).map((it, i) => (
              <div key={it.key} className="stage-flow" style={{ animationDelay: `${i * 140}ms` }}>
                <Artwork
                  src={it.poster}
                  alt={it.title}
                  title={it.title}
                  className="aspect-[2/3] w-full overflow-hidden rounded-md shadow-sm ring-1 ring-border-subtle"
                />
              </div>
            ))}
          </div>
          <div className="mt-3 flex justify-end">
            <Badge variant="secondary">{t("{count} titles", { count: result.total })}</Badge>
          </div>
        </>
      ) : (
        <div className="grid grid-cols-9 gap-2.5">
          {Array.from({ length: 9 }, (_, i) => (
            <Skeleton key={i} className="aspect-[2/3] rounded-md" />
          ))}
        </div>
      )}
    </Panel>
  );
}
