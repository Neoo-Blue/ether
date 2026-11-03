import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { addonUrlHasDebridKey, normalizeAddonUrl } from "../components/addons/addons";
import { calls, renderApp } from "./harness";

const WAIT = { timeout: 4000 };

async function openAddAddon() {
  fireEvent.click(await screen.findByRole("button", { name: /Add indexer/ }, WAIT));
  fireEvent.click(await screen.findByRole("menuitem", { name: "Stremio add-on…" }));
  return screen.findByRole("dialog", { name: "Add a Stremio add-on" });
}

describe("Sources › Indexers Stremio add-ons", () => {
  it("adds an add-on for its streams only, through the shared add-on flow", async () => {
    const { fake } = renderApp("/sources/indexers");
    const drawer = await openAddAddon();
    fireEvent.change(within(drawer).getByLabelText("Install link"), {
      target: { value: "stremio://streams.example/opts=1/manifest.json" },
    });
    expect(within(drawer).queryByText(/includes a debrid API key/)).toBeNull();
    fireEvent.click(within(drawer).getByRole("button", { name: "Continue" }));
    // The page's role starts on; the add-on's other roles start off.
    const streams = await within(drawer).findByRole("switch", { name: "Streams to play" });
    expect(streams).toBeChecked();
    expect(
      within(drawer).getByRole("switch", { name: "Catalogs for collections and Home rows" }),
    ).not.toBeChecked();
    fireEvent.click(within(drawer).getByRole("button", { name: "Add add-on" }));
    const installs = () =>
      calls(fake, "POST", "/plugins/addons").filter((c) => c.path === "/plugins/addons");
    await waitFor(() => expect(installs()).toHaveLength(1));
    expect(installs()[0]!.body).toMatchObject({
      url: "stremio://streams.example/opts=1/manifest.json",
      roles: ["stream_source"],
    });
    // No link is ever written into the indexer list.
    expect(calls(fake, "PUT", "/config/indexers")).toHaveLength(0);
  });

  it("shows an add-on row by name and switches its streams with the add-on", async () => {
    const { fake } = renderApp("/sources/indexers");
    const list = await screen.findByRole("region", { name: "Indexers" }, WAIT);
    expect(await within(list).findByText("Add-on")).toBeInTheDocument();
    fireEvent.click(within(list).getByRole("button", { name: "Edit Stream Addon" }));
    const drawer = await screen.findByRole("dialog", { name: "Stream Addon" });
    expect(within(drawer).queryByLabelText(/URL|link/i)).toBeNull();
    expect(within(drawer).getByText(/Searches addon\.example/)).toBeInTheDocument();
    const toggle = within(drawer).getByRole("switch", { name: "Streams" });
    await waitFor(() => expect(toggle).toBeChecked());
    fireEvent.click(toggle);
    await waitFor(() =>
      expect(calls(fake, "PUT", "/plugins/addon.mockstreams/roles")).toHaveLength(1),
    );
    expect(calls(fake, "PUT", "/plugins/addon.mockstreams/roles")[0]!.body).toMatchObject({
      off: expect.arrayContaining(["stream_source", "list_source"]),
    });
  });

  it("warns when the link embeds a debrid key", async () => {
    renderApp("/sources/indexers");
    const drawer = await openAddAddon();
    fireEvent.change(within(drawer).getByLabelText("Install link"), {
      target: { value: "https://addon.example/sort=size|realdebrid=ABC123/manifest.json" },
    });
    expect(await within(drawer).findByText(/includes a debrid API key/)).toBeVisible();
  });

  it("detects keys in plain and base64 add-on configurations", () => {
    const config = btoa(JSON.stringify({ debridService: "torbox", debridApiKey: "k-123" }));
    expect(addonUrlHasDebridKey(`https://addon.example/${config}/manifest.json`)).toBe(true);
    const urlSafe = config.replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
    expect(addonUrlHasDebridKey(`https://addon.example/${urlSafe}/manifest.json`)).toBe(true);
    expect(
      addonUrlHasDebridKey("https://addon.example/providers=a,b%7CTorBox=xyz/manifest.json"),
    ).toBe(true);
    const keyless = btoa(JSON.stringify({ maxResults: 20, debridService: "" }));
    expect(addonUrlHasDebridKey(`https://addon.example/${keyless}/manifest.json`)).toBe(false);
    expect(addonUrlHasDebridKey("https://addon.example/qualityfilter=cam/manifest.json")).toBe(
      false,
    );
    expect(normalizeAddonUrl(" stremio://addon.example/manifest.json")).toBe(
      "https://addon.example/manifest.json",
    );
  });
});
