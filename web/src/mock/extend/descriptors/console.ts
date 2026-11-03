/**
 * Built-in console descriptors the mock server serves. The JSON beside this
 * file is generated from Go (internal/app/extend_console.go); a Go test
 * fails when it drifts. Regenerate with:
 * ETHER_UPDATE_FIXTURES=1 go test ./internal/app -run TestConsoleDescriptorFixtureMatchesMock
 */
import type { Descriptor } from "../../../lib/extend";
import fixture from "./console.json";

export const descriptors: Descriptor[] = fixture as Descriptor[];
