import { Badge, Button, Dialog, Skeleton, Textarea, cn } from "@braedonsaunders/appkit-ui";
import {
  ArrowLeft,
  ArrowUp,
  Bell,
  Captions,
  Check,
  Code2,
  LayoutTemplate,
  ListOrdered,
  PanelsTopLeft,
  Sparkles,
  Tv,
  Wrench,
  type LucideIcon,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { Shelf } from "../../components/Poster";
import { settingHref } from "../../components/SettingFocus";
import { Workspace, PanelHead } from "../../components/workspace";
import { t } from "../../i18n";
import { api, apiFetch } from "../../lib/api";
import type { TitleCard } from "../../lib/contract";
import type { CollectionSpec } from "../../lib/contract";
import { blankSpec } from "../../lib/rules";
import { PermissionRows } from "./marks";
import {
  PLUGIN_SOURCE,
  buildSteps,
  installedPlugins,
  listings,
  suggestions,
  templates,
  type BuildStep,
  type Template,
} from "./proto";

type Phase = "idle" | "building" | "ready";

/** Plugin Studio: describe a plugin, watch it get built, see the real result, install. */
export default function Studio() {
  const [params] = useSearchParams();
  const remix = params.get("remix");
  const remixName = useMemo(() => {
    if (!remix) return "";
    return (
      installedPlugins().find((p) => p.id === remix)?.name ??
      listings().find((l) => l.id === remix)?.name ??
      ""
    );
  }, [remix]);
  const [prompt, setPrompt] = useState(
    remixName ? t("Start from {name} and ", { name: remixName }) : "",
  );
  const [phase, setPhase] = useState<Phase>("idle");
  const [asked, setAsked] = useState("");

  const start = (text: string) => {
    if (!text.trim()) return;
    setAsked(text.trim());
    setPhase("building");
  };

  if (phase === "idle")
    return (
      <FrontDoor prompt={prompt} setPrompt={setPrompt} onStart={start} remixName={remixName} />
    );
  return <Build asked={asked} onReady={() => setPhase("ready")} ready={phase === "ready"} />;
}

const TEMPLATE_ICON: Record<Template["icon"], LucideIcon> = {
  channels: Tv,
  home: LayoutTemplate,
  releases: ListOrdered,
  notify: Bell,
  page: PanelsTopLeft,
  subtitles: Captions,
};

function FrontDoor({
  prompt,
  setPrompt,
  onStart,
  remixName,
}: {
  prompt: string;
  setPrompt: (v: string) => void;
  onStart: (text: string) => void;
  remixName: string;
}) {
  const input = useRef<HTMLTextAreaElement>(null);
  const chips = useMemo(suggestions, []);
  const cards = useMemo(templates, []);
  useEffect(() => input.current?.focus(), []);
  const fill = (text: string) => {
    setPrompt(text);
    requestAnimationFrame(() => {
      const el = input.current;
      if (!el) return;
      el.focus();
      el.setSelectionRange(text.length, text.length);
    });
  };
  return (
    <Workspace
      title={t("Studio")}
      actions={
        <Link
          to="/plugins"
          className="inline-flex items-center gap-1.5 text-sm text-fg-muted hover:text-fg"
        >
          <ArrowLeft size={14} /> {t("Plugins")}
        </Link>
      }
    >
      <div className="app-scroll min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto max-w-3xl space-y-10 px-4 pb-12 pt-[min(12vh,7rem)]">
          <div className="space-y-5">
            <h2 className="text-center text-2xl font-semibold tracking-tight text-fg">
              {remixName
                ? t("Make {name} yours", { name: remixName })
                : t("Describe the plugin you want")}
            </h2>
            <form
              onSubmit={(e) => {
                e.preventDefault();
                onStart(prompt);
              }}
              className="rounded-2xl border border-border bg-surface p-2 shadow-sm transition focus-within:border-primary/50 focus-within:shadow-md"
            >
              <Textarea
                ref={input}
                value={prompt}
                onChange={(e) => setPrompt(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && !e.shiftKey) {
                    e.preventDefault();
                    onStart(prompt);
                  }
                }}
                rows={3}
                aria-label={t("Describe the plugin you want")}
                placeholder={t("A Home row, a channel list, a rule for releases, anything")}
                className="resize-none border-0 bg-transparent text-base shadow-none focus-visible:ring-0"
              />
              <div className="flex items-center justify-between gap-2 px-2 pb-1">
                <span className="text-xs text-fg-subtle">
                  {t("Builds and checks on your server. Nothing changes until you install.")}
                </span>
                <Button
                  type="submit"
                  size="icon"
                  disabled={!prompt.trim()}
                  aria-label={t("Build")}
                  className="rounded-full"
                >
                  <ArrowUp size={16} />
                </Button>
              </div>
            </form>
            <div
              className="flex flex-wrap justify-center gap-2"
              aria-label={t("Ideas for this server")}
            >
              {chips.map((c) => (
                <button
                  key={c}
                  type="button"
                  onClick={() => fill(c)}
                  className="inline-flex items-center gap-1.5 rounded-full border border-border bg-surface px-3 py-1.5 text-sm text-fg-muted transition hover:border-border-strong hover:text-fg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
                >
                  <Sparkles size={13} className="text-primary" aria-hidden /> {c}
                </button>
              ))}
            </div>
          </div>

          <section aria-label={t("Start from")} className="space-y-3">
            <h3 className="text-sm font-semibold text-fg">{t("Or start from")}</h3>
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {cards.map((c) => {
                const Icon = TEMPLATE_ICON[c.icon];
                return (
                  <button
                    key={c.key}
                    type="button"
                    onClick={() => fill(c.prompt)}
                    className="group flex items-start gap-3 rounded-xl border border-border bg-surface p-4 text-left transition hover:border-border-strong hover:shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
                  >
                    <span className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
                      <Icon size={20} aria-hidden />
                    </span>
                    <span className="min-w-0">
                      <span className="block text-sm font-semibold text-fg">{c.title}</span>
                      <span className="mt-0.5 block text-sm text-fg-muted">{c.description}</span>
                    </span>
                  </button>
                );
              })}
            </div>
          </section>
        </div>
      </div>
    </Workspace>
  );
}

