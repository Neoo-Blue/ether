/**
 * Every individual owner setting ⌘K can jump to. Schema-driven forms are
 * listed from their field groups; hand-built sections are listed by the
 * exact row title they render. `more` names the "More options" disclosure
 * (its accessible label) that hides the row, so the jump can open it.
 */

import { t } from "../i18n";
import type { FieldDef, FieldGroup } from "../pages/settings/SchemaForm";
import * as S from "../pages/settings/schema";
import { UpcomingEpisodeSettings } from "../pages/catalog/HomeReleaseSettings";

export interface SettingHit {
  /** Visible row title; the jump looks for this text on the page. */
  title: string;
  /** Section or page the setting sits in, shown as the search subtitle. */
  where: string;
  href: string;
  /** Accessible label of the "More options" disclosure hiding the row. */
  more?: string;
  keywords?: string;
}

function moreLabel(title?: string) {
  return title ? t("More options: {title}", { title }) : t("More options");
}

function fieldHits(
  defs: FieldDef[],
  href: string,
  where: string,
  more: string | undefined,
): SettingHit[] {
  return defs.flatMap((f) => {
    if (f.kind === "hidden") return [];
    if (f.kind === "group") return fieldHits(f.fields, href, where, more);
    return [
      {
        title: f.label,
        where,
        href,
        more,
        keywords: typeof f.help === "string" ? f.help : undefined,
      },
    ];
  });
}

function groupHits(
  groups: readonly FieldGroup[],
  href: string,
  page: string,
  more?: (g: FieldGroup) => string | undefined,
): SettingHit[] {
  return groups.flatMap((g) => {
    const where = g.title ? `${page} › ${g.title}` : page;
    return [
      ...fieldHits(g.fields, href, where, undefined),
      ...fieldHits(g.more ?? [], href, where, more ? more(g) : moreLabel(g.title)),
    ];
  });
}

function rows(href: string, where: string, titles: string[], more?: string): SettingHit[] {
  return titles.map((title) => ({ title, where, href, more }));
}

