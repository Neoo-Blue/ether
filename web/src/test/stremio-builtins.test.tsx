import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderApp } from "./harness";

describe("Stremio built-ins", () => {
  it("adds a subtitle add-on from the subtitle providers page", async () => {
    renderApp("/playback/subtitles");
    fireEvent.click(await screen.findByRole("button", { name: "Add a Stremio add-on" }));
    expect(await screen.findByRole("dialog", { name: "Add a Stremio add-on" })).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Add a subtitle provider in Plugins" }),
    ).toHaveAttribute("href", expect.stringContaining("/plugins"));
    expect(screen.queryByLabelText("Add-on URL")).not.toBeInTheDocument();
  });

  it("starts a Stremio Home row by choosing an add-on catalog", async () => {
    renderApp("/collections/new?source=addonCatalog&draft=new-addon-row");
    const picker = await screen.findByRole("dialog", { name: "Choose a Stremio add-on catalog" });
    expect(
      await within(picker).findByText("No installed add-on offers a movie or series catalog yet."),
    ).toBeInTheDocument();
    fireEvent.click(within(picker).getByRole("button", { name: "Add a Stremio add-on" }));
    const add = await screen.findByRole("dialog", { name: "Add a Stremio add-on" });
    expect(within(add).getByText("Install link")).toBeInTheDocument();
  });

  it("offers the Stremio source in Live TV", async () => {
    renderApp("/livetv");
    fireEvent.click(await screen.findByRole("button", { name: "Add" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /Stremio add-on/ }));
    expect(await screen.findByLabelText("Add-on URL")).toBeInTheDocument();
  });
});
