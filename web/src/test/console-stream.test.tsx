import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import App from "../App";
import { createFakeServer } from "../mock/server";
import { useRegistry } from "../lib/extend";
import { calls, renderApp, settleUi } from "./harness";
import { pushMockStreamEvent, resetMockStream } from "../mock/extend/stream";
import { resetStreamState } from "../lib/stream";

afterEach(() => {
  resetMockStream();
  resetStreamState();
});

/** A registry consumer: none ships in the console yet (WS1/WS10 land them). */
function RegistryProbe() {
  const { descriptors } = useRegistry("command");
  return <div data-testid="registry-probe">{descriptors.length}</div>;
}

function renderAppWithProbe() {
  const fake = createFakeServer({});
  vi.stubGlobal("fetch", vi.fn(fake.fetch));
  const utils = render(
    <MemoryRouter initialEntries={["/"]}>
      <App />
      <RegistryProbe />
    </MemoryRouter>,
  );
  return { ...utils, fake };
}

describe("console live stream", () => {
  it("opens a shared stream with the session header", async () => {
    const { fake } = renderApp("/");
    await screen.findByRole("button", { name: "Server health: All systems normal" });
    const streams = calls(fake, "GET", "/stream");
    expect(streams.length).toBeGreaterThanOrEqual(1);
  });

  it("applies a pushed dashboard without polling", async () => {
    const { fake } = renderApp("/");
    await screen.findByRole("button", { name: "Server health: All systems normal" });
    const dashboardCalls = () => calls(fake, "GET", "/dashboard").length;
    const before = dashboardCalls();
    expect(before).toBeGreaterThanOrEqual(1);

    pushMockStreamEvent("dashboard", {
      sections: {},
      names: [],
      setupComplete: true,
      version: "mock",
    });
    // The pushed payload replaces state: health drops to incomplete.
    await screen.findByRole("button", { name: "Server health: Status incomplete" });
    // No refetch paid for the update; the safety poll is minutes away.
    await settleUi(() => expect(dashboardCalls()).toBe(before));
  });

  it("refetches the registry on extend.changed", async () => {
    const { fake } = renderAppWithProbe();
    await screen.findByRole("button", { name: "Server health: All systems normal" });
    await screen.findByTestId("registry-probe");
    const registryCalls = () => calls(fake, "GET", "/extend/registry").length;
    const before = registryCalls();
    expect(before).toBeGreaterThanOrEqual(1);
    pushMockStreamEvent("extend.changed", { version: 99 });
    await settleUi(() => expect(registryCalls()).toBeGreaterThan(before));
  });
});
