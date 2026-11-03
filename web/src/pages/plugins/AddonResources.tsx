import { Badge, Switch, toast } from "@braedonsaunders/appkit-ui";
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { ADDON_ROLE_LABEL, addonsApi, type AddonRole } from "../../components/addons/addons";
import { t } from "../../i18n";
import { errorMessage } from "../../lib/api";
import { invalidateRegistry } from "../../lib/extend";
import type { PluginDetail } from "./types";

/**
 * What a Stremio add-on does in Ether: every role its manifest offers,
 * each switched on or off here, with a link to where it shows up.
 */
export function AddonResources({
  detail,
  onChanged,
}: {
  detail: PluginDetail;
  onChanged?: () => void;
}) {
  const [roles, setRoles] = useState(detail.roles ?? []);
  const [busy, setBusy] = useState<string | null>(null);
  useEffect(() => setRoles(detail.roles ?? []), [detail.roles]);

  const toggle = async (role: string, on: boolean) => {
    const off = roles.filter((r) => (r.role === role ? !on : !r.on)).map((r) => r.role);
    setBusy(role);
    try {
      const res = await addonsApi.setRoles(detail.id, off);
      setRoles(res.roles);
      invalidateRegistry();
      onChanged?.();
    } catch (e) {
      toast.error(t("That didn't work"), { description: errorMessage(e) });
    } finally {
      setBusy(null);
    }
  };

  return (
    <div className="space-y-4">
      <p className="text-sm text-fg-muted">
        {detail.host
          ? t("Reached at {host}. Choose what this add-on does in Ether.", { host: detail.host })
          : t("Choose what this add-on does in Ether.")}
      </p>
      {roles.length === 0 ? (
        <p className="rounded-xl border border-border p-4 text-sm text-fg-muted">
          {t("This add-on offers nothing Ether can use yet.")}
        </p>
      ) : (
        <ul className="space-y-3">
          {roles.map((r) => {
            const info = roleInfo(r.role);
            return (
              <li key={r.role} className="rounded-xl border border-border p-4">
                <div className="flex items-center justify-between gap-3">
                  <h3 className="font-medium">{ADDON_ROLE_LABEL[r.role as AddonRole] ?? r.role}</h3>
                  <div className="flex items-center gap-2">
                    {!detail.enabled ? <Badge variant="secondary">{t("Add-on off")}</Badge> : null}
                    <Switch
                      aria-label={ADDON_ROLE_LABEL[r.role as AddonRole] ?? r.role}
                      checked={r.on}
                      disabled={busy !== null}
                      onChange={(e) => void toggle(r.role, e.currentTarget.checked)}
                    />
                  </div>
                </div>
                <p className="mt-2 text-sm text-fg-muted">{info.detail}</p>
                {r.on && detail.enabled && info.href ? (
                  <Link className="mt-2 inline-block text-sm text-primary underline" to={info.href}>
                    {info.link}
                  </Link>
                ) : null}
              </li>
            );
          })}
        </ul>
      )}
      {detail.manifest.catalogs?.length ? (
        <section className="space-y-2">
          <h3 className="text-sm font-semibold">{t("Catalogs")}</h3>
          <ul className="divide-y divide-border rounded-xl border border-border px-4">
            {detail.manifest.catalogs.map((c) => (
              <li key={`${c.type}:${c.id}`} className="flex justify-between gap-3 py-3 text-sm">
                <span>{c.name ?? c.id}</span>
                <span className="text-fg-muted">{c.type}</span>
              </li>
            ))}
          </ul>
        </section>
      ) : null}
    </div>
  );
}

function roleInfo(role: string): { detail: string; href?: string; link?: string } {
  switch (role) {
    case "stream_source":
      return {
        detail: t(
          "Searched like an indexer when someone presses Play, in the order you set under Sources.",
        ),
        href: "/sources/indexers",
        link: t("Open Sources"),
      };
    case "list_source":
      return {
        detail: t(
          "Its catalogs can fill collections and Home rows. They refresh in the background.",
        ),
        href: "/collections",
        link: t("Open Collections"),
      };
    case "livetv_channels":
      return {
        detail: t("Its TV channels and guide join Live TV."),
        href: "/livetv",
        link: t("Open Live TV"),
      };
    case "subtitles":
      return {
        detail: t(
          "Searched beside your other subtitle providers. A file downloads only when a viewer picks it.",
        ),
        href: "/playback/subtitles",
        link: t("Open subtitle providers"),
      };
    case "metadata":
      return {
        detail: t(
          "Fills in details for titles its catalogs add that the catalogue doesn't know yet.",
        ),
      };
    case "plugin_directory":
      return { detail: t("Lists more add-ons you can browse and add.") };
    default:
      return { detail: t("Ether doesn't use this yet.") };
  }
}
