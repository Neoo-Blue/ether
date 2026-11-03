import { Badge, Button, Sparkline, Switch, SubtabNav } from "@braedonsaunders/appkit-ui";
import { ArrowRight, GitFork, Sparkles, Undo2 } from "lucide-react";
import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { Drawer } from "../../components/Drawer";
import { ActionMenu } from "../../components/ActionMenu";
import { t } from "../../i18n";
import { PermissionRows, PluginMark } from "./marks";
import type { Installed, PermissionIcon } from "./proto";

type Tab = "overview" | "permissions" | "activity" | "logs";

/** One installed plugin: on/off, what it did, what it can do, its history and logs. */
export function PluginDrawer({
  plugin,
  onClose,
  onToggle,
}: {
  plugin: Installed;
  onClose: () => void;
  onToggle: (on: boolean) => void;
}) {
  const navigate = useNavigate();
  const detail = drawerFixture(plugin.id);
  const [tab, setTab] = useState<Tab>("overview");
  const [undone, setUndone] = useState<string[]>([]);
  const on = plugin.state !== "off";
  return (
    <Drawer
      open
      onClose={onClose}
      size="lg"
      title={plugin.name}
      headerActions={
        <ActionMenu
          label={t("More actions")}
          items={[
            {
              key: "update",
              label: t("Update to {version}", { version: detail.next }),
              onSelect: () => undefined,
            },
            {
              key: "rollback",
              label: t("Roll back to {version}", { version: detail.previous }),
              onSelect: () => undefined,
            },
            { key: "sep", separator: true },
            { key: "remove", label: t("Uninstall"), onSelect: () => undefined, danger: true },
          ]}
        />
      }
      subtabs={
        <SubtabNav
          ariaLabel={t("{name} sections", { name: plugin.name })}
          active={tab}
          onSelect={(k) => setTab(k as Tab)}
          tabs={[
            { key: "overview", label: t("Overview") },
            { key: "permissions", label: t("Permissions") },
            { key: "activity", label: t("Activity") },
            { key: "logs", label: t("Logs") },
          ]}
        />
      }
    >
      <div className="space-y-5">
        <header className="flex flex-wrap items-center gap-4 rounded-xl border border-border bg-surface p-5">
          <PluginMark category={plugin.category} size="lg" />
          <div className="min-w-0 flex-1 space-y-1.5">
            <div className="flex flex-wrap items-center gap-2">
              <p className="text-sm text-fg">{plugin.summary}</p>
              <span className="text-xs tabular-nums text-fg-subtle">{detail.version}</span>
            </div>
            <Badge variant={!on ? "secondary" : plugin.state === "shadow" ? "info" : "success"}>
              {!on ? t("Off") : plugin.state === "shadow" ? t("Trying out") : t("On")}
            </Badge>
          </div>
          <Switch
            aria-label={t("{name} on", { name: plugin.name })}
            checked={on}
            onChange={(e) => onToggle(e.currentTarget.checked)}
          />
        </header>

        {tab === "overview" ? (
          <>
            <div className="flex items-center justify-between gap-4 rounded-xl border border-border bg-surface p-4">
              <div>
                <p className="text-xs text-fg-muted">{t("This week")}</p>
                <p className="mt-0.5 font-semibold text-fg">{plugin.impact}</p>
              </div>
              {plugin.trend.length ? (
                <Sparkline points={plugin.trend} tone="good" area className="h-10 w-36" />
              ) : null}
            </div>
            <div className="flex flex-wrap gap-2">
              <Button
                onClick={() => navigate(`/plugins/studio?open=${encodeURIComponent(plugin.id)}`)}
              >
                <Sparkles size={15} /> {t("Open in Studio")}
              </Button>
              <Button
                variant="outline"
                onClick={() => navigate(`/plugins/studio?remix=${encodeURIComponent(plugin.id)}`)}
              >
                <GitFork size={15} /> {t("Make it mine")}
              </Button>
              <Link
                to={plugin.appearsAt.href}
                className="inline-flex h-9 items-center gap-1.5 px-2 text-sm text-primary hover:underline"
              >
                {t("See it in {place}", { place: plugin.appearsAt.label })} <ArrowRight size={14} />
              </Link>
            </div>
          </>
        ) : null}

        {tab === "permissions" ? (
          <section className="rounded-xl border border-border bg-surface p-4">
            <PermissionRows items={detail.permissions} />
            <p className="mt-4 text-xs text-fg-subtle">{detail.network}</p>
          </section>
        ) : null}

        {tab === "activity" ? (
          <ul className="divide-y divide-border rounded-xl border border-border bg-surface">
            {detail.activity.map((a) => (
              <li key={a.id} className="flex items-center gap-3 px-4 py-3">
                <div className="min-w-0 flex-1">
                  <p className="text-sm text-fg">{a.what}</p>
                  <p className="text-xs text-fg-subtle">{a.when}</p>
                </div>
                {undone.includes(a.id) ? (
                  <span className="text-xs text-fg-muted">{t("Undone")}</span>
                ) : (
                  <Button variant="ghost" size="sm" onClick={() => setUndone((u) => [...u, a.id])}>
                    <Undo2 size={14} /> {t("Undo")}
                  </Button>
                )}
              </li>
            ))}
          </ul>
        ) : null}

        {tab === "logs" ? (
          <pre className="overflow-x-auto rounded-xl border border-border bg-bg-subtle p-4 font-mono text-xs leading-relaxed text-fg-muted">
            {detail.logs}
          </pre>
        ) : null}
      </div>
    </Drawer>
  );
}

