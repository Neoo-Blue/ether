import { fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { PluginViewContext, type PluginViewContextValue } from "./context";
import { ViewSpecPage } from "./Render";
import { FixtureGallery } from "./fixtures/Gallery";
import { galleryData, gallerySpec } from "./fixtures/galleryDoc";

function fakeContext(overrides: Partial<PluginViewContextValue> = {}): PluginViewContextValue {
  return {
    plugin: "fixture.gallery",
    params: {},
    setParams: () => {},
    refresh: () => {},
    postAction: async () => null,
    open: () => {},
    ...overrides,
  };
}

function renderGallery(ctx: PluginViewContextValue = fakeContext()) {
  return render(
    <MemoryRouter>
      <PluginViewContext.Provider value={ctx}>
        <ViewSpecPage spec={gallerySpec} data={galleryData} />
      </PluginViewContext.Provider>
    </MemoryRouter>,
  );
}

describe("ViewSpec renderer", () => {
  it("renders every block, cell and widget from the gallery fixture", () => {
    renderGallery();
    // page-header + actions
    expect(screen.getByRole("heading", { name: "Gallery plugin" })).toBeTruthy();
    expect(screen.getByText("Every block and widget, rendered by Ether.")).toBeTruthy();
    expect(screen.getByText("Fixture")).toBeTruthy();
    // filter-bar
    expect(screen.getByPlaceholderText("Filter the gallery…")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Mode A" })).toBeTruthy();
    // summary-line (money cell) and stat tiles in a grid
    expect(screen.getByText("Total spent")).toBeTruthy();
    expect(screen.getByText("$1,240.00")).toBeTruthy();
    expect(screen.getByText("1,204")).toBeTruthy();
    expect(screen.getByText("Plays today")).toBeTruthy();
    // health strip widget
    expect(screen.getByText("Guide")).toBeTruthy();
    expect(screen.getByText("2 slow")).toBeTruthy();
    // tabs frame + headings + text
    expect(screen.getByRole("heading", { name: "Every cell kind" })).toBeTruthy();
    // table cells: text with prefix/suffix, money, number, date, badge,
    // link, record-link, drill, txn, widget cell, span total row
    const table = screen.getByRole("table");
    expect(within(table).getByText("First row")).toBeTruthy();
    expect(within(table).getAllByText("A-1").length).toBeGreaterThan(0);
    expect(within(table).getByText("owner@example.org")).toBeTruthy();
    expect(within(table).getAllByText("$42.00").length).toBeGreaterThan(0);
    expect(within(table).getByText("Nov 1, 2026")).toBeTruthy();
    expect(within(table).getAllByText("Ready").length).toBeGreaterThan(0);
    expect(within(table).getByText("$49.50")).toBeTruthy();
    expect(within(table).getByText("New")).toBeTruthy();
    // text fallback for the row without a name
    expect(within(table).getByText("Untitled")).toBeTruthy();
    // link cell points at a console path; unsafe schemes never appear
    const links = within(table).getAllByRole("link");
    expect(links.some((a) => a.getAttribute("href") === "/catalog")).toBe(true);
    // pagination
    expect(screen.getByText("Page 2 of 5")).toBeTruthy();
    // paper + panel + shelf + grid + title card
    expect(screen.getByText("Paper statement")).toBeTruthy();
    expect(screen.getByText("Poster shelf panel")).toBeTruthy();
    expect(screen.getAllByText("The Example").length).toBeGreaterThan(1);
    expect(screen.getAllByText("Example: The Series").length).toBeGreaterThan(1);
    // live chart legend
    expect(screen.getByText("Plays")).toBeTruthy();
    expect(screen.getByText("Errors")).toBeTruthy();
    // repeat + channel rows
    expect(screen.getAllByText("Example One").length).toBeGreaterThan(0);
    expect(screen.getByText("Morning Example · News")).toBeTruthy();
    // section frame + settings form + title match
    expect(screen.getByText("Plugin settings")).toBeTruthy();
    expect(screen.getByText("Region")).toBeTruthy();
    expect(screen.getByText("Match the unmatched folder")).toBeTruthy();
    // when flags
    expect(screen.getByText("Always shown")).toBeTruthy();
    expect(screen.queryByText("ShouldNotAppear")).toBeNull();
  });

  it("wires pagination, sorting and search to view parameters", () => {
    const setParams = vi.fn();
    renderGallery(fakeContext({ setParams }));
    fireEvent.click(screen.getByRole("button", { name: "Next page" }));
    expect(setParams).toHaveBeenCalledWith({ page: "3" });
    fireEvent.click(screen.getByRole("button", { name: "Sort by Name" }));
    expect(setParams).toHaveBeenCalledWith(expect.objectContaining({ sort: "name", dir: "desc" }));
  });

  it("posts actions from action buttons and the settings form", async () => {
    const postAction = vi.fn(async () => null);
    renderGallery(fakeContext({ postAction }));
    fireEvent.click(screen.getByRole("button", { name: "Ping the plugin" }));
    expect(postAction).toHaveBeenCalledWith("ping", {});
    fireEvent.click(screen.getByRole("button", { name: "Save settings" }));
    expect(postAction).toHaveBeenCalledWith(
      "save-settings",
      expect.objectContaining({
        values: expect.objectContaining({ region: "us" }),
      }),
    );
  });

  it("opens the drawer frame in the console Drawer", () => {
    renderGallery();
    fireEvent.click(screen.getByRole("button", { name: "Open the drawer" }));
    expect(screen.getByText("Drawer body from the plugin loader.")).toBeTruthy();
    expect(screen.getByText("Inside")).toBeTruthy();
  });

  it("caps tables at 100 rows per page", () => {
    const rows = Array.from({ length: 105 }, (_, i) => ({
      id: `r${i}`,
      name: `Row ${i}`,
    }));
    render(
      <MemoryRouter>
        <PluginViewContext.Provider value={fakeContext()}>
          <ViewSpecPage
            spec={{
              specVersion: 1,
              layout: "list",
              header: [],
              body: [
                {
                  kind: "table",
                  variant: "app",
                  rows: { $: "rows" },
                  rowKey: { $: "id" },
                  columns: [{ header: "Name", cell: { kind: "text", field: { $: "name" } } }],
                },
              ],
            }}
            data={{ rows }}
          />
        </PluginViewContext.Provider>
      </MemoryRouter>,
    );
    expect(screen.getByText("Row 99")).toBeTruthy();
    expect(screen.queryByText("Row 100")).toBeNull();
    expect(screen.getByText("Showing the first 100 of 105 rows")).toBeTruthy();
  });

  it("refuses unsafe link targets and shows widget errors in place", () => {
    render(
      <MemoryRouter>
        <PluginViewContext.Provider value={fakeContext()}>
          <ViewSpecPage
            spec={{
              specVersion: 1,
              layout: "list",
              header: [],
              body: [
                {
                  kind: "table",
                  variant: "app",
                  rows: { $: "rows" },
                  rowKey: { $: "id" },
                  columns: [
                    {
                      header: "Open",
                      cell: { kind: "link", field: { $: "name" }, href: { $: "href" } },
                    },
                  ],
                },
                { kind: "widget", widget: "ether.badge", props: {} },
              ],
            }}
            data={{ rows: [{ id: "1", name: "Click", href: "javascript:alert(1)" }] }}
          />
        </PluginViewContext.Provider>
      </MemoryRouter>,
    );
    expect(screen.queryByRole("link")).toBeNull();
    expect(screen.getByText("Click")).toBeTruthy();
    expect(screen.getByText("This part of the plugin page could not be shown.")).toBeTruthy();
  });

  it("renders the fixture gallery story with its activity log", () => {
    render(
      <MemoryRouter>
        <FixtureGallery />
      </MemoryRouter>,
    );
    expect(screen.getByRole("heading", { name: "Gallery plugin" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Ping the plugin" }));
    expect(screen.getByText(/action ping/)).toBeTruthy();
  });
});
