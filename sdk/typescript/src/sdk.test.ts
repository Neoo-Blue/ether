import test from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import {
  assertValidViewSpec,
  createCatalogTemplate,
  createConditionTemplate,
  createI18n,
  createMockHostClient,
  createRankerTemplate,
  createRemoteHostClient,
  createScrobblerTemplate,
  createSubtitlesTemplate,
  createTestHarness,
  createViewTemplate,
  decodeSettings,
  dispatchOverTunnelFrame,
  fixtures,
  lintCopy,
  lintSettingsSchema,
  plugin,
  PluginRouteError,
  serve,
} from "./index.ts";

test("settings decode and lint follow SPEC §5.5", () => {
  const schema = {
    fields: [
      {
        key: "apiKey",
        kind: "secret" as const,
        label: "Access token",
        hint: "Token used to query the upstream service",
        required: true,
      },
      {
        key: "maxResults",
        kind: "number" as const,
        label: "Maximum results",
        default: 20,
        min: 1,
        max: 100,
      },
      {
        key: "includeAdult",
        kind: "bool" as const,
        label: "Include adult titles",
        default: false,
      },
    ],
  };

  assert.deepEqual(lintSettingsSchema(schema), []);
  assert.equal(lintCopy("Valid sentence case label", 40), "");
  assert.notEqual(lintCopy("Invalid — em dash", 40), "");
  assert.notEqual(lintCopy("Invalid exclamation!", 40), "");
  assert.notEqual(lintCopy("Simply invalid word", 40), "");

  const defaults = decodeSettings<{
    apiKey?: string;
    maxResults: number;
    includeAdult: boolean;
  }>(schema, {});
  assert.equal(defaults.settings.maxResults, 20);
  assert.equal(defaults.settings.includeAdult, false);
  assert.equal(defaults.errors["/apiKey"], "is required");

  const valid = decodeSettings<typeof defaults.settings>(schema, {
    apiKey: "tok_1234",
    maxResults: 50,
    includeAdult: true,
  });
  assert.deepEqual(valid.errors, {});
  assert.equal(valid.settings.apiKey, "tok_1234");
  assert.equal(valid.settings.maxResults, 50);
  assert.equal(valid.settings.includeAdult, true);
});

test("i18n helper resolves locale strings and falls back to base language", () => {
  const i18n = createI18n(
    {
      en: { greeting: "Hello {name}", fallbackOnly: "Base text" },
      "fr-CA": { greeting: "Bonjour {name}" },
    },
    "en",
  );
  assert.equal(i18n.t("greeting", { name: "Ada" }, "fr-CA"), "Bonjour Ada");
  assert.equal(i18n.t("$t:fallbackOnly", undefined, "fr-CA"), "Base text");
  assert.equal(i18n.t("missing.key", undefined, "fr-CA"), "missing.key");
});

