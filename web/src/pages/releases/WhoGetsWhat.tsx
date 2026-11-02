import { Badge, SearchSelect, SettingsSection, cn } from "@braedonsaunders/appkit-ui";
import { useState } from "react";
import { ErrorPanel, LoadingRows } from "../../components/feedback";
import { Segmented } from "../../components/fields";
import { useViewers, viewerName } from "../../components/pickers";
import { SettingsRow } from "../../components/SettingsRow";
import { t } from "../../i18n";
import { searchSelectLabels } from "../../i18n/appkit-labels";
import { api } from "../../lib/api";
import { formatNumber } from "../../lib/format";
import { useAsync } from "../../lib/hooks";
import type { QualityAnswer, QualityLayer } from "../../lib/types";

type Where = "home" | "away";

export function heightName(h: number | undefined): string {
  if (!h) return t("Any resolution");
  return h === 2160 ? t("4K") : `${h}p`;
}

export function rateName(kbps: number): string {
  return t("{rate} Mbps", { rate: formatNumber(kbps / 1000, 1) });
}

/** The big line: "1080p · HDR off", plus the bitrate cap when there is one. */
export function answerHeadline(a: QualityAnswer): string {
  if (a.blocked) return t("Can't play away from home");
  const hdr = a.dolbyVision ? t("HDR and Dolby Vision") : a.hdr ? t("HDR on") : t("HDR off");
  const parts = [heightName(a.maxHeight), hdr];
  if (a.bitrateKbps) parts.push(t("Up to {rate}", { rate: rateName(a.bitrateKbps) }));
  return parts.join(" · ");
}

function deviceHDR(l: QualityLayer): string {
  if (l.hdr && l.dolbyVision) return t("It plays HDR and Dolby Vision.");
  if (l.hdr) return t("It plays HDR but not Dolby Vision.");
  return t("It doesn't play HDR.");
}

/** Plain, translated copy for one layer. `viewer` names the checked viewer. */
export function layerText(l: QualityLayer, viewer: string): string {
  const profile = l.profile ?? "";
  const n = l.rule ?? 0;
  switch (l.kind) {
    case "profile":
      switch (l.source) {
        case "rule":
          return l.beatsRemote
            ? t("Rule {n} picks {profile}, ahead of the Away profile.", { n, profile })
            : t("Rule {n} picks {profile}.", { n, profile });
        case "remote":
          return t("Away from home uses {profile}.", { profile });
        case "default":
          return t("No rule matches, so {profile} applies.", { profile });
        case "first":
          return t("No default profile is set, so {profile} applies.", { profile });
        default:
          return t("No profiles yet, so the built-in profile applies.");
      }
    case "device":
      return `${
        l.maxHeight
          ? t("This app plays up to {height}.", { height: heightName(l.maxHeight) })
          : t("This app plays any resolution.")
      } ${deviceHDR(l)}`;
    case "rule-height":
      return l.ignored
        ? t("Rule {n}'s maximum resolution is ignored because this app's resolution is fixed.", {
            n,
          })
        : t("Rule {n} caps resolution at {height}.", { n, height: heightName(l.maxHeight) });
    case "android-lift":
      return t("Rule {n} lifts Android's usual 1080p limit to {height}.", {
        n,
        height: heightName(l.maxHeight),
      });
    case "remote-cap":
      return l.bitrateKbps
        ? t("Away from home is limited to {rate}.", { rate: rateName(l.bitrateKbps) })
        : t("No bitrate limit away from home.");
    case "viewer-cap":
      return t("{name} is limited to {rate}.", {
        name: viewer,
        rate: rateName(l.bitrateKbps ?? 0),
      });
    case "remote-blocked":
      return t("{name} can't stream away from home.", { name: viewer });
  }
  return l.reason;
}

function layerMeta(l: QualityLayer): string {
  switch (l.kind) {
    case "profile":
    case "device":
    case "rule-height":
    case "android-lift":
      return heightName(l.maxHeight);
    case "remote-cap":
    case "viewer-cap":
      return l.bitrateKbps ? rateName(l.bitrateKbps) : t("No limit");
  }
  return "";
}

