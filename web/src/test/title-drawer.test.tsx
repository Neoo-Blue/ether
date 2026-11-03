import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderAppWith } from "./routes";
import { calls } from "./harness";

function day(offset: number): string {
  const d = new Date();
  const local = new Date(Date.UTC(d.getFullYear(), d.getMonth(), d.getDate() + offset));
  return local.toISOString().slice(0, 10);
}

const show = { key: "2000000000000100", title: "Harbor Lights", type: "show", year: 2024 };
const movie = { key: "1000000000000200", title: "Glass Orchard", type: "movie", year: 2026 };

const ep = (episode: number, airDate: string, extra: object = {}) => ({
  key: `40000000010000${String(episode).padStart(2, "0")}`,
  season: 2,
  episode,
  title: `Part ${episode}`,
  airDate,
  aired: airDate !== "" && airDate <= day(0),
  ...extra,
});

const schedule = {
  seasons: [
    { season: 1, title: "Season 1", episodes: 8, aired: 8, airDate: day(-400) },
    { season: 2, title: "Season 2", episodes: 5, aired: 2, airDate: day(-10) },
  ],
  last: ep(2, day(-3)),
  next: ep(3, day(4), { overview: "A secret future synopsis" }),
  episodes: [
    ep(1, day(-10), { premiere: true }),
    ep(2, day(-3)),
    ep(3, day(4), { overview: "A secret future synopsis" }),
    ep(4, day(11)),
    ep(5, "", { finale: true }),
  ],
};

function respond(_method: string, path: string) {
  if (path === "/catalog/browse") return { items: [show, movie], total: 2 };
  if (path === `/catalog/title/${show.key}`)
    return { ...show, status: "Returning Series", seasonCount: 2, episodeCount: 13 };
  if (path === `/catalog/title/${show.key}/schedule`) return schedule;
  if (path === `/catalog/title/${show.key}/season/2`)
    return { season: 2, episodes: schedule.episodes };
  if (path === `/catalog/title/${movie.key}`)
    return { ...movie, releaseDate: day(-30), status: "Released" };
  if (path === `/catalog/title/${movie.key}/releases`)
    return {
      region: "US",
      dates: [
        { country: "FR", kind: "premiere", date: day(-60), note: "Cannes" },
        { country: "US", kind: "theatrical", date: day(-30), certification: "PG-13" },
        { country: "US", kind: "digital", date: day(20) },
      ],
    };
  return undefined;
}

describe("Catalog title drawer", () => {
  it("shows a series' next episode, seasons and upcoming agenda", async () => {
    const { fake } = renderAppWith(`/catalog?title=${show.key}`, respond);
    const drawer = await screen.findByRole("dialog", { name: "Harbor Lights" });
    const tabs = within(drawer).getByRole("tablist", { name: "Title sections" });
    expect(
      within(tabs)
        .getAllByRole("tab")
        .map((t) => t.textContent),
    ).toEqual(["Overview", "Episodes", "Calendar", "Availability"]);
    expect(await within(drawer).findByText("Next episode")).toBeInTheDocument();
    expect(within(drawer).getByText("S2 · E3 · Part 3")).toBeInTheDocument();
    // Future synopses stay hidden on the overview.
    expect(within(drawer).queryByText("A secret future synopsis")).not.toBeInTheDocument();

    fireEvent.click(within(drawer).getByText("S2 · E3 · Part 3"));
    expect(await within(drawer).findByText("A secret future synopsis")).toBeInTheDocument();
    expect(within(drawer).getByRole("tab", { name: /Season 2/ })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(within(drawer).getByText("Date not announced")).toBeInTheDocument();
    expect(calls(fake, "GET", `/catalog/title/${show.key}/season/2`)).toHaveLength(1);

    fireEvent.click(within(tabs).getByRole("tab", { name: "Calendar" }));
    expect(
      await within(drawer).findByText("2 upcoming episodes", { exact: false }),
    ).toBeInTheDocument();
    expect(within(drawer).getByText("Nothing airs today")).toBeInTheDocument();
    expect(within(drawer).getByText("Announced, date to follow")).toBeInTheDocument();
    expect(within(drawer).getByText("Recently aired")).toBeInTheDocument();
    // One schedule read serves every tab.
    expect(calls(fake, "GET", `/catalog/title/${show.key}/schedule`)).toHaveLength(1);
  });

  it("lays out a movie's release milestones and timeline", async () => {
    renderAppWith(`/catalog?title=${movie.key}`, respond);
    const drawer = await screen.findByRole("dialog", { name: "Glass Orchard" });
    const tabs = within(drawer).getByRole("tablist", { name: "Title sections" });
    expect(
      within(tabs)
        .getAllByRole("tab")
        .map((t) => t.textContent),
    ).toEqual(["Overview", "Releases", "Availability"]);
    fireEvent.click(within(tabs).getByRole("tab", { name: "Releases" }));
    expect(await within(drawer).findByText("PG-13")).toBeInTheDocument();
    // The owner's region first; the French premiere only under all countries.
    expect(within(drawer).queryByText(/Cannes/)).not.toBeInTheDocument();
    expect(within(drawer).getByText("Today")).toBeInTheDocument();
    fireEvent.click(within(drawer).getByRole("radio", { name: /All countries/ }));
    await waitFor(() => expect(within(drawer).getByText(/Cannes/)).toBeInTheDocument());
  });
});
