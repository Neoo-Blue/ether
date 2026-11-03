import { Button, Popover, SubtabNav } from "@braedonsaunders/appkit-ui";
import { useLayoutEffect, useRef, useState } from "react";
import type { PageTab } from "./layout";
import { t } from "../i18n";

/** Measure the real tab controls and reserve room for AppKit's overflow popover. */
export function OverflowTabs({
  tabs,
  active,
  onSelect,
  ariaLabel,
}: {
  tabs: readonly PageTab[];
  active: string;
  onSelect: (key: string) => void;
  ariaLabel: string;
}) {
  const container = useRef<HTMLDivElement>(null);
  const measure = useRef<HTMLDivElement>(null);
  const more = useRef<HTMLDivElement>(null);
  const [visible, setVisible] = useState<string[]>(tabs.map((tab) => tab.key));
  const [open, setOpen] = useState(false);
  useLayoutEffect(() => {
    const update = () => {
      if (!container.current || !measure.current) return;
      const widths = Array.from(measure.current.querySelectorAll('[role="tab"]')).map(
        (el) => el.getBoundingClientRect().width,
      );
      const available = container.current.clientWidth;
      if (!available) {
        setVisible(tabs.map((tab) => tab.key));
        return;
      }
      const total = widths.reduce((a, b) => a + b + 4, -4);
      if (total <= available) {
        setVisible(tabs.map((tab) => tab.key));
        return;
      }
      let remaining = available - (more.current?.getBoundingClientRect().width ?? 80) - 8;
      const selected = tabs.findIndex((tab) => tab.key === active);
      const keys = new Set<string>();
      if (selected >= 0) {
        keys.add(active);
        remaining -= widths[selected]! + 4;
      }
      tabs.forEach((tab, i) => {
        if (i !== selected && widths[i]! + 4 <= remaining) {
          keys.add(tab.key);
          remaining -= widths[i]! + 4;
        }
      });
      setVisible(tabs.filter((tab) => keys.has(tab.key)).map((tab) => tab.key));
    };
    update();
    const observer = new ResizeObserver(update);
    if (container.current) observer.observe(container.current);
    if (measure.current) observer.observe(measure.current);
    return () => observer.disconnect();
  }, [tabs, active]);
  const hidden = tabs.filter((tab) => !visible.includes(tab.key));
  return (
    <div ref={container} className="relative flex min-w-0 items-center gap-2">
      <div ref={measure} aria-hidden inert className="pointer-events-none invisible absolute w-max">
        <SubtabNav tabs={[...tabs]} active={active} />
      </div>
      <div className="min-w-0 flex-1">
        <SubtabNav
          tabs={tabs.filter((tab) => visible.includes(tab.key))}
          active={active}
          onSelect={onSelect}
          ariaLabel={ariaLabel}
        />
      </div>
      <div
        ref={more}
        className={hidden.length ? "shrink-0" : "invisible absolute right-0 pointer-events-none"}
        aria-hidden={!hidden.length || undefined}
        inert={!hidden.length}
      >
        <Popover
          open={open && !!hidden.length}
          onOpenChange={setOpen}
          align="end"
          trigger={
            <Button
              variant="ghost"
              size="sm"
              aria-haspopup="menu"
              aria-expanded={open}
              onClick={() => setOpen(!open)}
            >
              {t("More")}
            </Button>
          }
        >
          <div role="menu" aria-label={ariaLabel} className="max-h-80 min-w-48 overflow-y-auto p-1">
            {hidden.map((tab) => (
              <button
                key={tab.key}
                role="menuitem"
                className="block w-full rounded-md px-3 py-2 text-left text-sm text-fg hover:bg-bg-subtle"
                onClick={() => {
                  setOpen(false);
                  onSelect(tab.key);
                }}
              >
                {tab.label}
              </button>
            ))}
          </div>
        </Popover>
      </div>
    </div>
  );
}
