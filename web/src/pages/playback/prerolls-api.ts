import { apiFetch } from "../../lib/api";

export type DeviceKind = "tv" | "mobile" | "desktop" | "web";
export type PrerollRules = {
  kinds: ("movie" | "episode")[];
  firstEpisodeOnly: boolean;
  bingeGapMinutes: number;
  genres: string[];
  excludeGenres: string[];
  collections: string[];
  maxContentRating: string;
  dateFrom: string;
  dateTo: string;
  audience: { mode: "everyone" | "only" | "except"; viewers: number[] };
  network: "any" | "home" | "away";
  devices: DeviceKind[];
};
export type PrerollInput = {
  name: string;
  enabled: boolean;
  rules: PrerollRules;
  frequency: { mode: "always" | "daily" | "hours" | "title"; hours: number };
  priority: number;
  order: "before_trailers" | "after_trailers";
};
export type Preroll = PrerollInput & {
  id: string;
  video: {
    state: "processing" | "ready" | "failed";
    durationMs: number;
    width: number;
    height: number;
    thumbUrl: string;
    previewUrl: string;
    problem: "" | "unreadable" | "too_long" | "convert_failed";
    source: "upload" | "path";
  };
  plays30d: number;
  createdAt: string;
  updatedAt: string;
};
export type Selection = "priority" | "shuffle";
export type Simulation = {
  winner: { id: string; name: string } | null;
  candidates: { id: string; name: string; plays: boolean; reasons: { code: string; ok: boolean }[] }[];
};
export type Play = { viewerId: number; viewerName: string; title: string; ratingKey: string; at: string; client: string };
const root = "/admin/prerolls";
const json = (method: string, body: unknown): RequestInit => ({ method, body: JSON.stringify(body) });
const item = (id: string) => `${root}/${encodeURIComponent(id)}`;
export const prerollsApi = {
  list: () => apiFetch<{ prerolls: Preroll[]; selection: Selection }>(root),
  selection: (selection: Selection) => apiFetch<void>(`${root}/selection`, json("PUT", { selection })),
  create: (data: PrerollInput) => apiFetch<Preroll>(root, json("POST", data)),
  update: (id: string, data: PrerollInput) => apiFetch<Preroll>(item(id), json("PUT", data)),
  remove: (id: string) => apiFetch<void>(item(id), { method: "DELETE" }),
  path: (id: string, path: string) => apiFetch<Preroll>(`${item(id)}/video`, json("POST", { path })),
  plays: (id: string) => apiFetch<{ plays: Play[] }>(`${item(id)}/plays?limit=50`),
  simulate: (data: { viewerId: number; ratingKey: string; device: DeviceKind; away: boolean; resume: boolean; at?: string }) => apiFetch<Simulation>(`${root}/simulate`, json("POST", data)),
  importPms: (paths: string) => apiFetch<{ created: Preroll[] }>(`${root}/import/pms`, json("POST", { paths })),
};
