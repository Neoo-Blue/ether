import { plugin, type Manifest } from "../../../typescript/src/index.ts";

export interface HelloSettings {
  greeting: string;
}

export const helloManifest: Manifest = {
  id: "org.ether.examples.hello",
  version: "0.1.0",
  name: "Hello plugin",
  description: "Minimal greeting plugin for Ether.",
  types: ["movie"],
  idPrefixes: ["tt"],
  catalogs: [
    {
      type: "movie",
      id: "hello",
      name: "Hello movies",
      extra: [{ name: "search", isRequired: false }],
    },
  ],
  resources: ["catalog", "meta", "subtitles"],
  ether: {
    protocol: 1,
    kind: "extension",
    capabilities: ["list_source", "subtitles", "ui", "ranking_step", "rule_condition"],
    summary: "Minimal greeting plugin for Ether.",
    runtime: {
      kind: "hosted",
      entry: "plugin.ts",
    },
    permissions: {
      catalog: "none",
    },
    settings: {
      fields: [
        {
          key: "greeting",
          kind: "text",
          label: "Greeting word",
          hint: "Word used to greet viewers in the catalog and view",
          default: "Hello",
        },
      ],
    },
    contributes: {
      listSources: [
        {
          key: "hello",
          label: "Hello movies",
          hint: "Sample movie list from the hello plugin",
          catalog: "hello",
          refresh: "1h",
        },
      ],
      subtitles: {
        languages: ["en"],
        formats: ["vtt"],
      },
      pages: [
        {
          key: "home",
          label: "Hello overview",
          route: "/ether/view/home",
        },
      ],
      rankingSteps: [
        {
          key: "cached_boost",
          label: "Boost cached releases",
          hint: "Adds a score bonus when a release is cached",
        },
      ],
      ruleConditions: [
        {
          key: "is_cached",
          label: "Release is cached",
          operators: ["eq"],
        },
      ],
    },
  },
};

export const helloPlugin = plugin<HelloSettings>({
  manifest: helloManifest,
  defaultSettings: {
    greeting: "Hello",
  },
  routes: {
    test() {
      return {
        ok: true,
        message: "Hello plugin is healthy.",
      };
    },
    catalog({ type, search }, ctx) {
      const greeting = ctx.settings.greeting || "Hello";
      const target = search || "from Ether";
      return {
        metas: [
          {
            id: "tt0133093",
            type,
            name: `${greeting} ${target}`,
            releaseInfo: "1999",
            description: "A computer hacker learns about the true nature of reality.",
          },
        ],
      };
    },
    meta({ type, id }) {
      return {
        meta: {
          id,
          type,
          name: "The Matrix",
          releaseInfo: "1999",
          description: "A computer hacker learns about the true nature of reality.",
        },
      };
    },
    subtitles({ id }) {
      return {
        subtitles: [
          {
            id: `${id}:en`,
            url: `https://subtitles.example.invalid/${encodeURIComponent(id)}/en.vtt`,
            lang: "en",
            label: "English",
            format: "vtt",
          },
        ],
      };
    },
    view(_req, ctx) {
      const greeting = ctx.settings.greeting || "Hello";
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
              title: "Greeting",
              blocks: [
                {
                  kind: "text",
                  content: { $: "greetingText" },
                },
              ],
            },
          ],
        },
        data: {
          title: "Hello plugin",
          subtitle: "Greeting view",
          greetingText: `${greeting} from Ether`,
        },
      };
    },
    action(_req, ctx) {
      const greeting = ctx.settings.greeting || "Hello";
      return {
        toast: `${greeting} from Ether`,
        refresh: true,
      };
    },
    rankKey(candidate) {
      return candidate.cached ? 1000 : 100;
    },
    condMatch(candidate, _op, value) {
      const want = typeof value === "boolean" ? value : true;
      return Boolean(candidate.cached) === want;
    },
  },
});

export const handle = helloPlugin.handle;
export const rank_key = helloPlugin.rank_key;
export const cond_match = helloPlugin.cond_match;
export default helloPlugin;
