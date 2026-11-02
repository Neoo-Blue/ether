/**
 * Server › Plugins › Develop placeholder (WS10a). WS11 replaces this with Studio.
 */
import { Section } from "../../../components/layout";
import { t } from "../../../i18n";

export default function Develop() {
  return (
    <Section title={t("Plugin Studio")}>
      <p className="text-sm text-fg-muted">{t("Build and test plugins in a local sandbox.")}</p>
    </Section>
  );
}
