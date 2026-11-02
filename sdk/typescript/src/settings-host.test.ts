import test from "node:test";
import assert from "node:assert/strict";
import { plugin, createMockHostClient } from "./index.ts";
test("request settings refresh from the host, including asynchronous sidecars", async () => {
  const host = createMockHostClient();
  let value = "one";
  host.settings = { get: async () => ({ word: value }) };
  const p = plugin({
    manifest: {
      id: "test.settings",
      name: "Settings",
      version: "1",
      ether: {
        protocol: 1,
        summary: "Settings",
        kind: "extension",
        runtime: { kind: "hosted", entry: "x" },
        settings: { fields: [{ key: "word", kind: "text" }] },
      },
    },
    host,
    routes: {
      test(ctx) {
        return { ok: true, message: ctx.settings.word as string };
      },
    },
  });
  assert.equal(
    ((await p.dispatch({ method: "POST", path: "/ether/test" })).body as any)
      .message,
    "one",
  );
  value = "two";
  assert.equal(
    ((await p.dispatch({ method: "POST", path: "/ether/test" })).body as any)
      .message,
    "two",
  );
});

test("settings failure leaves routes available with empty settings", async () => {
  const host = createMockHostClient();
  host.settings = { get: async () => { throw new Error("failure"); } };
  const p = plugin({ manifest: { id: "test.settings", name: "Settings", version: "1" }, host, routes: { test: (ctx) => ({ ok: Object.keys(ctx.settings).length === 0, message: "ok" }) } });
  assert.equal(((await p.dispatch({method: "POST", path: "/ether/test"})).body as any).ok, true);
});
