package studio

import "github.com/braedonsaunders/ether/internal/plugins"

// Studio's showcase templates extend Ether's native surfaces with real data:
// live channels in the Live TV guide, and playable sources on catalogue titles.

func templateChannels() Template {
	manifest := map[string]any{
		"id":          "community.studio.channels",
		"version":     "0.1.0",
		"name":        "Free live channels",
		"description": "Free ad-supported live channels from Pluto TV in the Live TV guide.",
		"types":       []string{"tv"},
		"resources":   []string{"stream"},
		"ether": map[string]any{
			"protocol":     1,
			"kind":         "livetv_source",
			"capabilities": []string{"livetv_channels", "ui"},
			"summary":      "$t:summary",
			"author":       map[string]any{"name": "Server owner"},
			"license":      "MIT",
			"runtime":      map[string]any{"kind": "hosted", "entry": "src/plugin.ts"},
			"permissions": map[string]any{
				"network": []string{"api.pluto.tv", "boot.pluto.tv"},
			},
			"settings": map[string]any{"$ref": "settings.schema.json"},
			"strings":  map[string]any{"en": "i18n/en.json"},
			"contributes": map[string]any{
				"channels": map[string]any{"refresh": "1h", "test": true},
				"pages": []map[string]any{
					{"key": "lineup", "label": "$t:lineup_label", "route": "/ether/view/lineup"},
				},
			},
		},
	}
	settings := map[string]any{
		"fields": []map[string]any{
			{
				"key":     "region",
				"kind":    "select",
				"label":   "$t:region",
				"hint":    "$t:region_hint",
				"default": "auto",
				"options": []map[string]any{
					{"value": "auto", "label": "$t:region_auto"},
					{"value": "us", "label": "$t:region_us"},
					{"value": "ca", "label": "$t:region_ca"},
					{"value": "gb", "label": "$t:region_gb"},
				},
			},
			{
				"key":     "categories",
				"kind":    "text",
				"label":   "$t:categories",
				"hint":    "$t:categories_hint",
				"default": "",
			},
		},
	}
	stringsEN := map[string]string{
		"summary":          "Free live channels with a programme guide in Live TV.",
		"lineup_label":     "Free channels",
		"active_channels":  "Channels in the guide",
		"guide_channels":   "Lineup",
		"unreachable":      "Pluto TV could not be reached. Live TV keeps the last lineup.",
		"region":           "Region",
		"region_hint":      "Which country's lineup to load; automatic follows this server's location.",
		"region_auto":      "Automatic",
		"region_us":        "United States",
		"region_ca":        "Canada",
		"region_gb":        "United Kingdom",
		"categories":       "Categories",
		"categories_hint":  "Comma-separated categories to keep, such as Movies or News; leave empty for all.",
		"channels_working": "Loaded {count} channels.",
	}
	code := `// Free live channels from Pluto TV, shown in Ether's Live TV guide.
// Routes follow the Live TV source contract: /ether/channels.json lists
// channels, /ether/guide/{id}.json returns programmes and
// /stream/tv/{id}.json returns a playable HLS stream.
type Request = {
  method: string;
  path: string;
  query?: Record<string, string[] | string>;
  settings?: Record<string, unknown>;
};
type PlutoChannel = {
  _id: string;
  name: string;
  number?: number;
  category?: string;
  summary?: string;
  colorLogoPNG?: { path?: string };
  stitched?: { urls?: { type: string; url: string }[] };
  timelines?: { _id?: string; start: string; stop: string; title: string; episode?: { description?: string } }[];
};

const API = "https://api.pluto.tv";

function setting(req: Request, key: string, fallback: string): string {
  const v = req.settings?.[key];
  return typeof v === "string" && v.trim() ? v.trim() : fallback;
}
function q(req: Request, key: string): string {
  const v = req.query?.[key];
  return Array.isArray(v) ? v[0] || "" : v || "";
}
function getJSON(url: string): any {
  const res = ether.http.fetch({ url });
  if (res.status !== 200) throw new Error("Pluto TV answered HTTP " + res.status);
  return JSON.parse(res.body);
}
function region(req: Request): string {
  return setting(req, "region", "auto");
}
function channels(req: Request): PlutoChannel[] {
  const data = getJSON(API + "/v2/channels.json?marketingRegion=" + encodeURIComponent(region(req)));
  if (!Array.isArray(data)) return [];
  const wanted = setting(req, "categories", "")
    .split(",")
    .map((c) => c.trim().toLowerCase())
    .filter(Boolean);
  return data.filter(
    (c: PlutoChannel) =>
      c && c._id && c.name &&
      (c.stitched?.urls || []).some((u) => u.type === "hls" && u.url) &&
      (wanted.length === 0 || wanted.includes((c.category || "").toLowerCase())),
  );
}
function uuid(): string {
  return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, (c) => {
    const r = Math.floor(Math.random() * 16);
    return (c === "x" ? r : (r & 3) | 8).toString(16);
  });
}
function streamURL(id: string): string {
  const params =
    "appName=web&appVersion=8.0.0&deviceVersion=122.0.0&deviceModel=web&deviceMake=chrome" +
    "&deviceType=web&clientID=" + uuid() + "&clientModelNumber=1.0.0&serverSideAds=false";
  const res = ether.http.fetch({
    url: "https://boot.pluto.tv/v4/start?" + params,
    headers: { Origin: "https://pluto.tv", Referer: "https://pluto.tv/" },
  });
  if (res.status !== 200) throw new Error("Pluto TV session answered HTTP " + res.status);
  const boot = JSON.parse(res.body);
  const base = boot.servers?.stitcher || "";
  if (!boot.sessionToken || !/^https:\/\/[a-z0-9.-]+\.pluto\.tv$/i.test(base)) {
    throw new Error("Pluto TV session response is invalid");
  }
  return (
    base + "/v2/stitch/hls/channel/" + encodeURIComponent(id) + "/master.m3u8?jwt=" +
    encodeURIComponent(boot.sessionToken) + "&masterJWTPassthrough=true" +
    (boot.stitcherParams ? "&" + boot.stitcherParams : "")
  );
}

export function handle(req: Request) {
  if (req.path === "/ether/health" || req.path === "/ether/configure") {
    return { status: 200, body: req.path === "/ether/health" ? { status: "ok", reason: "" } : { errors: {} } };
  }
  if (req.path === "/ether/test") {
    try {
      const n = channels(req).length;
      return { status: 200, body: { ok: n > 0, message: "Loaded " + n + " channels." } };
    } catch (e) {
      return { status: 200, body: { ok: false, message: String(e) } };
    }
  }
  if (req.path === "/ether/channels.json") {
    const list = channels(req).map((c) => ({
      id: c._id,
      name: c.name,
      number: c.number ? String(c.number) : undefined,
      group: c.category,
      logo: c.colorLogoPNG?.path,
      guideId: c._id,
    }));
    return { status: 200, body: { channels: list } };
  }
  if (req.path.startsWith("/ether/guide/") && req.path.endsWith(".json")) {
    const id = decodeURIComponent(req.path.slice("/ether/guide/".length, -5));
    const from = q(req, "from") || new Date().toISOString();
    const to = q(req, "to") || new Date(Date.now() + 86400000).toISOString();
    const ch = getJSON(
      API + "/v2/channels/" + encodeURIComponent(id) + "?start=" + encodeURIComponent(from) +
        "&stop=" + encodeURIComponent(to) + "&marketingRegion=" + encodeURIComponent(region(req)),
    ) as PlutoChannel;
    const programmes = (ch.timelines || [])
      .filter((t) => t.start && t.stop && t.title)
      .map((t) => ({ id: t._id, channelId: id, title: t.title, start: t.start, stop: t.stop, description: t.episode?.description }));
    return { status: 200, body: { programmes } };
  }
  if (req.path.startsWith("/stream/tv/") && req.path.endsWith(".json")) {
    const id = decodeURIComponent(req.path.slice("/stream/tv/".length, -5));
    return { status: 200, body: { streams: [{ name: "Pluto TV", url: streamURL(id) }] } };
  }
  if (req.path === "/ether/view/lineup") {
    let list: PlutoChannel[] = [];
    let problem = "";
    try {
      list = channels(req);
    } catch (e) {
      problem = "$t:unreachable";
    }
    return {
      status: 200,
      body: {
        title: "$t:lineup_label",
        spec: {
          specVersion: 1,
          layout: "detail",
          header: [{ kind: "page-header", title: "$t:lineup_label", description: "$t:summary" }],
          body: [
            { kind: "stat-tile", iconKey: "tv", accent: "primary", label: "$t:active_channels", value: { $: "count" } },
            {
              kind: "panel",
              title: "$t:guide_channels",
              blocks: [
                ...(problem ? [{ kind: "text", content: problem }] : []),
                {
                  kind: "repeat",
                  items: { $: "channels" },
                  itemKey: { $: "id" },
                  blocks: [
                    {
                      kind: "widget",
                      widget: "ether.channelRow",
                      props: { name: { $: "name" }, number: { $: "number" }, group: { $: "group" } },
                    },
                  ],
                },
              ],
            },
          ],
        },
        data: {
          count: String(list.length),
          channels: list.slice(0, 60).map((c) => ({
            id: c._id,
            name: c.name,
            number: c.number ? String(c.number) : "",
            group: c.category || "",
          })),
        },
      },
    };
  }
  return { status: 404, body: { error: { code: "not_found", message: "Route not found." } } };
}

(globalThis as any).handle = handle;
`
	return Template{
		ID:              "channels",
		Label:           "Live TV channels",
		Summary:         "Add free live channels with a programme guide to Live TV.",
		Kind:            plugins.KindLiveTVSource,
		Capabilities:    []plugins.Capability{plugins.CapLiveTV, plugins.CapUI},
		DefaultPluginID: "community.studio.channels",
		DefaultName:     "Free live channels",
		DefaultRoute:    "/ether/view/lineup",
		DefaultPage:     "lineup",
		Files: map[string]string{
			"manifest.json":        prettyJSON(manifest),
			"settings.schema.json": prettyJSON(settings),
			"i18n/en.json":         prettyJSON(stringsEN),
			"src/plugin.ts":        code,
		},
	}
}

