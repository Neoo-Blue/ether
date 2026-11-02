import { Tooltip } from "@braedonsaunders/appkit-ui";
import { CircleHelp } from "lucide-react";
import { t } from "../../i18n";
export function QualityHelp({ summary, detail }: { summary: string; detail: string }) {
  return (
    <span className="inline-flex items-center gap-2">
      {summary}
      <Tooltip content={detail}>
        <button type="button" aria-label={t("More information")} className="text-fg-muted">
          <CircleHelp size={14} />
        </button>
      </Tooltip>
    </span>
  );
}