type StepState = "pending" | "running" | "done";

const ROW_NAME = "4K nobody has watched";

function Build({ asked, ready, onReady }: { asked: string; ready: boolean; onReady: () => void }) {
  const navigate = useNavigate();
  const steps = useMemo(buildSteps, []);
  const [at, setAt] = useState(0);
  const [elapsed, setElapsed] = useState(0);
  const [turns, setTurns] = useState<{ ask: string; done?: string }[]>([]);
  const [refining, setRefining] = useState(false);
  const [reply, setReply] = useState("");
  const [showCode, setShowCode] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const started = useRef(Date.now());

  useEffect(() => {
    if (at >= steps.length) {
      setElapsed(Math.round((Date.now() - started.current) / 1000));
      onReady();
      return;
    }
    const timer = window.setTimeout(() => setAt((n) => n + 1), steps[at]!.ms);
    return () => window.clearTimeout(timer);
  }, [at, steps, onReady]);

  // The surrounding Home rows, for context only.
  const [titles, setTitles] = useState<TitleCard[] | null>(null);
  useEffect(() => {
    const ctl = new AbortController();
    const load = (tries: number) =>
      apiFetch<{ items: TitleCard[] }>("/catalog/browse?sort=popularity&limit=40", {
        signal: ctl.signal,
      })
        .then((r) => setTitles(r.items))
        .catch(() => {
          if (!ctl.signal.aborted && tries > 0) window.setTimeout(() => void load(tries - 1), 300);
        });
    void load(5);
    return () => ctl.abort();
  }, []);

  // The plugin's own result: exactly the collection that installing creates,
  // so the preview, the counts and the landed page always agree.
  const [kids, setKids] = useState(false);
  const spec = useMemo(() => studioSpec(kids), [kids]);
  const [result, setResult] = useState<{ total: number; items: TitleCard[] } | null>(null);
  useEffect(() => {
    const ctl = new AbortController();
    const load = (tries: number) =>
      api
        .previewCollection(spec, ctl.signal)
        .then((r) => setResult({ total: r.total, items: r.items }))
        .catch(() => {
          if (!ctl.signal.aborted && tries > 0) window.setTimeout(() => void load(tries - 1), 300);
        });
    void load(5);
    return () => ctl.abort();
  }, [spec]);

  const refine = (text: string) => {
    const v = text.trim();
    if (!v) return;
    setReply("");
    setTurns((list) => [...list, { ask: v }]);
    setRefining(true);
    window.setTimeout(() => {
      const kidsAsk = /kid|child|family/i.test(v);
      if (kidsAsk) setKids(true);
      setTurns((list) => {
        const next = list.slice();
        next[next.length - 1] = {
          ask: v,
          done: kidsAsk ? t("Now skips kids' titles") : t("Updated the preview"),
        };
        return next;
      });
      setRefining(false);
    }, 1200);
  };

  const install = async () => {
    const saved = await api.createCollection({ ...spec, enabled: true }).catch(() => null);
    const at = saved?.id
      ? `/collections?collection=${encodeURIComponent(saved.id)}`
      : "/collections";
    navigate(settingHref(at, ROW_NAME));
  };

  const state = (i: number): StepState => (i < at ? "done" : i === at ? "running" : "pending");
  const previewReady = ready && result !== null;
  const lines = PLUGIN_SOURCE(kids).trimEnd().split("\n").length;
  const resultLine = (step: BuildStep) =>
    step.key === "check"
      ? result
        ? t("{count} titles match on this server", { count: result.total })
        : step.result
      : step.key === "build"
        ? t("Plugin written, {count} lines", { count: lines })
        : step.result;

  return (
    <>
      <Workspace
        title={t("Studio")}
        subtitle={asked}
        actions={
          <>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setShowCode((v) => !v)}
              disabled={!ready}
            >
              <Code2 size={15} /> {showCode ? t("Show result") : t("Show code")}
            </Button>
            <Button onClick={() => setConfirm(true)} disabled={!ready}>
              {t("Install")}
            </Button>
          </>
        }
        asideLabel={t("Build")}
        mainLabel={t("Preview")}
        aside={
          <div className="flex min-h-full flex-col">
            <div className="flex-1 space-y-6 p-4 sm:p-5">
              <div className="ml-auto w-fit max-w-[85%] rounded-2xl rounded-br-md bg-primary px-3.5 py-2.5 text-sm text-primary-fg">
                {asked}
              </div>
              <ol className="relative space-y-0" aria-label={t("Build steps")}>
                {steps.map((s, i) => (
                  <StepRow
                    key={s.key}
                    step={{ ...s, result: resultLine(s) }}
                    state={state(i)}
                    last={i === steps.length - 1}
                  />
                ))}
              </ol>
              {ready ? (
                <p className="flex items-center gap-2 text-sm text-fg-muted">
                  <Check size={14} className="text-success" aria-hidden />
                  {t("Working preview in {seconds} s", { seconds: elapsed })}
                </p>
              ) : null}
              {turns.map((turn, i) => (
                <div key={`${turn.ask}-${i}`} className="space-y-2">
                  <div className="ml-auto w-fit max-w-[85%] rounded-2xl rounded-br-md bg-primary px-3.5 py-2.5 text-sm text-primary-fg">
                    {turn.ask}
                  </div>
                  {turn.done ? (
                    <p className="flex items-center gap-2 text-sm text-fg">
                      <Check size={14} className="shrink-0 text-success" aria-hidden /> {turn.done}
                    </p>
                  ) : (
                    <Skeleton className="h-5 w-48" />
                  )}
                </div>
              ))}
            </div>
            <form
              onSubmit={(e) => {
                e.preventDefault();
                refine(reply);
              }}
              className="sticky bottom-0 w-full border-t border-border bg-bg-subtle/95 p-3 backdrop-blur"
            >
              <div className="flex w-full items-end gap-2 rounded-xl border border-border bg-surface p-1.5 focus-within:border-primary/50">
                <Textarea
                  value={reply}
                  onChange={(e) => setReply(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && !e.shiftKey) {
                      e.preventDefault();
                      refine(reply);
                    }
                  }}
                  rows={1}
                  disabled={!ready || refining}
                  aria-label={t("Tell it what to change")}
                  placeholder={t("Tell it what to change")}
                  className="min-h-9 w-full min-w-0 flex-1 resize-none border-0 bg-transparent shadow-none focus-visible:ring-0"
                />
                <Button
                  type="submit"
                  size="icon"
                  disabled={!ready || !reply.trim()}
                  aria-label={t("Send")}
                  className="shrink-0 rounded-full"
                >
                  <ArrowUp size={16} />
                </Button>
              </div>
            </form>
          </div>
        }
        main={
          showCode ? (
            <pre className="m-4 overflow-auto rounded-xl border border-border bg-surface p-4 font-mono text-xs leading-relaxed text-fg sm:m-5">
              {PLUGIN_SOURCE(kids)}
            </pre>
          ) : (
            <div className="flex min-h-full flex-col">
              <PanelHead className="pb-3">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <h2 className="text-sm font-semibold text-fg">{t("Home as Alex sees it")}</h2>
                  {previewReady ? (
                    <div className="flex flex-wrap gap-1.5">
                      <Badge variant="secondary">
                        {t("{count} titles", { count: result?.total ?? 0 })}
                      </Badge>
                      <Badge variant="secondary">{t("Rebuilt nightly")}</Badge>
                      <Badge variant="secondary">{t("Everyone's Home")}</Badge>
                    </div>
                  ) : null}
                </div>
              </PanelHead>
              <div className="space-y-7 p-4 sm:p-5">
                <HomeRowPreview
                  title={t("Continue watching")}
                  items={titles?.slice(0, 10) ?? null}
                  dim
                />
                <div
                  className={cn(
                    "-mx-2 rounded-xl p-2 transition",
                    previewReady && "bg-primary/5 ring-1 ring-primary/30",
                  )}
                >
                  {previewReady ? (
                    <HomeRowPreview title={ROW_NAME} items={result?.items ?? []} badge={t("New")} />
                  ) : (
                    <ShelfSkeleton />
                  )}
                </div>
                <HomeRowPreview
                  title={t("Trending on your server")}
                  items={titles?.slice(30, 42) ?? null}
                  dim
                />
              </div>
            </div>
          )
        }
      />
      {confirm ? (
        <Dialog
          open
          onClose={() => setConfirm(false)}
          title={t("Install {name}", { name: ROW_NAME })}
          footer={
            <>
              <Button variant="ghost" onClick={() => setConfirm(false)}>
                {t("Cancel")}
              </Button>
              <Button onClick={() => void install()}>{t("Install")}</Button>
            </>
          }
        >
          <PermissionRows
            items={[
              { icon: "history", label: t("Sees which titles viewers have started") },
              { icon: "collections", label: t("Creates one collection, shown on Home") },
            ]}
          />
        </Dialog>
      ) : null}
    </>
  );
}

