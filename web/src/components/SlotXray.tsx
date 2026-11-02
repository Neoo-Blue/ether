import { Badge, Button, cn } from "@braedonsaunders/appkit-ui";
import { Eye, EyeOff, Plus, Sparkles } from "lucide-react";
import { useEffect, useState, useSyncExternalStore, type ReactNode } from "react";
import { t } from "../i18n";

export interface SlotSpec {
  slot: string;
  label: string;
  surface: string;
  description: string;
  template: string;
}

export const SLOT_CATALOG: readonly SlotSpec[] = [
  {
    slot: "page",
    label: "Console page",
    surface: "Sidebar and /p/{plugin}/{page}",
    description: "Adds a full ViewSpec v1 page in the console navigation.",
    template: "page",
  },
  {
    slot: "dashboard.panel",
    label: "Dashboard panel",
    surface: "Home overview",
    description: "Renders a live status or summary card on the Home dashboard.",
    template: "page",
  },
  {
    slot: "title.panel",
    label: "Title detail panel",
    surface: "Catalogue title drawer",
    description: "Attaches a panel inside movie and series detail views.",
    template: "page",
  },
  {
    slot: "viewer.tab",
    label: "Viewer profile tab",
    surface: "People › Viewer profile",
    description: "Adds a subtab on each viewer profile page.",
    template: "page",
  },
  {
    slot: "actionMenu.title",
    label: "Title action menu",
    surface: "Catalogue title actions",
    description: "Adds an item to the action menu on a movie or series.",
    template: "page",
  },
  {
    slot: "actionMenu.viewer",
    label: "Viewer action menu",
    surface: "People › Viewer actions",
    description: "Adds an item to the action menu on a viewer profile.",
    template: "page",
  },
  {
    slot: "actionMenu.collection",
    label: "Collection action menu",
    surface: "Collections › Actions",
    description: "Adds an item to the action menu on a collection.",
    template: "list",
  },
  {
    slot: "actionMenu.release",
    label: "Release action menu",
    surface: "Releases › Candidate actions",
    description: "Adds an item to the action menu on a release candidate.",
    template: "local_files",
  },
  {
    slot: "command",
    label: "Command palette action",
    surface: "Command search",
    description: "Registers a quick command in Command-K search.",
    template: "page",
  },
];

type Listener = () => void;

interface SlotXrayState {
  enabled: boolean;
  selectedSlot: string | null;
}

let state: SlotXrayState = {
  enabled: false,
  selectedSlot: null,
};

const listeners = new Set<Listener>();

function emit() {
  for (const l of listeners) {
    l();
  }
}

export function setSlotXrayEnabled(enabled: boolean) {
  if (state.enabled === enabled) return;
  state = { ...state, enabled };
  emit();
}

export function toggleSlotXray() {
  state = { ...state, enabled: !state.enabled };
  emit();
}

export function selectSlotForPlugin(slot: string | null) {
  state = { ...state, selectedSlot: slot };
  emit();
}

