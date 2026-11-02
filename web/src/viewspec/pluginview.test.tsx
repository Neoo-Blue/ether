import { Toaster } from "@braedonsaunders/appkit-ui";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import PluginPage from "./PluginPage";
import { PluginView } from "./PluginView";
import { galleryDoc } from "./fixtures/galleryDoc";

interface Call {
  method: string;
  url: string;
  body?: unknown;
}

/**
 * A fake owner API for one plugin: view documents, a scripted action result
 * per action name, and proposal decisions. Records every call.
 */
function fakePluginApi(opts: {
  doc?: unknown;
  actions?: Record<string, unknown>;
  proposalResult?: unknown;
}) {
  const calls: Call[] = [];
  const fetchFn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = (init?.method ?? "GET").toUpperCase();
    const body = typeof init?.body === "string" ? JSON.parse(init.body) : undefined;
    calls.push({ method, url, body });
    const json = (value: unknown, status = 200) =>
      new Response(JSON.stringify(value), {
        status,
        headers: { "content-type": "application/json" },
      });
    if (url.includes("/views/")) return json(opts.doc ?? galleryDoc);
    const action = /\/actions\/([^/?]+)/.exec(url)?.[1];
    if (method === "POST" && action) {
      return json(opts.actions?.[decodeURIComponent(action)] ?? {});
    }
    if (method === "POST" && url.includes("/proposals/")) {
      return json(opts.proposalResult ?? { refresh: true });
    }
    return json({ error: { code: "not_found", message: "not found" } }, 404);
  });
  vi.stubGlobal("fetch", fetchFn);
  return { calls, fetchFn };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("PluginView", () => {
  it("loads, validates and renders a plugin view, then handles a toast action", async () => {
    const { calls } = fakePluginApi({
      actions: { ping: { toast: { text: "Pong from the plugin", kind: "success" } } },
    });
    render(
      <MemoryRouter>
        <Toaster />
        <PluginView plugin="fixture.gallery" page="home" params={{ slot: "title-1" }} />
      </MemoryRouter>,
    );
    expect(await screen.findByRole("heading", { name: "Gallery plugin" })).toBeTruthy();
    const viewCall = calls.find((c) => c.url.includes("/views/"));
    expect(viewCall?.url).toContain("/plugins/fixture.gallery/views/home");
    expect(viewCall?.url).toContain("slot=title-1");

    fireEvent.click(screen.getByRole("button", { name: "Ping the plugin" }));
    await waitFor(() => {
      expect(calls.some((c) => c.url.includes("/actions/ping"))).toBe(true);
    });
    const actionCall = calls.find((c) => c.url.includes("/actions/ping"));
    expect(actionCall?.body).toEqual({ params: {} });
    expect(await screen.findByText("Pong from the plugin")).toBeTruthy();
  });

  it("shows a proposal card and posts the owner's decision", async () => {
    const { calls } = fakePluginApi({
      actions: {
        ping: {
          proposal: {
            id: "prop-1",
            title: "Rebuild the example collection",
            description: "The plugin wants to write to your library.",
            lines: ["Adds 12 titles", "Removes 2 titles"],
          },
        },
      },
    });
    render(
      <MemoryRouter>
        <PluginView plugin="fixture.gallery" page="home" />
      </MemoryRouter>,
    );
    await screen.findByRole("heading", { name: "Gallery plugin" });
    fireEvent.click(screen.getByRole("button", { name: "Ping the plugin" }));
    expect(await screen.findByText("Rebuild the example collection")).toBeTruthy();
    expect(screen.getByText("Needs your approval")).toBeTruthy();
    expect(screen.getByText("Adds 12 titles")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Approve" }));
    await waitFor(() => {
      expect(calls.some((c) => c.url.includes("/proposals/prop-1"))).toBe(true);
    });
    const decision = calls.find((c) => c.url.includes("/proposals/prop-1"));
    expect(decision?.body).toEqual({ decision: "approved" });
    await waitFor(() => {
      expect(screen.queryByText("Rebuild the example collection")).toBeNull();
    });
  });

  it("refuses an invalid document from the server with a retry", async () => {
    fakePluginApi({
      doc: {
        spec: {
          specVersion: 1,
          layout: "list",
          header: [],
          body: [{ kind: "text", content: "x", className: "evil" }],
        },
        data: {},
      },
    });
    render(
      <MemoryRouter>
        <PluginView plugin="bad.plugin" page="home" />
      </MemoryRouter>,
    );
    expect(await screen.findByText("This plugin sent a page Ether cannot show.")).toBeTruthy();
    expect(screen.getByRole("button", { name: /Try again/ })).toBeTruthy();
  });

  it("refresh results re-fetch the view", async () => {
    const { calls } = fakePluginApi({ actions: { ping: { refresh: true } } });
    render(
      <MemoryRouter>
        <PluginView plugin="fixture.gallery" page="home" />
      </MemoryRouter>,
    );
    await screen.findByRole("heading", { name: "Gallery plugin" });
    const before = calls.filter((c) => c.url.includes("/views/")).length;
    fireEvent.click(screen.getByRole("button", { name: "Ping the plugin" }));
    await waitFor(() => {
      expect(calls.filter((c) => c.url.includes("/views/")).length).toBe(before + 1);
    });
  });
});

describe("PluginPage", () => {
  it("renders at /p/:plugin/:page with plugin chrome and badge", async () => {
    fakePluginApi({});
    render(
      <MemoryRouter initialEntries={["/p/fixture.gallery/home"]}>
        <Routes>
          <Route path="/p/:plugin/:page/*" element={<PluginPage />} />
        </Routes>
      </MemoryRouter>,
    );
    // DomainPage title (from the loaded document) and the plugin badge.
    expect(await screen.findByText("From fixture.gallery")).toBeTruthy();
    expect(screen.getAllByRole("heading", { name: "Gallery plugin" }).length).toBeGreaterThan(0);
  });
});
