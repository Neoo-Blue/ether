import type { Preroll, PrerollInput, Selection } from "../pages/playback/prerolls-api";

/** Isolated owner API fake for the pre-roll console. */
export function createPrerollsMock(seed = false) {
  let selection: Selection = "priority";
  let next = 1;
  let items: Preroll[] = [];
  const make = (input: PrerollInput): Preroll => ({
    ...input,
    id: `pr_${next++}`,
    video: {
      state: "ready",
      durationMs: 9500,
      width: 1920,
      height: 1080,
      thumbUrl: "",
      previewUrl: "",
      problem: "",
      source: "path",
    },
    plays30d: 0,
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
  });
  if (seed)
    items = ["Halloween sting", "Family welcome", "Cinema intro"].map((name, i) =>
      make({
        name,
        enabled: true,
        rules: {
          kinds: ["movie"],
          firstEpisodeOnly: false,
          bingeGapMinutes: 30,
          genres: i === 0 ? ["Horror"] : [],
          excludeGenres: [],
          collections: [],
          maxContentRating: "",
          dateFrom: "",
          dateTo: "",
          audience: { mode: "everyone", viewers: [] },
          network: "any",
          devices: [],
        },
        frequency: { mode: i === 0 ? "daily" : "always", hours: 6 },
        priority: 3 - i,
        order: "before_trailers",
      }),
    );
  return async (url: URL, init: RequestInit = {}) => {
    const path = url.pathname.replace(/^\/ether\/api\/admin\/prerolls/, "");
    const method = init.method ?? "GET";
    const body = typeof init.body === "string" ? JSON.parse(init.body) : {};
    const reply = (data: unknown, status = 200) =>
      new Response(JSON.stringify(data), {
        status,
        headers: { "Content-Type": "application/json" },
      });
    if (!path && method === "GET") return reply({ prerolls: items, selection });
    if (!path && method === "POST") {
      const entry = make(body);
      items.push(entry);
      return reply(entry);
    }
    if (path === "/selection" && method === "PUT") {
      selection = body.selection;
      return reply({});
    }
    if (path === "/import/pms" && method === "POST") {
      const created = String(body.paths)
        .split(/[;,]/)
        .filter(Boolean)
        .map((path) =>
          make({
            name: path.split(/[\\/]/).pop() || path,
            enabled: true,
            rules: {
              kinds: ["movie"],
              firstEpisodeOnly: false,
              bingeGapMinutes: 30,
              genres: [],
              excludeGenres: [],
              collections: [],
              maxContentRating: "",
              dateFrom: "",
              dateTo: "",
              audience: { mode: "everyone", viewers: [] },
              network: "any",
              devices: [],
            },
            frequency: { mode: "always", hours: 6 },
            priority: 0,
            order: "before_trailers",
          }),
        );
      items.push(...created);
      return reply({ created });
    }
    if (path === "/simulate" && method === "POST") {
      const candidates = items.map((p, i) => ({
        id: p.id,
        name: p.name,
        plays: p.enabled && i === 0,
        reasons: [
          { code: !p.enabled ? "disabled" : i ? "lower_priority" : "kind", ok: p.enabled && !i },
        ],
      }));
      return reply({ winner: candidates.find((c) => c.plays) ?? null, candidates });
    }
    const [, id, action] = path.split("/");
    const index = items.findIndex((p) => p.id === id);
    if (index < 0) return reply({ error: { code: "not_found" } }, 404);
    if (action === "plays") return reply({ plays: [] });
    if (action === "video" && method === "POST") {
      items[index] = {
        ...items[index]!,
        video: {
          ...items[index]!.video,
          state: "processing",
          source: init.body instanceof FormData ? "upload" : "path",
        },
      };
      return reply(items[index]);
    }
    if (!action && method === "PUT") {
      items[index] = { ...items[index]!, ...body };
      return reply(items[index]);
    }
    if (!action && method === "DELETE") {
      items.splice(index, 1);
      return reply({});
    }
    return reply({ error: { code: "not_found" } }, 404);
  };
}
