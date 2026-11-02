import { Badge, Button, Input, SearchSelect } from "@braedonsaunders/appkit-ui";
import { ArrowDown, ArrowUp, Plus, Trash2, X } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { Artwork } from "../../components/Artwork";
import { Field, Segmented } from "../../components/fields";
import { LookupPicker, TitleSearch } from "../../components/pickers";
import { titleSubtitle } from "../../components/Poster";
import { DragHandle, SortableList, arrayMove } from "../../components/Sortable";
import { t, tx } from "../../i18n";
import { searchSelectLabels } from "../../i18n/appkit-labels";
import type { LookupKind, Source, TitleCard } from "../../lib/contract";
import { isPluginDescriptor, textOf } from "../../lib/extend";
import {
  describeSource,
  isSourceComplete,
  paramField,
  paramOptions,
  type ParamField,
  type SourceDescriptor,
} from "../../lib/rules";
import { useTitle } from "../../lib/titles";

type SourceRecord = Source & Record<string, unknown>;

export function SourceEditor({
  source,
  onChange,
  onRemove,
  index,
  region,
  removable,
  descriptor,
  descriptors,
}: {
  source: Source;
  onChange: (s: Source) => void;
  onRemove: () => void;
  index: number;
  region: string;
  removable: boolean;
  descriptor?: SourceDescriptor;
  descriptors: readonly SourceDescriptor[];
}) {
  const n = index + 1;
  const label = t("Source {n}", { n });
  const plugin = descriptor ? isPluginDescriptor(descriptor) : false;
  return (
    <div className="rounded-lg border border-border bg-surface p-3" role="group" aria-label={label}>
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="flex flex-wrap items-center gap-1.5 text-sm font-medium text-fg">
            {descriptor ? textOf(descriptor.label) : source.kind}
            {plugin ? (
              <Badge variant="secondary">
                {t("From {owner}", { owner: descriptor?.owner ?? "" })}
              </Badge>
            ) : null}
          </p>
          <p className="text-xs text-fg-muted">
            {isSourceComplete(source) && source.kind !== "catalog"
              ? describeSource(source, descriptors)
              : descriptor
                ? textOf(descriptor.hint)
                : undefined}
          </p>
        </div>
        {removable ? (
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="size-8 shrink-0 text-fg-muted hover:text-danger"
            onClick={onRemove}
            aria-label={t("Remove source {n}", { n })}
          >
            <Trash2 size={15} />
          </Button>
        ) : null}
      </div>
      <div className="mt-2 empty:hidden">
        <SourceBody
          source={source as SourceRecord}
          descriptor={descriptor}
          onChange={onChange}
          region={region}
          label={label}
        />
      </div>
    </div>
  );
}

/**
 * Params are edited through the widget family the descriptor names
 * (meta.editor). WS1f's served-schema renderer will replace the named
 * editors; until then each renders the descriptor's params (labels,
 * options, placeholders) with the existing field components, and unknown
 * editors fall back to a generic params form so plugin kinds stay usable.
 */
