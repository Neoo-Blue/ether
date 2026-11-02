import { Link } from "react-router-dom";
import { ConfigSection } from "../../components/ConfigSection";
import { DomainPage, useRouteTab } from "../../components/layout";
import { localized, t, tx } from "../../i18n";
import * as S from "../settings/schema";
import { DeviceCapabilities } from "./Devices";
import { HardwareStatus } from "./Hardware";
import { MarkerCoverage } from "./Markers";
import { SkipButtons } from "./SkipButtons";
import { HouseholdAudio } from "./HouseholdAudio";
import { SubtitleTiming } from "./SubtitleTiming";

const TABS = localized(
  () =>
    [
      { key: "subtitles", label: t("Audio & subtitles") },
      { key: "transcoding", label: t("Streams & transcoding") },
      { key: "devices", label: t("App compatibility") },
      { key: "extras", label: t("Extras") },
    ] as const,
);

function TrailerStatus({ draft }: { draft: Record<string, unknown> }) {
  if (!draft.enabled || String(draft.resolverCommand ?? "").trim()) return null;
  return (
    <p role="status" className="text-sm text-fg-muted">
      {t(
        "Trailers use yt-dlp. Install yt-dlp, or set your own resolver command under More options.",
      )}
    </p>
  );
}

/**
 * How a chosen release reaches the screen: audio and subtitles, streams and
 * transcoding, what each app can play, and what plays around the title.
 * Forms stay mounted across tabs, so edits on several tabs save together.
 */
export default function Playback() {
  const tab = useRouteTab(TABS);
  return (
    <DomainPage title={t("Playback")} base="/playback" tabs={TABS} active={tab}>
      {tab === "transcoding" ? <HardwareStatus /> : null}
      <ConfigSection
        hidden={tab !== "transcoding"}
        configKey="server"
        endpoint="server"
        groups={S.STREAM_LIMITS}
        label={t("Who can watch at once")}
      />
      <ConfigSection
        hidden={tab !== "transcoding"}
        configKey="transcode"
        endpoint="transcode"
        groups={S.TRANSCODE}
        label={t("Streams & transcoding")}
      />
      {tab === "devices" ? <DeviceCapabilities /> : null}
      <ConfigSection
        hidden={tab !== "extras"}
        configKey="extras"
        groups={S.EXTRAS}
        label={t("Trailers")}
        before={(draft) => <TrailerStatus draft={draft} />}
      />
      <SkipButtons hidden={tab !== "extras"} />
      {tab === "extras" ? <MarkerCoverage /> : null}
      <ConfigSection
        hidden={tab !== "extras"}
        configKey="personalize"
        groups={S.UP_NEXT}
        label={t("Up next")}
      />
      {tab === "subtitles" ? (
        <>
          <HouseholdAudio />
          <p className="max-w-3xl text-sm text-fg-muted">
            {tx(
              "When someone turns subtitles on and the file has none, Ether downloads them from OpenSubtitles. The account is set up in {link}.",
              {
                link: (
                  <Link to="/services/overview" className="text-primary underline">
                    {t("Integrations")}
                  </Link>
                ),
              },
            )}
          </p>
          <SubtitleTiming />
        </>
      ) : null}
    </DomainPage>
  );
}
