import { describe, expect, it } from "vitest";
import { gallerySpec } from "./fixtures/galleryDoc";
import { validatePluginSpec } from "./validate";

function specWith(block: unknown) {
  return { specVersion: 1, layout: "list", header: [], body: [block] };
}

describe("validatePluginSpec", () => {
  it("accepts the full gallery document", () => {
    const result = validatePluginSpec(gallerySpec);
    expect(result.ok, result.ok ? "" : result.errors.join("; ")).toBe(true);
  });

  it("accepts every Ether widget and frame name", () => {
    const widgets: Record<string, Record<string, unknown>> = {
      "ether.posterGrid": { items: { $: "titles" } },
      "ether.posterShelf": { items: [] },
      "ether.titleCard": { tmdbId: 550 },
      "ether.liveChart": { series: [] },
      "ether.settingsForm": { groups: [], values: {}, action: "save" },
      "ether.actionButton": { label: "Go", action: "go" },
      "ether.titleMatch": { action: "match" },
      "ether.healthStrip": { items: [] },
      "ether.channelRow": { name: "One" },
      "ether.badge": { label: "New" },
    };
    for (const [name, props] of Object.entries(widgets)) {
      const result = validatePluginSpec(specWith({ kind: "widget", widget: name, props }));
      expect(result.ok, `${name}: ${result.ok ? "" : result.errors.join("; ")}`).toBe(true);
    }
    for (const name of ["ether.section", "ether.tabs", "ether.drawer"]) {
      const props =
        name === "ether.tabs"
          ? { tabs: [{ key: "a", label: "A" }] }
          : name === "ether.drawer"
            ? { title: "x" }
            : {};
      const result = validatePluginSpec(
        specWith({ kind: "frame", frame: name, props, blocks: [] }),
      );
      expect(result.ok, `${name}: ${result.ok ? "" : result.errors.join("; ")}`).toBe(true);
    }
  });

  it("rejects unknown widgets, frames and bad props", () => {
    const bad = [
      { kind: "widget", widget: "some-widget" },
      { kind: "widget", widget: "ether.unknown", props: {} },
      { kind: "frame", frame: "card", blocks: [] },
      { kind: "widget", widget: "ether.badge", props: { bogus: 1 } },
      { kind: "widget", widget: "ether.badge", props: {} },
      { kind: "widget", widget: "ether.posterGrid", props: { items: "no" } },
      { kind: "widget", widget: "ether.titleCard", props: { reason: "x" } },
    ];
    for (const block of bad) {
      expect(validatePluginSpec(specWith(block)).ok, JSON.stringify(block)).toBe(false);
    }
  });

  it("rejects every class-name field", () => {
    const docs: unknown[] = [
      specWith({ kind: "text", content: "x", className: "text-red" }),
      specWith({ kind: "panel", title: "x", bodyClassName: "p-0", blocks: [] }),
      specWith({
        kind: "repeat",
        items: { $: "r" },
        itemKey: { $: "id" },
        itemClassName: "x",
        blocks: [],
      }),
      specWith({ kind: "page-header", title: "x", actionsClassName: "flex" }),
      specWith({
        kind: "table",
        rows: { $: "r" },
        rowKey: { $: "id" },
        columns: [
          {
            header: "x",
            headerClassName: "w-40",
            cell: { kind: "text", field: { $: "a" } },
          },
        ],
      }),
      specWith({
        kind: "table",
        rows: { $: "r" },
        rowKey: { $: "id" },
        columns: [
          {
            header: "x",
            cell: { kind: "text", field: { $: "a" }, className: "mono" },
          },
        ],
      }),
      specWith({
        kind: "table",
        rows: { $: "r" },
        rowKey: { $: "id" },
        columns: [
          {
            header: "x",
            cell: { kind: "text", field: { $: "a" }, fallbackClassName: "dim" },
          },
        ],
      }),
      {
        specVersion: 1,
        layout: "list",
        bodyClassName: "flex",
        header: [],
        body: [],
      },
    ];
    for (const doc of docs) {
      const result = validatePluginSpec(doc);
      expect(result.ok, JSON.stringify(doc)).toBe(false);
      if (!result.ok) {
        expect(result.errors.join(" ")).toContain("class-name");
      }
    }
  });

  it("refuses prototype paths and over-deep nesting", () => {
    for (const path of ["__proto__", "a.__proto__.b", "constructor", "x.prototype"]) {
      expect(validatePluginSpec(specWith({ kind: "text", content: { $: path } })).ok, path).toBe(
        false,
      );
    }
    let block: unknown = { kind: "grid", blocks: [] };
    for (let i = 0; i < 12; i++) block = { kind: "grid", blocks: [block] };
    expect(validatePluginSpec(specWith(block)).ok).toBe(false);
  });

  it("keeps the unions closed and enforces Ether filter and table rules", () => {
    const bad: unknown[] = [
      specWith({ kind: "script", src: "evil.js" }),
      specWith({ kind: "heading", level: 2, content: "x", onClick: "x" }),
      { specVersion: 99, layout: "list", header: [], body: [] },
      specWith({ kind: "filter-bar", controls: { period: true } }),
      specWith({
        kind: "filter-bar",
        controls: { search: true },
        customers: { $: "c" },
      }),
      specWith({
        kind: "table",
        variant: "report",
        rows: { $: "r" },
        rowKey: { $: "id" },
        columns: [{ header: "x", cell: { kind: "text", field: { $: "a" } } }],
      }),
    ];
    for (const doc of bad) {
      expect(validatePluginSpec(doc).ok, JSON.stringify(doc)).toBe(false);
    }
    expect(
      validatePluginSpec(specWith({ kind: "filter-bar", controls: { search: true } })).ok,
    ).toBe(true);
  });
});
