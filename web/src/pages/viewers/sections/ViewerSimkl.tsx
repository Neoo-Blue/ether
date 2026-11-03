import { Button, SettingsSection, Spinner, toast } from "@braedonsaunders/appkit-ui";
import { useState } from "react";
import { Link } from "react-router-dom";
import { ErrorPanel } from "../../../components/feedback";
import { SettingsRow } from "../../../components/SettingsRow";
import { getLocale, localized, t, tn, tx } from "../../../i18n";
import { apiFetch, errorMessage } from "../../../lib/api";
import { formatNumber } from "../../../lib/format";
import { useAsync } from "../../../lib/hooks";

type Status = {
  connected: boolean;
  needsReconnect?: boolean;
  lastSync?: string;
  warning?: string;
  queued?: number;
  backoffUntil?: number;
};
type Device = {
  userCode: string;
  verificationUri: string;
  verificationUriComplete: string;
  expiresAt: string;
  interval: number;
};

/** Human text for the sync warning codes the server reports. */
const WARNINGS = localized<Record<string, string>>(() => ({
  auth_revoked: t("Simkl authorization expired or was revoked. Reconnect to continue."),
  queue_full: t("The delivery queue is full; some completions were not queued."),
  anime_mapping: t("An anime episode was skipped because its numbering could not be verified."),
  not_found: t("Simkl could not resolve a history item."),
}));

/** The Simkl approval page, only when it is Simkl's own HTTPS page. */
function approvalUrl(device: Device | null): string {
  if (!device) return "";
  try {
    const u = new URL(device.verificationUriComplete || device.verificationUri);
    return u.protocol === "https:" && u.hostname === "simkl.com" ? u.href : "";
  } catch {
    return "";
  }
}

/**
 * One viewer's Simkl account, on their Limits tab. Connect, sync and
 * disconnect run immediately; the app credentials live on Integrations › Simkl.
 */
export function ViewerSimkl({ viewerId, ready }: { viewerId: number; ready: boolean }) {
  if (!ready) {
    return (
      <SettingsSection title="Simkl">
        <p className="px-5 py-4 text-sm text-fg-muted">
          {tx("Turn on Simkl in {link} to connect this viewer's account.", {
            link: (
              <Link to="/services/simkl" className="font-medium text-primary hover:underline">
                {t("Integrations")}
              </Link>
            ),
          })}
        </p>
      </SettingsSection>
    );
  }
  return <SimklAccount viewerId={viewerId} />;
}

function SimklAccount({ viewerId }: { viewerId: number }) {
  const status = useAsync(
    (signal) => apiFetch<Status>(`/simkl/${viewerId}/status`, { signal }),
    [viewerId],
  );
  const [device, setDevice] = useState<Device | null>(null);
  const [busy, setBusy] = useState(false);

  const post = <T,>(path: string) => apiFetch<T>(`/simkl/${viewerId}/${path}`, { method: "POST" });
  const refresh = async () => status.setData(await apiFetch<Status>(`/simkl/${viewerId}/status`));
  const act = async (fn: () => Promise<void>) => {
    setBusy(true);
    try {
      await fn();
    } catch (e) {
      toast.error(t("Simkl was not updated"), { description: errorMessage(e) });
    } finally {
      setBusy(false);
    }
  };

  if (status.error) return <ErrorPanel error={status.error} onRetry={() => void status.reload()} />;
  if (!status.data) {
    return (
      <div className="py-6 text-center">
        <Spinner size={18} label={t("Loading Simkl")} />
      </div>
    );
  }
  const st = status.data;
  const connected = st.connected && !st.needsReconnect;
  const verification = approvalUrl(device);
  return (
    <SettingsSection
      title="Simkl"
      description={t("Imports this viewer's watched history and sends what they finish.")}
    >
      <SettingsRow
        title={t("Connection")}
        description={
          st.connected
            ? st.needsReconnect
              ? t("Reconnect required")
              : t("Connected")
            : t("Not connected")
        }
        control={
          st.connected || device ? (
            <Button
              disabled={busy}
              onClick={() =>
                void act(async () => {
                  await post("disconnect");
                  setDevice(null);
                  await refresh();
                  toast.success(t("Simkl disconnected."));
                })
              }
            >
              {t("Disconnect")}
            </Button>
          ) : null
        }
      />
      {!connected && !device ? (
        <SettingsRow
          title={t("Connect")}
          description={t("Approve a code on Simkl while signed in as this viewer.")}
          control={
            <Button
              disabled={busy}
              onClick={() =>
                void act(async () => {
                  setDevice(await post<Device>("start"));
                })
              }
            >
              {t("Connect")}
            </Button>
          }
        />
      ) : null}
      {device ? (
        <SettingsRow
          title={t("Approval code")}
          description={
            <>
              {tx("Open the Simkl approval page and enter code {code}.", {
                code: <strong>{device.userCode}</strong>,
              })}{" "}
              {tn(
                device.interval,
                "The code expires at {time}. Wait at least {count} second between checks.",
                "The code expires at {time}. Wait at least {count} seconds between checks.",
                { time: new Date(device.expiresAt).toLocaleTimeString(getLocale()) },
              )}
            </>
          }
          control={
            <div className="flex flex-wrap items-center gap-2">
              {verification ? (
                <a
                  href={verification}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-sm font-medium text-primary hover:underline"
                >
                  {t("Open Simkl approval")}
                </a>
              ) : null}
              <Button
                disabled={busy}
                onClick={() =>
                  void act(async () => {
                    const r = await post<{ status: string }>("poll");
                    if (r.status === "authorized") {
                      setDevice(null);
                      await refresh();
                      toast.success(t("Simkl account connected."));
                    } else toast.info(t("Waiting for approval."));
                  })
                }
              >
                {t("Check connection")}
              </Button>
            </div>
          }
        />
      ) : null}
      {connected ? (
        <SettingsRow
          title={t("Sync history")}
          description={t(
            "Sync keeps existing history and does not remove watched items. Anime episodes without a verified mapping are skipped.",
          )}
          control={
            <Button
              disabled={busy}
              onClick={() =>
                void act(async () => {
                  const r = await post<{ imported: number; skipped: number }>("sync");
                  await refresh();
                  toast.success(
                    tn(
                      r.imported,
                      "Processed {count} history entry; skipped {skipped}.",
                      "Processed {count} history entries; skipped {skipped}.",
                      { skipped: formatNumber(r.skipped) },
                    ),
                  );
                })
              }
            >
              {t("Sync history")}
            </Button>
          }
        />
      ) : null}
      {st.queued ? (
        <p className="py-2 text-sm">
          {t("Queued history entries: {count}", { count: formatNumber(st.queued) })}
        </p>
      ) : null}
      {st.lastSync ? (
        <p className="py-2 text-sm text-fg-muted">
          {t("Last imported activity: {when}", { when: st.lastSync })}
        </p>
      ) : null}
      {st.backoffUntil && st.backoffUntil > Date.now() ? (
        <p className="py-2 text-sm text-fg-muted">
          {t("Sync paused until {when}.", {
            when: new Date(st.backoffUntil).toLocaleString(getLocale()),
          })}
        </p>
      ) : null}
      {st.warning ? (
        <p className="py-2 text-sm text-fg-muted">
          {WARNINGS[st.warning] ?? t("A history item could not be sent.")}
        </p>
      ) : null}
    </SettingsSection>
  );
}
