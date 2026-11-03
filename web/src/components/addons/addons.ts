import { localized, t } from "../../i18n";
import { apiFetch } from "../../lib/api";

/** A role a Stremio add-on can play in Ether (a plugin capability). */
export type AddonRole =
  | "stream_source"
  | "list_source"
  | "livetv_channels"
  | "subtitles"
  | "metadata"
  | "plugin_directory";

export interface AddonRoleState {
  role: AddonRole | string;
  on: boolean;
}

export interface AddonCatalog {
  /** The add-on list source key ("catalog-…"), as in plugin:<id>/<key>. */
  key: string;
  type: string;
  id: string;
  name: string;
}

/** POST /plugins/addons/preview */
export interface AddonPreview {
  id: string;
  name: string;
  description?: string;
  logo?: string;
  version?: string;
  host: string;
  roles: AddonRoleState[] | null;
  catalogs?: AddonCatalog[] | null;
  alreadyInstalled?: boolean;
  configureFirst?: boolean;
}

/** An installed add-on (plugin summary). */
export interface AddonSummary {
  id: string;
  name: string;
  version: string;
  enabled: boolean;
  icon?: string;
  summary?: string;
  host?: string;
  health?: string;
  roles?: AddonRoleState[] | null;
  stremioOnly?: boolean;
}

export const ADDON_ROLE_LABEL = localized<Record<AddonRole, string>>(() => ({
  stream_source: t("Streams to play"),
  list_source: t("Catalogs for collections and Home rows"),
  livetv_channels: t("Live TV channels"),
  subtitles: t("Subtitles"),
  metadata: t("Title details"),
  plugin_directory: t("Add-on directory"),
}));

export const addonsApi = {
  /** Installed Stremio add-ons (plugins without an Ether block). */
  list: async () =>
    ((await apiFetch<{ plugins: AddonSummary[] | null }>("/plugins")).plugins ?? []).filter(
      (p) => p.stremioOnly,
    ),
  setRoles: (id: string, off: string[]) =>
    apiFetch<{ roles: AddonRoleState[] }>(`/plugins/${encodeURIComponent(id)}/roles`, {
      method: "PUT",
      body: JSON.stringify({ off }),
    }),
};
