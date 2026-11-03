import LiveStudio from "./LiveStudio";
import { Badge, Button, Dialog, Skeleton, Textarea, cn } from "@braedonsaunders/appkit-ui";
import {
  ArrowLeft,
  ArrowUp,
  Bell,
  Captions,
  Check,
  LayoutTemplate,
  ListOrdered,
  PanelsTopLeft,
  Sparkles,
  Square,
  Tv,
  type LucideIcon,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { Segmented } from "../../components/fields";
import { settingHref } from "../../components/SettingFocus";
import { Workspace, PanelHead } from "../../components/workspace";
import { t } from "../../i18n";
import { api } from "../../lib/api";
import { BlocksView } from "./Blocks";
import { CodeView, useCodeDraft, type Engine } from "./CodeView";
import { PermissionRows } from "./marks";
import { PageBuilder } from "./PageBuilder";
import {
  HomePreview,
  homeSummary,
  useHomeFacts,
  ROW_NAME,
  studioSpec,
  useContextTitles,
  usePreview,
} from "./preview";
import { installedPlugins, listings, suggestions, templates, type Template } from "./proto";
import { GuideStrip, StageRunner, scriptFor, type Script } from "./Stage";

type Phase = "home" | "stage" | "editor";
type View = "describe" | "blocks" | "page" | "code";

const TEMPLATE_ICON: Record<Template["icon"], LucideIcon> = {
  channels: Tv,
  home: LayoutTemplate,
  releases: ListOrdered,
  notify: Bell,
  page: PanelsTopLeft,
  subtitles: Captions,
};

/**
 * Plugin Studio. Its home is a stage with the conversation docked at the
 * bottom: describe a plugin and it assembles on the stage as the agent
 * works, then settles into the editor (Describe, Blocks, Code) beside the
 * live result.
 */
export default function Studio() {
  return import.meta.env.MODE === "mock" ? <PrototypeStudio /> : <LiveStudio />;
}
function PrototypeStudio() {
  const [params] = useSearchParams();
  const demo = params.get("demo") ?? (params.get("open") ? "home" : null);
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
  const [phase, setPhase] = useState<Phase>(demo ? "editor" : "home");
  const [asked, setAsked] = useState(
    demo === "page"
      ? t("A page with our year in review")
      : demo === "channels"
        ? t("Find live TV sports channels from Rwanda")
        : demo
          ? t("Make a Home row of 4K titles nobody has watched")
          : "",
  );
  const script = useMemo(() => scriptFor(asked), [asked]);
  const start = (text: string) => {
    if (!text.trim()) return;
    setAsked(text.trim());
    setPrompt("");
    setPhase("stage");
  };
  const settle = useCallback(() => setPhase("editor"), []);

  if (phase === "editor")
    return (
      <Editor
        asked={asked}
        script={script}
        initialView={(params.get("view") as View | null) ?? (demo === "page" ? "page" : "describe")}
        engine={(params.get("engine") as Engine | null) ?? "codemirror"}
        page={demo === "page"}
      />
    );

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
      <div className="flex min-h-0 flex-1 flex-col">
        <div className="app-scroll min-h-0 flex-1 overflow-y-auto">
          {phase === "stage" ? (
            <StageRunner script={script} onDone={settle} />
          ) : (
            <StageHome remixName={remixName} onPick={(text) => setPrompt(text)} />
          )}
        </div>
        <Composer
          value={prompt}
          onChange={setPrompt}
          onSend={start}
          working={phase === "stage"}
          onStop={settle}
          placeholder={
            phase === "stage"
              ? t("Building. You can add to it as it goes")
              : t("Describe the plugin you want")
          }
        />
      </div>
    </Workspace>
  );
}

