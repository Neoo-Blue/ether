/**
 * Prototype fixtures for the plugin UI (Plugins page, Marketplace, Studio).
 * Throwaway: the real build reads these from /api/plugins and the agent.
 */
import { t } from "../../i18n";

export type Category =
  "channels" | "home" | "subtitles" | "sources" | "notifications" | "releases" | "pages" | "tools";

export function categoryLabel(c: Category): string {
  return {
    channels: t("Channels"),
    home: t("Home rows"),
    subtitles: t("Subtitles"),
    sources: t("Sources"),
    notifications: t("Notifications"),
    releases: t("Releases"),
    pages: t("Pages"),
    tools: t("Tools"),
  }[c];
}

export const CATEGORIES: Category[] = [
  "channels",
  "home",
  "subtitles",
  "sources",
  "notifications",
  "releases",
  "pages",
  "tools",
];

export interface Installed {
  id: string;
  name: string;
  summary: string;
  category: Category;
  state: "on" | "shadow" | "attention";
  /** The plugin's real effect this week, one short line. */
  impact: string;
  /** Daily counts for the last 14 days, for the impact sparkline. */
  trend: number[];
  appearsAt: { label: string; href: string };
}

export function installedPlugins(): Installed[] {
  return [
    {
      id: "dev.ether.dub-language",
      name: t("Dub language"),
      summary: t("Prefers releases dubbed in each viewer's language."),
      category: "releases",
      state: "on",
      impact: t("Picked a better release 37 times this week"),
      trend: [2, 4, 3, 5, 6, 4, 7, 5, 6, 8, 5, 7, 9, 6],
      appearsAt: { label: t("Quality"), href: "/quality" },
    },
    {
      id: "dev.ether.pluto-tv",
      name: "Pluto TV",
      summary: t("Free live channels with a full guide."),
      category: "channels",
      state: "on",
      impact: t("120 channels, 98% guide coverage"),
      trend: [118, 119, 120, 120, 117, 120, 120, 121, 120, 120, 119, 120, 120, 120],
      appearsAt: { label: t("Live TV"), href: "/livetv" },
    },
    {
      id: "dev.ether.wrapped",
      name: t("Ether Wrapped"),
      summary: t("A yearly recap page and Home panel for every viewer."),
      category: "pages",
      state: "on",
      impact: t("Viewed 14 times this week"),
      trend: [0, 1, 0, 2, 1, 3, 1, 0, 2, 1, 2, 1, 0, 1],
      appearsAt: { label: t("Home"), href: "/home" },
    },
    {
      id: "community.subdl",
      name: "SubDL",
      summary: t("Subtitles from SubDL beside OpenSubtitles."),
      category: "subtitles",
      state: "shadow",
      impact: t("Would have supplied 9 subtitles this week"),
      trend: [0, 1, 1, 0, 2, 1, 0, 1, 1, 0, 1, 0, 1, 0],
      appearsAt: { label: t("Playback"), href: "/playback" },
    },
  ];
}

export interface Listing {
  id: string;
  name: string;
  summary: string;
  category: Category;
  author: string;
  repo: string;
  verified: boolean;
  installs: number;
  stars: number;
  version: string;
  updated: string;
  installed?: "current" | "update";
  adds: { label: string; where: string }[];
  permissions: { icon: PermissionIcon; label: string }[];
  changelog: { version: string; note: string }[];
  limits: string[];
  /** Why it is suggested for this server (For your server only). */
  because?: string;
  source?: string;
  /** A standard Stremio add-on, installed unchanged. */
  stremio?: boolean;
}

export type PermissionIcon = "network" | "history" | "collections" | "notify" | "folders" | "kv";

