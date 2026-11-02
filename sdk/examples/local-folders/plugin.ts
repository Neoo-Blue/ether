import {
  plugin,
  type Manifest,
  type PluginContext,
  type FolderEntry,
  type CatalogTitleSummary,
  type EtherHost,
} from "../../typescript/src/index.ts";
import manifest from "./manifest.json" with { type: "json" };
import en from "./i18n/en.json" with { type: "json" };
import fr from "./i18n/fr.json" with { type: "json" };

type Settings = { folder: string };
type Match = { tmdbId: number; type: string };
type Item = { path: string; season?: number; episode?: number };
const read = <T>(v: unknown): T => v as T;
const parse = (value: string | null, fallback: any) => {
  try {
    return value ? JSON.parse(value) : fallback;
  } catch {
    return fallback;
  }
};
const video = /\.(mkv|mp4|m4v|avi|mov|ts|webm)$/i;
function identity(name: string) {
  const clean = name
    .replace(/\.(mkv|mp4|m4v|avi|mov|ts|webm)$/i, "")
    .replace(/[._]/g, " ")
    .trim();
  const year = clean.match(/\((\d{4})\)/);
  return {
    title: (year ? clean.slice(0, year.index) : clean).trim(),
    year: year ? Number(year[1]) : undefined,
  };
}
function scan(ctx: PluginContext<Settings>) {
  const root = ctx.settings.folder;
  if (!root) return;
  const grouped: Record<string, Item[]> = {};
  const pending: { path: string; top: string; depth: number }[] = [
    { path: "", top: "", depth: 0 },
  ];
  while (pending.length) {
    const cur = pending.shift()!;
    const entries = read<FolderEntry[]>(ctx.ether.folders.list(root, cur.path));
    if (entries.length >= 1000)
      throw new Error("Folder has too many entries to scan safely");
    for (const e of entries) {
      if (e.dir && cur.depth < 10)
        pending.push({
          path: e.path,
          top: cur.top || e.name,
          depth: cur.depth + 1,
        });
      else if (!e.dir && video.test(e.name)) {
        read(ctx.ether.folders.stat(root, e.path));
        const top = cur.top || e.name;
        const ep = e.path.match(/s(\d{1,2})e(\d{1,3})/i);
        (grouped[top] ||= []).push({
          path: e.path,
          ...(ep ? { season: Number(ep[1]), episode: Number(ep[2]) } : {}),
        });
      }
    }
  }
  const byId: Record<string, Item[]> = {},
    unmatched: string[] = [];
  for (const top of Object.keys(grouped)) {
    const manual = parse(
      read<string | null>(ctx.ether.kv.get("match:" + root + ":" + top)),
      null,
    ) as Match | null;
    const isSeries = grouped[top].some((i) => i.episode !== undefined);
    const guess = identity(top);
    const found = manual
      ? []
      : read<{ titles: CatalogTitleSummary[] }>(
          ctx.ether.catalog.search(guess.title, 20, 0),
        ).titles || [];
    const candidates = found
      .filter(
        (t) =>
          t.tmdbId &&
          (isSeries
            ? ["show", "series"].includes(t.type)
            : t.type === "movie") &&
          t.title.toLocaleLowerCase() === guess.title.toLocaleLowerCase() &&
          (!guess.year || t.year === guess.year),
      )
      .map((t) => ({ tmdbId: t.tmdbId!, type: isSeries ? "series" : "movie" }));
    const match =
      manual || (candidates.length === 1 ? candidates[0] : undefined);
    if (!match) {
      unmatched.push(top);
      continue;
    }
    const key = `index:${match.type}:${match.tmdbId}`;
    (byId[key] ||= []).push(...grouped[top]);
  }
  const old = read<string[]>(ctx.ether.kv.list("index:"));
  for (const key of Object.keys(byId))
    ctx.ether.kv.put(key, JSON.stringify(byId[key]));
  for (const key of old) if (!byId[key]) ctx.ether.kv.delete(key);
  ctx.ether.kv.put("unmatched", JSON.stringify(unmatched));
  ctx.ether.kv.put("scanned", root);
}
export function createPlugin(host?: EtherHost) {
  return plugin<Settings>({
    manifest: manifest as Manifest,
    host,
    strings: { en, fr },
    defaultSettings: { folder: "" },
    routes: {
      job(_req, ctx) {
        scan(ctx);
        return { ok: true };
      },
      stream(req, ctx) {
        if (
          read<string | null>(ctx.ether.kv.get("scanned")) !==
            ctx.settings.folder ||
          !ctx.settings.folder
        )
          return { streams: [] };
        const parts = req.id.match(/^tmdb:(\d+)(?::(\d+):(\d+))?$/);
        if (!parts) return { streams: [] };
        const items = parse(
          read<string | null>(
            ctx.ether.kv.get(`index:${req.type}:${parts[1]}`),
          ),
          [],
        ) as Item[];
        return {
          streams: items
            .filter(
              (i) =>
                req.type !== "series" ||
                (i.season === Number(parts[2]) &&
                  i.episode === Number(parts[3])),
            )
            .map((i) => ({
              name: "Local",
              title: i.path.split(/[\\/]/).pop(),
              ether: { file: i.path, releaseName: i.path.split(/[\\/]/).pop() },
            })),
        };
      },
      view(_req, ctx) {
        const unmatched = (
          read<string | null>(ctx.ether.kv.get("scanned")) ===
          ctx.settings.folder
            ? parse(read<string | null>(ctx.ether.kv.get("unmatched")), [])
            : []
        ) as string[];
        const body: any[] = [
          {
            kind: "widget",
            widget: "ether.actionButton",
            props: { action: "scan", label: "$t:scan" },
          },
          ...unmatched.map((name, i) => ({
            kind: "panel",
            title: { $: `folder${i}` },
            blocks: [
              {
                kind: "widget",
                widget: "ether.titleMatch",
                props: {
                  action: "match",
                  label: "$t:search",
                  params: { folder: name },
                },
              },
            ],
          })),
        ];
        if (!unmatched.length)
          body.push({
            kind: "text",
            content: ctx.settings.folder
              ? read<string | null>(ctx.ether.kv.get("scanned")) ===
                ctx.settings.folder
                ? "$t:empty"
                : "$t:notScanned"
              : "$t:noFolder",
          });
        return {
          spec: {
            specVersion: 1,
            layout: "list",
            header: [
              {
                kind: "page-header",
                title: "$t:page",
                description: "$t:description",
              },
            ],
            body,
          },
          data: Object.fromEntries(
            unmatched.map((name, i) => [`folder${i}`, name]),
          ),
        };
      },
      action(req, ctx) {
        if (req.action === "scan") {
          if (!ctx.settings.folder) throw new Error(ctx.t("noFolder"));
          scan(ctx);
          return { toast: ctx.t("scanned"), refresh: true };
        }
        const input = req.input as {
          folder?: string;
          tmdbId?: number;
          mediaType?: string;
        };
        const unmatched = parse(
          read<string | null>(ctx.ether.kv.get("unmatched")),
          [],
        ) as string[];
        if (
          req.action !== "match" ||
          !input.folder ||
          !unmatched.includes(input.folder) ||
          !input.tmdbId ||
          !["movie", "show", "series"].includes(input.mediaType || "")
        )
          throw new Error("Invalid match");
        ctx.ether.kv.put(
          "match:" + ctx.settings.folder + ":" + input.folder,
          JSON.stringify({
            tmdbId: input.tmdbId,
            type: input.mediaType === "movie" ? "movie" : "series",
          }),
        );
        scan(ctx);
        return { toast: ctx.t("saved"), refresh: true };
      },
    },
  });
}
export const handle = createPlugin().handle;
