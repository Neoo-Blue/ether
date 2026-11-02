import { screen, within } from "@testing-library/react";
import { expect, it } from "vitest";
import { renderApp } from "./harness";
import { RELEASE_SEARCH } from "../pages/settings/schema";
import { destinations } from "../lib/sitemap";
import { settingsIndex } from "../lib/settings-index";

it("gives the checker its own searchable destination and inline controls", async () => {
  renderApp("/quality/check");
  const result = await screen.findByTestId("quality-headline", {}, { timeout: 10000 });
  expect(result).toHaveTextContent("4K");
  expect(screen.getAllByTestId("quality-headline")).toHaveLength(1);
  const viewer = screen.getByRole("button", { name: "Viewer" });
  const row = viewer.closest(".grid")!;
  expect(row).toHaveClass("sm:grid-cols-3");
  expect(within(row as HTMLElement).getByRole("button", { name: "App" })).toBeVisible();
  expect(within(row as HTMLElement).getByRole("radio", { name: "Away" })).toBeVisible();
  expect(destinations().some((d) => d.href === "/quality/check")).toBe(true);
  expect(settingsIndex().find((d) => d.title === "Check quality")?.href).toBe("/quality/check");
});

it("keeps the four search settings visible", () => {
  expect(RELEASE_SEARCH[0]!.more).toBeUndefined();
  expect(RELEASE_SEARCH[0]!.fields.map((f) => f.key)).toEqual([
    "maxCandidates",
    "healthSamples",
    "searchCacheTtl",
    "unavailableCooloff",
  ]);
});

it("orders profile sections before away, source order, and versions", async () => {
  renderApp("/quality/profiles");
  await screen.findByRole("button", { name: "Add profile" }, { timeout: 10000 });
  await screen.findByText("Same-quality versions to offer", {}, { timeout: 10000 });
  const headings = screen.getAllByRole("heading").map((h) => h.textContent);
  const order = ["Profiles", "Away from home", "Which source goes first", "Version menu"].map((s) =>
    headings.indexOf(s),
  );
  expect(order.every((i) => i >= 0)).toBe(true);
  expect(order).toEqual([...order].sort((a, b) => a - b));
  expect(screen.queryByTestId("quality-headline")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Import from TRaSH Guides" }),
  ).not.toBeInTheDocument();
});
