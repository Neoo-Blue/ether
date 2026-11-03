import { ConfirmRoot } from "@braedonsaunders/appkit-ui";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { invalidateRegistry } from "../lib/extend";
import { createFakeServer } from "../mock/server";
import Plugins from "../pages/plugins/Plugins";

let fakeServer = createFakeServer({ latencyMs: 0 });
let customIntercept:
  ((url: URL, init?: RequestInit) => Promise<Response | null> | Response | null) | null = null;

function renderPlugins(initialEntry = "/plugins") {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <ConfirmRoot />
      <Routes>
        <Route path="/plugins/:tab?/*" element={<Plugins />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("Server › Plugins (WS10a)", () => {
  const realFetch = window.fetch.bind(window);

  beforeEach(() => {
    invalidateRegistry();
    customIntercept = null;
    fakeServer = createFakeServer({ latencyMs: 0 });
    window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = new URL(
        input instanceof Request ? input.url : String(input),
        window.location.origin,
      );
      if (customIntercept) {
        const res = await customIntercept(url, init);
        if (res) return res;
      }
      if (url.origin === window.location.origin && url.pathname.startsWith("/ether/api/")) {
        return fakeServer.fetch(url.toString(), init);
      }
      return realFetch(input, init);
    };
  });

  afterEach(() => {
    window.fetch = realFetch;
    cleanup();
  });

  it("renders installed plugins with kind, health, version, update, Unverified, and shadow badges and toggles enable/disable", async () => {
    renderPlugins("/plugins");

    expect(await screen.findByText("Pluto TV")).toBeTruthy();
    expect(screen.getByText("Dub language")).toBeTruthy();
    expect(screen.getByText("Ether Wrapped")).toBeTruthy();

    // An installed Stremio add-on is listed with the plugins.
    expect(screen.getByText("Stream Addon")).toBeTruthy();

    expect(screen.getByText("Update v1.3.0")).toBeTruthy();
    expect(screen.getAllByText("Unverified")).toHaveLength(2);
    expect(screen.getByText("Trying out", { selector: "span *, span" })).toBeTruthy();

    const plutoSwitch = screen.getByRole("switch", { name: "Enable Pluto TV" }) as HTMLInputElement;
    expect(plutoSwitch.checked).toBe(true);

    fireEvent.click(plutoSwitch);
    await waitFor(() => {
      expect(plutoSwitch.checked).toBe(false);
    });

    fireEvent.click(plutoSwitch);
    await waitFor(() => {
      expect(plutoSwitch.checked).toBe(true);
    });
  });

  it("renders the Installed empty state explaining plugins in one sentence and offering the marketplace and Studio", async () => {
    customIntercept = (url, init) => {
      if (url.pathname === "/ether/api/plugins" && (!init?.method || init.method === "GET")) {
        return new Response(JSON.stringify({ plugins: [] }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      return null;
    };

    renderPlugins("/plugins");

    expect(await screen.findByText("No plugins installed")).toBeTruthy();
    expect(
      screen.getByText(
        "Plugins add sources, channels, subtitle providers and custom pages to Ether.",
      ),
    ).toBeTruthy();

    expect(screen.getByRole("button", { name: "Open the marketplace" })).toBeTruthy();
    expect(screen.getByRole("button", { name: /Install from a link/i })).toBeTruthy();
    expect(screen.getAllByRole("link", { name: /Build a plugin/i }).length).toBeGreaterThanOrEqual(
      2,
    );
  });

  it("opens the plugin drawer and covers Overview, Settings (with validation & Test), Permissions, Health, Activity, Logs, Updates, and Remove", async () => {
    renderPlugins("/plugins");

    const plutoRow = await screen.findByText("Pluto TV");
    fireEvent.click(plutoRow);

    // 1. Overview tab
    expect(await screen.findByText("Where it appears")).toBeTruthy();
    expect((await screen.findAllByText("Live TV")).length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText("Source repository")).toBeTruthy();

    // 2. Settings tab: stored secret ("Saved"), validation error mapping, save, and Test button
    fireEvent.click(screen.getByRole("tab", { name: "Settings" }));
    expect(await screen.findByText("Region")).toBeTruthy();
    expect(
      screen.getByText("Saved. Leave as is to keep it, or type a new value to replace it."),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /Test/i }));
    expect(await screen.findByText(/OK · \d+ ms/)).toBeTruthy();

    // Trigger field validation error (/region = "invalid")
    customIntercept = (url, init) => {
      if (
        url.pathname === "/ether/api/plugins/community.plutotv.ether/settings" &&
        init?.method === "PUT"
      ) {
        return fakeServer.fetch(url.toString(), {
          ...init,
          body: JSON.stringify({ settings: { region: "invalid", token: "Saved" } }),
        });
      }
      return null;
    };

    fireEvent.click(screen.getByRole("button", { name: /Save settings/i }));
    expect(
      (await screen.findAllByText(/This value is not supported\./)).length,
    ).toBeGreaterThanOrEqual(1);

    // Restore transport and save valid settings
    customIntercept = null;
    fireEvent.click(screen.getByRole("button", { name: /Save settings/i }));
    await waitFor(() => {
      expect(screen.queryAllByText(/This value is not supported\./)).toHaveLength(0);
    });

    // 3. Permissions tab: plain-language grants and revoking a grant
    fireEvent.click(screen.getByRole("tab", { name: "Permissions" }));
    expect(await screen.findByText("Granted access")).toBeTruthy();
    expect(screen.getByText("Connects to api.pluto.tv, *.pluto.tv")).toBeTruthy();

    const revokeBtn = screen.getByRole("button", {
      name: "Revoke Connects to api.pluto.tv, *.pluto.tv",
    });
    fireEvent.click(revokeBtn);
    expect(
      await screen.findByRole("button", { name: "Restore Connects to api.pluto.tv, *.pluto.tv" }),
    ).toBeTruthy();

    // 4. Health tab: status, privacy label, and impact metrics
    fireEvent.click(screen.getByRole("tab", { name: "Health" }));
    expect(await screen.findByText("Privacy label")).toBeTruthy();
    expect(screen.getAllByText("api.pluto.tv").length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText("42 calls")).toBeTruthy();
    expect(screen.getByText("180 ms / 720 ms")).toBeTruthy();

    // 5. Logs tab
    fireEvent.click(screen.getByRole("tab", { name: "Logs" }));
    expect(await screen.findByText(/refreshed 214 channels/i)).toBeTruthy();

    // 6. Updates tab: permission diff and approve update, plus rollback
    fireEvent.click(screen.getByRole("tab", { name: "Updates" }));
    expect(await screen.findByText("Connects to ads.pluto.tv")).toBeTruthy();

    const autoUpdate = screen.getByRole("switch", { name: "Auto-update" }) as HTMLInputElement;
    const previousAutoUpdate = autoUpdate.checked;
    fireEvent.click(autoUpdate);
    await waitFor(() => expect(autoUpdate.checked).toBe(!previousAutoUpdate));

    fireEvent.click(screen.getByRole("button", { name: /Approve permissions and update/i }));
    expect(await screen.findByText("Up to date")).toBeTruthy();

    // Roll back
    fireEvent.click(screen.getByRole("button", { name: /^Roll back$/i }));
    expect(await screen.findByText("No earlier version is stored on this server.")).toBeTruthy();

    // 7. Activity tab on Ether Wrapped (which has shadow report & reversible journal entries)
    fireEvent.click(screen.getByText("Ether Wrapped"));
    fireEvent.click(await screen.findByRole("tab", { name: "Activity" }));
    expect(await screen.findByText("Shadow report")).toBeTruthy();
    expect(screen.getByText("Activity journal")).toBeTruthy();
    expect(screen.getAllByText("Your year in viewing").length).toBeGreaterThanOrEqual(1);

    fireEvent.click(screen.getByRole("button", { name: /Undo everything this plugin did/i }));
    expect(await screen.findByText("Undo everything this plugin did?")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Undo changes" }));
    expect(await screen.findByText("Undone")).toBeTruthy();

    // 8. Remove tab
    fireEvent.click(screen.getByRole("tab", { name: "Remove" }));
    expect(await screen.findByText("Undo its changes first")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Remove plugin" }));

    expect(await screen.findByText("Remove Ether Wrapped?")).toBeTruthy();
    const removeButtons = screen.getAllByRole("button", { name: "Remove plugin" });
    fireEvent.click(removeButtons[removeButtons.length - 1]!);

    await waitFor(() => {
      expect(screen.queryByText("Where it appears")).toBeNull();
    });
  });

  it("installs a plugin from a link after previewing its permission sheet and opens the installed drawer", async () => {
    renderPlugins("/plugins");
    await screen.findByText("Pluto TV");

    fireEvent.click(screen.getByRole("button", { name: /Install from a link/i }));
    const urlInput = await screen.findByLabelText("Plugin address");
    fireEvent.change(urlInput, {
      target: { value: "https://plugins.example.test/iptv/manifest.json" },
    });

    fireEvent.click(screen.getByRole("button", { name: /Review permissions/i }));

    // PermissionSheet appears with Unverified warning and plain-language permissions
    const sheet = await screen.findByTestId("permission-sheet");
    expect(within(sheet).getByText("Not reviewed by Ether")).toBeTruthy();
    expect(within(sheet).getByText("Connects to plugins.example.test")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /^Install$/i }));

    // Installing closes InstallDrawer, adds Preview plugin to the list, and opens its drawer
    expect(await screen.findByText("Where it appears")).toBeTruthy();
    expect(screen.getAllByText("Preview plugin").length).toBeGreaterThanOrEqual(1);
  });

  it("browses, filters, and installs from the Marketplace tab, and shows 'The catalogue is not available yet' when empty", async () => {
    renderPlugins("/plugins/marketplace");

    expect(await screen.findByText("SubDL Subtitles")).toBeTruthy();
    expect(screen.getByText("Local folders")).toBeTruthy();

    // Search filter
    const searchInput = screen.getByLabelText("Search catalogue");
    fireEvent.change(searchInput, { target: { value: "SubDL" } });
    expect(screen.getByText("SubDL Subtitles")).toBeTruthy();
    expect(screen.queryByText("Local folders")).toBeNull();

    // Open SubDL Subtitles card drawer and install
    fireEvent.click(screen.getByText("SubDL Subtitles"));
    const sheet = await screen.findByTestId("permission-sheet");
    expect(within(sheet).getByText("Connects to api.subdl.com")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /^Install$/i }));

    // Navigates to Installed tab and opens the new plugin's drawer
    expect(await screen.findByText("Where it appears")).toBeTruthy();
  });

  it("pairs and revokes a remote sidecar", async () => {
    let revoked = false;
    customIntercept = async (url, init) => {
      const base = "/ether/api/plugins/community.plutotv.ether";
      if (url.pathname === base && (!init?.method || init.method === "GET")) {
        const response = await fakeServer.fetch(url.toString(), init);
        const detail = await response.json();
        detail.manifest.ether.runtime = { kind: "remote" };
        return Response.json(detail);
      }
      if (url.pathname === `${base}/pairings` && (!init?.method || init.method === "GET")) {
        return Response.json({
          pairings: revoked
            ? []
            : [{ id: "sidecar-1", created: 1700000000000, lastUsed: 0, expires: 1900000000000 }],
        });
      }
      if (url.pathname === `${base}/pairings/sidecar-1` && init?.method === "DELETE") {
        revoked = true;
        return Response.json({ status: "revoked" });
      }
      return null;
    };
    renderPlugins();
    fireEvent.click(await screen.findByText("Pluto TV"));
    fireEvent.click(await screen.findByRole("button", { name: "Connect a sidecar" }));
    expect(await screen.findByText(/PAIR-1234/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Revoke" }));
    expect(await screen.findByText("No sidecars connected.")).toBeTruthy();
  });

  it("shows 'The catalogue is not available yet' when GET /api/plugins/catalogue is empty or 404", async () => {
    customIntercept = (url) => {
      if (url.pathname.startsWith("/ether/api/plugins/catalogue")) {
        return new Response(JSON.stringify({ entries: [] }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      return null;
    };

    renderPlugins("/plugins/marketplace");

    expect(await screen.findByText("The catalogue is not available yet")).toBeTruthy();
  });
});
