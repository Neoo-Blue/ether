import { Button, SettingsSection } from "@braedonsaunders/appkit-ui";
import { useCallback, useEffect, useState } from "react";
import { Artwork } from "../../components/Artwork";
import { EyeOff } from "lucide-react";
import { t } from "../../i18n";

export type NotInterestedEntry = {
  handle: string;
  unavailable: boolean;
  title?: string;
  year?: number;
  poster?: string;
};

type Props = {
  load: () => Promise<NotInterestedEntry[]>;
  restore: (handle: string) => Promise<unknown>;
};

/** Restore actions use opaque handles, including for entries no longer visible. */
export function NotInterested({ load, restore }: Props) {
  const [entries, setEntries] = useState<NotInterestedEntry[]>();
  const [error, setError] = useState(false);
  const [busy, setBusy] = useState<string>();
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let active = true;
    setEntries(undefined);
    setError(false);
    void load()
      .then((items) => {
        if (active) setEntries(items);
      })
      .catch(() => {
        if (active) setError(true);
      });
    return () => {
      active = false;
    };
  }, [load, attempt]);

  const show = useCallback(
    async (handle: string) => {
      if (busy) return;
      setBusy(handle);
      setError(false);
      try {
        await restore(handle);
        setEntries((items) => items?.filter((item) => item.handle !== handle));
      } catch {
        setError(true);
      } finally {
        setBusy(undefined);
      }
    },
    [busy, restore],
  );

  return (
    <SettingsSection title={t("Not interested")}>
      <div className="space-y-2 px-5 py-4">
        {error && <p role="alert">{t("The request could not be completed. Try again.")}</p>}
        {!entries && !error && <p role="status">{t("Loading…")}</p>}
        {!entries && error && (
          <Button type="button" variant="outline" onClick={() => setAttempt((n) => n + 1)}>
            {t("Retry")}
          </Button>
        )}
        {entries?.length === 0 && (
          <div className="flex items-center gap-2 text-sm text-fg-muted">
            <EyeOff size={16} />
            {t("Nothing hidden from recommendations")}
          </div>
        )}
        {entries && entries.length > 0 && (
          <ul className="max-h-[60dvh] overflow-y-auto divide-y divide-border">
            {entries.map((entry) => {
              const label =
                entry.unavailable || !entry.title ? t("Unavailable title") : entry.title;
              return (
                <li
                  key={entry.handle}
                  className="flex flex-wrap items-center justify-between gap-3 py-2"
                >
                  <div className="flex min-w-0 flex-1 items-center gap-3">
                    <Artwork
                      src={entry.unavailable ? undefined : entry.poster}
                      alt=""
                      title={label}
                      className="w-10 aspect-[2/3] shrink-0 rounded"
                    />
                    <span className="text-sm">
                      {label}
                      {!entry.unavailable && entry.year ? ` (${entry.year})` : ""}
                    </span>
                  </div>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    disabled={busy !== undefined}
                    aria-label={t("Show {title} in recommendations", { title: label })}
                    onClick={() => void show(entry.handle)}
                  >
                    {t("Restore")}
                  </Button>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </SettingsSection>
  );
}
