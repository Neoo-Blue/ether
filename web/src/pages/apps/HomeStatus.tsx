import { useState } from "react";
import { ConnectionStatusRow, AddressChip } from "../../components/AppStatusList";
import { t } from "../../i18n";
import {
  chosenAddress,
  homeSummary,
  usableAddresses,
  type ReachabilityStatus,
} from "./reachability";

/** The At home line names the address the owner is using, and keeps the rest behind a disclosure. */
export function HomeStatusRow({
  status,
  address,
}: {
  status: ReachabilityStatus | null | undefined;
  address?: string;
}) {
  const [showOther, setShowOther] = useState(false);
  const chosen = chosenAddress(status, address);
  const others = usableAddresses(status).filter((item) => item !== chosen);
  const home = homeSummary(status, chosen);
  return (
    <div>
      <ConnectionStatusRow
        state={status?.lan}
        title={home.title}
        detail={home.detail}
        address={chosen}
      />
      {others.length > 0 ? (
        <div className="px-5 pb-3">
          <button
            type="button"
            className="text-sm text-primary"
            aria-expanded={showOther}
            onClick={() => setShowOther((v) => !v)}
          >
            {t("Other addresses")}
          </button>
          {showOther ? (
            <div className="mt-2 flex flex-wrap gap-2">
              {others.map((item) => (
                <AddressChip key={item} address={item} />
              ))}
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
