import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AddonCatalogDrawer } from "./AddonCatalogDrawer";

vi.mock("../../components/addons/addons", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../components/addons/addons")>();
  return {
    ...actual,
    addonsApi: {
      ...actual.addonsApi,
      list: async () => [
        {
          id: "addon.abc",
          name: "Catalog add-on",
          version: "1.0.0",
          enabled: true,
          stremioOnly: true,
        },
      ],
    },
  };
});

vi.mock("../../lib/extend", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../lib/extend")>();
  return {
    ...actual,
    useRegistry: () => ({
      loading: false,
      error: null,
      reload: () => {},
      descriptors: [
        {
          kind: "collectionSource",
          id: "plugin:addon.abc/catalog-1",
          owner: "addon.abc",
          group: "Catalog add-on",
          label: { resolved: "Popular · series" },
          meta: { editor: "plugin", catalog: "series/top" },
        },
        {
          kind: "collectionSource",
          id: "plugin:community.lists/picks",
          owner: "community.lists",
          group: "Curated lists",
          label: { resolved: "Staff picks" },
          meta: { editor: "plugin", catalog: "movie/picks" },
        },
      ],
    }),
  };
});

describe("Stremio add-on catalog picker", () => {
  it("lists only installed add-ons' catalogs and picks one as a source kind", async () => {
    const onPick = vi.fn();
    const onClose = vi.fn();
    render(<AddonCatalogDrawer open onClose={onClose} onPick={onPick} />);
    const dialog = await screen.findByRole("dialog", { name: "Choose a Stremio add-on catalog" });
    const group = await within(dialog).findByRole("region", { name: "Catalog add-on" });
    expect(within(dialog).queryByText("Staff picks")).not.toBeInTheDocument();
    fireEvent.click(within(group).getByRole("button", { name: "Popular · series" }));
    expect(onPick).toHaveBeenCalledWith({
      kind: "plugin:addon.abc/catalog-1",
      addon: "Catalog add-on",
      label: "Popular · series",
      mediaType: "show",
    });
    expect(onClose).toHaveBeenCalled();
  });
});
