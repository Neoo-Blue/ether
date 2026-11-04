import { describe, expect, it } from "vitest";
import { descriptors } from "../../mock/extend/descriptors/ranking";
import {
  descriptorLabel,
  descriptorOwnerName,
  insertStepAtDefaultPosition,
  newCondition,
  normalizeRanking,
  resolveRuleFieldMeta,
  type RankingStepDescriptor,
  type RuleFieldDescriptor,
} from "./ReleasePreferences";

const steps = descriptors.filter(
  (d) => d.kind === "rankingStep",
) as unknown as RankingStepDescriptor[];
const fields = descriptors.filter(
  (d) => d.kind === "ruleField",
) as unknown as RuleFieldDescriptor[];

const pluginStep: RankingStepDescriptor = {
  kind: "rankingStep",
  id: "plugin:example.dub/lang",
  owner: "example.dub",
  group: "Dub language",
  label: { plugin: "example.dub", key: "step.lang", resolved: "Dub language" },
  order: 99,
  meta: { editor: "none", canMove: true, defaultPosition: 1 },
};

describe("registry-driven ranking ladder", () => {
  it("serves every built-in step and field from the Go fixture", () => {
    expect(steps.map((s) => s.id)).toEqual([
      "language",
      "connection",
      "resolution",
      "releaseType",
      "hdr",
      "customScore",
      "verified",
      "videoCodec",
      "audioFormat",
      "size",
    ]);
    expect(fields.map((f) => f.id)).toEqual([
      "language",
      "subtitles",
      "audio_matches_original",
      "name",
      "group",
      "source",
      "video",
      "audio",
      "height",
      "size_gib",
      "hdr",
      "edition",
      "indexer",
      "protocol",
    ]);
    for (const id of ["connection", "verified"]) {
      const meta = steps.find((s) => s.id === id)!.meta!;
      expect(meta.required).toBe(true);
      expect(meta.canMove).toBe(false);
    }
  });

  it("builds the default ladder from descriptors, prefer rules first", () => {
    const profile = {
      ranking: [] as string[],
      releaseRules: [
        {
          id: "flux",
          name: "Prefer FLUX",
          action: "prefer" as const,
          conditions: [{ field: "group", operator: "equals" as const, value: "FLUX" }],
        },
      ],
    };
    expect(normalizeRanking(profile, steps)).toEqual(["rule:flux", ...steps.map((s) => s.id)]);
  });

  it("never auto-inserts plugin steps, but keeps ones the profile names", () => {
    const all = [...steps, pluginStep];
    expect(normalizeRanking({ ranking: [], releaseRules: [] }, all)).not.toContain(pluginStep.id);
    expect(
      normalizeRanking({ ranking: [pluginStep.id, "resolution"], releaseRules: [] }, all),
    ).toContain(pluginStep.id);
    // Unknown steps are dropped once the registry has answered.
    expect(
      normalizeRanking({ ranking: ["bogus", "resolution"], releaseRules: [] }, all),
    ).not.toContain("bogus");
    // Before the registry answers, the saved ladder is kept untouched.
    expect(normalizeRanking({ ranking: ["resolution"], releaseRules: [] }, [])).toEqual([
      "resolution",
    ]);
  });

  it("reinserts omitted built-in steps at their default positions", () => {
    const ladder = normalizeRanking(
      { ranking: ["verified", "connection"], releaseRules: [] },
      steps,
    );
    // Same placement core EffectiveRanking computes: an omitted step goes
    // ahead of the first step whose default position is larger, and the
    // saved relative order of verified/connection is kept. The custom
    // score goes directly before verified, where older ladders compared it.
    expect(ladder).toEqual([
      "language",
      "resolution",
      "releaseType",
      "hdr",
      "customScore",
      "verified",
      "connection",
      "videoCodec",
      "audioFormat",
      "size",
    ]);
  });

  it("puts an omitted custom score directly before verified", () => {
    // Plain default-position insertion would put it ahead of size here.
    const saved = ["size", "language", "connection", "resolution", "releaseType", "hdr"];
    const tail = ["verified", "videoCodec", "audioFormat"];
    expect(normalizeRanking({ ranking: [...saved, ...tail], releaseRules: [] }, steps)).toEqual([
      ...saved,
      "customScore",
      ...tail,
    ]);
  });

  it("adds a step at its default position", () => {
    const ladder = normalizeRanking({ ranking: [], releaseRules: [] }, steps);
    const next = insertStepAtDefaultPosition(ladder, [...steps, pluginStep], pluginStep.id);
    // defaultPosition 1 is connection's slot; the new step lands after it,
    // ahead of resolution.
    expect(next.indexOf(pluginStep.id)).toBe(2);
    expect(next[next.indexOf(pluginStep.id) + 1]).toBe("resolution");
    expect(next).toHaveLength(ladder.length + 1);
  });

  it("creates conditions with each field's declared operator and resolves manifest metadata", () => {
    expect(newCondition(fields, "name").operator).toBe("contains");
    expect(newCondition(fields, "group").operator).toBe("equals");
    expect(newCondition(fields, "height").operator).toBe("range");
    expect(newCondition(fields, "language").operator).toBe("equals");
    const size = fields.find((f) => f.id === "size_gib")!;
    expect(size.meta?.unit).toBe("GiB");
    expect(size.meta?.valueClass).toBe("number");

    const pluginField: RuleFieldDescriptor = {
      kind: "ruleField",
      id: "plugin:example.dub/dubbed",
      owner: "example.dub",
      group: "Dub language",
      label: { plugin: "example.dub", key: "field.dubbed", resolved: "Dubbed" },
      order: 100,
      meta: {
        valueClass: "" as unknown as "choice",
        operators: ["equals"],
        editor: {
          kind: "choice",
          options: [{ value: "ja", label: { resolved: "Japanese" } }],
        },
      },
    };
    const resolved = resolveRuleFieldMeta(pluginField.meta);
    expect(resolved?.defaultOperator).toBe("equals");
    expect(resolved?.valueClass).toBe("choice");
    expect(resolved?.choices).toEqual([{ value: "ja", label: { resolved: "Japanese" } }]);
    expect(descriptorOwnerName(pluginField)).toBe("Dub language");
    expect(descriptorLabel(pluginField)).toBe("Dubbed · From Dub language");
  });
});