function StageHome({ remixName, onPick }: { remixName: string; onPick: (text: string) => void }) {
  const chips = useMemo(suggestions, []);
  const cards = useMemo(templates, []);
  return (
    <div className="mx-auto max-w-3xl space-y-9 px-4 pb-10 pt-[min(10vh,5rem)]">
      <div className="space-y-4 text-center">
        <span className="mx-auto flex size-12 items-center justify-center rounded-2xl bg-primary/10 text-primary">
          <Sparkles size={24} aria-hidden />
        </span>
        <h2 className="text-2xl font-semibold tracking-tight text-fg">
          {remixName ? t("Make {name} yours", { name: remixName }) : t("What should Ether do?")}
        </h2>
        <p className="text-sm text-fg-muted">
          {t("It builds here, tested on your server. Nothing changes until you install.")}
        </p>
      </div>
      <div className="flex flex-wrap justify-center gap-2" aria-label={t("Ideas for this server")}>
        {[t("Find live TV sports channels from Rwanda"), ...chips].map((c) => (
          <button
            key={c}
            type="button"
            onClick={() => onPick(c)}
            className="inline-flex items-center gap-1.5 rounded-full border border-border bg-surface px-3 py-1.5 text-sm text-fg-muted transition hover:border-border-strong hover:text-fg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
          >
            <Sparkles size={13} className="text-primary" aria-hidden /> {c}
          </button>
        ))}
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
                onClick={() => onPick(c.prompt)}
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
  );
}

/** The conversation, docked at the bottom of the stage. */
function Composer({
  value,
  onChange,
  onSend,
  working,
  onStop,
  placeholder,
  compact,
}: {
  value: string;
  onChange: (v: string) => void;
  onSend: (text: string) => void;
  working?: boolean;
  onStop?: () => void;
  placeholder: string;
  compact?: boolean;
}) {
  const ref = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    if (!compact) ref.current?.focus();
  }, [compact, value]);
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        onSend(value);
      }}
      className={cn(
        "shrink-0 border-t border-border bg-bg-subtle/95 backdrop-blur",
        compact ? "p-3" : "px-4 py-4",
      )}
    >
      <div
        className={cn(
          "mx-auto flex w-full items-end gap-2 rounded-2xl border border-border bg-surface p-2 shadow-sm transition focus-within:border-primary/50 focus-within:shadow-md",
          !compact && "max-w-3xl",
        )}
      >
        <Textarea
          ref={ref}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              onSend(value);
            }
          }}
          rows={1}
          aria-label={placeholder}
          placeholder={placeholder}
          className="min-h-10 w-full min-w-0 flex-1 resize-none border-0 bg-transparent text-base shadow-none focus-visible:ring-0"
        />
        {working ? (
          <Button
            type="button"
            size="icon"
            variant="outline"
            onClick={onStop}
            aria-label={t("Stop")}
            className="shrink-0 rounded-full"
          >
            <Square size={14} />
          </Button>
        ) : (
          <Button
            type="submit"
            size="icon"
            disabled={!value.trim()}
            aria-label={t("Build")}
            className="shrink-0 rounded-full"
          >
            <ArrowUp size={16} />
          </Button>
        )}
      </div>
    </form>
  );
}

