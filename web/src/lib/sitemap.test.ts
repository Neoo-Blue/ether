import { describe, expect, it } from "vitest";
import { destinationsFrom, pagesFrom } from "./sitemap";
import { descriptors } from "../mock/extend/descriptors/console";
import type { PageMeta } from "./console-descriptors";
import type { Descriptor } from "./extend";

const pages = descriptors.filter((d) => d.kind === "page") as Descriptor<PageMeta>[];

describe("sitemap", () => {
  it("gives every destination exactly one home", () => {
    const hrefs = destinationsFrom(pages).map((d) => d.href);
    expect(new Set(hrefs).size).toBe(hrefs.length);
    expect(pagesFrom(pages).map((p) => p.title)).toContain("Overview");
  });
});
