import { test } from "node:test";
import { strict as assert } from "node:assert";
import { readFileSync } from "node:fs";
import { createMockHostClient } from "../../../typescript/src/index.ts";
import { createPlugin } from "../plugin.ts";
const channels = readFileSync(
  new URL("./fixtures/channels.json", import.meta.url),
  "utf8",
);
const boot = readFileSync(
  new URL("./fixtures/boot.json", import.meta.url),
  "utf8",
);
const guide = readFileSync(
  new URL("./fixtures/guide.json", import.meta.url),
  "utf8",
);
test("maps recorded channels, guide, and streams without network", async () => {
  const host = createMockHostClient({
    httpHandler: (r) => ({
      status: 200,
      headers: {},
      body: r.url.includes("boot.pluto.tv")
        ? boot
        : r.url.includes("channels.json")
          ? channels
          : guide,
    }),
  });
  const p = createPlugin(host);
  const list = (await p.dispatch({ path: "/ether/channels.json" })).body as any;
  assert.ok(list.channels[0].inputs[0].url.startsWith("plugin://"));
  assert.equal(list.channels[0].number, 1030);
  assert.equal(list.channels[0].group, "News + Opinion");
  const id = list.channels[0].id;
  const g = (
    await p.dispatch({
      path: `/ether/guide/${id}.json`,
      query: { from: "2026-11-02T00:00:00Z", to: "2026-11-03T00:00:00Z" },
    })
  ).body as any;
  assert.equal(g.programmes[0].title, "Bloomberg TV+");
  const s = (await p.dispatch({ path: `/stream/tv/${id}.json` })).body as any;
  assert.ok(s.streams[0].url.includes(id));
  assert.ok(s.streams[0].url.includes("fixture-token"));
});
