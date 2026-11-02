import { describe, expect, it } from "vitest";
import { safeHref } from "./cells";

describe("plugin link boundaries", () => {
  it("rejects browser-normalized external paths and malformed HTTPS URLs", () => {
    for (const href of [
      "/\\evil.example",
      "/\t/evil.example",
      "https://",
      "https://user:pass@example.com",
    ]) {
      expect(safeHref(href)).toBeNull();
    }
    expect(safeHref("/catalog")).toBe("/catalog");
    expect(safeHref("https://example.org/page")).toBe("https://example.org/page");
  });
});
