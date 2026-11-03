import { Badge, Button, Sparkline, buttonVariants, cn, toast } from "@braedonsaunders/appkit-ui";
import { ArrowRight, Eye, GitFork, Sparkles, Store } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { DomainPage } from "../../components/layout";
import { LevelBadge } from "../../components/status";
import { t } from "../../i18n";
import { Marketplace } from "./Marketplace";
import { PluginMark } from "./marks";
import { installedPlugins, type Installed, type Listing } from "./proto";

/** Installed plugins, their real effect, and the two ways to get more. */
export default function Plugins() {
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const [installed, setInstalled] = useState(installedPlugins);
  const [fresh, setFresh] = useState<string | null>(null);
  const marketOpen = params.get("marketplace") !== null;
  const setMarket = (open: boolean) => {
    const next = new URLSearchParams(params);
    if (open) next.set("marketplace", "");
    else next.delete("marketplace");
    setParams(next, { replace: true });
  };

  useEffect(() => {
    if (!fresh) return;
    const el = document.getElementById(`plugin-${fresh}`);
    el?.scrollIntoView({ block: "center", behavior: "smooth" });
    el?.classList.add("setting-focus");
    const timer = window.setTimeout(() => el?.classList.remove("setting-focus"), 2400);
    return () => window.clearTimeout(timer);
  }, [fresh]);

  const install = (l: Listing) => {
    setMarket(false);
    const added: Installed = {
      id: l.id,
      name: l.name,
      summary: l.summary,
      category: l.category,
      state: "on",
      impact: t("Installed just now"),
      trend: [],
      appearsAt: { label: l.adds[0]?.where ?? t("Plugins"), href: "/livetv" },
    };
    setInstalled((list) => [added, ...list.filter((p) => p.id !== l.id)]);
    setFresh(l.id);
    toast.success(t("{name} is on", { name: l.name }));
  };

  const totals = useMemo(() => {
    const on = installed.filter((p) => p.state === "on").length;
    const shadow = installed.filter((p) => p.state === "shadow").length;
    return { on, shadow };
  }, [installed]);

  return (
    <DomainPage
      title={t("Plugins")}
      base="/plugins"
      actions={
        <>
          <Button variant="outline" onClick={() => setMarket(true)}>
            <Store size={15} /> {t("Marketplace")}
          </Button>
          <Link to="/plugins/studio" className={buttonVariants()}>
            <Sparkles size={15} /> {t("Build a plugin")}
          </Link>
        </>
      }
    >
      <div className="grid gap-3 sm:grid-cols-3">
        <Tile label={t("Plugins on")} value={String(totals.on)} />
        <Tile label={t("Trying out")} value={String(totals.shadow)} />
        <Tile label={t("Health")} value={<LevelBadge level="ok" label={t("All working")} />} />
      </div>

      <ul className="grid gap-4 md:grid-cols-2 xl:grid-cols-3" aria-label={t("Installed plugins")}>
        {installed.map((p) => (
          <li key={p.id}>
            <PluginCard
              plugin={p}
              onRemix={() => navigate(`/plugins/studio?remix=${encodeURIComponent(p.id)}`)}
            />
          </li>
        ))}
      </ul>

      {marketOpen ? (
        <Marketplace
          open
          onClose={() => setMarket(false)}
          onInstall={install}
          onRemix={(l) => navigate(`/plugins/studio?remix=${encodeURIComponent(l.id)}`)}
        />
      ) : null}
    </DomainPage>
  );
}

function Tile({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="rounded-xl border border-border bg-surface px-4 py-3.5">
      <p className="text-xs text-fg-muted">{label}</p>
      <div className="mt-1 text-2xl font-semibold tabular-nums text-fg">{value}</div>
    </div>
  );
}

function PluginCard({ plugin, onRemix }: { plugin: Installed; onRemix: () => void }) {
  return (
    <article
      id={`plugin-${plugin.id}`}
      aria-label={plugin.name}
      className="flex h-full flex-col rounded-xl border border-border bg-surface p-5"
    >
      <div className="flex items-start gap-3">
        <PluginMark category={plugin.category} />
        <div className="min-w-0 flex-1 space-y-1">
          <h3 className="truncate font-semibold text-fg">{plugin.name}</h3>
          {plugin.state === "shadow" ? (
            <Badge variant="info">
              <Eye size={12} aria-hidden /> {t("Trying out")}
            </Badge>
          ) : plugin.state === "attention" ? (
            <LevelBadge level="warn" />
          ) : (
            <Badge variant="success">{t("On")}</Badge>
          )}
        </div>
      </div>
      <p className="mt-3 line-clamp-2 min-h-10 text-sm text-fg-muted">{plugin.summary}</p>
      <div className="mt-4 flex items-center justify-between gap-3 rounded-lg border border-border-subtle bg-bg-subtle px-3 py-2.5">
        <p className="text-sm font-medium text-fg">{plugin.impact}</p>
        {plugin.trend.length ? (
          <Sparkline
            points={plugin.trend}
            tone="good"
            area
            className="h-7 w-20 shrink-0"
            ariaLabel={t("Last 14 days")}
          />
        ) : null}
      </div>
      <div className="mt-auto flex flex-wrap items-center gap-2 pt-4">
        <Link
          to={plugin.appearsAt.href}
          className={cn(buttonVariants({ variant: "outline", size: "sm" }))}
        >
          {plugin.appearsAt.label} <ArrowRight size={14} />
        </Link>
        <Button variant="ghost" size="sm" onClick={onRemix}>
          <GitFork size={14} /> {t("Make it mine")}
        </Button>
      </div>
    </article>
  );
}
