/**
 * The dev fixture gallery: the gallery document rendered through the real
 * renderer with a recording view context, so the owner can review how every
 * block and widget looks without a plugin server. Mount it at any route in a
 * dev build, or open the gallery test. Actions and parameter changes are
 * logged under the page instead of posting anywhere.
 */
import { useMemo, useState } from "react";
import { t } from "../../i18n";
import { PluginViewContext, type PluginViewContextValue } from "../context";
import { ViewSpecPage } from "../Render";
import { galleryData, gallerySpec } from "./galleryDoc";

export function FixtureGallery() {
  const [log, setLog] = useState<string[]>([]);
  const [params, setParamsState] = useState<Record<string, string>>({});
  const context = useMemo<PluginViewContextValue>(
    () => ({
      plugin: "fixture.gallery",
      params,
      setParams: (patch) => {
        setLog((prev) => [...prev.slice(-7), `params ${JSON.stringify(patch)}`]);
        setParamsState((prev) => {
          const next = { ...prev };
          for (const [key, value] of Object.entries(patch)) {
            if (value === undefined || value === "") delete next[key];
            else next[key] = value;
          }
          return next;
        });
      },
      refresh: () => setLog((prev) => [...prev.slice(-7), "refresh"]),
      postAction: async (action, actionParams) => {
        setLog((prev) => [
          ...prev.slice(-7),
          `action ${action} ${JSON.stringify(actionParams ?? {})}`,
        ]);
        return null;
      },
      open: (target) => setLog((prev) => [...prev.slice(-7), `open ${JSON.stringify(target)}`]),
    }),
    [params],
  );
  return (
    <PluginViewContext.Provider value={context}>
      <div className="space-y-6">
        <ViewSpecPage spec={gallerySpec} data={galleryData} />
        <section
          aria-label={t("Fixture activity")}
          className="rounded-xl border border-border bg-surface px-5 py-4"
        >
          <h2 className="text-sm font-semibold text-fg">{t("Fixture activity")}</h2>
          {log.length ? (
            <ul className="mt-2 space-y-1 font-mono text-xs text-fg-muted">
              {log.map((line, i) => (
                <li key={i}>{line}</li>
              ))}
            </ul>
          ) : (
            <p className="mt-1 text-sm text-fg-muted">
              {t("Use a control and the fixture records what the plugin would receive.")}
            </p>
          )}
        </section>
      </div>
    </PluginViewContext.Provider>
  );
}
