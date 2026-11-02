import { screen, waitFor } from "@testing-library/react";
import { beforeAll, describe, expect, it } from "vitest";
import { settingHref } from "../components/SettingFocus";
import { renderApp } from "./harness";

const WAIT = { timeout: 5000 };

beforeAll(() => {
  Element.prototype.scrollIntoView ??= () => {};
});

describe("⌘K setting jump", () => {
  it("opens the More options that hides a setting and highlights its row", async () => {
    const href = settingHref(
      "/catalog/settings",
      "Minimum popularity",
      "More options: What's browsable",
    );
    expect(href).toContain("#setting=");
    renderApp(href);
    const field = await screen.findByLabelText("Minimum popularity", {}, WAIT);
    await waitFor(() => expect(document.querySelector(".setting-focus")).not.toBeNull(), WAIT);
    expect(document.querySelector(".setting-focus")).toContainElement(field);
  });

  it("highlights a visible setting without opening anything", async () => {
    renderApp(settingHref("/quality/searching", "Start before the health check finishes"));
    await waitFor(() => expect(document.querySelector(".setting-focus")).not.toBeNull(), WAIT);
    expect(document.querySelector(".setting-focus")).toHaveTextContent(
      "Start before the health check finishes",
    );
    expect(screen.queryByText("Concurrent checks")).toBeNull();
  });
});