interface DrawerFixture {
  version: string;
  next: string;
  previous: string;
  permissions: { icon: PermissionIcon; label: string }[];
  network: string;
  activity: { id: string; what: string; when: string }[];
  logs: string;
}
function drawerFixture(id: string): DrawerFixture {
  const base = { version: "1.0.2", next: "1.1.0", previous: "1.0.1" };
  switch (id) {
    case "dev.ether.pluto-tv":
      return {
        version: "1.2.0",
        next: "1.2.1",
        previous: "1.1.0",
        permissions: [
          { icon: "network", label: t("Reads the Pluto TV channel list and guide") },
          { icon: "collections", label: t("Adds channels to Live TV") },
        ],
        network: t("Sample activity: contacted the channel and guide service."),
        activity: [
          { id: "channels", what: t("Refreshed 120 channels"), when: t("2 h ago") },
          { id: "guide", what: t("Updated the guide: 98% coverage"), when: t("2 h ago") },
          { id: "logos", what: t("Added channel logos"), when: t("Yesterday") },
        ],
        logs: "12:04:51  channels_refresh  120 channels\n12:04:52  guide_refresh  coverage=98%\n12:04:52  refresh_complete",
      };
    case "dev.ether.wrapped":
      return {
        ...base,
        permissions: [
          { icon: "history", label: t("Reads watch history for the yearly recap") },
          { icon: "collections", label: t("Shows a recap page and Home panel") },
        ],
        network: t("Contacted no other sites this week."),
        activity: [
          { id: "recap", what: t("Rebuilt the household yearly recap"), when: t("Yesterday") },
          { id: "panel", what: t("Updated the recap panel on Home"), when: t("Yesterday") },
        ],
        logs: "00:05:00  recap_build  period=12_months\n00:05:01  recap_ready  widgets=6\n12:04:51  page_views  weekly=14",
      };
    case "community.subdl":
      return {
        ...base,
        permissions: [{ icon: "network", label: t("Searches SubDL for subtitles") }],
        network: t("Sample activity: searched SubDL; downloaded no subtitles."),
        activity: [
          {
            id: "search",
            what: t("Found 9 subtitle matches while trying out"),
            when: t("This week"),
          },
          { id: "dry", what: t("Kept existing subtitle choices unchanged"), when: t("This week") },
        ],
        logs: "12:04:51  subtitle_search  mode=tryout\n12:04:52  matches  weekly=9\n12:04:52  downloads=0",
      };
    case "dev.ether.dub-language":
      return {
        ...base,
        permissions: [
          { icon: "history", label: t("Reads viewer language preferences") },
          { icon: "kv", label: t("Remembers each viewer's language") },
        ],
        network: t("Contacted no other sites this week."),
        activity: [
          {
            id: "dub",
            what: t("Picked the Spanish dub of Velvet Comet for Lucía"),
            when: t("2 h ago"),
          },
          { id: "language", what: t("Saved Lucía's language as Spanish"), when: t("Yesterday") },
        ],
        logs: "12:04:51  rank_key  4 releases\n12:04:51  picked Spanish dub  viewer=Lucía\n12:04:51  language_preference=Spanish",
      };
    default:
      return {
        ...base,
        permissions: [],
        network: t("No sample activity available."),
        activity: [],
        logs: "",
      };
  }
}