function Answer({ answer, viewer }: { answer: QualityAnswer; viewer: string }) {
  return (
    <div className="space-y-3 px-5 py-4" aria-live="polite">
      <div>
        <p className="text-2xl font-semibold text-fg" data-testid="quality-headline">
          {answerHeadline(answer)}
        </p>
        {!answer.blocked ? (
          <p className="text-sm text-fg-muted">
            {t("Uses the {profile} profile.", { profile: answer.profile })}
          </p>
        ) : null}
      </div>
      <ol className="space-y-2" aria-label={t("What decided it")}>
        {answer.layers.map((l, i) => (
          <li
            key={`${l.kind}-${i}`}
            className={cn(
              "flex flex-wrap items-start justify-between gap-2 text-sm",
              l.ignored && "text-fg-muted line-through",
            )}
          >
            <span className="min-w-0 flex-1">{layerText(l, viewer)}</span>
            <span className="flex shrink-0 items-center gap-2">
              {l.edited ? <Badge variant="secondary">{t("Edited")}</Badge> : null}
              {layerMeta(l) ? <span className="text-fg-muted">{layerMeta(l)}</span> : null}
              {l.decisive ? <Badge variant="info">{t("Decides")}</Badge> : null}
            </span>
          </li>
        ))}
      </ol>
      <p className="text-xs text-fg-muted">
        {t("Apps can ask for less in their own quality settings.")}
      </p>
    </div>
  );
}

/**
 * "Who gets what": pick a viewer, an app and At home or Away, and see the
 * quality ceiling that combination gets and which setting decided it. The
 * answer comes from the server's playback code. Pass `refreshKey` (such as
 * the saved quality settings) to check again after a save.
 */
export function WhoGetsWhat({ refreshKey }: { refreshKey?: unknown }) {
  const { viewers, error: viewersError } = useViewers();
  const clients = useAsync(() => api.qualityClients(), []);
  const [pickedViewer, setViewer] = useState(0);
  const [pickedClient, setClient] = useState("");
  const [where, setWhere] = useState<Where>("home");

  const viewerId = pickedViewer || viewers?.find((v) => v.isOwner)?.id || viewers?.[0]?.id || 0;
  const clientId = pickedClient || clients.data?.[0]?.id || "";
  const remote = where === "away";
  const answer = useAsync<QualityAnswer | null>(
    (signal) =>
      viewerId && clientId
        ? api.explainQuality({ userId: viewerId, clientId, remote }, signal)
        : Promise.resolve(null),
    [viewerId, clientId, remote, refreshKey],
  );
  const viewer = viewers?.find((v) => v.id === viewerId);

  const failed = viewersError ?? clients.error;
  return (
    <SettingsSection
      title={t("Who gets what")}
      description={t("See the quality a viewer gets on an app, and why.")}
    >
      {failed ? (
        <ErrorPanel error={failed} onRetry={() => void clients.reload()} />
      ) : !viewers || !clients.data ? (
        <LoadingRows rows={3} />
      ) : (
        <>
          <SettingsRow
            title={t("Viewer")}
            control={
              <div className="w-56">
                <SearchSelect
                  {...searchSelectLabels()}
                  value={String(viewerId)}
                  onChange={(v) => setViewer(Number(v))}
                  options={viewers.map((v) => ({ value: String(v.id), label: viewerName(v) }))}
                  ariaLabel={t("Viewer")}
                />
              </div>
            }
          />
          <SettingsRow
            title={t("App")}
            control={
              <div className="w-56">
                <SearchSelect
                  {...searchSelectLabels()}
                  value={clientId}
                  onChange={setClient}
                  options={clients.data.map((c) => ({ value: c.id, label: c.name }))}
                  ariaLabel={t("App")}
                />
              </div>
            }
          />
          <SettingsRow
            title={t("Where")}
            control={
              <Segmented
                ariaLabel={t("Where")}
                size="sm"
                value={where}
                onChange={setWhere}
                options={[
                  { value: "home", label: t("At home") },
                  { value: "away", label: t("Away") },
                ]}
              />
            }
          />
          {answer.error ? (
            <ErrorPanel error={answer.error} onRetry={() => void answer.reload()} />
          ) : answer.data ? (
            <Answer answer={answer.data} viewer={viewer ? viewerName(viewer) : t("This viewer")} />
          ) : (
            <LoadingRows rows={2} />
          )}
        </>
      )}
    </SettingsSection>
  );
}
