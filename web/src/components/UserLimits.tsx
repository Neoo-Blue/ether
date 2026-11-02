import { SettingsRow } from "./SettingsRow";
import { SearchSelect } from "@braedonsaunders/appkit-ui";
import type { ReactNode } from "react";
import { localized, t } from "../i18n";
import { RATING_LEVELS, canonicalRating } from "../lib/contentRatings";
import type { EtherUser } from "../lib/types";
import { NumberInput } from "./fields";

/**
 * Rating-limit choices: one per age level, so a "PG" limit also admits
 * TV-PG and other countries' equivalents. Values are the level keys.
 */
export const CONTENT_RATINGS = localized(() => [
  { value: "", label: t("Unrestricted") },
  ...RATING_LEVELS.map((l) => ({
    value: l.value,
    label: t("{labels} ({name})", { labels: l.labels, name: l.name }),
  })),
]);

/** The user-record limits a viewer's Limits tab edits. */
export type UserLimits = Pick<EtherUser, "streamLimit" | "maxContentRating">;

export function userLimits(u: EtherUser): UserLimits {
  return { streamLimit: u.streamLimit, maxContentRating: u.maxContentRating };
}

/** "Streams at once" for one viewer, for a form saved elsewhere. */
export function StreamLimitRow({
  value,
  onChange,
  defaultStreamLimit,
  hint,
}: {
  value: number;
  onChange: (next: number) => void;
  defaultStreamLimit?: number;
  /** Extra copy after the description (e.g. a settings link). */
  hint?: ReactNode;
}) {
  return (
    <SettingsRow
      title={t("Streams at once")}
      description={
        <>
          {defaultStreamLimit !== undefined
            ? t("0 uses the server default ({limit}).", { limit: defaultStreamLimit })
            : t("0 uses the server default.")}
          {hint ? <> {hint}</> : null}
        </>
      }
      control={
        <NumberInput
          aria-label={t("Streams at once")}
          className="w-24"
          value={value}
          min={0}
          onChange={(n) => onChange(Math.max(0, Math.round(n)))}
        />
      }
    />
  );
}

/** One viewer's content rating limit, for a form saved elsewhere. */
export function RatingLimitRow({
  value,
  onChange,
  isOwner,
}: {
  value: string;
  onChange: (next: string) => void;
  isOwner?: boolean;
}) {
  return (
    <SettingsRow
      title={t("Content rating limit")}
      description={
        isOwner
          ? t("The owner is never limited by content rating.")
          : t("Hides titles above this age level, and unrated titles.")
      }
      control={
        <div className="w-56">
          <SearchSelect
            value={canonicalRating(value)}
            onChange={onChange}
            options={CONTENT_RATINGS}
            ariaLabel={t("Content rating limit")}
            searchable={false}
            placeholder={t("Select…")}
            selectLabel={t("Select")}
            closeLabel={t("Close")}
          />
        </div>
      }
    />
  );
}
