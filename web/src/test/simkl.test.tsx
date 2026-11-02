import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ViewerSimkl } from "../pages/viewers/sections/ViewerSimkl";

const { fetchMock } = vi.hoisted(() => ({ fetchMock: vi.fn() }));
vi.mock("../lib/api", () => ({
  apiFetch: fetchMock,
  errorMessage: (e: unknown) => String(e),
}));

describe("Viewer Simkl account", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    let connected = false;
    fetchMock.mockImplementation(async (path: string) => {
      if (path.endsWith("/status")) return { connected };
      if (path.endsWith("/start"))
        return {
          userCode: "ABCD-EFGH",
          verificationUri: "https://simkl.com/pin",
          verificationUriComplete: "https://simkl.com/pin?user_code=ABCD-EFGH",
          expiresAt: "2099-01-01T00:00:00Z",
          interval: 5,
        };
      if (path.endsWith("/poll")) {
        connected = true;
        return { status: "authorized" };
      }
      if (path.endsWith("/sync")) return { imported: 2, skipped: 0 };
      return {};
    });
  });

  it("connects, polls and syncs only this viewer", async () => {
    render(
      <MemoryRouter>
        <ViewerSimkl viewerId={2} ready />
      </MemoryRouter>,
    );
    fireEvent.click(await screen.findByRole("button", { name: "Connect" }));
    expect(await screen.findByText("ABCD-EFGH")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open Simkl approval" })).toHaveAttribute(
      "href",
      "https://simkl.com/pin?user_code=ABCD-EFGH",
    );
    fireEvent.click(screen.getByRole("button", { name: "Check connection" }));
    fireEvent.click(await screen.findByRole("button", { name: "Sync history" }));
    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith("/simkl/2/sync", { method: "POST" }),
    );
    expect(fetchMock).toHaveBeenCalledWith("/simkl/2/start", { method: "POST" });
    expect(fetchMock).toHaveBeenCalledWith("/simkl/2/poll", { method: "POST" });
    for (const [path] of fetchMock.mock.calls) expect(String(path)).toMatch(/^\/simkl\/2\//);
    // There is no viewer picker: the section belongs to one viewer.
    expect(screen.queryByLabelText("Viewer")).toBeNull();
  });

  it("points to Integrations when Simkl is not set up", () => {
    render(
      <MemoryRouter>
        <ViewerSimkl viewerId={2} ready={false} />
      </MemoryRouter>,
    );
    expect(screen.getByRole("link", { name: "Integrations" })).toHaveAttribute(
      "href",
      "/services/simkl",
    );
    expect(screen.queryByRole("button", { name: "Connect" })).toBeNull();
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