test("plugin router dispatches all standard and capability endpoints", async () => {
  const host = createMockHostClient();
  const harness = createTestHarness<{ prefix: string }>(
    {
      manifest: {
        id: "org.ether.test.all",
        name: "Test all capabilities",
        version: "1.2.3",
        description: "Test plugin covering all capabilities.",
        types: ["movie", "series"],
        idPrefixes: ["tt"],
        ether: {
          protocol: 1,
          kind: "extension",
          capabilities: [
            "list_source",
            "stream_source",
            "subtitles",
            "markers",
            "events",
            "ui",
            "tools",
            "ranking_step",
            "rule_condition",
          ],
          summary: "Test plugin covering all capabilities.",
          runtime: { kind: "hosted", entry: "plugin.ts" },
          settings: {
            fields: [
              {
                key: "prefix",
                kind: "text",
                label: "Title prefix",
                default: "Ether",
                required: true,
              },
            ],
          },
          contributes: {
            pages: [
              {
                key: "home",
                label: "Overview",
                route: "/ether/view/home",
              },
            ],
          },
        },
      },
      strings: {
        en: { title: "{prefix} overview" },
      },
      routes: {
        options(field) {
          if (field === "prefix") {
            return [{ value: "Ether", label: "Ether" }];
          }
          return [];
        },
        catalog({ type, id, search }, ctx) {
          return {
            metas: [
              fixtures.catalogItem({
                id: `${id}:1`,
                type,
                name: `${ctx.settings.prefix} ${search ?? "Featured"}`,
              }),
            ],
          };
        },
        meta({ type, id }) {
          return {
            meta: fixtures.movie({ id, type }),
          };
        },
        stream({ id }) {
          return {
            streams: [
              {
                name: "Direct 1080p",
                url: `https://streams.example.invalid/${id}.mp4`,
              },
            ],
          };
        },
        subtitles({ id }) {
          return {
            subtitles: [
              {
                id: `${id}:en`,
                url: `https://subs.example.invalid/${id}.vtt`,
                lang: "en",
              },
            ],
          };
        },
        markers() {
          return {
            markers: [{ kind: "intro", start: 10, end: 65, label: "Skip intro" }],
          };
        },
        event(events, ctx) {
          for (const ev of events) {
            void ctx.ether.kv.put(`evt:${ev.id}`, ev.name);
          }
          return { ack: events.map((e) => e.id) };
        },
        view(_req, ctx) {
          return {
            spec: {
              specVersion: 1,
              layout: "list",
              header: [
                {
                  kind: "page-header",
                  title: { $: "title" },
                },
              ],
              body: [
                {
                  kind: "text",
                  content: "Ready.",
                },
              ],
            },
            data: {
              title: ctx.t("title", { prefix: ctx.settings.prefix }),
            },
          };
        },
        action({ action, input }, ctx) {
          void ctx.ether.notify.owner(`Action ${action}: ${String(input.target ?? "")}`);
          return {
            toast: "Done.",
            refresh: true,
          };
        },
        tool({ name, input }) {
          return {
            executed: name,
            echo: input,
          };
        },
        rankKey(candidate) {
          return candidate.cached ? 100 : 10;
        },
        condMatch(candidate, _op, value) {
          return Boolean(candidate.verified) === Boolean(value);
        },
        file({ path, range }) {
          const content = new TextEncoder().encode(`file:${path}`);
          if (range === "bytes=0-3") {
            return {
              status: 206,
              contentType: "text/plain",
              contentRange: `bytes 0-3/${content.byteLength}`,
              totalSize: content.byteLength,
              body: content.subarray(0, 4),
            };
          }
          return {
            status: 200,
            contentType: "text/plain",
            totalSize: content.byteLength,
            body: content,
          };
        },
      },
    },
    host,
  );

  const manifest = await harness.manifest();
  assert.equal(manifest.id, "org.ether.test.all");

  const health = await harness.health();
  assert.equal(health.status, 200);
  assert.deepEqual(health.body, { status: "ok" });

  const cfgRes = await harness.configure({ prefix: "Custom" });
  assert.equal(cfgRes.status, 200);
  assert.deepEqual(cfgRes.body.errors, {});

  const optsRes = await harness.options("prefix");
  assert.equal(optsRes.status, 200);
  assert.equal(optsRes.body.length, 1);

  const catRes = await harness.catalog("movie", "top", { search: "Matrix" });
  assert.equal(catRes.status, 200);
  assert.equal(catRes.body.metas[0]?.name, "Custom Matrix");

  const metaRes = await harness.meta("movie", "tt0133093");
  assert.equal(metaRes.status, 200);
  assert.equal(metaRes.body.meta.id, "tt0133093");

  const streamRes = await harness.stream("movie", "tt0133093");
  assert.equal(streamRes.status, 200);
  assert.equal(streamRes.body.streams.length, 1);

  // Listing does not download and does not require a pick.
  const subListing = await harness.subtitles("movie", "tt0133093");
  assert.equal(subListing.status, 200);

  // Subtitles with X-Ether-Pick succeeds
  const pick = fixtures.pickToken("tt0133093");
  const subAllowed = await harness.subtitles("movie", "tt0133093", pick);
  assert.equal(subAllowed.status, 200);
  assert.equal(subAllowed.body.subtitles.length, 1);

  // Markers
  const markersRes = await harness.markers("movie", "tt0133093");
  assert.equal(markersRes.status, 200);
  assert.equal(markersRes.body.markers[0]?.kind, "intro");

  // Event batch
  const ev = fixtures.playbackEvent();
  const evRes = await harness.event([ev]);
  assert.equal(evRes.status, 200);
  assert.deepEqual(evRes.body.ack, [ev.id]);
  assert.equal(host.state.kv.get(`evt:${ev.id}`), "playback.completed");

  // ViewSpec page
  const viewRes = await harness.view("home");
  assert.equal(viewRes.status, 200);
  assert.equal(viewRes.body.data.title, "Custom overview");

  // Action
  const actRes = await harness.action("sync", { target: "library" });
  assert.equal(actRes.status, 200);
  assert.equal(actRes.body.toast, "Done.");
  assert.deepEqual(host.state.notifications, ["Action sync: library"]);

  // Tool
  const toolRes = await harness.tool("lookup", { q: "Arrival" });
  assert.equal(toolRes.status, 200);
  assert.equal(toolRes.body.executed, "lookup");

  // RankKey & CondMatch
  const cand = fixtures.candidate({ cached: true, verified: true });
  assert.equal(harness.rankKey(cand), 100);
  assert.equal(harness.condMatch(cand, "eq", true), true);
  assert.equal(harness.condMatch(cand, "eq", false), false);

  const rankHttp = await harness.requestHosted({
    method: "POST",
    path: "/ether/rank-key",
    body: JSON.stringify({ candidate: cand }),
  });
  assert.equal(rankHttp.status, 200);
  assert.deepEqual(rankHttp.body, { score: 100 });

  // File range & path traversal guard
  const fileFull = await harness.file("clips/intro.txt");
  assert.equal(fileFull.status, 200);
  assert.equal(new TextDecoder().decode(fileFull.body as Uint8Array), "file:clips/intro.txt");

  const fileRange = await harness.file("clips/intro.txt", "bytes=0-3");
  assert.equal(fileRange.status, 206);
  assert.equal(new TextDecoder().decode(fileRange.body as Uint8Array), "file");

  const badFile = await harness.request({
    method: "GET",
    path: "/ether/file/../secrets.txt",
  });
  assert.equal(badFile.status, 400);
});

