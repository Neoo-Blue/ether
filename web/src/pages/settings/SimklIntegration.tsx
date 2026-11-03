import { SettingsSection } from "@braedonsaunders/appkit-ui";
import type { ReactNode } from "react";
import { localized, t } from "../../i18n";
import { SchemaField, type FieldDef } from "./SchemaForm";

type Obj = Record<string, unknown>;

const FIELDS = localized<FieldDef[]>(() => [
  { key: "clientId", label: t("Client ID"), kind: "text", wide: true },
  { key: "clientSecret", label: t("Client secret"), kind: "secret" },
]);

/**
 * Simkl's app credentials (Integrations › Simkl and its setup drawer). The
 * on/off switch is the Integrations overview card; each viewer connects their
 * own account from their Limits tab.
 */
export function SimklSettings({
  value,
  onChange,
  errors,
  footer,
}: {
  value: Obj;
  onChange: (next: Obj) => void;
  errors: Record<string, string>;
  footer?: ReactNode;
}) {
  return (
    <SettingsSection
      title={undefined}
      description={t("Each viewer connects their own Simkl account from their Limits tab.")}
      footer={footer ?? undefined}
    >
      {FIELDS.map((f) => (
        <SchemaField
          key={f.key}
          def={f}
          value={value[f.key]}
          path={`simkl.${f.key}`}
          error={errors[`simkl.${f.key}`]}
          onChange={(v) => onChange({ ...value, [f.key]: v })}
        />
      ))}
    </SettingsSection>
  );
}
