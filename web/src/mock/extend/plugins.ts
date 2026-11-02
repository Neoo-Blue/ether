/**
 * Mock plugin host (WS7): stateful fixture data for every /api/plugins
 * route the real host serves, so WS10a can build Server › Plugins against
 * the same shapes. Three installed plugins: a verified Live TV source
 * with an update waiting, an Unverified ranking-step extension, and a
 * verified extension with a page, a list source and an attention health.
 */
import type { Descriptor } from "../../lib/extend";
import type { ExtendHandler } from "./index";

export interface PluginSummary {
  id: string;
  name: string;
  version: string;
  kind?: string;
  summary?: string;
  icon?: string;
  enabled: boolean;
  verified: boolean;
  shadow: boolean;
  autoUpdate: boolean;
  updateVersion?: string;
  prevVersion?: string;
  health: string;
  capabilities?: string[];
  sourceKind?: string;
  stremioOnly?: boolean;
  installedAt: number;
}

export interface PluginHealth {
  status: string;
  reason?: string;
  restarts: number;
  slow: number;
  drops: number;
  lastError?: string;
  lastOk?: string;
  nextRetry?: string;
  observed?: Record<string, number>;
  egressBytes: number;
}

export interface PluginDetail extends PluginSummary {
  manifest: Record<string, unknown>;
  permissions: {
    lines: string[];
    declared: Record<string, unknown>;
    grants: { viewers?: number[]; folders?: string[]; revoked?: string[] };
    needsViewers: boolean;
    needsFolders: boolean;
  };
  healthDetail: PluginHealth;
  privacy: {
    declaredHosts: string[];
    observedHosts: Record<string, number>;
    undeclared?: string[];
  };
  impact: {
    calls: number;
    errors: number;
    p50Ms: number;
    p95Ms: number;
    egressBytes: number;
    kvBytes: number;
    slow: number;
    eventDrops: number;
    restarts: number;
  };
  addedPermissions?: string[];
}

interface Fixture {
  detail: PluginDetail;
  settings: Record<string, unknown>;
  schema: Record<string, unknown>;
  descriptors: Descriptor[];
  journal: Record<string, unknown>[];
  shadowJournal: Record<string, unknown>[];
  logs: string[];
  calls: Record<string, unknown>[];
}

const NOW = 1_762_000_000;