test("hosted QuickJS mode rethrows __ETHER_FAULT__ errors and routes PluginRouteError", async () => {
  const p = plugin({
    manifest: {
      id: "org.ether.fault",
      name: "Fault test",
      version: "0.1.0",
      ether: {
        protocol: 1,
        kind: "extension",
        capabilities: ["list_source"],
        summary: "Fault test plugin.",
        runtime: { kind: "hosted", entry: "plugin.ts" },
      },
    },
    routes: {
      catalog({ id }) {
        if (id === "fault") {
          throw new Error("__ETHER_FAULT__:forbidden:network domain not allowed");
        }
        if (id === "custom") {
          throw new PluginRouteError(429, "rate_limited", "Upstream rate limit reached.");
        }
        return { metas: [] };
      },
    },
  });

  assert.throws(
    () =>
      p.handle({
        method: "GET",
        path: "/catalog/movie/fault.json",
      }),
    /__ETHER_FAULT__:forbidden/,
  );

  const customRes = await p.handle({
    method: "GET",
    path: "/catalog/movie/custom.json",
  });
  assert.equal(customRes.status, 429);
  assert.deepEqual(customRes.body, {
    error: { code: "rate_limited", message: "Upstream rate limit reached." },
  });
});

test("assertValidViewSpec rejects any *ClassName field", () => {
  assert.throws(() => {
    assertValidViewSpec({
      specVersion: 1,
      layout: "list",
      header: [],
      body: [
        {
          kind: "panel",
          title: "Bad panel",
          blocks: [],
          containerClassName: "bg-red-500",
        },
      ],
    });
  }, /forbids containerClassName/);
});

