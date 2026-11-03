/** Built-in notify descriptors the mock server serves. The JSON is
 * generated from Go (internal/notify); TestMockDescriptorsMatchGo fails on
 * drift. Do not edit the JSON by hand. */
import type { Descriptor } from "../../../lib/extend";
import notify from "./notify.json";

export const descriptors: Descriptor[] = notify as Descriptor[];
