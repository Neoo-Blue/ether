import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { buildNavigation } from "../App";
import { descriptors } from "../mock/extend/descriptors/console";
import { fixturePluginPage } from "../lib/console-descriptors.test";
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

  it("renders the sidebar from the mock registry", async () => {
    renderApp("/");
    await settleUi(() => {
      expect(screen.getAllByRole("link", { name: "Integrations" }).length).toBeGreaterThan(0);
      expect(screen.getAllByRole("link", { name: "Plugins" }).length).toBeGreaterThan(0);
    });
  });

  it("keeps a plugin page route instead of redirecting home", async () => {
    renderApp("/p/dev.example/wrapped");
    await settleUi(() => {
      expect(screen.getAllByRole("link", { name: "Plugins" }).length).toBeGreaterThan(0);
    });
    // The placeholder plugin page renders nothing; a redirect home would
    // mount the overview dashboard in the main region.
    const main = document.querySelector("main");
    expect(main?.textContent ?? "").not.toContain("Now playing");
  });
});