export function useSlotXray() {
  const snap = useSyncExternalStore(
    (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    () => state,
    () => state,
  );
  return {
    enabled: snap.enabled,
    selectedSlot: snap.selectedSlot,
    setEnabled: setSlotXrayEnabled,
    toggle: toggleSlotXray,
    selectSlot: selectSlotForPlugin,
  };
}

/**
 * Wraps a live console region to mark a plugin extension slot when Slot X-ray is active.
 */
export function SlotAnchor({
  slot,
  label,
  onPickSlot,
  children,
  className,
}: {
  slot: string;
  label?: string;
  onPickSlot?: (slot: string) => void;
  children?: ReactNode;
  className?: string;
}) {
  const { enabled, selectSlot } = useSlotXray();
  const meta = SLOT_CATALOG.find((s) => s.slot === slot);
  const displayLabel = label ?? meta?.label ?? slot;

  if (!enabled) {
    return <div className={className}>{children}</div>;
  }

  return (
    <div
      data-slot-xray={slot}
      className={cn(
        "relative rounded-xl border-2 border-dashed border-primary/70 bg-primary/5 p-3 transition-colors",
        className,
      )}
    >
      <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <Badge variant="info">{slot}</Badge>
          <span className="text-xs font-medium text-fg">{t(displayLabel)}</span>
        </div>
        <Button
          size="sm"
          variant="secondary"
          onClick={() => {
            selectSlot(slot);
            onPickSlot?.(slot);
          }}
        >
          <Plus className="mr-1.5 h-3.5 w-3.5" />
          {t("New plugin for slot")}
        </Button>
      </div>
      {children}
    </div>
  );
}

/**
 * Interactive Slot X-ray inspector bar and live console slot map (SPEC WS11e).
 * Outlines every extension slot on the console with its identifier and surface;
 * clicking any slot opens New plugin pre-wired for that slot.
 */
export function SlotXrayOverlay({ onSelectSlot }: { onSelectSlot: (slot: SlotSpec) => void }) {
  const { enabled, setEnabled, selectSlot } = useSlotXray();
  const [domSlots, setDomSlots] = useState<string[]>([]);

  useEffect(() => {
    if (!enabled || typeof document === "undefined") {
      setDomSlots([]);
      return;
    }
    const nodes = Array.from(document.querySelectorAll<HTMLElement>("[data-slot-xray]"));
    const ids = nodes
      .map((n) => n.getAttribute("data-slot-xray") || "")
      .filter((v, idx, arr) => Boolean(v) && arr.indexOf(v) === idx);
    setDomSlots(ids);
  }, [enabled]);

  if (!enabled) return null;

  return (
    <section
      aria-label={t("Slot X-ray")}
      data-testid="slot-xray-overlay"
      className="rounded-xl border-2 border-dashed border-primary/60 bg-surface p-5 shadow-sm"
    >
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-border pb-4">
        <div className="space-y-1">
          <div className="flex items-center gap-2">
            <Sparkles className="h-4 w-4 text-primary" />
            <h2 className="text-sm font-semibold text-fg">{t("Slot X-ray active")}</h2>
            <Badge variant="info">{t("9 console slots")}</Badge>
          </div>
          <p className="text-sm text-fg-muted">
            {t(
              "Every extension slot in the console is outlined below. Click any slot to scaffold a plugin pre-wired for that surface.",
            )}
          </p>
        </div>
        <Button size="sm" variant="secondary" onClick={() => setEnabled(false)}>
          <EyeOff className="mr-1.5 h-4 w-4" />
          {t("Hide Slot X-ray")}
        </Button>
      </div>

      <div className="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {SLOT_CATALOG.map((item) => {
          const mountedOnPage = domSlots.includes(item.slot);
          return (
            <button
              key={item.slot}
              type="button"
              data-testid={`slot-xray-item-${item.slot}`}
              onClick={() => {
                selectSlot(item.slot);
                onSelectSlot(item);
              }}
              className="group flex flex-col justify-between rounded-lg border-2 border-dashed border-primary/40 bg-bg-subtle/60 p-3.5 text-left transition hover:border-primary hover:bg-primary/5 focus:outline-none focus:ring-2 focus:ring-primary"
            >
              <div className="space-y-1.5">
                <div className="flex items-center justify-between gap-2">
                  <code className="rounded bg-surface px-1.5 py-0.5 font-mono text-xs font-semibold text-primary">
                    {item.slot}
                  </code>
                  {mountedOnPage ? (
                    <Badge variant="success">{t("On screen")}</Badge>
                  ) : (
                    <span className="text-xs text-fg-muted">{t(item.surface)}</span>
                  )}
                </div>
                <div className="text-sm font-medium text-fg">{t(item.label)}</div>
                <p className="text-xs text-fg-muted">{t(item.description)}</p>
              </div>
              <div className="mt-3 flex items-center text-xs font-medium text-primary group-hover:underline">
                <Plus className="mr-1 h-3.5 w-3.5" />
                {t("Create plugin for {slot}", { slot: item.slot })}
              </div>
            </button>
          );
        })}
      </div>
    </section>
  );
}

export function SlotXrayToggleButton() {
  const { enabled, toggle } = useSlotXray();
  return (
    <Button
      size="sm"
      variant={enabled ? "default" : "secondary"}
      onClick={toggle}
      aria-pressed={enabled}
      data-testid="slot-xray-toggle"
    >
      {enabled ? <EyeOff className="mr-1.5 h-4 w-4" /> : <Eye className="mr-1.5 h-4 w-4" />}
      {t("Slot X-ray")}
    </Button>
  );
}