function Editor({
  asked,
  script,
  initialView,
  engine,
  page,
}: {
  asked: string;
  script: Script;
  initialView: View;
  engine: Engine;
  page: boolean;
}) {
  const navigate = useNavigate();
  const [view, setView] = useState<View>(initialView);
  const [kids, setKids] = useState(false);
  const draft = useCodeDraft(kids);
  const facts = useHomeFacts(kids);
  const [turns, setTurns] = useState<{ ask: string; done?: string }[]>([]);
  const [reply, setReply] = useState("");
  const [confirm, setConfirm] = useState(false);
  const [pane, setPane] = useState<"steps" | "preview">("preview");
  const titles = useContextTitles();
  const result = usePreview(useMemo(() => studioSpec(kids), [kids]));
  const channels = script.key === "channels" && !page;

  const refine = (text: string) => {
    const v = text.trim();
    if (!v) return;
    setReply("");
    setTurns((list) => [...list, { ask: v }]);
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
    }, 1100);
  };

  const install = async () => {
    if (channels) {
      navigate(settingHref("/livetv", "Kigali Sport 1"));
      return;
    }
    const created = await api
      .createCollection({ ...studioSpec(kids), enabled: true })
      .catch(() => null);
    const at = created?.id
      ? `/collections?collection=${encodeURIComponent(created.id)}`
      : "/collections";
    navigate(settingHref(at, ROW_NAME));
  };

  const preview = channels ? (
    <ChannelsPreview />
  ) : (
    <HomePreview ready result={result} titles={titles} skipKids={kids} />
  );

  const views: { value: View; label: string }[] = [
    { value: "describe", label: t("Describe") },
    { value: "blocks", label: t("Blocks") },
    ...(page ? [{ value: "page" as View, label: t("Page") }] : []),
    { value: "code", label: t("Code") },
  ];

  const header = (
    <>
      <Segmented
        size="sm"
        ariaLabel={t("Edit as")}
        value={view}
        onChange={setView}
        options={views}
      />
      <Button onClick={() => setConfirm(true)}>{t("Install")}</Button>
    </>
  );

  const describeAside = (
    <div className="flex min-h-full flex-col">
      <div className="flex-1 space-y-5 p-4 sm:p-5">
        <div className="ml-auto w-fit max-w-[85%] rounded-2xl rounded-br-md bg-primary px-3.5 py-2.5 text-sm text-primary-fg">
          {asked}
        </div>
        <ul className="space-y-2.5" aria-label={t("What it did")}>
          {(script.key === "home" && !page ? homeSummary(facts) : builtSummary(script, page)).map(
            (line) => (
              <li key={line} className="flex items-start gap-2 text-sm text-fg">
                <Check size={14} className="mt-0.5 shrink-0 text-success" aria-hidden /> {line}
              </li>
            ),
          )}
        </ul>
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
      <div className="sticky bottom-0">
        <Composer
          compact
          value={reply}
          onChange={setReply}
          onSend={refine}
          placeholder={t("Tell it what to change")}
        />
      </div>
    </div>
  );

  return (
    <>
      {view === "describe" ? (
        <Workspace
          title={t("Studio")}
          subtitle={asked}
          actions={header}
          asideLabel={t("Describe")}
          mainLabel={t("Preview")}
          aside={
            <>
              <div className="border-b border-border p-2 lg:hidden">
                <Segmented
                  size="sm"
                  ariaLabel={t("Show")}
                  value={pane}
                  onChange={setPane}
                  options={[
                    { value: "steps", label: t("Steps") },
                    { value: "preview", label: t("Preview") },
                  ]}
                />
              </div>
              <div className={cn(pane === "preview" && "hidden lg:block")}>{describeAside}</div>
              <div className={cn("lg:hidden", pane === "steps" && "hidden")}>
                {page ? <PageBuilder /> : preview}
              </div>
            </>
          }
          main={
            <div className="hidden min-h-full lg:block">{page ? <PageBuilder /> : preview}</div>
          }
        />
      ) : (
        <Workspace title={t("Studio")} subtitle={asked} actions={header}>
          {view === "blocks" ? (
            <BlocksView skipKids={kids} onSkipKids={setKids} />
          ) : view === "page" ? (
            <PageBuilder />
          ) : (
            <CodeView engine={engine} draft={draft} onSkipKids={setKids} preview={preview} />
          )}
        </Workspace>
      )}
      {confirm ? (
        <Dialog
          open
          onClose={() => setConfirm(false)}
          title={t("Install {name}", { name: channels ? t("Rwanda sports channels") : ROW_NAME })}
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
            items={
              channels
                ? [
                    { icon: "network", label: t("Reads 3 stream addresses and their guide") },
                    { icon: "collections", label: t("Adds 3 channels to Live TV") },
                  ]
                : [
                    { icon: "history", label: t("Sees which titles viewers have started") },
                    { icon: "collections", label: t("Creates one collection, shown on Home") },
                  ]
            }
          />
        </Dialog>
      ) : null}
    </>
  );
}

function builtSummary(script: Script, page: boolean): string[] {
  if (page)
    return [
      t("Reads watch history for the last 12 months"),
      t("Groups plays by month and by viewer"),
      t("A page with 6 widgets, in the sidebar"),
    ];
  return script.key === "channels"
    ? [
        t("Searched 4 kinds of source, found 7 streams"),
        t("Removed duplicates, left 4 channels"),
        t("Played each one through Ether: 3 work"),
        t("Built a guide for all 3"),
      ]
    : []; // Home facts come from the shared recipe queries above.
}

function ChannelsPreview() {
  return (
    <div className="flex min-h-full flex-col">
      <PanelHead className="pb-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-sm font-semibold text-fg">{t("Live TV guide as viewers see it")}</h2>
          <div className="flex flex-wrap gap-1.5">
            <Badge variant="secondary">{t("3 channels")}</Badge>
            <Badge variant="secondary">{t("Guide from the same lists")}</Badge>
          </div>
        </div>
      </PanelHead>
      <div className="space-y-5 p-4 sm:p-5">
        <GuideStrip
          channels={[
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
          ]}
        />
      </div>
    </div>
  );
}
