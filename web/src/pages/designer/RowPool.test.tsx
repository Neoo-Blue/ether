import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { RowPool } from "./RowPool";

vi.mock("../../lib/extend", async (load) => {
  const actual = await load<typeof import("../../lib/extend")>();
  const id = "plugin:fixture/picks";
  return {
    ...actual,
    useRegistry: (kind: string) => ({
      descriptors:
        kind === "rowSource"
          ? [
              {
                id,
                kind,
                owner: "fixture",
                label: { resolved: "Picks" },
                meta: { hubId: id, collectionSource: id, sections: ["home"] },
              },
            ]
          : [
              {
                id,
                kind,
                owner: "fixture",
                label: { resolved: "Picks" },
                meta: { editor: "plugin", defaults: { params: { genre: "drama" } } },
              },
            ],
      loading: false,
      error: null,
      reload: () => {},
    }),
  };
});
vi.mock("../../lib/api", async (load) => {
  const actual = await load<typeof import("../../lib/api")>();
  return { ...actual, api: { ...actual.api, rowInsights: async () => [] } };
});
function Destination() {
  const location = useLocation();
  return <output>{JSON.stringify(location.state)}</output>;
}
describe("plugin Home row sources", () => {
  it("starts an enabled collection with the plugin defaults and Home target", () => {
    render(
      <MemoryRouter initialEntries={["/"]}>
        <Routes>
          <Route
            path="/"
            element={
              <RowPool
                draft={{ global: [], viewers: [] }}
                onChange={() => {}}
                layer="global"
                section="home"
                viewers={[]}
                available={[]}
                focusRow={null}
                onFocusRow={() => {}}
              />
            }
          />
          <Route path="/collections/new" element={<Destination />} />
        </Routes>
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Create collection" }));
    const state = JSON.parse(screen.getByText(/plugin:fixture/).textContent!);
    expect(state.spec).toMatchObject({
      enabled: true,
      name: "Picks",
      targets: { homeRow: true },
      sources: [{ kind: "plugin:fixture/picks", params: { genre: "drama" } }],
    });
  });
});
