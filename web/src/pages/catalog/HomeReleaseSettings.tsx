import { useAsync, useInterval } from "../../lib/hooks";
import { localized, t } from "../../i18n";
import { apiFetch } from "../../lib/api";
import { formatNumber } from "../../lib/format";
import type { FieldGroup } from "../settings/SchemaForm";

export const UpcomingEpisodeSettings: FieldGroup = localized(() => ({
  title: t("Upcoming content"),
  fields: [
    {
      key: "showUpcomingMovies",
      label: t("Show upcoming movies"),
      kind: "bool",
      help: t("List movies before their release date."),
    },
    {
      key: "showUpcomingEpisodes",
      label: t("Show upcoming episodes"),
      kind: "bool",
      help: t("List upcoming episodes in seasons that are already airing."),
    },
  ],
}));

export function HomeReleaseStatus() {
  const data = useAsync(
    () =>
      apiFetch<{ value: { homeReleaseChecked: number; homeReleasePending: number } }>(
        "/status/catalog",
      ),
    [],
  );
  useInterval(() => void data.reload(), 60000);
  const status = data.data?.value;
  if (!status) return null;
  return (
    <section
      className="rounded-xl border border-border bg-surface p-5"
      aria-label={t("Movie release metadata")}
    >
      <h3 className="text-sm font-semibold">{t("Movie release metadata")}</h3>
      <div className="mt-4 grid grid-cols-2 gap-4">
        <div>
          <p className="text-2xl font-semibold tabular-nums">
            {formatNumber(status.homeReleaseChecked ?? 0)}
          </p>
          <p className="text-xs text-fg-muted">{t("Checked")}</p>
        </div>
        <div>
          <p className="text-2xl font-semibold tabular-nums">
            {formatNumber(status.homeReleasePending ?? 0)}
          </p>
          <p className="text-xs text-fg-muted">{t("Awaiting details")}</p>
        </div>
      </div>
    </section>
  );
}
