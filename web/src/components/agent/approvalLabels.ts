import type { AgentApprovalRequestLabels } from "@braedonsaunders/appkit-ai/react";
import { t } from "../../i18n";

/** Approval card copy shared by Ask Ether and Studio. The server continues
 * the turn after a decision, so "carry on" is literal. */
export function approvalRequestLabels(): Partial<AgentApprovalRequestLabels> {
  return {
    title: t("Approval needed"),
    note: t("Add a note (optional)"),
    approve: t("Approve"),
    reject: t("Decline"),
    deciding: t("Saving decision"),
    approved: t("Approved. Ether will carry on from here."),
    rejected: t("Declined. Nothing changed."),
    expired: t("This approval request has expired."),
    failed: t("The decision could not be saved. Try again."),
    carryOutFailed: t("Approved, but the change could not be carried out."),
  };
}