export function settingsIndex(): SettingHit[] {
  const general = t("General");
  const quality = t("Quality");
  const playback = t("Playback");
  const catalog = t("Catalog settings");
  const plex = "Plex";
  const away = t("Away from home");
  return [
    {
      title: t("Poster source"),
      where: t("Artwork"),
      href: "/metadata/artwork",
      keywords: "TMDB RPDB Custom PostersPlus XRDB BetterPosters",
    },
    ...["PostersPlus", "XRDB", "BetterPosters", "RPDB"].map((title) => ({
      title,
      where: t("Integrations"),
      href: "/services/overview",
      keywords: "poster API key",
    })),
    // General
    ...groupHits(S.SERVER, "/server", general),
    ...rows("/server", general, [
      t("Send indexers, debrid and Usenet through a proxy"),
      t("Proxy address"),
      t("How apps connect"),
      t("Port"),
      t("Local URL"),
      t("Remote URL"),
    ]),
    ...rows(
      "/server",
      general,
      [t("Username"), t("Password"), t("Names that stay on this network")],
      t("More options: How Ether reaches the internet"),
    ),
    // Apps
    ...groupHits(S.AWAY, "/apps/overview", t("Apps"), (g) =>
      g.id === "certificate" ? t("Use your own certificate") : moreLabel(away),
    ).map((h) => ({ ...h, where: `${t("Apps")} › ${away}` })),
    ...groupHits(
      S.PLEX.filter((g) => g.id === "compatibility"),
      "/apps/plex/account",
      plex,
    ),
    ...rows("/apps/plex/account", plex, [t("Use Plex's metadata for Plex apps")]),
    ...groupHits(
      S.PLEX.filter((g) => g.id !== "compatibility"),
      "/apps/plex/remote",
      plex,
    ),
    ...groupHits(S.DOWNLOADS, "/apps/plex/downloads", plex),
    // Catalog
    ...groupHits(S.CATALOG, "/catalog/settings", catalog),
    ...groupHits([UpcomingEpisodeSettings], "/catalog/settings", catalog),
    ...rows("/catalog/settings", catalog, [
      t("Hide obscure titles"),
      t("Watchlist monitoring"),
      t("Plex watchlists"),
      t("Trakt watchlists"),
      t("Tell me too"),
      t("Check every"),
    ]),
    ...rows("/catalog/settings", `${catalog} › ${t("New releases")}`, [
      t("When they can play"),
      t("On digital release"),
      t("Right away"),
    ]),
    ...rows("/catalog/rules", catalog, [t("Rules"), t("Hide"), t("Show")]),
    // Sources
    ...groupHits(S.DEBRID, "/sources/debrid", t("Debrid"), () => t("More options")),
    ...rows(
      "/sources/indexers",
      t("Indexers"),
      [t("Daily budget for new-release checks")],
      t("More options: {title}", { title: t("Daily budgets") }),
    ),
    ...rows(
      "/sources/indexers",
      t("Site definitions"),
      [t("Definitions source"), "FlareSolverr"],
      t("More options: {title}", { title: t("Site definitions") }),
    ),
    // Quality
    ...rows("/quality/profiles", `${quality} › ${away}`, [t("Away profile"), t("Bitrate limit")]),
    ...rows("/quality/profiles", quality, [t("Source order")]),
    ...rows("/quality/check", quality, [t("Check quality")]),
    ...groupHits(S.PLAY_VERSIONS, "/quality/profiles", quality),
    ...rows("/quality/searching", `${quality} › ${t("Start speed")}`, [
      t("Start before the health check finishes"),
    ]),
    ...rows(
      "/quality/searching",
      `${quality} › ${t("Start speed")}`,
      [t("Concurrent checks")],
      t("More options: Start speed"),
    ),
    ...groupHits(S.RELEASE_SEARCH, "/quality/searching", quality),
    ...groupHits(S.RELEASE_BLOCKS, "/quality/blocklist", quality),
    // Playback
    ...rows("/playback/subtitles", `${playback} › ${t("Languages")}`, [
      t("Audio languages"),
      t("Prefer the original language"),
      t("Subtitle languages"),
      t("Show subtitles"),
      t("Subtitle style"),
    ]),
    ...rows(
      "/playback/subtitles",
      `${playback} › ${t("Subtitle appearance")}`,
      [t("Size"), t("Text colour"), t("Background"), t("Position")],
      t("More options: {title}", { title: t("Subtitle appearance") }),
    ),
    ...rows(
      "/playback/subtitles",
      `${playback} › ${t("Audio processing")}`,
      [t("Normalize loudness"), t("Night mode compression"), t("Boost centre-channel dialogue")],
      t("More options: {title}", { title: t("Audio processing") }),
    ),
    ...groupHits(S.STREAM_LIMITS, "/playback/transcoding", playback),
    ...groupHits(S.TRANSCODE, "/playback/transcoding", playback),
    ...groupHits(S.EXTRAS, "/playback/extras", playback),
    ...rows("/playback/extras", `${playback} › ${t("Skip buttons")}`, [
      t("Skip intro"),
      t("Skip credits"),
    ]),
    ...groupHits(S.UP_NEXT, "/playback/extras", playback),
    // Home
    ...groupHits(S.PERSONAL_TOUCHES, "/home", t("Home")),
    // Notifications
    ...rows("/notifications/report", t("Weekly report"), [
      t("Email the weekly report"),
      t("Day"),
      t("Time"),
      t("Report viewer"),
    ]),
    // Maintenance
    ...groupHits(S.UPDATES, "/maintenance/updates", t("Updates")),
    ...rows("/maintenance/health", t("Health"), [
      t("Send uptime heartbeat"),
      t("Secret heartbeat URL"),
    ]),
    ...groupHits(S.SPIKE, "/maintenance/health", t("Health")),
    ...groupHits(S.LOG_LEVEL, "/maintenance/logs", t("Logs")),
  ];
}
