import { describe, expect, it } from "vitest";
import { descriptors } from "../mock/extend/descriptors/livetv";
import { descriptors as collectionDescriptors } from "../mock/extend/descriptors/collections";

describe("Live TV source kinds from the registry", () => {
  it("serves the built-in kinds with editors", () => {
    const byId = new Map(descriptors.map((d) => [d.id, d]));
    expect([...byId.keys()].sort()).toEqual(["guideOnly", "m3u", "stremio", "virtual", "xtream"]);
    const meta = (id: string) => byId.get(id)?.meta as { editor?: string } | undefined;
    expect(meta("m3u")?.editor).toBe("playlist");
    expect(meta("guideOnly")?.editor).toBe("guide");
    expect(meta("xtream")?.editor).toBe("xtream");
    expect(meta("stremio")?.editor).toBe("stremio");
    expect(byId.get("m3u")?.label.source).toBe("M3U playlist");
    expect(byId.get("guideOnly")?.label.source).toBe("Guide only (XMLTV)");
  });

  it("keeps the Xtream password a secret field", () => {
    const d = descriptors.find((x) => x.id === "xtream");
    const params = (d?.meta as { params?: { name: string; kind: string }[] })?.params ?? [];
    expect(params.find((p) => p.name === "password")?.kind).toBe("secret");
  });

  it("serves row source kinds beside collection sources", () => {
    const rows = collectionDescriptors.filter((d) => d.kind === "rowSource");
    expect(rows.length).toBeGreaterThan(20);
    const cw = rows.find((d) => d.id === "continue-watching");
    expect(cw?.label.source).toBe("Continue Watching");
    expect((cw?.meta as { hubId?: string })?.hubId).toBe("continue-watching");
  });
});
