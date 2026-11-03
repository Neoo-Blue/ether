import { Badge, Switch, SettingsSection, Tooltip } from "@braedonsaunders/appkit-ui";
import { useEffect, useState } from "react";
import { SettingsRow } from "../../components/SettingsRow";
import { t } from "../../i18n";

type Props = {
  load: () => Promise<{ enabled: boolean }>;
  save?: (enabled: boolean) => Promise<unknown>;
};

/** A viewer's opt-in; loading and failed writes never change the saved value. */
export function FavoriteEpisodeAlerts({ load, save }: Props) {
  const [enabled, setEnabled] = useState<boolean>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(false);

  useEffect(() => {
    let active = true;
    setEnabled(undefined);
    setError(false);
    void load()
      .then((value) => {
        if (active) setEnabled(value.enabled);
      })
      .catch(() => {
        if (active) setError(true);
      });
    return () => {
      active = false;
    };
  }, [load]);

  async function change() {
    if (enabled === undefined || busy || !save) return;
    setBusy(true);
    setError(false);
    try {
      await save(!enabled);
      setEnabled(!enabled);
    } catch {
      setError(true);
    } finally {
      setBusy(false);
    }
  }

  return (
    <SettingsSection title={t("Alerts")}>
      <SettingsRow
        title={
          <span className="inline-flex items-center gap-2">
            {t("New episodes from favourite shows")}
            {!save && (
              <Tooltip content={t("The viewer can change this setting in their profile.")}>
                <button
                  type="button"
                  aria-label={t("About episode alerts")}
                  className="text-fg-muted"
                >
                  ?
                </button>
              </Tooltip>
            )}
          </span>
        }
        control={
          save ? (
            <label className="flex items-center gap-2 text-sm">
              <Switch
                aria-label={t("New episodes from favourite shows")}
                checked={enabled ?? false}
                disabled={enabled === undefined || busy}
                onChange={() => void change()}
              />
            </label>
          ) : (
            // Read-only for the owner: this is the viewer's own opt-in, so show
            // it as status rather than a switch that can never move.
            <p className="flex items-center gap-2 text-sm">
              {enabled !== undefined ? (
                <Badge variant={enabled ? "success" : "default"}>
                  {enabled ? t("On") : t("Off")}
                </Badge>
              ) : null}
            </p>
          )
        }
      />
      {error && <p role="alert">{t("The request could not be completed. Try again.")}</p>}
    </SettingsSection>
  );
}
