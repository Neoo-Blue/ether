import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderApp } from "./harness";

describe("Stremio built-ins", () => {
  it("sends subtitle add-ons through Plugins, the one install path", async () => {
    renderApp("/playback/subtitles");
    const link = await screen.findByRole("link", { name: "Add a subtitle provider in Plugins" });
    expect(link).toHaveAttribute("href", expect.stringContaining("/plugins"));
  });

  it("starts a Stremio Home row in the collection editor", async () => {
    renderApp("/collections/new?source=stremioCatalog");
    expect(await screen.findByText("Stremio catalog")).toBeInTheDocument();
    expect(await screen.findByLabelText("Add-on URL")).toBeInTheDocument();
  });

  it("adds a Live TV add-on through the shared add-on drawer", async () => {
    renderApp("/livetv");
    fireEvent.click(await screen.findByRole("button", { name: "Add" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /Stremio add-on/ }));
    expect(await screen.findByText("Install link")).toBeInTheDocument();
    expect(screen.getByPlaceholderText("https://… /manifest.json")).toBeInTheDocument();
    expect(screen.queryByLabelText("Add-on URL")).not.toBeInTheDocument();
  });
});
