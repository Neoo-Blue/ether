import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { createFakeServer } from "../mock/server";
import { answerHeadline, layerText, WhoGetsWhat } from "../pages/releases/WhoGetsWhat";
import type { QualityAnswer } from "../lib/types";
import { calls, settleUi } from "./harness";

const WAIT = { timeout: 4000 };

async function renderChecker(
  setup?: (fake: ReturnType<typeof createFakeServer>) => Promise<unknown>,
) {
  const fake = createFakeServer();
  await setup?.(fake);
  vi.stubGlobal("fetch", vi.fn(fake.fetch));
  render(<WhoGetsWhat />);
  return fake;
}

const headline = () => screen.getByTestId("quality-headline").textContent;

describe("who gets what", () => {
  it("checks the owner on the first app at home, then away", async () => {
    const fake = await renderChecker();
    await settleUi(() => expect(headline()).toBe("4KHDRDolby Vision"));
    const layers = screen.getByRole("list", { name: "What decided it" });
    const rule = within(layers).getByText("Rule 1 picks 4K.").closest("li")!;
    expect(within(rule).getByText("Sets the limit")).toBeInTheDocument();
    expect(calls(fake, "POST", "/quality/explain").at(-1)?.body).toEqual({
      userId: 1,
      clientId: "plex-apple-tv",
      remote: false,
    });

    fireEvent.click(screen.getByRole("radio", { name: "Away" }));
    await settleUi(() => expect(headline()).toBe("4KHDRDolby VisionUp to 8 Mbps"));
    expect(screen.getByText("Rule 1 picks 4K, ahead of the Away profile.")).toBeInTheDocument();
    const cap = screen.getByText("Away from home is limited to 8 Mbps.").closest("li")!;
    expect(within(cap).getByText("Sets the limit")).toBeInTheDocument();
  });

  it("shows the device ceiling and the viewer's own limits", async () => {
    const fake = await renderChecker((f) =>
      f.fetch("/ether/api/safety/policies/2", {
        method: "PUT",
        body: JSON.stringify({ maxBitrateKbps: 4000, remoteDisabled: true }),
      }),
    );
    await settleUi(() => expect(headline()).toBeTruthy());

    fireEvent.click(screen.getByRole("button", { name: "Viewer" }));
    fireEvent.click(await screen.findByRole("option", { name: "Sam Rivera" }, WAIT));
    fireEvent.click(screen.getByRole("button", { name: "App" }));
    fireEvent.click(await screen.findByRole("option", { name: "Plex for Android" }, WAIT));
    await settleUi(() => expect(headline()).toBe("1080pSDRUp to 4 Mbps"));
    expect(screen.getByText("No rule matches, so 1080p applies.")).toBeInTheDocument();
    expect(screen.getByText("Sam Rivera is limited to 4 Mbps.")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("radio", { name: "Away" }));
    await settleUi(() => expect(headline()).toBe("Can't play away from home"));
    expect(screen.getByText("Sam Rivera can't stream away from home.")).toBeInTheDocument();
    expect(calls(fake, "POST", "/quality/explain").at(-1)?.body).toEqual({
      userId: 2,
      clientId: "plex-android",
      remote: true,
    });
  });

  it("words every layer the server can send", () => {
    const answer: QualityAnswer = {
      profile: "At home",
      maxHeight: 720,
      hdr: true,
      dolbyVision: false,
      bitrateKbps: 0,
      blocked: false,
      layers: [
        {
          kind: "profile",
          reason: "",
          profile: "At home",
          source: "rule",
          rule: 2,
          beatsRemote: true,
          maxHeight: 2160,
          decisive: false,
        },
        {
          kind: "device",
          reason: "",
          maxHeight: 2160,
          hdr: true,
          dolbyVision: false,
          edited: true,
          decisive: false,
        },
        { kind: "rule-height", reason: "", rule: 2, maxHeight: 720, decisive: true },
        { kind: "remote-cap", reason: "", decisive: false },
      ],
    };
    expect(answerHeadline(answer)).toBe("720p · HDR on");
    expect(answer.layers.map((l) => layerText(l, "Sam"))).toEqual([
      "Rule 2 picks At home, ahead of the Away profile.",
      "This app plays up to 4K. It plays HDR but not Dolby Vision.",
      "Rule 2 caps resolution at 720p.",
      "No bitrate limit away from home.",
    ]);
    expect(
      layerText(
        { kind: "rule-height", reason: "", rule: 1, ignored: true, decisive: false },
        "Sam",
      ),
    ).toBe("Rule 1's maximum resolution is ignored because this app's resolution is fixed.");
    expect(
      layerText(
        { kind: "android-lift", reason: "", rule: 3, maxHeight: 2160, decisive: false },
        "Sam",
      ),
    ).toBe("Rule 3 lifts Android's usual 1080p limit to 4K.");
    expect(
      answerHeadline({ ...answer, maxHeight: 0, hdr: false, bitrateKbps: 12500, layers: [] }),
    ).toBe("Any resolution · HDR off · Up to 12.5 Mbps");
  });
});
