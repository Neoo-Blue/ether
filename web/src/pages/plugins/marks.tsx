import { Badge, cn } from "@braedonsaunders/appkit-ui";
import {
  Bell,
  BadgeCheck,
  Captions,
  Database,
  FolderOpen,
  History,
  Layers,
  LayoutTemplate,
  ListOrdered,
  Network,
  PanelsTopLeft,
  Tv,
  Wrench,
  Globe,
  type LucideIcon,
} from "lucide-react";
import { t } from "../../i18n";
import type { Category, PermissionIcon } from "./proto";

const CATEGORY_ICON: Record<Category, LucideIcon> = {
  channels: Tv,
  home: LayoutTemplate,
  subtitles: Captions,
  sources: Network,
  notifications: Bell,
  releases: ListOrdered,
  pages: PanelsTopLeft,
  tools: Wrench,
};

/** The plugin's mark: its category icon on the card tile used by Integrations. */
export function PluginMark({
  category,
  size = "md",
  className,
}: {
  category: Category;
  size?: "md" | "lg";
  className?: string;
}) {
  const Icon = CATEGORY_ICON[category];
  return (
    <span
      aria-hidden
      className={cn(
        "flex shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary",
        size === "lg" ? "size-14" : "size-11",
        className,
      )}
    >
      <Icon size={size === "lg" ? 28 : 22} />
    </span>
  );
}

export function CategoryIcon({ category, size = 15 }: { category: Category; size?: number }) {
  const Icon = CATEGORY_ICON[category];
  return <Icon size={size} aria-hidden />;
}

const PERMISSION_ICON: Record<PermissionIcon, LucideIcon> = {
  network: Globe,
  history: History,
  collections: Layers,
  notify: Bell,
  folders: FolderOpen,
  kv: Database,
};

/** One plain permission line: icon plus a short sentence, no legalese. */
export function PermissionRows({ items }: { items: { icon: PermissionIcon; label: string }[] }) {
  return (
    <ul className="space-y-2.5">
      {items.map((p) => {
        const Icon = PERMISSION_ICON[p.icon];
        return (
          <li key={p.label} className="flex items-center gap-3 text-sm text-fg">
            <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-bg-subtle text-fg-muted">
              <Icon size={16} aria-hidden />
            </span>
            {p.label}
          </li>
        );
      })}
    </ul>
  );
}

export function VerifiedBadge({ verified, source }: { verified: boolean; source?: string }) {
  return verified ? (
    <Badge variant="success">
      <BadgeCheck size={12} aria-hidden /> {t("Verified")}
    </Badge>
  ) : (
    <Badge variant="warning" title={source}>
      {t("Unverified")}
    </Badge>
  );
}
