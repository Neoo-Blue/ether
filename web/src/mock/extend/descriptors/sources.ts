/** Built-in sources descriptors the mock server serves (mirror of internal/app/extend_sources.go). */
import type { Descriptor } from "../../../lib/extend";

export const descriptors: Descriptor[] = [
  {
    kind: "sourceKind",
    id: "debrid",
    owner: "builtin",
    label: { source: "Debrid service" },
    hint: { source: "Streams cached torrents over HTTPS from an owner debrid account." },
    order: 10,
    meta: { class: "debrid" },
  },
  {
    kind: "sourceKind",
    id: "usenet",
    owner: "builtin",
    label: { source: "Usenet provider" },
    hint: { source: "Fetches articles directly from primary and backup NNTP providers." },
    order: 20,
    meta: { class: "usenet" },
  },
  {
    kind: "sourceKind",
    id: "indexer",
    owner: "builtin",
    label: { source: "Release indexer" },
    hint: { source: "Searches Newznab, Torznab, built-in sites and Stremio add-ons for releases." },
    order: 30,
    meta: { class: "indexer" },
  },
  {
    kind: "sourceKind",
    id: "direct",
    owner: "builtin",
    label: { source: "Direct stream source" },
    hint: { source: "Plays direct HTTP streams or granted local folders." },
    order: 40,
    meta: { class: "direct" },
  },
];