function SourceBody({
  source,
  descriptor,
  onChange,
  region,
  label,
}: {
  source: SourceRecord;
  descriptor?: SourceDescriptor;
  onChange: (s: Source) => void;
  region: string;
  label: string;
}) {
  const set = (patch: Record<string, unknown>) => onChange({ ...source, ...patch } as Source);
  switch (descriptor?.meta?.editor) {
    case "none":
      return null;
    case "tmdbChart": {
      const all = paramOptions(paramField(descriptor, "list"));
      const mediaType = source.mediaType === "show" ? "show" : "movie";
      const lists = all.filter((l) => !l.media || l.media.includes(mediaType));
      return (
        <div className="flex flex-wrap items-center gap-2">
          <Segmented
            ariaLabel={t("{label} media", { label })}
            size="sm"
            value={mediaType}
            onChange={(m) =>
              set({
                mediaType: m,
                list: all.find((l) => l.value === source.list)?.media?.includes(m)
                  ? source.list
                  : "popular",
              })
            }
            options={paramOptions(paramField(descriptor, "mediaType")).map((o) => ({
              value: o.value,
              label: o.label,
            }))}
          />
          <div className="min-w-48 flex-1">
            <SearchSelect
              value={String(source.list ?? "")}
              onChange={(v) => v && set({ list: v })}
              options={lists.map((l) => ({ value: l.value, label: l.label }))}
              ariaLabel={t("{label} chart", { label })}
              searchable={false}
              {...searchSelectLabels()}
            />
          </div>
        </div>
      );
    }
    case "lookup": {
      const field = paramField(descriptor, "id");
      const lookup = (field?.lookup ?? "keyword") as LookupKind;
      const id = typeof source.id === "number" ? source.id : 0;
      const name = typeof source.label === "string" ? source.label : undefined;
      const picked = id > 0 ? [{ id, name: name ?? `#${id}` }] : [];
      const regionField = paramField(descriptor, "region");
      return (
        <div className="space-y-2">
          <LookupPicker
            kind={lookup}
            multiple={false}
            region={regionField ? String(source.region ?? "") || region : region}
            value={picked}
            ariaLabel={t("{label} search", { label })}
            onChange={(p) => set({ id: p[0]?.id ?? 0, label: p[0]?.name })}
          />
          {descriptor.meta?.params
            ?.filter((f) => f.kind === "select" && f.name !== "id")
            .map((f) => (
              <ParamInput key={f.name} field={f} source={source} set={set} label={label} />
            ))}
          {regionField ? (
            <label className="flex items-center gap-2 text-xs text-fg-muted">
              {textOf(regionField.label)}
              <Input
                value={String(source.region ?? "")}
                placeholder={region}
                maxLength={2}
                className="h-8 w-16 uppercase"
                aria-label={t("{label} region", { label })}
                onChange={(e) => set({ region: e.currentTarget.value.toUpperCase() })}
              />
            </label>
          ) : null}
        </div>
      );
    }
    case "urlList": {
      const field = paramField(descriptor, "url");
      const url = String(source.url ?? "");
      const bad = url.trim() !== "" && !isSourceComplete(source as Source);
      return (
        <div className="space-y-1">
          <Input
            type="url"
            value={url}
            placeholder={field?.placeholder}
            aria-label={t("{label} URL", { label })}
            aria-invalid={bad || undefined}
            onChange={(e) => set({ url: e.currentTarget.value })}
          />
          {bad ? (
            <p className="text-xs text-danger">
              {t("Enter a full https:// link to a public list.")}
            </p>
          ) : source.kind === "traktList" ? (
            <p className="text-xs text-fg-muted">
              {tx("Uses the Trakt account connected in {link}.", {
                link: (
                  <Link to="/services/trakt" className="text-primary underline">
                    {t("Integrations")}
                  </Link>
                ),
              })}
            </p>
          ) : source.kind === "mdblist" ? (
            <p className="text-xs text-fg-muted">
              {tx("Uses the MDBList account connected in {link}.", {
                link: (
                  <Link to="/services/ratings" className="text-primary underline">
                    {t("Integrations")}
                  </Link>
                ),
              })}
            </p>
          ) : null}
        </div>
      );
    }
    case "listSelect":
      return (
        <SearchSelect
          value={String(source.list ?? "")}
          onChange={(v) => v && set({ list: v })}
          options={paramOptions(paramField(descriptor, "list"))}
          ariaLabel={t("{label} list", { label })}
          searchable={false}
          {...searchSelectLabels()}
        />
      );
    case "plugin": {
      const params = (source.params ?? {}) as Record<string, string>;
      return (
        <GenericParams
          descriptor={descriptor}
          source={params}
          label={label}
          set={(patch) => {
            const next = { ...params };
            for (const [key, value] of Object.entries(patch)) next[key] = String(value);
            set({ params: next });
          }}
        />
      );
    }
    case "discover":
      return (
        <DiscoverParams
          params={(source.params ?? {}) as Record<string, string>}
          onChange={(params) => set({ params })}
          label={label}
        />
      );
    case "titles":
      return (
        <HandPicked keys={(source.keys ?? []) as string[]} onChange={(keys) => set({ keys })} />
      );
    default:
      return <GenericParams descriptor={descriptor} source={source} set={set} label={label} />;
  }
}

