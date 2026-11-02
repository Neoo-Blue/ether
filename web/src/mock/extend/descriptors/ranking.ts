/**
 * Built-in ranking descriptors the mock server serves. The JSON fixture is
 * dumped from the Go registry by TestBuiltinRankingDescriptorsMatchMockFixture
 * (internal/app); drift between Go and this file fails that test.
 */
import type { Descriptor } from "../../../lib/extend";
import fixture from "./ranking.json";

export const descriptors = fixture as unknown as Descriptor[];