function StepRow({ step, state, last }: { step: BuildStep; state: StepState; last: boolean }) {
  return (
    <li className="relative flex gap-3 pb-5 last:pb-0">
      {!last ? (
        <span
          aria-hidden
          className={cn(
            "absolute left-[13px] top-7 h-[calc(100%-1.75rem)] w-px transition-colors duration-500",
            state === "done" ? "bg-primary/50" : "bg-border",
          )}
        />
      ) : null}
      <span
        aria-hidden
        className={cn(
          "relative z-10 flex size-7 shrink-0 items-center justify-center rounded-full border transition-all duration-300",
          state === "done" && "border-primary bg-primary text-primary-fg",
          state === "running" && "border-primary bg-surface",
          state === "pending" && "border-border bg-surface",
        )}
      >
        {state === "done" ? (
          <Check size={14} strokeWidth={3} />
        ) : state === "running" ? (
          <span className="size-2.5 animate-pulse rounded-full bg-primary motion-reduce:animate-none" />
        ) : null}
      </span>
      <div className="min-w-0 flex-1 pt-0.5">
        <p
          className={cn("text-sm font-medium", state === "pending" ? "text-fg-subtle" : "text-fg")}
        >
          {step.label}
        </p>
        {state === "done" ? (
          <div className="space-y-1">
            <p className="text-sm text-fg-muted">{step.result}</p>
            {step.fixed ? (
              <p className="flex items-center gap-1.5 text-sm text-fg-muted">
                <Wrench size={13} className="shrink-0 text-primary" aria-hidden /> {step.fixed}
              </p>
            ) : null}
          </div>
        ) : state === "running" ? (
          <Skeleton className="mt-1.5 h-4 w-40" />
        ) : null}
      </div>
    </li>
  );
}

