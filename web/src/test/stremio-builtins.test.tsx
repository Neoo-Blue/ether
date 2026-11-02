import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { calls, renderApp } from "./harness";

describe("Stremio built-ins", () => {
  it("adds a subtitle add-on to provider order", async () => {
    const { fake } = renderApp("/playback/subtitles");
    const add = await screen.findByRole("button", { name: "Add Stremio subtitle add-on" });
    await waitFor(() => expect(add).toBeEnabled());
    fireEvent.click(add);
    const drawer = await screen.findByRole("dialog", { name: "Stremio subtitle add-on" });
    fireEvent.change(within(drawer).getByLabelText("Name"), { target: { value: "My subtitles" } });
    fireEvent.change(within(drawer).getByLabelText("Add-on URL"), {
      target: { value: "https://addon.example/config/manifest.json" },
    });
    fireEvent.click(within(drawer).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(calls(fake, "PUT", "/config")).toHaveLength(1));
    expect(await screen.findByText("My subtitles")).toBeInTheDocument();
  });

  it("starts a Stremio Home row in the collection editor", async () => {
    renderApp("/collections/new?source=stremioCatalog");
    expect(await screen.findByText("Stremio catalog")).toBeInTheDocument();
    expect(await screen.findByLabelText("Add-on URL")).toBeInTheDocument();
  });

  it("offers the Stremio source in Live TV", async () => {
    renderApp("/livetv");
    fireEvent.click(await screen.findByRole("button", { name: "Add" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /Stremio add-on/ }));
    expect(await screen.findByLabelText("Add-on URL")).toBeInTheDocument();
  });
});
