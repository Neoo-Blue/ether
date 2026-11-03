import { Button } from "@braedonsaunders/appkit-ui";
import { Plus, Puzzle } from "lucide-react";
import { useState } from "react";
import { AddAddonDrawer } from "../../components/addons/AddAddonDrawer";
import { addonsApi, type AddonCatalog } from "../../components/addons/addons";
import { Drawer } from "../../components/Drawer";
import { ErrorPanel, LoadingRows } from "../../components/feedback";
import { t } from "../../i18n";
import { textOf, useRegistry } from "../../lib/extend";
import { useAsync } from "../../lib/hooks";
import type { CollectionSourceMeta } from "../../lib/rules";

/** One catalog an installed add-on offers as a collection source. */
export interface AddonCatalogChoice {
  /** The source kind: plugin:<add-on id>/<catalog key>. */
  kind: string;
  addon: string;
  label: string;
  /** The catalog's media type, when known. */
  mediaType?: "movie" | "show";
}

function mediaOf(type: string | undefined): "movie" | "show" | undefined {
  if (type === "movie") return "movie";
  if (type === "series") return "show";
  return undefined;
}

const listable = (c: AddonCatalog) => c.type === "movie" || c.type === "series";

/**
 * Pick a catalog of an installed Stremio add-on, or add an add-on first.
 * Catalogs come from the add-ons' collection source descriptors; an add-on
 * added here shows its catalogs straight away, before the registry reloads.
 */
export function AddonCatalogDrawer({
  open,
  onClose,
  onPick,
}: {
  open: boolean;
  onClose: () => void;
  onPick: (choice: AddonCatalogChoice) => void;
}) {
  const registry = useRegistry<CollectionSourceMeta & { catalog?: string }>("collectionSource");
  const [adding, setAdding] = useState(false);
  const [added, setAdded] = useState<AddonCatalogChoice[]>([]);
  const addons = useAsync(() => (open ? addonsApi.list() : Promise.resolve([])), [open]);

  const installed = new Set((addons.data ?? []).map((a) => a.id));
  const choices: AddonCatalogChoice[] = [...added];
  for (const d of registry.descriptors) {
    if (!installed.has(d.owner) || choices.some((c) => c.kind === d.id)) continue;
    choices.push({
      kind: d.id,
      addon: d.group || d.owner,
      label: textOf(d.label),
      mediaType: mediaOf(d.meta?.catalog?.split("/")[0]),
    });
  }
  const groups = new Map<string, AddonCatalogChoice[]>();
  for (const c of choices) groups.set(c.addon, [...(groups.get(c.addon) ?? []), c]);

  const pick = (c: AddonCatalogChoice) => {
    onPick(c);
    onClose();
  };

  return (
    <>
      <Drawer
        open={open && !adding}
        onClose={onClose}
        title={t("Choose a Stremio add-on catalog")}
        size="md"
        footer={
          <div className="flex w-full justify-end">
            <Button variant="outline" onClick={() => setAdding(true)}>
              <Plus size={14} aria-hidden /> {t("Add a Stremio add-on")}
            </Button>
          </div>
        }
      >
        <div className="space-y-4">
          <p className="text-sm text-fg-muted">
            {t(
              "Titles from the catalog are matched to your catalogue and refreshed in the background.",
            )}
          </p>
          {addons.error ? (
            <ErrorPanel error={addons.error} onRetry={addons.reload} />
          ) : addons.loading || registry.loading ? (
            <LoadingRows rows={2} />
          ) : groups.size === 0 ? (
            <p className="rounded-lg border border-dashed border-border px-3 py-2.5 text-sm text-fg-muted">
              {t("No installed add-on offers a movie or series catalog yet.")}
            </p>
          ) : (
            [...groups].map(([addon, list]) => (
              <section key={addon} className="space-y-1.5" aria-label={addon}>
                <h3 className="flex items-center gap-1.5 text-sm font-semibold">
                  <Puzzle size={14} aria-hidden className="text-fg-muted" />
                  {addon}
                </h3>
                <ul className="space-y-1">
                  {list.map((c) => (
                    <li key={c.kind}>
                      <button
                        type="button"
                        onClick={() => pick(c)}
                        className="w-full rounded-lg border border-border bg-surface px-3 py-2 text-left text-sm hover:bg-surface-hover"
                      >
                        {c.label}
                      </button>
                    </li>
                  ))}
                </ul>
              </section>
            ))
          )}
        </div>
      </Drawer>
      <AddAddonDrawer
        open={open && adding}
        role="list_source"
        onClose={() => setAdding(false)}
        onAdded={(addon, preview) => {
          const fresh = (preview.catalogs ?? []).filter(listable).map((c) => ({
            kind: `plugin:${addon.id}/${c.key}`,
            addon: addon.name,
            label: `${c.name || c.id} · ${c.type}`,
            mediaType: mediaOf(c.type),
          }));
          setAdded((prev) => [
            ...fresh,
            ...prev.filter((p) => !fresh.some((f) => f.kind === p.kind)),
          ]);
          void addons.reload();
        }}
      />
    </>
  );
}