function HomeRowPreview({
  title,
  items,
  dim,
  badge,
}: {
  title: string;
  items: TitleCard[] | null;
  dim?: boolean;
  badge?: string;
}) {
  return (
    <section aria-label={title} className={cn("space-y-2.5", dim && "opacity-45")}>
      <h3 className="flex items-center gap-2 text-sm font-semibold text-fg">
        {title}
        {badge ? <Badge variant="info">{badge}</Badge> : null}
      </h3>
      {items ? (
        <Shelf items={items} ariaLabel={title} posterWidth="w-24 sm:w-28" />
      ) : (
        <ShelfSkeleton />
      )}
    </section>
  );
}

function ShelfSkeleton() {
  return (
    <div className="space-y-2.5">
      <Skeleton className="h-4 w-44" />
      <div className="flex gap-3 overflow-hidden">
        {Array.from({ length: 8 }, (_, i) => (
          <Skeleton key={i} className="aspect-[2/3] w-24 shrink-0 rounded-md sm:w-28" />
        ))}
      </div>
    </div>
  );
}

/** The collection the prototype plugin builds, before and after "skip kids' titles". */
function studioSpec(skipKids: boolean): CollectionSpec {
  const spec = blankSpec("mixed");
  spec.name = ROW_NAME;
  spec.description = t("Built in Studio");
  spec.rules = [
    { field: "watched", op: "is", value: false },
    { field: "rating", op: "gte", value: 7 },
  ];
  spec.exclude = skipKids
    ? [
        { field: "genre", op: "in", value: ["Family", "Animation"] },
        { field: "contentRating", op: "in", value: ["G", "PG", "TV-Y", "TV-Y7", "TV-G"] },
      ]
    : [];
  spec.sort = "rating";
  spec.sortDesc = true;
  spec.limit = 18;
  spec.targets = { ...spec.targets, homeRow: true, collectionsTab: true };
  return spec;
}
