import { Badge, Input, SearchSelect, cn } from "@braedonsaunders/appkit-ui";
import {
  BarChart3,
  GripVertical,
  Hash,
  LayoutGrid,
  Rows3,
  Table2,
  Type,
  UserRound,
  type LucideIcon,
} from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";
import { Columns } from "../../components/charts";
import { PosterCard } from "../../components/Poster";
import { t } from "../../i18n";
import { studioSpec, usePreview } from "./preview";

const WIDGETS: { icon: LucideIcon; label: string }[] = [
  { icon: Hash, label: "Stat tile" },
  { icon: BarChart3, label: "Chart" },
  { icon: LayoutGrid, label: "Poster grid" },
  { icon: Table2, label: "Table" },
  { icon: UserRound, label: "Title card" },
  { icon: Type, label: "Heading" },
  { icon: Rows3, label: "List" },
];

type WidgetId = "hours" | "plays" | "viewers" | "months" | "top" | "people";

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const HOURS = [82, 74, 91, 66, 58, 49, 63, 70, 88, 104, 112, 97];

/**
 * The page builder for plugins with a page: drag ViewSpec widgets onto a
 * grid and bind each to a block's output. The canvas is the real renderer,
 * so what you see is the page viewers and the owner will get.
 */
export function PageBuilder() {
  const [selected, setSelected] = useState<WidgetId>("months");
  const top = usePreview(useMemo(() => studioSpec(false), []));
  return (
    <div className="app-scroll min-h-0 flex-1 overflow-y-auto lg:grid lg:grid-cols-[13rem_minmax(0,1fr)_19rem] lg:overflow-hidden">
      <aside
        aria-label={t("Widgets")}
        className="app-scroll hidden border-r border-border bg-bg-subtle p-3 lg:block lg:min-h-0 lg:overflow-y-auto"
      >
        <p className="mb-1.5 px-1 text-[11px] font-semibold uppercase tracking-wide text-fg-subtle">
          {t("Widgets")}
        </p>
        <ul className="space-y-1">
          {WIDGETS.map((w) => (
            <li key={w.label}>
              <button
                type="button"
                draggable
                className="group flex w-full items-center gap-2 rounded-lg border border-transparent px-2 py-1.5 text-left text-sm text-fg-muted transition hover:border-border hover:bg-surface hover:text-fg"
              >
                <w.icon size={15} aria-hidden />
                <span className="flex-1">{t(w.label)}</span>
                <GripVertical size={13} aria-hidden className="opacity-0 group-hover:opacity-60" />
              </button>
            </li>
          ))}
        </ul>
      </aside>

      <main aria-label={t("Page")} className="app-scroll min-w-0 bg-bg lg:min-h-0 lg:overflow-y-auto">
        <div className="mx-auto max-w-4xl space-y-4 p-5">
          <div className="flex items-baseline justify-between">
            <h2 className="text-xl font-semibold tracking-tight text-fg">{t("Household year in review")}</h2>
            <span className="text-xs text-fg-subtle">{t("Drop widgets anywhere on the grid")}</span>
          </div>
          <div className="grid grid-cols-6 gap-3">
            <Cell id="hours" span="col-span-2" selected={selected} onSelect={setSelected}>
              <Stat label={t("Hours watched")} value="1,054" />
            </Cell>
            <Cell id="plays" span="col-span-2" selected={selected} onSelect={setSelected}>
              <Stat label={t("Plays")} value="1,287" />
            </Cell>
            <Cell id="viewers" span="col-span-2" selected={selected} onSelect={setSelected}>
              <Stat label={t("Viewers")} value="6" />
            </Cell>
            <Cell id="months" span="col-span-6" selected={selected} onSelect={setSelected}>
              <p className="mb-3 text-sm font-semibold text-fg">{t("Hours by month")}</p>
              <Columns
                ariaLabel={t("Hours by month")}
                height={120}
                data={MONTHS.map((m, i) => ({ key: m, label: m, value: HOURS[i]! }))}
              />
            </Cell>
            <Cell id="top" span="col-span-6 sm:col-span-4" selected={selected} onSelect={setSelected}>
              <p className="mb-3 text-sm font-semibold text-fg">{t("Most watched")}</p>
              <div className="grid grid-cols-6 gap-2">
                {(top?.items ?? []).slice(0, 6).map((it) => (
                  <PosterCard key={it.key} item={it} showMeta={false} />
                ))}
              </div>
            </Cell>
            <Cell id="people" span="col-span-6 sm:col-span-2" selected={selected} onSelect={setSelected}>
              <p className="mb-2 text-sm font-semibold text-fg">{t("Who watched most")}</p>
              <table className="w-full text-sm">
                <tbody>
                  {[
                    ["Alex", "312 h"],
                    ["Lucía", "241 h"],
                    ["Marc", "198 h"],
                    ["Sam", "144 h"],
                  ].map(([n, h]) => (
                    <tr key={n} className="border-b border-border-subtle last:border-0">
                      <td className="py-1.5 text-fg">{n}</td>
                      <td className="py-1.5 text-right tabular-nums text-fg-muted">{h}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </Cell>
          </div>
        </div>
      </main>

      <aside
        aria-label={t("Widget settings")}
        className="app-scroll space-y-4 border-t border-border bg-bg-subtle p-4 lg:min-h-0 lg:overflow-y-auto lg:border-l lg:border-t-0"
      >
        <div className="flex items-center gap-2">
          <BarChart3 size={16} className="text-fg-muted" aria-hidden />
          <h2 className="text-sm font-semibold text-fg">{t("Chart")}</h2>
          <Badge variant="secondary" className="ml-auto">
            {t("6 columns wide")}
          </Badge>
        </div>
        <Field label={t("Title")}>
          <Input defaultValue={t("Hours by month")} aria-label={t("Title")} />
        </Field>
        <Field label={t("Shows")}>
          <SearchSelect
            value="hours-by-month"
            onChange={() => undefined}
            options={[
              { value: "hours-by-month", label: t("Hours by month · from the Group block") },
              { value: "plays-by-day", label: t("Plays by weekday · from the Group block") },
            ]}
            ariaLabel={t("Shows")}
          />
        </Field>
        <Field label={t("Style")}>
          <SearchSelect
            value="columns"
            onChange={() => undefined}
            options={[
              { value: "columns", label: t("Columns") },
              { value: "line", label: t("Line") },
              { value: "bars", label: t("Bars") },
            ]}
            ariaLabel={t("Style")}
          />
        </Field>
      </aside>
    </div>
  );
}

function Cell({
  id,
  span,
  selected,
  onSelect,
  children,
}: {
  id: WidgetId;
  span: string;
  selected: WidgetId;
  onSelect: (id: WidgetId) => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={() => onSelect(id)}
      className={cn(
        "flex flex-col items-stretch rounded-xl border bg-surface p-4 text-left transition focus-visible:outline-none",
        span,
        selected === id
          ? "border-primary/60 ring-2 ring-primary/25"
          : "border-border hover:border-dashed hover:border-border-strong",
      )}
    >
      {children}
    </button>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <>
      <p className="text-xs text-fg-muted">{label}</p>
      <p className="mt-1 text-2xl font-semibold tabular-nums text-fg">{value}</p>
    </>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="block space-y-1.5">
      <span className="text-xs font-medium text-fg-muted">{label}</span>
      {children}
    </label>
  );
}
