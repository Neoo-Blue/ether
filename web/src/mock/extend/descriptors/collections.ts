/** Built-in collection and row source descriptors the mock server serves.
 * The JSON is generated from Go (internal/collections, internal/personalize);
 * TestMockDescriptorsMatchGo fails on drift. Do not edit the JSON by hand. */
import type { Descriptor } from "../../../lib/extend";
import collections from "./collections.json";
import rows from "./rows.json";

export const descriptors: Descriptor[] = [
  ...(collections as Descriptor[]),
  ...(rows as Descriptor[]),
  {
    kind: "rowSource",
    id: "stremioCatalog",
    owner: "builtin",
    label: { source: "Stremio catalog" },
    hint: { source: "A Home row from a Stremio catalog" },
    group: "collections",
    order: 1000,
    meta: {
      hubId: "collection:*",
      rowKind: "stremioCatalog",
      sections: ["home", "movies", "shows"],
      collectionSource: "stremioCatalog",
    },
  },
];
