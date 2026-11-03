/** Built-in Live TV source descriptors the mock server serves. The JSON is
 * generated from Go (internal/livetv); TestMockDescriptorsMatchGo fails on
 * drift. Do not edit the JSON by hand. */
import type { Descriptor } from "../../../lib/extend";
import livetv from "./livetv.json";

export const descriptors: Descriptor[] = livetv as Descriptor[];