func templateVideoSource() Template {
	manifest := map[string]any{
		"id":          "community.studio.sources",
		"version":     "0.1.0",
		"name":        "Free classic films",
		"description": "Playable public-domain films from the Internet Archive for titles in your catalogue.",
		"types":       []string{"movie"},
		"resources":   []string{"stream"},
		"idPrefixes":  []string{"tt"},
		"ether": map[string]any{
			"protocol":     1,
			"kind":         "source",
			"capabilities": []string{"stream_source", "ui"},
			"summary":      "$t:summary",
			"author":       map[string]any{"name": "Server owner"},
			"license":      "MIT",
			"runtime":      map[string]any{"kind": "hosted", "entry": "src/plugin.ts"},
			"permissions": map[string]any{
				"network": []string{"archive.org"},
			},
			"strings": map[string]any{"en": "i18n/en.json"},
			"contributes": map[string]any{
				"source": map[string]any{"class": "direct"},
				"pages": []map[string]any{
					{"key": "overview", "label": "$t:page_label", "route": "/ether/view/overview"},
				},
			},
		},
	}
	stringsEN := map[string]string{
		"summary":     "Finds free, legal copies of classic films on the Internet Archive.",
		"page_label":  "Free classic films",
		"sample":      "Sources for a sample title",
		"sources":     "Sources found",
		"none":        "No copy found for this title.",
		"unreachable": "The Internet Archive could not be reached.",
	}
	code := `// Playable sources for catalogue titles from the Internet Archive's
// public-domain feature films. Ether calls /stream/movie/{imdbId}.json with
// title and year hints whenever it looks for a release, and plays the URL.
type Request = {
  method: string;
  path: string;
  query?: Record<string, string[] | string>;
};
type Doc = { identifier: string; title?: string; year?: string | number };
type File = { name: string; format?: string; size?: string; height?: string };

const SEARCH = "https://archive.org/advancedsearch.php";

function q(req: Request, key: string): string {
  const v = req.query?.[key];
  return Array.isArray(v) ? v[0] || "" : v || "";
}
function getJSON(url: string): any {
  const res = ether.http.fetch({ url });
  if (res.status !== 200) throw new Error("Internet Archive answered HTTP " + res.status);
  return JSON.parse(res.body);
}
function words(s: string): string {
  return s.toLowerCase().replace(/[^a-z0-9]+/g, " ").trim();
}
function candidates(title: string, year: number): Doc[] {
  const query =
    'title:("' + title.replace(/"/g, "") + '") AND mediatype:movies AND ' +
    "(collection:feature_films OR collection:moviesandfilms OR collection:classic_cartoons)";
  const url =
    SEARCH + "?q=" + encodeURIComponent(query) +
    "&fl[]=identifier&fl[]=title&fl[]=year&rows=12&sort[]=downloads+desc&output=json";
  const docs: Doc[] = getJSON(url).response?.docs || [];
  const want = words(title);
  return docs.filter((d) => {
    const got = words(d.title || "");
    if (!got.startsWith(want) && !got.includes(" " + want)) return false;
    const y = Number(d.year || 0) || Number((/(19|20)\d\d/.exec(d.title || "") || [""])[0]);
    return !year || !y || Math.abs(y - year) <= 1;
  });
}
function bestFile(id: string): File | null {
  const meta = getJSON("https://archive.org/metadata/" + encodeURIComponent(id));
  const files: File[] = (meta.files || []).filter(
    (f: File) => /\.mp4$/i.test(f.name) && !/ia\.mp4$|\.ogv$/i.test(f.name),
  );
  files.sort((a, b) => Number(b.height || 0) - Number(a.height || 0) || Number(b.size || 0) - Number(a.size || 0));
  return files[0] || null;
}
function streamsFor(title: string, year: number) {
  const out: any[] = [];
  for (const doc of candidates(title, year).slice(0, 3)) {
    const f = bestFile(doc.identifier);
    if (!f) continue;
    const height = Number(f.height || 0);
    const res = height >= 1000 ? "1080p" : height >= 700 ? "720p" : height >= 400 ? "480p" : "SD";
    out.push({
      name: "Internet Archive",
      title: (doc.title || title) + "\n" + res + " MP4",
      url: "https://archive.org/download/" + encodeURIComponent(doc.identifier) + "/" + encodeURIComponent(f.name),
      behaviorHints: { filename: f.name, videoSize: Number(f.size || 0) || undefined },
      ether: {
        id: doc.identifier + "/" + f.name,
        releaseName: title.replace(/[^A-Za-z0-9]+/g, ".") + (year ? "." + year : "") + "." + res + ".WEB.MP4",
        size: Number(f.size || 0) || undefined,
      },
    });
  }
  return out;
}

export function handle(req: Request) {
  if (req.path === "/ether/health" || req.path === "/ether/configure") {
    return { status: 200, body: req.path === "/ether/health" ? { status: "ok", reason: "" } : { errors: {} } };
  }
  if (req.path === "/ether/test") {
    try {
      const n = streamsFor("Night of the Living Dead", 1968).length;
      return { status: 200, body: { ok: n > 0, message: "Found " + n + " sources for a sample title." } };
    } catch (e) {
      return { status: 200, body: { ok: false, message: String(e) } };
    }
  }
  if (req.path.startsWith("/stream/movie/") && req.path.endsWith(".json")) {
    const title = q(req, "title");
    if (!title) return { status: 200, body: { streams: [] } };
    return { status: 200, body: { streams: streamsFor(title, Number(q(req, "year")) || 0) } };
  }
  if (req.path === "/ether/view/overview") {
    const sample = { title: "Night of the Living Dead", year: 1968 };
    let found: any[] = [];
    let problem = "";
    try {
      found = streamsFor(sample.title, sample.year);
      if (found.length === 0) problem = "$t:none";
    } catch (e) {
      problem = "$t:unreachable";
    }
    return {
      status: 200,
      body: {
        title: "$t:page_label",
        spec: {
          specVersion: 1,
          layout: "detail",
          header: [{ kind: "page-header", title: "$t:page_label", description: "$t:summary" }],
          body: [
            { kind: "stat-tile", iconKey: "film", accent: "primary", label: "$t:sources", value: { $: "count" } },
            {
              kind: "panel",
              title: "$t:sample",
              blocks: [
                { kind: "text", content: { $: "sampleTitle" } },
                ...(problem ? [{ kind: "text", content: problem }] : []),
                { kind: "repeat", items: { $: "streams" }, itemKey: { $: "id" }, blocks: [{ kind: "text", content: { $: "label" } }] },
              ],
            },
          ],
        },
        data: {
          count: String(found.length),
          sampleTitle: sample.title + " (" + sample.year + ")",
          streams: found.map((s) => ({ id: s.ether.id, label: s.title.replace("\n", " · ") })),
        },
      },
    };
  }
  return { status: 404, body: { error: { code: "not_found", message: "Route not found." } } };
}

(globalThis as any).handle = handle;
`
	return Template{
		ID:              "video_source",
		Label:           "Video sources",
		Summary:         "Find playable copies of catalogue titles, shown in each title's sources.",
		Kind:            plugins.KindSource,
		Capabilities:    []plugins.Capability{plugins.CapStreamSource, plugins.CapUI},
		DefaultPluginID: "community.studio.sources",
		DefaultName:     "Free classic films",
		DefaultRoute:    "/ether/view/overview",
		DefaultPage:     "overview",
		Files: map[string]string{
			"manifest.json": prettyJSON(manifest),
			"i18n/en.json":  prettyJSON(stringsEN),
			"src/plugin.ts": code,
		},
	}
}
