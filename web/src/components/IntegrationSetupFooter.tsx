import { Button } from "@braedonsaunders/appkit-ui";
import { t } from "../i18n";

export function IntegrationSetupFooter({
  onCancel,
  onSave,
  disabled,
  form,
}: {
  onCancel: () => void;
  onSave?: () => void;
  disabled?: boolean;
  form?: string;
}) {
  return (
    <div className="flex w-full justify-end gap-2">
      <Button variant="ghost" size="sm" disabled={disabled} onClick={onCancel}>
        {t("Cancel")}
      </Button>
      <Button
        size="sm"
        disabled={disabled}
        type={form ? "submit" : "button"}
        form={form}
        onClick={onSave}
      >
        {t("Save and enable")}
      </Button>
    </div>
  );
}
