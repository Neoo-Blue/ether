import { Badge } from "@braedonsaunders/appkit-ui";
import { Link } from "react-router-dom";
import { t } from "../../i18n";
import { useRegistry, type DescriptorKind } from "../../lib/extend";
import type { PluginDetail } from "./types";

/** What an imported add-on's declared resources do inside Ether, and
 * whether each one is connected to its native surface right now. */
export function AddonResources({ detail }: { detail: PluginDetail }) {
  const registry = useRegistry();
  const resources = (detail.manifest.resources ?? []).map((r) =>
    typeof r === "string" ? r : r.name,
  );
  return (
    <div className="space-y-4">
      <p className="text-sm text-fg-muted">
        {t("Declared capabilities and their current native support.")}
      </p>
      <ul className="space-y-3">
        {resources.map((resource) => {
          const support = resourceSupport(resource);
          const active =
            detail.enabled &&
            support.kind &&
            registry.descriptors.some((d) => d.owner === detail.id && d.kind === support.kind);
          return (
            <li key={resource} className="rounded-xl border border-border p-4">
              <div className="flex items-center justify-between gap-3">
                <h3 className="font-medium">{support.label}</h3>
                <Badge variant={active ? "success" : "secondary"}>
                  {!detail.enabled ? t("Off") : active ? t("Connected") : t("Not supported yet")}
                </Badge>
              </div>
              <p className="mt-2 text-sm text-fg-muted">{support.detail}</p>
              {active && support.href ? (
                <Link
                  className="mt-2 inline-block text-sm text-primary underline"
                  to={support.href}
                >
                  {t("Open in Ether")}
                </Link>
              ) : null}
            </li>
          );
        })}
      </ul>
      {detail.manifest.catalogs?.length ? (
        <ul className="divide-y divide-border rounded-xl border border-border px-4">
          {detail.manifest.catalogs.map((c) => (
            <li key={`${c.type}:${c.id}`} className="flex justify-between gap-3 py-3 text-sm">
              <span>{c.name ?? c.id}</span>
              <span className="text-fg-muted">{c.type}</span>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

function resourceSupport(resource: string): {
  label: string;
  kind?: DescriptorKind;
  detail: string;
  href?: string;
} {
  switch (resource) {
    case "catalog":
      return {
        label: t("Catalogues"),
        kind: "collectionSource",
        detail: t(
          "Available to collections and Home rows. Titles need a supported catalogue identity; shared visibility rules still apply.",
        ),
        href: "/collections",
      };
    case "meta":
      return {
        label: t("Metadata"),
        kind: "metadataProvider",
        detail: t(
          "Movie and show details are stored when a collection uses this add-on. IMDb and TMDB identities are supported; episode mapping is not yet.",
        ),
        href: "/collections",
      };
    case "subtitles":
      return {
        label: t("Subtitles"),
        kind: "subtitleProvider",
        detail: t(
          "Uses the shared subtitle broker for search and explicit download. Provider checks validate timed text; they do not verify playback rendering.",
        ),
        href: "/playback/subtitles",
      };
    case "stream":
      return {
        label: t("Sources"),
        kind: "sourceKind",
        detail: t(
          "Mapped into native source search. Playback depends on the returned stream format, upstream access and configured transports.",
        ),
        href: "/sources/indexers",
      };
    case "addon_catalog":
      return {
        label: t("Plugin directories"),
        kind: "pluginDirectory",
        detail: t(
          "Available through Browse plugins. Each entry is fetched and reviewed before installation.",
        ),
      };
    default:
      return {
        label: resource,
        detail: t("This declared resource does not have a native adapter yet."),
      };
  }
}