test("serve() runs HTTP sidecar server and dispatchOverTunnelFrame streams binary files", async () => {
  const def = createCatalogTemplate({
    id: "org.ether.http.sidecar",
    name: "HTTP sidecar",
  });
  const inst = plugin({
    ...def,
    routes: {
      ...def.routes,
      file() {
        return {
          status: 206,
          contentType: "video/mp4",
          contentRange: "bytes 0-7/16",
          body: new Uint8Array([1, 2, 3, 4, 5, 6, 7, 8]),
        };
      },
    },
  });

  const handle = await serve(inst, { mode: "http", port: 0 });
  try {
    assert.equal(handle.mode, "http");
    assert.ok(handle.url);

    const manifestRes = await fetch(`${handle.url}/manifest.json`);
    assert.equal(manifestRes.status, 200);
    const manifestJson = (await manifestRes.json()) as { id: string };
    assert.equal(manifestJson.id, "org.ether.http.sidecar");

    const catRes = await fetch(`${handle.url}/catalog/movie/featured/search=Arrival.json`);
    assert.equal(catRes.status, 200);
    const catJson = (await catRes.json()) as { metas: Array<{ name: string }> };
    assert.equal(catJson.metas[0]?.name, "Result for Arrival");
  } finally {
    await handle.close();
  }

  // Test tunnel frame dispatch for JSON and binary file streaming
  const jsonFrames = await dispatchOverTunnelFrame(inst, {
    type: "request",
    id: "req-1",
    method: "GET",
    path: "/ether/health",
  });
  assert.equal(jsonFrames.length, 1);
  assert.equal(jsonFrames[0]?.type, "response");
  assert.equal(jsonFrames[0]?.status, 200);

  const fileFrames = await dispatchOverTunnelFrame(
    inst,
    {
      type: "request",
      id: "req-file",
      method: "GET",
      path: "/ether/file/sample.mp4",
      headers: { Range: ["bytes=0-7"] },
    },
    1024,
  );
  assert.equal(fileFrames.length, 3);
  assert.equal(fileFrames[0]?.type, "response_start");
  assert.equal(fileFrames[0]?.status, 206);
  assert.equal(fileFrames[1]?.type, "response_chunk");
  assert.equal(fileFrames[2]?.type, "response_end");
});

test("createRemoteHostClient talks to /ether/plugin-host/* endpoints with credential header", async () => {
  const receivedRequests: Array<{ method: string; url: string; cred: string; body: string }> = [];
  const server = http.createServer(async (req, res) => {
    const chunks: Uint8Array[] = [];
    for await (const c of req) {
      chunks.push(c instanceof Uint8Array ? c : new Uint8Array(c));
    }
    let total = 0;
    for (const c of chunks) total += c.byteLength;
    const merged = new Uint8Array(total);
    let offset = 0;
    for (const c of chunks) {
      merged.set(c, offset);
      offset += c.byteLength;
    }
    const body = new TextDecoder().decode(merged);
    receivedRequests.push({
      method: req.method ?? "GET",
      url: req.url ?? "/",
      cred: String(req.headers["x-ether-plugin-credential"] ?? ""),
      body,
    });

    if (req.url === "/ether/plugin-host/kv/greeting" && req.method === "GET") {
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ key: "greeting", value: "hello" }));
      return;
    }
    if (req.url?.startsWith("/ether/plugin-host/kv") && req.method === "GET") {
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ keys: ["greeting"] }));
      return;
    }
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ ok: true }));
  });

  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const addr = server.address() as { port: number };
  const baseUrl = `http://127.0.0.1:${addr.port}`;

  try {
    const client = createRemoteHostClient({ baseUrl, credential: "cred-xyz" });
    const val = await client.kv.get("greeting");
    assert.equal(val, "hello");

    await client.kv.put("greeting", "updated");
    const keys = await client.kv.list("");
    assert.deepEqual(keys, ["greeting"]);

    await client.collections.upsert({ name: "Top picks", itemIds: ["tt0133093"] });
    await client.notify.owner("Sync complete.");
    assert.ok(receivedRequests.every((r) => r.cred === "cred-xyz"));
  } finally {
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
});

