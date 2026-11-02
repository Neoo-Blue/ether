import test from "node:test";
import assert from "node:assert/strict";
import { createTestHarness, fixtures, serve } from "../../../typescript/src/index.ts";
import { helloPlugin } from "./plugin.ts";

test("hello typescript plugin responds identically in hosted and HTTP sidecar modes", async () => {
  const harness = createTestHarness(helloPlugin);

  const health = await harness.health();
  assert.equal(health.status, 200);
  assert.deepEqual(health.body, { status: "ok" });

  const cat = await harness.catalog("movie", "hello");
  assert.equal(cat.status, 200);
  assert.equal(cat.body.metas[0]?.name, "Hello from Ether");

  const meta = await harness.meta("movie", "tt0133093");
  assert.equal(meta.status, 200);
  assert.equal(meta.body.meta.name, "The Matrix");

  const subDenied = await harness.subtitles("movie", "tt0133093");
  assert.equal(subDenied.status, 403);

  const subOk = await harness.subtitles("movie", "tt0133093", fixtures.pickToken("tt0133093"));
  assert.equal(subOk.status, 200);
  assert.equal(subOk.body.subtitles[0]?.lang, "en");

  const view = await harness.view("home");
  assert.equal(view.status, 200);
  assert.equal(view.body.data.greetingText, "Hello from Ether");

  const sidecar = await serve(helloPlugin, { mode: "http", port: 0 });
  try {
    const res = await fetch(`${sidecar.url}/catalog/movie/hello.json`);
    assert.equal(res.status, 200);
    const json = await res.json();
    assert.deepEqual(json, cat.body);
  } finally {
    await sidecar.close();
  }
});
