import { localized, t } from "../../i18n";
import { ConfigSection } from "../../components/ConfigSection";
import { DomainPage, useRouteTab } from "../../components/layout";
import { RELEASE_SEARCH } from "../settings/schema";
import { StartSpeed } from "../playback/StartSpeed";
import { BlocklistTab } from "./Blocklist";
import { ClientRulesTab } from "./ClientRules";
import { HistoryTab } from "./History";
import { PinnedTab } from "./Pinned";
import { QualityTab } from "./Quality";

const TABS = localized(
  () =>
    [
      { key: "profiles", label: t("Profiles") },
      { key: "rules", label: t("Rules") },
      { key: "searching", label: t("Searching") },
      { key: "blocklist", label: t("Blocklist") },
      { key: "pinned", label: t("Pinned") },
      { key: "history", label: t("History") },
    ] as const,
);

/** Which release Ether picks for each play, and why. */
export default function Releases() {
  const tab = useRouteTab(TABS);
  return (
    <DomainPage title={t("Quality")} base="/quality" tabs={TABS} active={tab}>
      {tab === "profiles" ? <QualityTab /> : null}
      {tab === "rules" ? <ClientRulesTab /> : null}
      {tab === "searching" ? (
        <>
          <StartSpeed />
          <ConfigSection
            configKey="resolver"
            endpoint="resolver"
            groups={RELEASE_SEARCH}
            label={t("Searching")}
          />
        </>
      ) : null}
      {tab === "blocklist" ? <BlocklistTab /> : null}
      {tab === "pinned" ? <PinnedTab /> : null}
      {tab === "history" ? <HistoryTab /> : null}
    </DomainPage>
  );
}
