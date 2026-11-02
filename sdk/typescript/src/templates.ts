import type { Capability, Manifest, PluginKind } from "./types.ts";
import type { PluginDefinition } from "./router.ts";

export interface TemplateOptions {
  id: string;
  name: string;
  version?: string;
  summary?: string;
  description?: string;
}

function baseManifest(
  opts: TemplateOptions,
  kind: PluginKind,
  capabilities: Capability[],
  extra?: Partial<Manifest>,
): Manifest {
  const summary = opts.summary ?? `${opts.name} plugin for Ether.`;
  return {
    id: opts.id,
    name: opts.name,
    version: opts.version ?? "0.1.0",
    description: opts.description ?? summary,
    types: ["movie", "series"],
    idPrefixes: ["tt"],
    ether: {
      protocol: 1,
      kind,
      capabilities,
      summary,
      runtime: {
        kind: "hosted",
        entry: "plugin.ts",
      },
      permissions: {
        catalog: "none",
      },
    },
    ...extra,
  };
}

export function createCatalogTemplate(
  opts: TemplateOptions,
): PluginDefinition<{ apiKey: string }> {
  const base = baseManifest(opts, "extension", ["list_source"], {
    catalogs: [
      {
        type: "movie",
        id: "featured",
        name: "Featured movies",
        extra: [{ name: "search", isRequired: false }],
      },
    ],
    resources: ["catalog", "meta"],
  });
  return {
    manifest: {
      ...base,
      ether: {
        ...base.ether!,
        settings: {
          fields: [
            {
              key: "apiKey",
              kind: "secret",
              label: "Access token",
              hint: "Token used to query the upstream catalog",
            },
          ],
        },
        contributes: {
          listSources: [
            {
              key: "featured",
              label: "Featured movies",
              hint: "Curated movies from the upstream list",
              catalog: "featured",
              refresh: "1h",
            },
          ],
        },
      },
    },
    routes: {
      catalog({ type, id, search }) {
        return {
          metas: [
            {
              id: "tt0133093",
              type,
              name: search ? `Result for ${search}` : `Featured in ${id}`,
              releaseInfo: "1999",
            },
          ],
        };
      },
      meta({ type, id }) {
        return {
          meta: {
            id,
            type,
            name: `Title ${id}`,
            releaseInfo: "1999",
            description: "Catalog metadata entry.",
          },
        };
      },
    },
  };
}

export function createSubtitlesTemplate(
  opts: TemplateOptions,
): PluginDefinition<{ preferredLanguage: string }> {
  const base = baseManifest(opts, "extension", ["subtitles"], {
    resources: ["subtitles"],
  });
  return {
    manifest: {
      ...base,
      ether: {
        ...base.ether!,
        settings: {
          fields: [
            {
              key: "preferredLanguage",
              kind: "select",
              label: "Preferred language",
              default: "en",
              options: [
                { value: "en", label: "English" },
                { value: "es", label: "Spanish" },
                { value: "fr", label: "French" },
              ],
            },
          ],
        },
        contributes: {
          subtitles: {
            languages: ["en", "es", "fr"],
            formats: ["vtt", "srt"],
          },
        },
      },
    },
    defaultSettings: {
      preferredLanguage: "en",
    },
    routes: {
      subtitles({ id }, ctx) {
        const lang = ctx.settings.preferredLanguage || "en";
        return {
          subtitles: [
            {
              id: `${id}:${lang}`,
              url: `https://subtitles.example.invalid/${encodeURIComponent(id)}/${lang}.vtt`,
              lang,
              label: lang.toUpperCase(),
              format: "vtt",
            },
          ],
        };
      },
    },
  };
}

export function createScrobblerTemplate(opts: TemplateOptions): PluginDefinition {
  const base = baseManifest(opts, "integration", ["events"]);
  return {
    manifest: {
      ...base,
      ether: {
        ...base.ether!,
        contributes: {
          integration: {
            category: "scrobbler",
            adds: ["events"],
            connect: "none",
          },
          events: {
            subscribe: [
              "playback.started",
              "playback.paused",
              "playback.stopped",
              "playback.completed",
            ],
          },
        },
      },
    },
    routes: {
      event(events, ctx) {
        const ack: string[] = [];
        for (const ev of events) {
          void ctx.ether.kv.put(`last_event:${ev.viewerId ?? 0}`, JSON.stringify(ev));
          ack.push(ev.id);
        }
        return { ack };
      },
    },
  };
}

export function createViewTemplate(opts: TemplateOptions): PluginDefinition {
  const base = baseManifest(opts, "extension", ["ui"]);
  return {
    manifest: {
      ...base,
      ether: {
        ...base.ether!,
        contributes: {
          pages: [
            {
              key: "home",
              label: opts.name,
              route: "/ether/view/home",
            },
          ],
        },
      },
    },
    routes: {
      async view(_req, ctx) {
        const rawCount = await ctx.ether.kv.get("refresh_count");
        const refreshCount = Number(rawCount ?? "0");
        return {
          spec: {
            specVersion: 1,
            layout: "list",
            header: [
              {
                kind: "page-header",
                title: { $: "title" },
                description: { $: "subtitle" },
              },
            ],
            body: [
              {
                kind: "panel",
                title: "Status",
                blocks: [
                  {
                    kind: "text",
                    content: { $: "statusText" },
                  },
                ],
              },
            ],
          },
          data: {
            title: opts.name,
            subtitle: `Refreshed ${refreshCount} times`,
            statusText: "Plugin view is ready.",
          },
        };
      },
      async action({ action }, ctx) {
        if (action === "refresh") {
          const rawCount = await ctx.ether.kv.get("refresh_count");
          const current = Number(rawCount ?? "0") + 1;
          await ctx.ether.kv.put("refresh_count", String(current));
          return {
            toast: "State refreshed.",
            refresh: true,
          };
        }
        return { toast: "Unknown action." };
      },
    },
  };
}

export function createRankerTemplate(
  opts: TemplateOptions,
): PluginDefinition<{ boost4k: boolean }> {
  const base = baseManifest(opts, "extension", ["ranking_step"]);
  return {
    manifest: {
      ...base,
      ether: {
        ...base.ether!,
        settings: {
          fields: [
            {
              key: "boost4k",
              kind: "bool",
              label: "Boost 2160p releases",
              default: true,
            },
          ],
        },
        contributes: {
          rankingSteps: [
            {
              key: "resolution_boost",
              label: "Boost 2160p releases",
              hint: "Adds a score bonus when a candidate is 2160p",
            },
          ],
        },
      },
    },
    defaultSettings: {
      boost4k: true,
    },
    routes: {
      rankKey(candidate, config) {
        const is4k = candidate.resolution === 2160 || candidate.resolution === "2160p";
        const bonus = config.boost4k && is4k ? 1000 : 0;
        return (candidate.cached ? 500 : 0) + bonus;
      },
    },
  };
}

export function createConditionTemplate(opts: TemplateOptions): PluginDefinition {
  const base = baseManifest(opts, "extension", ["rule_condition"]);
  return {
    manifest: {
      ...base,
      ether: {
        ...base.ether!,
        contributes: {
          ruleConditions: [
            {
              key: "cached_release",
              label: "Release is cached",
              operators: ["eq"],
            },
          ],
        },
      },
    },
    routes: {
      condMatch(candidate, _operator, value) {
        const want = typeof value === "boolean" ? value : true;
        return Boolean(candidate.cached) === want;
      },
    },
  };
}