/** One served param field, for kinds without a named editor yet. */
function ParamInput({
  field,
  source,
  set,
  label,
}: {
  field: ParamField;
  source: Record<string, unknown>;
  set: (patch: Record<string, unknown>) => void;
  label: string;
}) {
  const value = source[field.name];
  if (field.kind === "select") {
    return (
      <Field label={textOf(field.label)}>
        <SearchSelect
          value={String(value ?? "")}
          onChange={(v) => v !== null && set({ [field.name]: v })}
          options={paramOptions(field)}
          ariaLabel={t("{label} {field}", { label, field: textOf(field.label) })}
          searchable={false}
          {...searchSelectLabels()}
        />
      </Field>
    );
  }
  if (field.kind === "bool") {
    return (
      <label className="flex items-center gap-2 text-sm text-fg">
        <input
          type="checkbox"
          checked={value === true || value === "true"}
          onChange={(e) => set({ [field.name]: e.currentTarget.checked })}
        />
        {textOf(field.label)}
      </label>
    );
  }
  return (
    <Field label={textOf(field.label)} hint={field.hint ? textOf(field.hint) : undefined}>
      <Input
        type={field.kind === "number" ? "number" : field.kind === "url" ? "url" : "text"}
        value={value === undefined || value === null ? "" : String(value)}
        placeholder={field.placeholder}
        aria-label={t("{label} {field}", { label, field: textOf(field.label) })}
        onChange={(e) =>
          set({
            [field.name]:
              field.kind === "number" ? Number(e.currentTarget.value) : e.currentTarget.value,
          })
        }
      />
    </Field>
  );
}

function GenericParams({
  descriptor,
  source,
  set,
  label,
}: {
  descriptor?: SourceDescriptor;
  source: Record<string, unknown>;
  set: (patch: Record<string, unknown>) => void;
  label: string;
}) {
  const params = descriptor?.meta?.params ?? [];
  if (!params.length) return null;
  return (
    <div className="space-y-2">
      {params.map((f) => (
        <ParamInput key={f.name} field={f} source={source} set={set} label={label} />
      ))}
    </div>
  );
}

function DiscoverParams({
  params,
  onChange,
  label,
}: {
  params: Record<string, string>;
  onChange: (p: Record<string, string>) => void;
  label: string;
}) {
  const entries = Object.entries(params);
  const [k, setK] = useState("");
  const [v, setV] = useState("");
  return (
    <div className="space-y-2">
      {entries.length ? (
        <div className="flex flex-wrap gap-1.5">
          {entries.map(([key, value]) => (
            <Badge key={key} variant="secondary" className="gap-1 font-mono text-[11px] text-fg">
              {key}={value}
              <button
                type="button"
                aria-label={t("Remove {key}", { key })}
                onClick={() => {
                  const n = { ...params };
                  delete n[key];
                  onChange(n);
                }}
                className="rounded-full p-0.5 hover:bg-surface-hover"
              >
                <X size={11} />
              </button>
            </Badge>
          ))}
        </div>
      ) : (
        <p className="text-xs text-fg-muted">
          {tx("Add TMDB discover parameters, e.g. {first} or {second}.", {
            first: <code>with_genres=27</code>,
            second: <code>vote_average.gte=7</code>,
          })}
        </p>
      )}
      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          if (k.trim() && v.trim()) {
            onChange({ ...params, [k.trim()]: v.trim() });
            setK("");
            setV("");
          }
        }}
      >
        <Input
          value={k}
          onChange={(e) => setK(e.currentTarget.value)}
          placeholder={t("parameter")}
          aria-label={t("{label} parameter name", { label })}
          className="font-mono text-xs"
        />
        <Input
          value={v}
          onChange={(e) => setV(e.currentTarget.value)}
          placeholder={t("value")}
          aria-label={t("{label} parameter value", { label })}
          className="font-mono text-xs"
        />
        <Button
          type="submit"
          variant="outline"
          size="icon"
          className="shrink-0"
          aria-label={t("Add parameter")}
          disabled={!k.trim() || !v.trim()}
        >
          <Plus size={15} />
        </Button>
      </form>
    </div>
  );
}

