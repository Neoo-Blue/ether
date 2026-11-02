/** Built-in subtitles descriptors the mock server serves (mirror of internal/app/extend_subtitles.go). */
import type { Descriptor } from "../../../lib/extend";

export const descriptors: Descriptor[] = [
  {
    kind: "subtitleProvider",
    id: "opensubtitles",
    owner: "builtin",
    label: { source: "OpenSubtitles" },
    hint: { source: "Search OpenSubtitles by IMDb, TMDB and release hash." },
    icon: "Subtitles",
    order: 10,
  },
  {
    kind: "subtitleProvider",
    id: "stremio",
    owner: "builtin",
    label: { source: "Stremio subtitle add-on" },
    hint: {
      source: "Search a Stremio add-on whose manifest provides subtitles (such as SubSense).",
    },
    icon: "Puzzle",
    order: 20,
  },
];
