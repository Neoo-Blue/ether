import { describe, expect, it } from "vitest";
import { destinations, PAGES } from "./sitemap";

describe("sitemap", () => {
  it("gives every destination exactly one home", () => {
    const hrefs = destinations().map((d) => d.href);
    expect(new Set(hrefs).size).toBe(hrefs.length);
    expect(PAGES.map((p) => p.title)).toContain("Overview");
  });

  it("points Plex sections at the Plex tab, never at dead Apps tabs", () => {
    const hrefs = destinations().map((d) => d.href);
    expect(hrefs).not.toContain("/apps/remote");
    expect(hrefs).not.toContain("/apps/secure");
    expect(hrefs).toContain("/apps/plex/account");
    expect(hrefs).toContain("/apps/plex/remote");
    expect(hrefs).toContain("/apps/plex/compatibility");
  });

  it("lands switchable integrations on their overview card and lists every tab", () => {
    const byTitle = new Map(
      destinations()
        .filter((d) => d.page === "Integrations")
        .map((d) => [d.title, d.href]),
    );
    for (const [title, card] of [
      ["Trakt", "trakt"],
      ["Simkl", "simkl"],
      ["Language model", "llm"],
      ["OpenSubtitles", "opensubtitles"],
      ["Ratings", "ratings"],
    ] as const) {
      expect(byTitle.get(title)).toBe(`/services/overview#${card}`);
    }
    const hrefs = destinations().map((d) => d.href);
    expect(hrefs).toContain("/services/tmdb");
    expect(hrefs).toContain("/services/introdb");
    expect(hrefs).toContain("/notifications/protection");
  });
});