test("starter templates construct valid plugins for every capability", async () => {
  const catalogHarness = createTestHarness(
    createCatalogTemplate({ id: "org.ether.tpl.cat", name: "Catalog template" }),
  );
  assert.equal((await catalogHarness.catalog("movie", "featured")).status, 200);

  const subHarness = createTestHarness(
    createSubtitlesTemplate({ id: "org.ether.tpl.sub", name: "Subtitles template" }),
  );
  assert.equal(
    (await subHarness.subtitles("movie", "tt0133093", fixtures.pickToken())).status,
    200,
  );

  const scrobbleHarness = createTestHarness(
    createScrobblerTemplate({ id: "org.ether.tpl.scrobble", name: "Scrobbler template" }),
  );
  assert.equal((await scrobbleHarness.event([fixtures.playbackEvent()])).status, 200);

  const viewHarness = createTestHarness(
    createViewTemplate({ id: "org.ether.tpl.view", name: "View template" }),
  );
  assert.equal((await viewHarness.view("home")).status, 200);
  assert.equal((await viewHarness.action("refresh")).status, 200);

  const rankHarness = createTestHarness(
    createRankerTemplate({ id: "org.ether.tpl.rank", name: "Ranker template" }),
  );
  assert.equal(
    rankHarness.rankKey(fixtures.candidate({ cached: true, resolution: 2160 })),
    1500,
  );

  const condHarness = createTestHarness(
    createConditionTemplate({ id: "org.ether.tpl.cond", name: "Condition template" }),
  );
  assert.equal(condHarness.condMatch(fixtures.candidate({ cached: true }), "eq", true), true);
  assert.equal(condHarness.condMatch(fixtures.candidate({ cached: false }), "eq", true), false);
});

test("the exported ether client selects HTTP in a sidecar", async () => {
  const originalFetch = globalThis.fetch;
  let requested = "";
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    requested = String(input);
    return new Response(JSON.stringify({ key: "greeting", value: "hello" }), {
      headers: { "Content-Type": "application/json" },
    });
  }) as typeof fetch;
  try {
    const url = new URL("./host.ts?sidecar-test", import.meta.url).href;
    const mod = await import(url) as typeof import("./host.ts");
    assert.equal(await mod.ether.kv.get("greeting"), "hello");
    assert.ok(requested.endsWith("/ether/plugin-host/kv/greeting"));
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("subtitle listing is separate from viewer-picked downloads", async () => {
  const instance = plugin({
    manifest: { id: "org.example.subtitles", version: "1.0.0", name: "Subtitles", ether: { protocol: 1, kind: "integration", capabilities: ["subtitles"] } } as any,
    routes: {
      subtitles: () => ({ subtitles: [] }),
      subtitlesDownload: ({ id }) => ({ format: "srt", encoding: "utf8", data: id }),
    },
  });
  const list = await instance.handle({ method: "GET", path: "/subtitles/movie/tt1.json" });
  assert.equal(list.status, 200);
  const denied = await instance.handle({ method: "POST", path: "/ether/subtitles/download", body: { id: "a", lang: "en", format: "srt" } });
  assert.equal(denied.status, 403);
  const download = await instance.handle({ method: "POST", path: "/ether/subtitles/download", headers: { "X-Ether-Pick": "pick" }, body: { id: "a", lang: "en", format: "srt" } });
  assert.equal(download.status, 200);
  assert.deepEqual(download.body, { format: "srt", encoding: "utf8", data: "a" });
});
