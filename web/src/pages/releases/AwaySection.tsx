import { SearchSelect, SettingsSection, toast } from "@braedonsaunders/appkit-ui";
import { useMemo, useState } from "react";
import { useApp } from "../../app-context";
import { NumberInput } from "../../components/fields";
import { SaveBar, useDraft } from "../../components/SaveBar";
import { SettingsRow } from "../../components/SettingsRow";
import { t } from "../../i18n";
import { searchSelectLabels } from "../../i18n/appkit-labels";
import { api } from "../../lib/api";
import { useAsync } from "../../lib/hooks";
import type { Quality } from "./useQuality";

type Draft = { remote: string; capMbps: number };

/**
 * Quality away from home: an optional Away profile (quality.remote) and an
 * optional bitrate cap (transcode.remoteMaxBitrateKbps). Neither is set by
 * default; device and viewer rules still win over the Away profile.
 */
export function AwaySection({
  quality,
  onSaved,
}: {
  quality: Quality;
  onSaved: () => Promise<void>;
}) {
  const { refreshDashboard } = useApp();
  const config = useAsync(() => api.getConfig(), []);
  const kbps = config.data?.transcode?.remoteMaxBitrateKbps;
  const saved = useMemo<Draft | null>(
    () =>
      kbps === undefined
        ? null
        : { remote: quality.remote ?? "", capMbps: Math.round((kbps ?? 0) / 100) / 10 },
    [kbps, quality.remote],
  );
  const { draft, setDraft, dirty, reset } = useDraft(saved);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<unknown>(null);
  if (!draft || !saved) return null;
  const options = [
    { value: "", label: t("Same as at home") },
    ...(quality.profiles ?? []).map((p) => ({ value: p.name, label: p.name })),
  ];
  async function submit() {
    if (!draft || !saved) return;
    setSaving(true);
    setError(null);
    try {
      if (draft.remote !== saved.remote) await api.putSection("quality", { remote: draft.remote });
      if (draft.capMbps !== saved.capMbps)
        await api.putSection("transcode", {
          remoteMaxBitrateKbps: Math.round(Math.max(0, draft.capMbps) * 1000),
        });
      toast.success(t("Settings saved"), { description: t("Away from home") });
      await Promise.all([config.reload(), onSaved()]);
      void refreshDashboard();
    } catch (e) {
      setError(e);
    } finally {
      setSaving(false);
    }
  }
  return (
    <SettingsSection title={t("Away from home")}>
      <SettingsRow
        title={t("Away profile")}
        description={t("Used away from home. Device and viewer rules still win.")}
        control={
          <div className="w-52">
            <SearchSelect
              {...searchSelectLabels()}
              value={draft.remote}
              onChange={(remote) => setDraft({ ...draft, remote })}
              options={options}
              ariaLabel={t("Away profile")}
              searchable={false}
            />
          </div>
        }
      />
      <SettingsRow
        title={<label htmlFor="away-bitrate-cap">{t("Bitrate limit")}</label>}
        description={t("Caps the stream for people away from home. 0 means no limit.")}
        control={
          <span className="flex items-center gap-2">
            <NumberInput
              id="away-bitrate-cap"
              className="w-24"
              min={0}
              step={0.5}
              value={draft.capMbps}
              onChange={(capMbps) => setDraft({ ...draft, capMbps })}
            />
            <span className="text-sm text-fg-muted">{t("Mbps")}</span>
          </span>
        }
      />
      <SaveBar
        dirty={dirty}
        saving={saving}
        error={error}
        onSave={submit}
        onDiscard={() => {
          reset();
          setError(null);
        }}
      />
    </SettingsSection>
  );
}