function fixturePlugins(): Fixture[] {
  const pluto: PluginDetail = {
    id: "community.plutotv.ether",
    name: "Pluto TV",
    version: "1.2.0",
    kind: "livetv_source",
    summary: "Free Pluto TV channels with their guide",
    enabled: true,
    verified: true,
    shadow: false,
    autoUpdate: true,
    updateVersion: "1.3.0",
    health: "ok",
    capabilities: ["livetv_channels"],
    sourceKind: "catalogue",
    installedAt: NOW - 86400 * 30,
    manifest: {
      id: "community.plutotv.ether",
      version: "1.2.0",
      name: "Pluto TV",
      description: "Free Pluto TV channels with their guide.",
      types: ["tv"],
      ether: {
        protocol: 1,
        kind: "livetv_source",
        capabilities: ["livetv_channels"],
        permissions: { network: ["api.pluto.tv", "*.pluto.tv"] },
        contributes: { channels: { refresh: "6h", test: true } },
      },
    },
    permissions: {
      lines: ["Connects to api.pluto.tv", "Connects to *.pluto.tv"],
      declared: { network: ["api.pluto.tv", "*.pluto.tv"] },
      grants: {},
      needsViewers: false,
      needsFolders: false,
    },
    healthDetail: {
      status: "ok",
      restarts: 0,
      slow: 1,
      drops: 0,
      observed: { "api.pluto.tv": 42, "boot.pluto.tv": 6 },
      egressBytes: 1_200_000,
    },
    privacy: {
      declaredHosts: ["api.pluto.tv", "*.pluto.tv"],
      observedHosts: { "api.pluto.tv": 42, "boot.pluto.tv": 6 },
    },
    impact: {
      calls: 48,
      errors: 0,
      p50Ms: 180,
      p95Ms: 720,
      egressBytes: 1_200_000,
      kvBytes: 2048,
      slow: 1,
      eventDrops: 0,
      restarts: 0,
    },
    addedPermissions: ["Connects to ads.pluto.tv"],
  };
  const dub: PluginDetail = {
    id: "community.example.dub",
    name: "Dub language",
    version: "0.4.0",
    kind: "extension",
    summary: "Prefers releases dubbed in your language",
    enabled: true,
    verified: false,
    shadow: false,
    autoUpdate: false,
    health: "ok",
    capabilities: ["ranking_step", "rule_condition"],
    sourceKind: "url",
    installedAt: NOW - 86400 * 5,
    manifest: {
      id: "community.example.dub",
      version: "0.4.0",
      name: "Dub language",
      ether: {
        protocol: 1,
        kind: "extension",
        capabilities: ["ranking_step", "rule_condition"],
        runtime: { kind: "hosted", entry: "dist/plugin.js" },
        contributes: {
          rankingSteps: [{ key: "dub", label: "Dub language" }],
          ruleConditions: [{ key: "dubbed", label: "Dubbed" }],
        },
      },
    },
    permissions: {
      lines: ["Needs no access to your server or the network"],
      declared: {},
      grants: {},
      needsViewers: false,
      needsFolders: false,
    },
    healthDetail: { status: "ok", restarts: 0, slow: 0, drops: 0, observed: {}, egressBytes: 0 },
    privacy: { declaredHosts: [], observedHosts: {} },
    impact: {
      calls: 200,
      errors: 0,
      p50Ms: 1,
      p95Ms: 2,
      egressBytes: 0,
      kvBytes: 0,
      slow: 0,
      eventDrops: 0,
      restarts: 0,
    },
  };
  const wrapped: PluginDetail = {
    id: "community.example.wrapped",
    name: "Ether Wrapped",
    version: "1.0.1",
    kind: "extension",
    summary: "Your year in viewing, with a shelf to match",
    enabled: true,
    verified: true,
    shadow: true,
    autoUpdate: false,
    health: "attention",
    capabilities: ["ui", "list_source", "events"],
    sourceKind: "catalogue",
    installedAt: NOW - 86400 * 12,
    manifest: {
      id: "community.example.wrapped",
      version: "1.0.1",
      name: "Ether Wrapped",
      ether: {
        protocol: 1,
        kind: "extension",
        capabilities: ["ui", "list_source", "events"],
        permissions: { catalog: "read", viewers: { watch_history: "granted_viewers" } },
        contributes: {
          pages: [{ key: "wrapped", label: "Ether Wrapped", route: "/ether/view/wrapped" }],
          listSources: [
            {
              key: "wrapped",
              label: "Your year in viewing",
              catalog: "wrapped",
              maxItems: 100,
              refresh: "24h",
            },
          ],
          events: { subscribe: ["playback.completed"] },
        },
      },
    },
    permissions: {
      lines: ["Reads your catalogue", "Reads watch history"],
      declared: { catalog: "read", viewers: { watch_history: "granted_viewers" } },
      grants: { viewers: [1] },
      needsViewers: true,
      needsFolders: false,
    },
    healthDetail: {
      status: "attention",
      reason: "Recent calls were slow.",
      restarts: 1,
      slow: 3,
      drops: 2,
      observed: {},
      egressBytes: 0,
    },
    privacy: { declaredHosts: [], observedHosts: {} },
    impact: {
      calls: 31,
      errors: 1,
      p50Ms: 240,
      p95Ms: 1900,
      egressBytes: 0,
      kvBytes: 8192,
      slow: 3,
      eventDrops: 2,
      restarts: 1,
    },
  };
  const text = (plugin: string, s: string) => ({ resolved: s, plugin });
  return [
    {
      detail: pluto,
      settings: { region: "us" },
      schema: { fields: [{ key: "region", kind: "select", label: "Region" }] },
      descriptors: [
        {
          kind: "liveTVSource",
          id: "plugin:community.plutotv.ether/channels",
          owner: pluto.id,
          label: text(pluto.id, "Pluto TV"),
          meta: { refresh: "6h" },
        },
      ],
      journal: [],
      shadowJournal: [],
      logs: ["info: refreshed 214 channels", "info: guide covers 6 days"],
      calls: [
        {
          id: 2,
          plugin: pluto.id,
          route: "/ether/health",
          method: "GET",
          status: 200,
          ms: 120,
          created: "2026-11-01T10:00:00Z",
        },
        {
          id: 1,
          plugin: pluto.id,
          route: "/ether/channels.json",
          method: "GET",
          status: 200,
          ms: 640,
          created: "2026-11-01T06:00:00Z",
        },
      ],
    },
    {
      detail: dub,
      settings: { language: "fr" },
      schema: { fields: [{ key: "language", kind: "language", label: "Dub language" }] },
      descriptors: [
        {
          kind: "rankingStep",
          id: "plugin:community.example.dub/dub",
          owner: dub.id,
          label: text(dub.id, "Dub language"),
          meta: {},
        },
        {
          kind: "ruleField",
          id: "plugin:community.example.dub/dubbed",
          owner: dub.id,
          label: text(dub.id, "Dubbed"),
          meta: {},
        },
      ],
      journal: [],
      shadowJournal: [],
      logs: [],
      calls: [],
    },
    {
      detail: wrapped,
      settings: {},
      schema: {},
      descriptors: [
        {
          kind: "page",
          id: "plugin:community.example.wrapped/wrapped",
          owner: wrapped.id,
          label: text(wrapped.id, "Ether Wrapped"),
          meta: { route: "/ether/view/wrapped" },
        },
        {
          kind: "collectionSource",
          id: "plugin:community.example.wrapped/wrapped",
          owner: wrapped.id,
          label: text(wrapped.id, "Your year in viewing"),
          meta: { catalog: "wrapped", maxItems: 100 },
        },
      ],
      journal: [
        {
          id: 1,
          plugin: wrapped.id,
          op: "collections.upsert",
          detail: "Your year in viewing",
          shadow: false,
          undone: false,
          created: "2026-10-30T09:00:00Z",
        },
      ],
      shadowJournal: [
        {
          id: 2,
          plugin: wrapped.id,
          op: "collections.upsert",
          detail: "Your year in viewing",
          shadow: true,
          undone: false,
          created: "2026-11-01T09:00:00Z",
        },
      ],
      logs: ["info: built the year shelf with 48 titles", "warn: poster lookup was slow"],
      calls: [
        {
          id: 1,
          plugin: wrapped.id,
          route: "/ether/view/wrapped",
          method: "GET",
          status: 200,
          ms: 1900,
          error: "",
          created: "2026-11-01T09:30:00Z",
        },
      ],
    },
  ];
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

function summary(d: PluginDetail): PluginSummary {
  const {
    manifest: _m,
    permissions: _p,
    healthDetail,
    privacy: _pr,
    impact: _i,
    addedPermissions: _a,
    ...s
  } = d;
  return { ...s, health: healthDetail.status };
}

export function createPluginsMock(): {
  handle: ExtendHandler;
  descriptors: () => Descriptor[];
  version: () => number;
} {
  let version = 1;
  const fixtures = fixturePlugins();
  const bump = () => version++;
  const byId = (id: string) => fixtures.find((f) => f.detail.id === id);

  const handle: ExtendHandler = (method, seg, q, body) => {
    if (seg[0] !== "plugins") return null;
    if (method === "GET" && seg.length === 1) {
      return json({ plugins: fixtures.map((f) => summary(f.detail)) });
    }
    if (method === "POST" && seg[1] === "install" && seg[2] === "preview") {
      const req = (body ?? {}) as { url?: string };
      return json({
        manifest: {
          id: "community.example.preview",
          version: "1.0.0",
          name: "Preview plugin",
          ether: { protocol: 1, kind: "extension", summary: "A plugin from a link" },
        },
        permissions: ["Connects to " + safeHost(req.url ?? "")],
        needsViewers: false,
        needsFolders: false,
        verified: false,
        stremioOnly: false,
      });
    }
    if (method === "POST" && seg[1] === "install" && seg.length === 2) {
      return json(
        {
          error: {
            code: "validation",
            message: "Preview a plugin first, then confirm.",
            fields: [],
          },
        },
        400,
      );
    }
    const f = byId(seg[1] ?? "");
    if (!f) {
      return json({ error: { code: "not_found", message: "That plugin is not installed." } }, 404);
    }
    const d = f.detail;
    if (method === "GET" && seg.length === 2) return json(d);
    if (method === "GET" && seg[2] === "settings") {
      return json({ settings: f.settings, schema: f.schema });
    }
    if (method === "PUT" && seg[2] === "settings") {
      const b = (body ?? {}) as { settings?: Record<string, unknown> };
      f.settings = b.settings ?? {};
      return json({ status: "saved" });
    }
    if (method === "GET" && seg[2] === "options") {
      return json({ options: [{ value: "us", label: "United States" }] });
    }
    if (method === "POST" && seg[2] === "test") {
      return json({ ok: d.enabled, message: d.enabled ? "Works" : "Turned off", details: [] });
    }
    if (method === "POST" && seg[2] === "enable") {
      d.enabled = true;
      bump();
      return json({ status: "enabled" });
    }
    if (method === "POST" && seg[2] === "disable") {
      d.enabled = false;
      bump();
      return json({ status: "disabled" });
    }
    if (method === "PUT" && seg[2] === "grants") {
      d.permissions.grants = (body ?? {}) as PluginDetail["permissions"]["grants"];
      return json({ status: "saved", grants: d.permissions.grants });
    }
    if (method === "POST" && seg[2] === "shadow") {
      d.shadow = Boolean((body as { shadow?: boolean } | null)?.shadow);
      return json({ shadow: d.shadow });
    }
    if (method === "GET" && seg[2] === "shadow") {
      return json({ entries: f.shadowJournal, count: f.shadowJournal.length });
    }
    if (method === "GET" && seg[2] === "journal") {
      const entries = q.get("shadow") === "1" ? f.shadowJournal : f.journal;
      return json({ entries });
    }
    if (method === "POST" && seg[2] === "undo") {
      const n = f.journal.length;
      f.journal = [];
      return json({ undone: n });
    }
    if (method === "GET" && seg[2] === "logs") return json({ lines: f.logs });
    if (method === "GET" && seg[2] === "calls") return json({ calls: f.calls });
    if (method === "POST" && seg[2] === "update" && seg[3] === "check") {
      return json({
        updateAvailable: Boolean(d.updateVersion),
        version: d.updateVersion ?? d.version,
        addedPermissions: d.addedPermissions ?? [],
      });
    }
    if (method === "POST" && seg[2] === "update" && seg.length === 3) {
      const accept = Boolean(
        (body as { acceptAddedPermissions?: boolean } | null)?.acceptAddedPermissions,
      );
      if (!d.updateVersion) {
        return json({ error: { code: "error", message: "No update is waiting." } }, 400);
      }
      if ((d.addedPermissions?.length ?? 0) > 0 && !accept) {
        return json(
          {
            error: {
              code: "permission_increase",
              message: "This update asks for more access. Review it, then confirm.",
              fields: (d.addedPermissions ?? []).map((a) => ({ field: "permissions", message: a })),
            },
          },
          409,
        );
      }
      d.prevVersion = d.version;
      d.version = d.updateVersion;
      d.updateVersion = undefined;
      d.addedPermissions = undefined;
      bump();
      return json(summary(d));
    }
    if (method === "POST" && seg[2] === "rollback") {
      if (!d.prevVersion) {
        return json(
          { error: { code: "error", message: "There is no earlier version to go back to." } },
          400,
        );
      }
      d.version = d.prevVersion;
      d.prevVersion = undefined;
      bump();
      return json(summary(d));
    }
    if (method === "DELETE" && seg.length === 2) {
      fixtures.splice(fixtures.indexOf(f), 1);
      bump();
      return json({ status: "removed" });
    }
    if (method === "GET" && seg[2] === "view") {
      return json({ spec: { specVersion: 1, blocks: [] }, data: {} });
    }
    if (method === "POST" && seg[2] === "actions") {
      return json({ toast: "Done" });
    }
    return null;
  };

  return {
    handle,
    descriptors: () => fixtures.filter((f) => f.detail.enabled).flatMap((f) => f.descriptors),
    version: () => version,
  };
}

function safeHost(url: string): string {
  try {
    return new URL(url).hostname || "the address you gave";
  } catch {
    return "the address you gave";
  }
}
