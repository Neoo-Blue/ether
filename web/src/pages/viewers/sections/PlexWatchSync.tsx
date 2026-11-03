import {
  Button,
  SettingsSection,
  Spinner,
  Switch,
  Tooltip,
  toast,
} from "@braedonsaunders/appkit-ui";
import { useState } from "react";
import { ErrorPanel } from "../../../components/feedback";
import { SettingsRow } from "../../../components/SettingsRow";
import { t } from "../../../i18n";
import { apiFetch, errorMessage } from "../../../lib/api";
import { useAsync } from "../../../lib/hooks";

type PlexWatchStatus = {
  connected: boolean;
  plexId?: number;
  username?: string;
  enabled: boolean;
  eligibility: string;
  revoked: boolean;
  pending: number;
  dead: number;
  held: number;
  heldLegacy: number;
  inFlight: boolean;
  skipped: number;
  restricted: boolean;
  pinCode?: string;
  pinUrl?: string;
  pinExpires?: string;
  accountMatch: boolean;
};

/** Per-viewer Plex account watch state, on Limits & preferences. */
export function PlexWatchSync({ viewerId, plexId }: { viewerId: number; plexId: number }) {
  const status = useAsync(
    (signal) => apiFetch<PlexWatchStatus>(`/viewers/${viewerId}/plex-watch`, { signal }),
    [viewerId],
  );
  const [busy, setBusy] = useState(false);

  const run = async (path: string, method: string, body?: unknown) => {
    setBusy(true);
    try {
      const next = await apiFetch<PlexWatchStatus>(`/viewers/${viewerId}/plex-watch${path}`, {
        method,
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      status.setData(next);
    } catch (e) {
      toast.error(t("Plex watch state was not updated"), { description: errorMessage(e) });
    } finally {
      setBusy(false);
    }
  };

  if (plexId <= 0) return null;
  if (status.error) return <ErrorPanel error={status.error} onRetry={() => void status.reload()} />;
  if (!status.data) {
    return (
      <div className="py-6 text-center">
        <Spinner size={18} label={t("Loading Plex watch state")} />
      </div>
    );
  }

  const st = status.data;
  return (
    <SettingsSection>
      <h2 className="flex items-center gap-2 px-5 py-4 text-sm font-semibold">
        {t("Plex account watch state")}
        <Tooltip
          content={t(
            "Connect this viewer's Plex account to import watched marks and ratings or export current state. Send future changes enables exports and ongoing updates; it does not upload existing history. Playback progress stays here.",
          )}
        >
          <button type="button" aria-label={t("About Plex watch state")} className="text-fg-muted">
            ?
          </button>
        </Tooltip>
      </h2>
      <SettingsRow
        title={t("Connection")}
        control={
          st.connected ? (
            <Button disabled={busy} onClick={() => void run("/disconnect", "POST")}>
              {t("Disconnect")}
            </Button>
          ) : (
            <Button disabled={busy} onClick={() => void run("/pin", "POST")}>
              {t("Connect this viewer's Plex account")}
            </Button>
          )
        }
      />
      {st.pinCode ? (
        <SettingsRow
          title={t("Approval code")}
          control={
            <div className="flex items-center gap-2">
              <code>{st.pinCode}</code>
              <Button disabled={busy} onClick={() => void run("/pin/finish", "POST")}>
                {t("Check approval")}
              </Button>
            </div>
          }
        />
      ) : null}
      {st.pinUrl ? (
        <SettingsRow
          title={t("Approve on plex.tv")}
          control={
            <a href={st.pinUrl} target="_blank" rel="noreferrer">
              {t("Open the approval page")}
            </a>
          }
        />
      ) : null}
      <SettingsRow
        title={t("Send future changes")}
        control={
          <Switch
            aria-label={t("Send future changes")}
            checked={st.enabled}
            disabled={busy || !st.connected}
            onChange={(e) => void run("", "PUT", { enabled: e.currentTarget.checked })}
          />
        }
      />
      <div className="flex flex-wrap gap-2 px-5 py-4">
        {(
          [
            ["/import", t("Import from Plex")],
            ["/export", t("Export current state")],
          ] as const
        ).map(([path, label]) => {
          const disabled = busy || !st.connected || (path === "/export" && !st.enabled);
          const button = (
            <Button
              variant={disabled ? "outline" : "default"}
              disabled={disabled}
              onClick={() => void run(path, "POST")}
            >
              {label}
            </Button>
          );
          return disabled ? (
            <Tooltip
              key={path}
              content={
                !st.connected
                  ? t("Connect the Plex account first")
                  : !st.enabled && path === "/export"
                    ? t("Turn on Send future changes first")
                    : t("Please wait")
              }
            >
              <span tabIndex={0}>{button}</span>
            </Tooltip>
          ) : (
            <span key={path}>{button}</span>
          );
        })}
      </div>
    </SettingsSection>
  );
}
