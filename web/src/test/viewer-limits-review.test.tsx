import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { FromPlexDefaults } from "../pages/viewers/FromPlexDefaults";
import { ViewerLiveTVAccess } from "../pages/viewers/sections/ViewerLiveTV";

afterEach(() => vi.unstubAllGlobals());
function requests(handler: (path: string, init?: RequestInit) => unknown) {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) =>
        new Response(JSON.stringify(handler(String(input), init)), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    ),
  );
}
it("hides Plex defaults when the Plex app is disabled", async () => {
  requests((path) =>
    path.includes("viewer-plex-defaults")
      ? { hasProfile: true, audioLanguage: "EN" }
      : { config: { frontends: { plex: { enabled: false } } } },
  );
  const { container } = render(<FromPlexDefaults viewerId={2} show="tracks" />);
  await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));
  expect(container).toBeEmptyDOMElement();
});
it("shows Plex track values as badges", async () => {
  requests((path) =>
    path.includes("viewer-plex-defaults")
      ? { hasProfile: true, audioLanguage: "EN", autoSelectAudio: true, subtitleMode: "always" }
      : { config: { frontends: { plex: { enabled: true } } } },
  );
  render(<FromPlexDefaults viewerId={2} show="tracks" />);
  expect(await screen.findByText("EN")).toHaveClass("rounded-full");
  expect(screen.getByText("Yes")).toHaveClass("rounded-full");
  expect(screen.getByText("Always on")).toHaveClass("rounded-full");
});
it("omits the channel list and preserves channel policy when disabling TV", async () => {
  const policy = {
    userId: 2,
    disabled: false,
    restricted: true,
    channels: ["news"],
    granted: ["private"],
    favorites: ["news"],
    maxRating: "",
  };
  let saved: unknown;
  const paths: string[] = [];
  requests((path, init) => {
    paths.push(path);
    if (init?.method === "PUT") saved = JSON.parse(String(init.body));
    return policy;
  });
  render(<ViewerLiveTVAccess viewerId={2} />);
  await userEvent.click(
    await screen.findByRole("switch", { name: "Disable Live TV for this viewer" }),
  );
  await userEvent.click(screen.getByRole("button", { name: /Save/ }));
  await waitFor(() => expect(saved).toEqual({ ...policy, disabled: true }));
  expect(screen.queryByText("Allowed channels")).not.toBeInTheDocument();
  expect(paths.some((path) => path.endsWith("/livetv/channels"))).toBe(false);
});
