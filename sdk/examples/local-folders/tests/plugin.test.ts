import { test } from "node:test";
import { strict as assert } from "node:assert";
import { readFileSync } from "node:fs";
import { createMockHostClient } from "../../../typescript/src/index.ts";
import { createPlugin } from "../plugin.ts";
const folders = JSON.parse(
  readFileSync(new URL("./fixtures/folders.json", import.meta.url), "utf8"),
);
test("scan indexes movies and episodes, manual match survives a new instance, and stale files disappear", async () => {
  const host = createMockHostClient({
    folders: new Map(Object.entries(folders)),
    catalog: new Map([
      [
        "a",
        {
          id: 1,
          title: "Example Movie",
          type: "movie",
          year: 2001,
          tmdbId: 10,
        },
      ],
      ["b", { id: 2, title: "Example Show", type: "show", tmdbId: 20 }],
    ]),
  });
  host.settings = { get: () => ({ folder: "/media" }) };
  let p = createPlugin(host);
  await p.dispatch({
    method: "POST",
    path: "/ether/configure",
    body: { folder: "/media" },
  });
  await p.dispatch({ method: "POST", path: "/ether/job/scan" });
  let movie = await p.dispatch({ path: "/stream/movie/tmdb%3A10.json" });
  assert.equal(
    (movie.body as any).streams[0].ether.file,
    "Example Movie (2001)/Example.Movie.2001.1080p.mkv",
  );
  let ep = await p.dispatch({ path: "/stream/series/tmdb%3A20%3A1%3A2.json" });
  assert.equal((ep.body as any).streams.length, 1);
  const page = await p.dispatch({ path: "/ether/view/matches" });
  assert.equal((page.body as any).data.folder0, "Unmatched");
  await p.dispatch({
    method: "POST",
    path: "/ether/action/match",
    body: { folder: "Unmatched", tmdbId: 30, mediaType: "movie" },
  });
  p = createPlugin(host);
  assert.equal(
    ((await p.dispatch({ path: "/stream/movie/tmdb%3A30.json" })).body as any)
      .streams.length,
    1,
  );
  host.state.folders.set("/media", []);
  await p.dispatch({ method: "POST", path: "/ether/job/scan" });
  assert.equal(
    ((await p.dispatch({ path: "/stream/movie/tmdb%3A10.json" })).body as any)
      .streams.length,
    0,
  );
});
