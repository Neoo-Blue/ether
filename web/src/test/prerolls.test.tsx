import { systemMessageKey, type MessageCatalog } from "@braedonsaunders/appkit-i18n";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { loadLocale } from "../i18n";
import de from "../i18n/locales/de.json";
import { ApiError } from "../lib/api";
import { fetchUpload, setUploadTransport } from "../pages/playback/prerolls-api";
import { calls, renderApp } from "./harness";

const WAIT = { timeout: 4000 };
const german = (source: string) => {
  const entry = (de as MessageCatalog)[systemMessageKey(source)];
  if (typeof entry !== "string") throw new Error(`no German copy for ${source}`);
  return entry;
};

beforeEach(() => setUploadTransport(fetchUpload));
afterEach(async () => {
  setUploadTransport();
  localStorage.clear();
  await loadLocale("en");
});

async function openPrerolls(seed = true) {
  const app = renderApp("/playback/prerolls", { prerolls: seed });
  if (seed) await screen.findByText("Halloween sting", {}, WAIT);
  return app;
}

describe("pre-rolls console", () => {
  it("summarises each pre-roll in the table and toggles one without opening it", async () => {
    const { fake } = await openPrerolls();
    const row = screen.getByText("Halloween sting").closest("tr")!;
    expect(within(row).getByText("Horror movies · Oct 1 – Oct 31")).toBeInTheDocument();
    expect(within(row).getByText("Everyone except The Kids")).toBeInTheDocument();
    expect(within(row).getByText("Once a day per viewer")).toBeInTheDocument();
    expect(within(row).getByText("41 plays")).toBeInTheDocument();
    const intro = screen.getByText("Cinema intro").closest("tr")!;
    expect(within(intro).getByText("Converting video…")).toBeInTheDocument();
    expect(within(intro).getByText("Everyone · At home only · On TV")).toBeInTheDocument();

    fireEvent.click(within(row).getByRole("switch", { name: "Play Halloween sting" }));
    await waitFor(() => expect(calls(fake, "PUT", "/admin/prerolls/pr_1")).toHaveLength(1));
    const body = calls(fake, "PUT", "/admin/prerolls/pr_1")[0]!.body as Record<string, unknown>;
    expect(body).toMatchObject({ name: "Halloween sting", enabled: false });
    expect(Object.keys(body).sort()).toEqual(
      ["enabled", "frequency", "name", "order", "priority", "rules"].sort(),
    );
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("edits several multi-value rules at once and keeps the draft through a language switch", async () => {
    const { fake } = await openPrerolls();
    fireEvent.click(screen.getByText("Halloween sting"));
    const dialog = await screen.findByRole("dialog", {}, WAIT);
    fireEvent.change(within(dialog).getByLabelText("Name"), {
      target: { value: "Halloween sting, extended" },
    });
    fireEvent.click(within(dialog).getByRole("radio", { name: "Both" }));
    const devices = within(dialog).getByRole("group", { name: "Devices" });
    fireEvent.click(within(devices).getByRole("button", { name: "TV" }));
    fireEvent.click(within(devices).getByRole("button", { name: "Web" }));
    fireEvent.click(within(devices).getByRole("button", { name: "TV" }));
    fireEvent.click(within(devices).getByRole("button", { name: "Desktop app" }));
    fireEvent.click(within(dialog).getByRole("radio", { name: "Every few hours" }));
    fireEvent.change(within(dialog).getByLabelText("Hours between plays"), {
      target: { value: "12" },
    });

    fireEvent.click(screen.getAllByRole("button", { name: "Display language" })[0]!);
    fireEvent.click(await screen.findByRole("menuitemradio", { name: /Deutsch/ }));
    const still = await screen.findByRole("dialog", {}, WAIT);
    await within(still).findByRole("button", { name: new RegExp(german("Delete")) }, WAIT);
    expect(within(still).getByLabelText(german("Name"))).toHaveValue("Halloween sting, extended");
    expect(within(still).getByLabelText(german("Hours between plays"))).toHaveValue(12);

    fireEvent.click(within(still).getByRole("button", { name: new RegExp(german("Save")) }));
    await waitFor(() => expect(calls(fake, "PUT", "/admin/prerolls/pr_1")).toHaveLength(1));
    expect(calls(fake, "PUT", "/admin/prerolls/pr_1")[0]!.body).toMatchObject({
      name: "Halloween sting, extended",
      frequency: { mode: "hours", hours: 12 },
      rules: {
        kinds: ["movie", "episode"],
        devices: ["web", "desktop"],
        genres: ["Horror"],
        dateFrom: "10-01",
        dateTo: "10-31",
        audience: { mode: "except", viewers: [3] },
      },
    });
  });

  it("explains which pre-roll would play and why the others don't", async () => {
    const { fake } = await openPrerolls();
    const horror = fake.catalog.find((t) => t.type === "movie" && t.genres?.includes("Horror"))!;
    const card = screen.getByRole("region", { name: "What would play?" });
    fireEvent.click(within(card).getByRole("button", { name: "Viewer" }));
    fireEvent.click(await screen.findByRole("option", { name: "Sam Rivera" }));
    fireEvent.change(within(card).getByLabelText("Title"), { target: { value: horror.title } });
    fireEvent.click(await within(card).findByRole("button", { name: new RegExp(horror.title) }, WAIT));
    fireEvent.click(within(card).getByRole("button", { name: "More options" }));
    fireEvent.change(within(card).getByLabelText("Date and time"), {
      target: { value: "2026-10-30T20:00" },
    });
    fireEvent.click(within(card).getByRole("button", { name: "See what plays" }));

    expect(await within(card).findByText("Before " + horror.title, {}, WAIT)).toBeInTheDocument();
    const list = within(card).getByRole("list", { name: "Every pre-roll" });
    const [first, ...others] = within(list).getAllByRole("listitem", { name: undefined }).filter(
      (li) => li.parentElement === list,
    );
    expect(within(first!).getByText("Halloween sting")).toBeInTheDocument();
    expect(within(first!).getByText("The date is within its dates.")).toBeInTheDocument();
    const intro = others.find((li) => within(li).queryByText("Cinema intro"))!;
    expect(within(intro).getByText("Its video isn't ready yet.")).toBeInTheDocument();
    const family = others.find((li) => within(li).queryByText("Family welcome"))!;
    expect(within(family).getByText("It doesn't play for this viewer.")).toBeInTheDocument();
    expect(calls(fake, "POST", "/admin/prerolls/simulate")[0]!.body).toMatchObject({
      viewerId: 2,
      ratingKey: horror.key,
      device: "tv",
      away: false,
      resume: false,
    });
  });

  it("starts empty, requires a name, and reports a failed upload in plain words", async () => {
    const { fake } = await openPrerolls(false);
    const empty = await screen.findByText("No pre-rolls yet", {}, WAIT);
    fireEvent.click(within(empty.parentElement!).getByRole("button", { name: "Add pre-roll" }));
    const dialog = await screen.findByRole("dialog", {}, WAIT);
    const save = () =>
      fireEvent.click(
        within(dialog)
          .getAllByRole("button", { name: "Add pre-roll" })
          .at(-1)!,
      );
    save();
    expect(await within(dialog).findByText("Give the pre-roll a name.")).toBeInTheDocument();
    const creates = () => calls(fake, "POST", "/admin/prerolls").filter((c) => c.path === "/admin/prerolls");
    expect(creates()).toHaveLength(0);

    fireEvent.change(within(dialog).getByLabelText("Name"), { target: { value: "Welcome" } });
    const video = new File([new Uint8Array(16)], "welcome.mp4", { type: "video/mp4" });
    fireEvent.change(dialog.querySelector('input[type="file"]')!, { target: { files: [video] } });
    expect(within(dialog).getByText("welcome.mp4")).toBeInTheDocument();
    setUploadTransport(() => Promise.reject(new ApiError(413, "too_large", "file too large")));
    save();
    expect(await within(dialog).findByText("That file is too large to upload.", {}, WAIT)).toBeInTheDocument();
    expect(creates()).toHaveLength(1);

    setUploadTransport(fetchUpload);
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(calls(fake, "POST", "/admin/prerolls/pr_1/video")).toHaveLength(1));
    expect(creates()).toHaveLength(1);
    expect(await screen.findByText("Welcome", { selector: "tr *" }, WAIT)).toBeInTheDocument();
  });
});