export function listings(): Listing[] {
  return [
    {
      id: "community.channel-doctor",
      name: t("Channel doctor"),
      summary: t("Hides dead channels from viewers and brings them back when they recover."),
      category: "channels",
      author: "Mara Okafor",
      repo: "github.com/maraokafor/ether-channel-doctor",
      verified: true,
      installs: 4210,
      stars: 312,
      version: "1.4.0",
      updated: t("3 days ago"),
      because: t("12 channels failed on this server today"),
      adds: [
        { label: t("A channel health check every 10 minutes"), where: t("Live TV") },
        { label: t("A failing-channels panel"), where: t("Live TV › Overview") },
      ],
      permissions: [
        { icon: "network", label: t("Checks your channel stream addresses") },
        { icon: "notify", label: t("Tells you when a channel stays down for a day") },
      ],
      changelog: [
        { version: "1.4.0", note: t("Checks fewer channels at once on slow connections") },
        { version: "1.3.0", note: t("Brings recovered channels back faster") },
      ],
      limits: [t("Never runs while someone is playing"), t("Stops after 2 seconds per channel")],
    },
    {
      id: "community.languages",
      name: t("Subtitles in their language"),
      summary: t("Finds subtitles in each viewer's language from three more sites."),
      category: "subtitles",
      author: "Lin Wei",
      repo: "github.com/linwei/ether-subtitle-trio",
      verified: true,
      installs: 2890,
      stars: 201,
      version: "2.0.1",
      updated: t("1 week ago"),
      because: t("Your viewers watch in Spanish and Polish"),
      adds: [{ label: t("Three subtitle providers"), where: t("Playback › Audio & subtitles") }],
      permissions: [{ icon: "network", label: t("Searches three subtitle sites") }],
      changelog: [{ version: "2.0.1", note: t("Faster search for Polish titles") }],
      limits: [t("Downloads only when a viewer picks a subtitle")],
    },
    {
      id: "dev.ether.pluto-tv",
      name: "Pluto TV",
      summary: t("Free live channels with a full guide."),
      category: "channels",
      author: "Ether",
      repo: "github.com/braedonsaunders/ether-plugins",
      verified: true,
      installs: 9120,
      stars: 640,
      version: "1.2.0",
      updated: t("2 weeks ago"),
      installed: "current",
      adds: [{ label: t("About 120 channels with a guide"), where: t("Live TV") }],
      permissions: [{ icon: "network", label: t("Reads the Pluto TV channel list") }],
      changelog: [{ version: "1.2.0", note: t("Adds channel logos") }],
      limits: [t("Refreshes the guide every 6 hours")],
    },
    {
      id: "community.criterion-row",
      name: t("Criterion picks"),
      summary: t("A Home row from the Criterion Collection that you own."),
      category: "home",
      author: "Sam Reyes",
      repo: "github.com/samreyes/criterion-row",
      verified: true,
      installs: 1530,
      stars: 98,
      version: "0.9.2",
      updated: t("5 days ago"),
      adds: [{ label: t("A Home row"), where: t("Home") }],
      permissions: [
        { icon: "network", label: t("Reads the Criterion catalogue") },
        { icon: "collections", label: t("Creates one collection") },
      ],
      changelog: [{ version: "0.9.2", note: t("Skips titles you have hidden") }],
      limits: [t("Refreshes once a day")],
    },
    {
      id: "community.anime-seasons",
      name: t("Seasonal anime"),
      summary: t("Catalogues of this season's anime, as Collections and Home rows."),
      category: "home",
      author: "Rin Sato",
      repo: "anime-seasons.example/manifest.json",
      verified: true,
      stremio: true,
      installs: 2210,
      stars: 174,
      version: "3.1.0",
      updated: t("2 days ago"),
      adds: [
        { label: t("Three catalogues"), where: t("Collections") },
        { label: t("Episode details for anime"), where: t("Catalog") },
      ],
      permissions: [{ icon: "network", label: t("Reads catalogues from the add-on") }],
      changelog: [{ version: "3.1.0", note: t("Adds the autumn season") }],
      limits: [t("Refreshes catalogues once a day")],
    },
    {
      id: "community.discord-now",
      name: t("Now playing in Discord"),
      summary: t("Posts what is playing to a Discord channel."),
      category: "notifications",
      author: "Kai Ito",
      repo: "github.com/kaiito/ether-discord-now",
      verified: false,
      installs: 740,
      stars: 41,
      version: "0.4.0",
      updated: t("1 month ago"),
      adds: [{ label: t("A notification type"), where: t("Notifications") }],
      permissions: [
        { icon: "history", label: t("Sees what is playing") },
        { icon: "notify", label: t("Sends to one Discord channel") },
      ],
      changelog: [{ version: "0.4.0", note: t("Hides titles from kids' profiles") }],
      limits: [t("Never sends more than once a minute")],
      source: "github.com/kaiito/catalogue",
    },
    {
      id: "dev.ether.dub-language",
      name: t("Dub language"),
      summary: t("Prefers releases dubbed in each viewer's language."),
      category: "releases",
      author: "Ether",
      repo: "github.com/braedonsaunders/ether-plugins",
      verified: true,
      installs: 3310,
      stars: 220,
      version: "1.1.0",
      updated: t("4 days ago"),
      installed: "update",
      adds: [{ label: t("A ranking step"), where: t("Quality › What matters most") }],
      permissions: [{ icon: "kv", label: t("Remembers each viewer's language") }],
      changelog: [{ version: "1.1.0", note: t("Understands more release names") }],
      limits: [t("2 ms per release, checked on every play")],
    },
    {
      id: "community.local-folders",
      name: t("Local folders"),
      summary: t("Plays files from folders you pick as the top release."),
      category: "sources",
      author: "Ether",
      repo: "github.com/braedonsaunders/ether-plugins",
      verified: true,
      installs: 5020,
      stars: 388,
      version: "1.0.3",
      updated: t("3 weeks ago"),
      adds: [{ label: t("A stream source"), where: t("Sources") }],
      permissions: [{ icon: "folders", label: t("Reads the folders you pick") }],
      changelog: [{ version: "1.0.3", note: t("Matches more folder names") }],
      limits: [t("Reads only the folders you pick")],
    },
    {
      id: "community.year-in-review",
      name: t("Server stats page"),
      summary: t("A page of charts about what your household watches."),
      category: "pages",
      author: "Noor Haddad",
      repo: "github.com/noorhaddad/ether-stats",
      verified: true,
      installs: 1980,
      stars: 150,
      version: "1.3.0",
      updated: t("6 days ago"),
      adds: [{ label: t("A page"), where: t("Sidebar") }],
      permissions: [{ icon: "history", label: t("Reads watch history") }],
      changelog: [{ version: "1.3.0", note: t("Adds a by-month chart") }],
      limits: [t("Computes once a night")],
    },
  ];
}

