import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { createPrerollsMock } from "../mock/prerolls";
import { Prerolls } from "../pages/playback/Prerolls";

it("loads the seeded table and saves its selection", async () => {
  const mock = createPrerollsMock(true);
  vi.stubGlobal(
    "fetch",
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = new URL(String(input), "http://localhost");
      if (url.pathname.includes("/admin/prerolls")) return mock(url, init);
      return Promise.resolve(new Response(JSON.stringify({ viewers: [] }), { status: 200 }));
    }),
  );
  render(<Prerolls />);
  expect(await screen.findByRole("button", { name: "Halloween sting" })).toBeInTheDocument();
  await userEvent.selectOptions(screen.getByLabelText("When several match"), "shuffle");
  await waitFor(() => expect(screen.getByLabelText("When several match")).toHaveValue("shuffle"));
  expect(
    (await (await mock(new URL("http://localhost/ether/api/admin/prerolls"))).json()).selection,
  ).toBe("shuffle");
});
afterEach(() => {
  vi.unstubAllGlobals();
});