/** Hand-picked titles: search to add, drag (or use the arrows) to set the order. */
function HandPicked({ keys, onChange }: { keys: string[]; onChange: (k: string[]) => void }) {
  const [known, setKnown] = useState<Record<string, TitleCard>>({});
  const move = (i: number, d: -1 | 1) => {
    const j = i + d;
    if (j < 0 || j >= keys.length) return;
    onChange(arrayMove(keys, i, j));
  };
  return (
    <div className="space-y-3">
      <TitleSearch
        excludeKeys={new Set(keys)}
        onPick={(title) => {
          setKnown((k) => ({ ...k, [title.key]: title }));
          onChange([...keys, title.key]);
        }}
      />
      {keys.length === 0 ? (
        <p className="text-xs text-fg-muted">{t("No titles yet. Search above to add some.")}</p>
      ) : (
        <SortableList
          items={keys}
          getId={(k) => k}
          onReorder={(next) => onChange(next)}
          className="space-y-1.5"
          renderItem={(key, handle, i) => (
            <PickedRow
              keyId={key}
              hint={known[key]}
              index={i}
              handle={handle}
              onUp={() => move(i, -1)}
              onDown={() => move(i, 1)}
              first={i === 0}
              last={i === keys.length - 1}
              onRemove={() => onChange(keys.filter((k) => k !== key))}
            />
          )}
        />
      )}
      {keys.length > 1 ? (
        <p className="text-[11px] text-fg-subtle">
          {t(
            "Drag the handle or use the arrows to reorder. Sort by “Source order” to keep this order.",
          )}
        </p>
      ) : null}
    </div>
  );
}

function PickedRow({
  keyId,
  hint,
  index,
  handle,
  onUp,
  onDown,
  onRemove,
  first,
  last,
}: {
  keyId: string;
  hint?: TitleCard;
  index: number;
  handle: Parameters<typeof DragHandle>[0]["handle"];
  onUp: () => void;
  onDown: () => void;
  onRemove: () => void;
  first: boolean;
  last: boolean;
}) {
  const loaded = useTitle(hint ? undefined : keyId);
  const card = hint ?? loaded;
  const name = card?.title ?? t("Title {id}", { id: keyId });
  return (
    <div className="flex items-center gap-2 rounded-md border border-border-subtle bg-bg-subtle p-1.5">
      <DragHandle handle={handle} label={t("Reorder {name}", { name })} />
      <span className="w-5 text-right text-xs tabular-nums text-fg-subtle">{index + 1}</span>
      <Artwork src={card?.poster} alt="" title="" className="h-12 w-8 shrink-0 rounded" />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm text-fg">{name}</span>
        {card ? (
          <span className="block truncate text-xs text-fg-muted">{titleSubtitle(card)}</span>
        ) : null}
      </span>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="size-7"
        onClick={onUp}
        disabled={first}
        aria-label={t("Move {name} up", { name })}
      >
        <ArrowUp size={14} />
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="size-7"
        onClick={onDown}
        disabled={last}
        aria-label={t("Move {name} down", { name })}
      >
        <ArrowDown size={14} />
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="size-7 hover:text-danger"
        onClick={onRemove}
        aria-label={t("Remove {name}", { name })}
      >
        <X size={14} />
      </Button>
    </div>
  );
}
