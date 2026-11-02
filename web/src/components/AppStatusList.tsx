import { Button, toast } from "@braedonsaunders/appkit-ui";
import { AlertTriangle, CheckCircle2, XCircle, Copy } from "lucide-react";
import type { ComponentProps } from "react";
import { SettingsRow } from "./SettingsRow";
import { t } from "../i18n";

export function StatusRow({
  state,
  title,
  ...props
}: ComponentProps<typeof SettingsRow> & { state?: string }) {
  const Icon =
    state === "reachable" ? CheckCircle2 : state === "unreachable" ? XCircle : AlertTriangle;
  const color =
    state === "reachable"
      ? "text-success"
      : state === "unreachable"
        ? "text-danger"
        : "text-warning";
  return (
    <SettingsRow
      {...props}
      title={
        <span className="flex items-center gap-3">
          <Icon className={color} size={20} aria-hidden />
          {title}
        </span>
      }
    />
  );
}

export function AddressChip({ address }: { address: string }) {
  return (
    <div className="flex min-w-0 items-center gap-2 rounded-lg border border-border bg-bg-subtle px-3 py-2">
      <a href={address} className="break-all font-mono text-sm text-primary">
        {address}
      </a>
      <Button
        variant="ghost"
        size="sm"
        aria-label={t("Copy address")}
        onClick={() => {
          void navigator.clipboard
            .writeText(address)
            .then(() => toast.success(t("Address copied")))
            .catch(() => toast.error(t("Couldn't copy address")));
        }}
      >
        <Copy size={16} />
      </Button>
    </div>
  );
}
