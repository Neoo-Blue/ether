import { Switch, SettingsSection } from "@braedonsaunders/appkit-ui";
import { useEffect, useState } from "react";
import { SettingsRow } from "../../components/SettingsRow";
import { t } from "../../i18n";

type Props = {
  load: () => Promise<{ enabled: boolean }>;
  save: (enabled: boolean) => Promise<unknown>;
};
export function FavoritesMirror({ load, save }: Props) {
  const [enabled, setEnabled] = useState<boolean>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(false);
  useEffect(() => {
    let active = true;
    void load()
      .then((v) => {
        if (active) setEnabled(v.enabled);
      })
      .catch(() => {
        if (active) setError(true);
      });
    return () => {
      active = false;
    };
  }, [load]);
  async function change() {
    if (enabled === undefined) return;
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
    <SettingsSection title={t("Favorites in Plex")}>
      <SettingsRow
        title={t("Show my favourites as a playlist in Plex.")}
        description={t("Your favourites are kept when this is off.")}
        control={
          <Switch
            aria-label={t("Show my favourites as a playlist in Plex.")}
            checked={enabled ?? false}
            disabled={enabled === undefined || busy}
            onChange={() => void change()}
          />
        }
      />
      {error && <p role="alert">{t("The request could not be completed. Try again.")}</p>}
    </SettingsSection>
  );
}
