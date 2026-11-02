import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { buildNavigation } from "../App";
import { descriptors } from "../mock/extend/descriptors/console";
import { fixturePluginPage } from "../lib/console-descriptors.test";
import type { Descriptor } from "../lib/extend";
import { renderApp, settleUi } from "./harness";

describe("registry navigation", () => {
  it("builds the sidebar in the same order as before", () => {
    const groups = buildNavigation(descriptors);
    expect(groups.map((g) => g.label)).toEqual(["", "Library", "Streaming", "Server"]);
    expect(groups[0]!.items.map((i) => i.href)).toEqual(["/", "/activity", "/viewers"]);
    expect(groups[0]!.items.map((i) => i.label)).toEqual(["Overview", "Activity", "Viewers"]);
    expect(groups[1]!.items.map((i) => i.href)).toEqual([
      "/catalog",
      "/collections",
      "/home",
      "/livetv",
      "/calendar",
      "/metadata",
    ]);
    expect(groups[2]!.items.map((i) => i.href)).toEqual([
      "/sources",
      "/releases",
      "/cache",
      "/playback",
    ]);
    expect(groups[3]!.items.map((i) => i.href)).toEqual([
      "/server",
      "/apps",
      "/services",
      "/plugins",
      "/notifications",
      "/cluster",
      "/maintenance",
    ]);
    // Mobile tab bar membership is unchanged.
    const mobile = groups.flatMap((g) => g.items).filter((i) => i.mobile);
    expect(mobile.map((i) => i.href)).toEqual(["/", "/activity", "/viewers", "/collections"]);
  });

  it("places a fixture plugin page in its nav group", () => {
    const groups = buildNavigation([...descriptors, fixturePluginPage]);
    const server = groups.find((g) => g.label === "Server")!;
    const item = server.items.find((i) => i.href === "/p/dev.example/wrapped");
    expect(item?.label).toBe("Ether Wrapped");
    expect(server.items.indexOf(item!)).toBe(server.items.length - 1);
  });

  it("deduplicates a plugin page paired with a host navItem", () => {
    const hostPage: Descriptor = {
      kind: "page",
      id: "plugin:community.example.wrapped/wrapped",
      owner: "community.example.wrapped",
      label: { resolved: "Ether Wrapped", plugin: "community.example.wrapped" },
      group: "Ether Wrapped",
      meta: {
        route: "/ether/view/wrapped",
        pluginRoute: "/api/plugins/community.example.wrapped/views/wrapped",
        group: "library",
        icon: "https://example.org/wrapped.svg",
      },
    };
    const hostNav: Descriptor = {
      kind: "navItem",
      id: "plugin:community.example.wrapped/page-wrapped",
      owner: "community.example.wrapped",
      label: { resolved: "Ether Wrapped", plugin: "community.example.wrapped" },
      group: "Ether Wrapped",
      meta: { route: "/plugins/community.example.wrapped/wrapped", pageKey: "wrapped" },
    };
    const groups = buildNavigation([...descriptors, hostPage, hostNav]);
    const library = groups.find((g) => g.label === "Library")!;
    const matches = library.items.filter((i) => i.href === "/p/community.example.wrapped/wrapped");
    expect(matches).toHaveLength(1);
    expect(matches[0]?.label).toBe("Ether Wrapped");
  });

  it("renders the sidebar and ⌘K plugin page destination from the mock registry", async () => {
    renderApp("/");
    await settleUi(() => {
      expect(screen.getAllByRole("link", { name: "Integrations" }).length).toBeGreaterThan(0);
      expect(screen.getAllByRole("link", { name: "Plugins" }).length).toBeGreaterThan(0);
      expect(screen.getAllByRole("link", { name: "Ether Wrapped" }).length).toBeGreaterThan(0);
    });
    const searchInput = screen.getByPlaceholderText("Search or jump to…");
    fireEvent.focus(searchInput);
    fireEvent.change(searchInput, { target: { value: "Ether Wrapped" } });
    await settleUi(() => {
      expect(screen.getByText("Go to")).toBeInTheDocument();
    });
  });

  it("keeps a plugin page route instead of redirecting home", async () => {
    renderApp("/p/dev.example/wrapped");
    await settleUi(() => {
      expect(screen.getAllByRole("link", { name: "Plugins" }).length).toBeGreaterThan(0);
      expect(screen.getByRole("heading", { name: "wrapped" })).toBeInTheDocument();
    });
    const main = document.querySelector("main");
    expect(main?.textContent ?? "").not.toContain("Now playing");
  });
});
