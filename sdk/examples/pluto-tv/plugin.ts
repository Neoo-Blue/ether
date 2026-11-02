import {
  plugin,
  type Manifest,
  type HostFetchResponse,
  type PluginContext,
  type EtherHost,
} from "../../typescript/src/index.ts";
import manifest from "./manifest.json" with { type: "json" };

type Settings = { region: string };
type Channel = {
  _id: string;
  name: string;
  number?: number;
  category?: string;
  colorLogoPNG?: { path?: string };
  stitched?: { urls?: { type: string; url: string }[] };
  timelines?: {
    _id?: string;
    start: string;
    stop: string;
    title: string;
    episode?: { description?: string };
  }[];
};
function fetchJSON(ctx: PluginContext<Settings>, path: string): unknown {
  const region = ctx.settings.region || "auto";
  const sep = path.includes("?") ? "&" : "?";
  const result = ctx.ether.http.fetch({
    url: `https://api.pluto.tv${path}${sep}marketingRegion=${encodeURIComponent(region)}`,
  }) as HostFetchResponse;
  if (result.status !== 200)
    throw new Error(`Pluto TV returned HTTP ${result.status}`);
  return JSON.parse(result.body);
}
function getChannels(ctx: PluginContext<Settings>): Channel[] {
  const data = fetchJSON(ctx, "/v2/channels.json");
  if (!Array.isArray(data)) return [];
  const configured = ctx.settings.region || "auto";
  const url = data[0]?.stitched?.urls?.[0]?.url || "";
  const detected = /[?&]marketingRegion=([^&]+)/i.exec(url)?.[1]?.toLowerCase();
  if (configured !== "auto" && detected && configured !== detected)
    throw new Error(
      "Pluto TV returned a different region; check server location",
    );
  return data;
}
function streamURL(ctx: PluginContext<Settings>, id: string): string {
  const client = "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(
    /[xy]/g,
    (c) => {
      const r = Math.floor(Math.random() * 16);
      return (c === "x" ? r : (r & 3) | 8).toString(16);
    },
  );
  const params = `appName=web&appVersion=8.0.0&deviceVersion=122.0.0&deviceModel=web&deviceMake=chrome&deviceType=web&clientID=${client}&clientModelNumber=1.0.0&serverSideAds=false&drmCapabilities=widevine%3AL3`;
  const result = ctx.ether.http.fetch({
    url: `https://boot.pluto.tv/v4/start?${params}`,
    headers: { Origin: "https://pluto.tv", Referer: "https://pluto.tv/" },
  }) as HostFetchResponse;
  if (result.status !== 200)
    throw new Error(`Pluto TV session returned HTTP ${result.status}`);
  const boot = JSON.parse(result.body) as {
    sessionToken?: string;
    stitcherParams?: string;
    servers?: { stitcher?: string };
  };
  const base = boot.servers?.stitcher || "";
  if (!boot.sessionToken || !/^https:\/\/[a-z0-9.-]+\.pluto\.tv$/i.test(base))
    throw new Error("Pluto TV session response is invalid");
  return `${base}/v2/stitch/hls/channel/${encodeURIComponent(id)}/master.m3u8?jwt=${encodeURIComponent(boot.sessionToken)}&masterJWTPassthrough=true${boot.stitcherParams ? "&" + boot.stitcherParams : ""}`;
}
export function createPlugin(host?: EtherHost) {
  return plugin<Settings>({
    manifest: manifest as Manifest,
    host,
    defaultSettings: { region: "auto" },
    routes: {
      channels(ctx) {
        return {
          channels: getChannels(ctx)
            .filter(
              (c) =>
                c._id &&
                c.name &&
                c.stitched?.urls?.some((u) => u.type === "hls" && u.url),
            )
            .map((c) => ({
              id: c._id,
              name: c.name,
              number: c.number,
              group: c.category,
              logo: c.colorLogoPNG?.path,
              guideId: c._id,
              inputs: [
                {
                  url: `plugin://tv.ether.examples.pluto/stream/tv/${encodeURIComponent(c._id)}`,
                },
              ],
            })),
        };
      },
      guide(req, ctx) {
        const query = `?start=${encodeURIComponent(req.from || new Date().toISOString())}&stop=${encodeURIComponent(req.to || new Date(Date.now() + 86400000).toISOString())}`;
        const channel = fetchJSON(
          ctx,
          `/v2/channels/${encodeURIComponent(req.channelId)}${query}`,
        ) as Channel;
        return {
          programmes: (channel.timelines || [])
            .filter((t) => t.start && t.stop && t.title)
            .map((t) => ({
              id: t._id,
              channelId: req.channelId,
              title: t.title,
              start: t.start,
              stop: t.stop,
              description: t.episode?.description,
            })),
        };
      },
      stream(req, ctx) {
        const channel = getChannels(ctx).find((c) => c._id === req.id);
        const url = channel ? streamURL(ctx, channel._id) : undefined;
        return { streams: url ? [{ name: channel!.name, url }] : [] };
      },
      test(ctx) {
        return { ok: getChannels(ctx).length > 0 };
      },
    },
  });
}
export const handle = createPlugin().handle;
