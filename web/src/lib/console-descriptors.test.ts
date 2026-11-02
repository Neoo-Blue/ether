import { describe, expect, it } from "vitest";
import {
  agentSuggestionsFor,
  pageRoute,
  type CommandMeta,
  type PageMeta,
} from "./console-descriptors";
import { destinationsFrom } from "./sitemap";
import { descriptors } from "../mock/extend/descriptors/console";
import type { Descriptor } from "./extend";

export const fixturePluginPage: Descriptor<PageMeta> = {
  kind: "page",
  id: "plugin:dev.example/wrapped",
  owner: "dev.example",
  label: { plugin: "dev.example", key: "wrapped", resolved: "Ether Wrapped" },
  icon: "https://example.org/wrapped.svg",
  group: "server",
  order: 80,
  meta: { navGroup: "server", keywords: { resolved: "year in review" } },
};

const commands = descriptors.filter((d) => d.kind === "command") as Descriptor<CommandMeta>[];

describe("console descriptors", () => {
  it("routes a plugin page to /p/<plugin>/<page>", () => {
    expect(pageRoute(fixturePluginPage)).toBe("/p/dev.example/wrapped");
    // A plugin page descriptor whose meta.route is the internal /ether/view/...
    // loader route still maps to its console /p/<plugin>/<page> route.
    expect(
      pageRoute({
        ...fixturePluginPage,
        meta: { route: "/ether/view/wrapped", group: "server" },
      }),
    ).toBe("/p/dev.example/wrapped");
  });

  it("lists a plugin page as a search destination", () => {
    const pages = descriptors.filter((d) => d.kind === "page") as Descriptor<PageMeta>[];
    const all = destinationsFrom([...pages, fixturePluginPage]);
    expect(all.find((d) => d.href === "/p/dev.example/wrapped")).toMatchObject({
      title: "Ether Wrapped",
      isPage: true,
    });
  });

  it("keeps agent suggestions per route and out of plain actions", () => {
    const onReleases = agentSuggestionsFor(commands, "/releases/profiles");
    expect(onReleases.map((c) => c.id)).toContain("ask.releases.why-pick");
    expect(agentSuggestionsFor(commands, "/livetv").map((c) => c.id)).toContain(
      "ask.livetv.failing",
    );
    expect(agentSuggestionsFor(commands, "/cache")).toEqual([]);
    const actions = commands.filter((c) => c.meta?.route && !c.meta.agentSuggestion);
    expect(actions.map((c) => c.id)).toContain("new-collection");
    expect(actions.map((c) => c.id)).not.toContain("ask.releases.why-pick");
  });
});
