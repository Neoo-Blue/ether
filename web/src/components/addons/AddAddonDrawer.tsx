import { Badge, Button, Input, Switch, toast } from "@braedonsaunders/appkit-ui";
import { AlertTriangle, ArrowLeft, Link2, Puzzle } from "lucide-react";
import { useEffect, useState } from "react";
import { Artwork } from "../Artwork";
import { Drawer } from "../Drawer";
import { MutationError } from "../feedback";
import { Field } from "../fields";
import { t } from "../../i18n";
import { apiFetch } from "../../lib/api";
import { invalidateRegistry } from "../../lib/extend";
import { ADDON_ROLE_LABEL, type AddonPreview, type AddonRole, type AddonSummary } from "./addons";

/**
 * The one way to add a Stremio add-on, from any page: paste its install
 * link, see what it can do, and choose which of those roles to switch on.
 * The page's own role starts on; a link already installed is reused and
 * only gains the roles switched on here.
 */
export function AddAddonDrawer({
  open,
  role,
  title,
  onClose,
  onAdded,
}: {
  open: boolean;
  /** The role the page is adding the add-on for; it starts switched on. */
  role?: AddonRole;
  title?: string;
  onClose: () => void;
  onAdded: (addon: AddonSummary, preview: AddonPreview) => void;
}) {
  const [url, setUrl] = useState("");
  const [preview, setPreview] = useState<AddonPreview | null>(null);
  const [on, setOn] = useState<Record<string, boolean>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);

  useEffect(() => {
    if (!open) {
      setUrl("");
      setPreview(null);
      setOn({});
      setError(null);
    }
  }, [open]);

  const read = async () => {
    setBusy(true);
    setError(null);
    try {
      const p = await apiFetch<AddonPreview>("/plugins/addons/preview", {
        method: "POST",
        body: JSON.stringify({ url: url.trim() }),
      });
      setPreview(p);
      const next: Record<string, boolean> = {};
      for (const r of p.roles ?? []) {
        // A new add-on starts with the page's role (or everything, from
        // Plugins); an installed one shows what it already does plus it.
        next[r.role] = p.alreadyInstalled ? r.on || r.role === role : role ? r.role === role : true;
      }
      setOn(next);
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  };

  const add = async () => {
    if (!preview) return;
    setBusy(true);
    setError(null);
    try {
      const roles = Object.entries(on)
        .filter(([, v]) => v)
        .map(([k]) => k);
      const added = await apiFetch<AddonSummary>("/plugins/addons", {
        method: "POST",
        body: JSON.stringify({ url: url.trim(), roles }),
      });
      if (preview.alreadyInstalled) {
        // Roles switched off here are switched off on the add-on too.
        const off = (preview.roles ?? []).map((r) => r.role).filter((r) => !on[r]);
        await apiFetch(`/plugins/${encodeURIComponent(added.id)}/roles`, {
          method: "PUT",
          body: JSON.stringify({ off }),
        });
      }
      invalidateRegistry();
      toast.success(
        preview.alreadyInstalled
          ? t("{name} updated", { name: added.name })
          : t("{name} added", { name: added.name }),
      );
      onAdded(added, preview);
      onClose();
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  };

  const roleMissing = !!role && !!preview && !(preview.roles ?? []).some((r) => r.role === role);
  const nothingOn = !!preview && !Object.values(on).some(Boolean);

  return (
    <Drawer
      open={open}
      onClose={onClose}
      title={title ?? t("Add a Stremio add-on")}
      size="md"
      footer={
        preview ? (
          <div className="flex w-full items-center justify-between gap-3">
            <Button variant="ghost" onClick={() => setPreview(null)} disabled={busy}>
              <ArrowLeft size={14} aria-hidden />
              {t("Back")}
            </Button>
            <Button
              onClick={() => void add()}
              disabled={busy || preview.configureFirst || nothingOn || roleMissing}
            >
              {preview.alreadyInstalled ? t("Save") : t("Add add-on")}
            </Button>
          </div>
        ) : (
          <div className="flex w-full justify-end">
            <Button onClick={() => void read()} disabled={busy || !url.trim()}>
              {t("Continue")}
            </Button>
          </div>
        )
      }
    >
      {!preview ? (
        <div className="space-y-4">
          <p className="text-sm text-fg-muted">
            {t(
              "Paste the install link from the add-on's configure page. Ether keeps it private: it may contain your debrid key.",
            )}
          </p>
          <Field label={t("Install link")}>
            <Input
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder="https://… /manifest.json"
              autoFocus
              onKeyDown={(e) => {
                if (e.key === "Enter" && url.trim()) void read();
              }}
            />
          </Field>
          <p className="flex items-start gap-2 text-xs text-fg-subtle">
            <Link2 size={13} className="mt-0.5 shrink-0" aria-hidden />
            {t(
              "stremio:// links work too. One add-on can give streams, catalogs, Live TV channels and subtitles; you choose which.",
            )}
          </p>
          {error ? <MutationError error={error} /> : null}
        </div>
      ) : (
        <div className="space-y-5">
          <div className="flex items-center gap-3">
            <Artwork
              src={preview.logo}
              alt=""
              icon={<Puzzle size={18} />}
              className="size-12 shrink-0 rounded-lg"
            />
            <div className="min-w-0">
              <p className="truncate font-semibold">{preview.name}</p>
              <p className="truncate text-xs text-fg-muted">
                {preview.host}
                {preview.version ? ` · ${preview.version}` : ""}
              </p>
            </div>
            {preview.alreadyInstalled ? (
              <Badge variant="secondary" className="ml-auto">
                {t("Already added")}
              </Badge>
            ) : null}
          </div>
          {preview.description ? (
            <p className="text-sm text-fg-muted">{preview.description}</p>
          ) : null}
          {preview.configureFirst ? (
            <p className="flex items-start gap-2 rounded-lg bg-warning-subtle p-3 text-sm">
              <AlertTriangle size={15} className="mt-0.5 shrink-0 text-warning" aria-hidden />
              {t(
                "This add-on needs configuring first. Open its configure page, set it up, and paste the link it gives you.",
              )}
            </p>
          ) : (
            <section className="space-y-1">
              <h3 className="text-sm font-semibold">{t("What it does in Ether")}</h3>
              {(preview.roles ?? []).length === 0 ? (
                <p className="text-sm text-fg-muted">
                  {t("This add-on offers nothing Ether can use yet.")}
                </p>
              ) : (
                (preview.roles ?? []).map((r) => (
                  <label
                    key={r.role}
                    className="flex items-center justify-between gap-3 rounded-lg px-1 py-2"
                  >
                    <span className="text-sm">
                      {ADDON_ROLE_LABEL[r.role as AddonRole] ?? r.role}
                    </span>
                    <Switch
                      checked={!!on[r.role]}
                      onChange={(e) => setOn({ ...on, [r.role]: e.currentTarget.checked })}
                      aria-label={ADDON_ROLE_LABEL[r.role as AddonRole] ?? r.role}
                    />
                  </label>
                ))
              )}
              {roleMissing ? (
                <p className="text-sm text-danger">
                  {t("This add-on does not offer {role}.", {
                    role: ADDON_ROLE_LABEL[role!].toLowerCase(),
                  })}
                </p>
              ) : null}
            </section>
          )}
          {error ? <MutationError error={error} /> : null}
        </div>
      )}
    </Drawer>
  );
}