/** Prompt suggestions drawn from this server's state. */
export function suggestions(): string[] {
  return [
    t("12 channels failed today: hide dead channels"),
    t("Make a Home row of 4K titles nobody has watched"),
    t("Tell me when a new episode of a favourite arrives"),
    t("Prefer releases with Spanish audio for Lucía"),
  ];
}

export interface Template {
  key: string;
  title: string;
  description: string;
  icon: "channels" | "home" | "releases" | "notify" | "page" | "subtitles";
  prompt: string;
}

export function templates(): Template[] {
  return [
    {
      key: "channels",
      title: t("Add channels from a website"),
      description: t("Turn a channel list into Live TV with a guide."),
      icon: "channels",
      prompt: t("Add the channels listed on a website to Live TV"),
    },
    {
      key: "home",
      title: t("Make a Home row"),
      description: t("Pick titles by any rule and show them on Home."),
      icon: "home",
      prompt: t("Make a Home row of "),
    },
    {
      key: "releases",
      title: t("Change which release wins"),
      description: t("Prefer or avoid releases by what is in their name."),
      icon: "releases",
      prompt: t("Prefer releases that "),
    },
    {
      key: "notify",
      title: t("Tell me when…"),
      description: t("Get a message when something happens on the server."),
      icon: "notify",
      prompt: t("Tell me when "),
    },
    {
      key: "subtitles",
      title: t("Add subtitles from a site"),
      description: t("Search another subtitle site when a viewer asks."),
      icon: "subtitles",
      prompt: t("Add subtitles from "),
    },
    {
      key: "page",
      title: t("Add a page"),
      description: t("A page of charts, lists or tools in the console."),
      icon: "page",
      prompt: t("Add a page that shows "),
    },
  ];
}

export type StepKey = "understand" | "plan" | "build" | "check" | "preview";

export interface BuildStep {
  key: StepKey;
  label: string;
  /** One plain line shown when the step resolves. */
  result: string;
  /** Milliseconds the scripted prototype spends on the step. */
  ms: number;
  /** A self-repair line shown under the result, if the agent fixed something. */
  fixed?: string;
}

export function buildSteps(): BuildStep[] {
  return [
    {
      key: "understand",
      label: t("Understand"),
      result: t("A Home row of 4K titles that no viewer has started"),
      ms: 900,
    },
    {
      key: "plan",
      label: t("Plan"),
      result: t("One collection, rebuilt nightly, shown on everyone's Home"),
      ms: 1100,
    },
    { key: "build", label: t("Build"), result: t("Plugin written, 41 lines"), ms: 1400 },
    {
      key: "check",
      label: t("Check on your server"),
      result: t("18 titles match on this server"),
      fixed: t("Fixed: 2 titles were 4K only in a release you blocked"),
      ms: 1600,
    },
    { key: "preview", label: t("Preview"), result: t("Ready to look at"), ms: 500 },
  ];
}

export function PLUGIN_SOURCE(skipKids: boolean): string {
  const kids = skipKids
    ? `
        // Skip kids' titles.
        .filter((title) => !title.isForKids())`
    : "";
  return `// Built in Studio. Edit freely.
export default definePlugin({
  collections: {
    "unwatched-4k": {
      title: t("4K nobody has watched"),
      refresh: "nightly",
      limit: 18,
      async build(ether) {
        const titles = await ether.catalog.search({ resolution: "2160p", minRating: 7 });
        const started = await ether.viewers.startedTitles();
        return titles
          .filter((title) => !started.has(title.id))${kids}
          .sortBy("rating");
      },
    },
  },
});
`;
}
