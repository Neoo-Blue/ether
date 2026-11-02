/**
 * Every owner destination in one place. The sidebar and ⌘K search read this,
 * so a section only ever has one home.
 */

import { localized, t, msg, searchTerms } from "../i18n";

export interface Destination {
  href: string;
  title: string;
  /** Page the destination belongs to, shown as the search subtitle. */
  page: string;
  keywords?: string;
  /** A whole page rather than a section within one. */
  isPage?: boolean;
}

export interface PageDef {
  href: string;
  title: string;
  /** In-page sections (tabs) in display order. */
  sections: { key: string; title: string; keywords?: string; href?: string }[];
  keywords?: string;
}

export const PAGES = localized<PageDef[]>(() => [
  {
    href: "/cache",
    title: t("Cache"),
    sections: [
      {
        key: "overview",
        title: t("Overview"),
        keywords: searchTerms(msg("storage readiness policy")),
      },
      {
        key: "explorer",
        title: t("Explorer"),
        keywords: searchTerms(msg("retained media releases pins")),
      },
      {
        key: "preloading",
        title: t("Preloading"),
        keywords: searchTerms(msg("household selection popular movies tv warm arrivals")),
      },
    ],
    keywords: searchTerms(msg("disk budget storage retention prefetch warming next episode")),
  },
  {
    href: "/",
    title: t("Overview"),
    sections: [],
    keywords: searchTerms(msg("home dashboard health attention status")),
  },
  {
    href: "/activity",
    title: t("Activity"),
    sections: [
      {
        key: "now",
        title: t("Now playing"),
        keywords: searchTerms(msg("streams sessions stop live")),
      },
      { key: "history", title: t("History"), keywords: searchTerms(msg("plays watched")) },
      {
        key: "stats",
        title: t("Stats"),
        keywords: searchTerms(msg("statistics devices hours viewers titles")),
      },
    ],
  },
  {
    href: "/viewers",
    title: t("Viewers"),
    sections: [],
    keywords: searchTerms(msg("users friends accounts people")),
  },
  {
    href: "/home",
    title: t("Home"),
    sections: [],
    keywords: searchTerms(msg("designer rows layout overrides plex home")),
  },
  {
    href: "/collections",
    title: t("Collections"),
    sections: [],
    keywords: searchTerms(msg("studio templates rows")),
  },
  {
    href: "/livetv",
    title: t("Live TV"),
    sections: [
      { key: "channels", title: t("Channels"), keywords: searchTerms(msg("virtual channel")) },
      {
        key: "sources",
        title: t("IPTV sources"),
        keywords: searchTerms(msg("m3u xmltv playlist")),
      },
      { key: "guide", title: t("Guide"), keywords: searchTerms(msg("epg schedule")) },
    ],
  },
  {
    href: "/calendar",
    title: t("Calendar"),
    sections: [
      { key: "month", title: t("Month"), keywords: searchTerms(msg("schedule grid")) },
      { key: "week", title: t("Week") },
      { key: "agenda", title: t("Agenda"), keywords: searchTerms(msg("list upcoming")) },
    ],
    keywords: searchTerms(
      msg("upcoming episodes premieres finales releases airing tonight sonarr radarr schedule"),
    ),
  },
  {
    href: "/catalog",
    title: t("Catalog"),
    sections: [
      {
        key: "settings",
        title: t("Catalog settings"),
        keywords: searchTerms(msg("tmdb language region popularity new releases playable digital")),
      },
    ],
    keywords: searchTerms(msg("catalogue movies shows browse search library")),
  },
  {
    href: "/metadata",
    title: t("Metadata"),
    sections: [
      {
        key: "ratings",
        title: t("Ratings"),
        keywords: searchTerms(msg("omdb mdblist imdb rotten badges")),
      },
      {
        key: "artwork",
        title: t("Artwork"),
        keywords: searchTerms(msg("posters overlays backdrop")),
      },
      {
        key: "episodes",
        title: t("Episode order"),
        keywords: searchTerms(msg("dvd absolute aired ordering")),
      },
    ],
  },
  {
    href: "/sources",
    title: t("Sources"),
    sections: [
      {
        key: "debrid",
        title: t("Debrid"),
        keywords: searchTerms(
          msg(
            "real-debrid realdebrid torbox alldebrid premiumize debrid-link debridlink offcloud torrent cached",
          ),
        ),
      },
      {
        key: "usenet",
        title: t("Usenet"),
        keywords: searchTerms(msg("nntp backfill connections")),
      },
      {
        key: "indexers",
        title: t("Indexers"),
        keywords: searchTerms(
          msg(
            "newznab torznab torrent built-in sites definitions cardigann flaresolverr prowlarr stremio addon budget api",
          ),
        ),
      },
      {
        key: "stats",
        title: t("Stats"),
        keywords: searchTerms(msg("quota price plan cost success speed plays failover")),
      },
    ],
  },
  {
    href: "/releases",
    title: t("Releases"),
    sections: [
      {
        key: "profiles",
        title: t("Profiles"),
        keywords: searchTerms(
          msg(
            "quality profiles resolution 4k 1080p remote cam scoring trash custom formats rules ranking",
          ),
        ),
      },
      {
        key: "devices",
        title: t("Device rules"),
        keywords: searchTerms(msg("device client profile")),
      },
      {
        key: "searching",
        title: t("Searching"),
        keywords: searchTerms(msg("how ether searches versions cache health samples candidates")),
      },
      {
        key: "blocklist",
        title: t("Blocklist"),
        keywords: searchTerms(msg("blocked bad release unblock terms groups")),
      },
      {
        key: "pinned",
        title: t("Pinned"),
        keywords: searchTerms(msg("pin release override search interactive manual pick find")),
      },
      {
        key: "history",
        title: t("History"),
        keywords: searchTerms(msg("traces resolver why release debug decisions")),
      },
    ],
  },
  {
    href: "/playback",
    title: t("Playback"),
    sections: [
      {
        key: "transcoding",
        title: t("Transcoding"),
        keywords: searchTerms(msg("ffmpeg hardware gpu bitrate startup race health")),
      },
      {
        key: "devices",
        title: t("App compatibility"),
        keywords: searchTerms(msg("compatibility codecs")),
      },
      {
        key: "markers",
        title: t("Skip buttons"),
        keywords: searchTerms(msg("intro credits skip chapters")),
      },
      {
        key: "subtitles",
        title: t("Audio & subtitles"),
        keywords: searchTerms(msg("audio languages subtitles timing sync opensubtitles household")),
      },
    ],
  },
  {
    href: "/notifications",
    title: t("Notifications"),
    sections: [
      {
        key: "channels",
        title: t("Channels"),
        keywords: searchTerms(msg("email discord webhook push alerts watchlist")),
      },
      {
        key: "protection",
        title: t("Server protection"),
        keywords: searchTerms(msg("crash watcher restart guard")),
      },
      { key: "report", title: t("Weekly report"), keywords: searchTerms(msg("newsletter digest")) },
      { key: "deliveries", title: t("Delivery history") },
    ],
  },
  {
    href: "/server",
    title: t("General"),
    sections: [
      {
        key: "server",
        title: t("Server"),
        keywords: searchTerms(msg("name port listen log level stream limit")),
      },
      {
        key: "downloads",
        title: t("Downloads"),
        keywords: searchTerms(msg("offline sync friends remux")),
      },
      {
        key: "personalize",
        title: t("Personalization"),
        keywords: searchTerms(msg("post-play up next match finish time clock")),
      },
      {
        key: "extras",
        title: t("Trailers & extras"),
        keywords: searchTerms(msg("cinema trailers yt-dlp resolver")),
      },
    ],
    keywords: searchTerms(msg("settings")),
  },
  {
    href: "/apps",
    title: t("Apps"),
    sections: [
      {
        key: "account",
        href: "/apps/plex/account",
        title: t("Plex account"),
        keywords: searchTerms(msg("plex sign in claim account")),
      },
      {
        key: "remote",
        href: "/apps/plex/remote",
        title: t("Remote access"),
        keywords: searchTerms(
          msg("upnp port forward public secure connections certificate plex.direct tls"),
        ),
      },
      {
        key: "compatibility",
        href: "/apps/plex/compatibility",
        title: t("Compatibility"),
        keywords: searchTerms(msg("plex version compatibility")),
      },
    ],
    keywords: searchTerms(msg("settings plex apps protocols")),
  },
  {
    href: "/services",
    title: t("Integrations"),
    sections: [
      {
        key: "overview",
        title: t("Integrations"),
        keywords: searchTerms(msg("status tmdb debrid prowlarr ratings")),
      },
      { key: "tmdb", title: "TMDB", keywords: searchTerms(msg("tmdb api key catalogue")) },
      // Tabs for these only exist once the service is on, so ⌘K lands on the
      // overview card where it can be enabled or configured.
      {
        key: "trakt",
        href: "/services/overview#trakt",
        title: "Trakt",
        keywords: searchTerms(msg("sync history")),
      },
      {
        key: "simkl",
        href: "/services/overview#simkl",
        title: "Simkl",
        keywords: searchTerms(msg("simkl sync history")),
      },
      {
        key: "llm",
        href: "/services/overview#llm",
        title: t("Language model"),
        keywords: searchTerms(msg("llm ollama openai recaps search")),
      },
      {
        key: "opensubtitles",
        href: "/services/overview#opensubtitles",
        title: "OpenSubtitles",
        keywords: searchTerms(msg("subtitles")),
      },
      {
        key: "ratings",
        href: "/services/overview#ratings",
        title: t("Ratings"),
        keywords: searchTerms(msg("omdb mdblist imdb rotten badges")),
      },
      {
        key: "introdb",
        title: "TheIntroDB",
        keywords: searchTerms(msg("introdb intro credits markers skip")),
      },
    ],
    keywords: searchTerms(msg("settings integrations")),
  },
  {
    href: "/cluster",
    title: t("Cluster"),
    sections: [],
    keywords: searchTerms(
      msg("high availability ha nodes failover postgres postgresql join token weight leader"),
    ),
  },
  {
    href: "/maintenance",
    title: t("Maintenance"),
    sections: [],
    keywords: searchTerms(msg("server maintenance")),
  },
  {
    href: "/maintenance/backups",
    title: t("Backups & import"),
    sections: [
      {
        key: "backups",
        title: t("Backups"),
        keywords: searchTerms(msg("snapshot restore schedule")),
      },
      {
        key: "import",
        title: t("Import & export"),
        keywords: searchTerms(msg("migrate plex csv letterboxd trakt")),
      },
    ],
  },
  {
    href: "/maintenance/updates",
    title: t("Updates"),
    sections: [],
    keywords: searchTerms(msg("version channel beta maintenance window update now install")),
  },
  {
    href: "/maintenance/troubleshooting",
    title: t("Troubleshooting"),
    sections: [],
    keywords: searchTerms(msg("local file test playback")),
  },
  {
    href: "/maintenance/logs",
    title: t("Logs"),
    sections: [],
    keywords: searchTerms(msg("errors debug support zip record a problem")),
  },
]);

export function destinations(): Destination[] {
  return PAGES.flatMap((p) => [
    { href: p.href, title: p.title, page: t("Page"), keywords: p.keywords, isPage: true },
    ...p.sections.map((s) => ({
      href: s.href ?? `${p.href}/${s.key}`,
      title: s.title,
      page: p.title,
      keywords: s.keywords,
    })),
  ]);
}
