import { t } from "../../i18n";
import { Button, Input, Select, Switch } from "@braedonsaunders/appkit-ui";
import { useEffect, useId, useState } from "react";
import { Drawer } from "../../components/Drawer";
import { Field, NumberInput, SecretInput } from "../../components/fields";
import { MutationError } from "../../components/feedback";
import { isPluginDescriptor, textOf, useRegistry, type Descriptor } from "../../lib/extend";
import {
  channelKindDescriptor,
  channelKindDescriptors,
  criticalEventIds,
  defaultSettingsForKind,
  eventAllowedForKind,
  eventKindDescriptor,
  eventKindDescriptors,
  transportLabel,
  type Channel,
  type SettingsField,
} from "./types";

/** Render the served settings schema for a channel kind. */
function ChannelSchemaFields({
  idPrefix,
  fields,
  channel,
  onChange,
}: {
  idPrefix: string;
  fields: readonly SettingsField[];
  channel: Channel;
  onChange: (patch: Partial<Channel>) => void;
}) {
  const visible = fields.filter((f) => f.kind !== "hidden");
  return (
    <>
      {visible.map((f) => {
        const fieldId = `${idPrefix}-${f.key}`;
        const label = textOf(f.label);
        const hint = textOf(f.hint) || undefined;
        const rawValue = f.secret
          ? (channel.secrets?.[f.key] ?? "")
          : (channel.settings?.[f.key] ?? "");
        const value =
          rawValue ||
          (f.key === "smtpMode" && channel.secrets?.endpoint?.endsWith(":465")
            ? "tls"
            : (f.default ?? ""));
        const setField = (val: string) => {
          if (f.secret) {
            onChange({ secrets: { ...channel.secrets, [f.key]: val } });
          } else {
            onChange({ settings: { ...channel.settings, [f.key]: val } });
          }
        };
        return (
          <Field
            key={f.key}
            label={label}
            hint={hint}
            htmlFor={fieldId}
            className={f.wide ? "sm:col-span-2" : undefined}
          >
            {f.kind === "select" ? (
              <Select
                id={fieldId}
                aria-label={label}
                value={value}
                onChange={(e) => setField(e.target.value)}
              >
                {(f.options ?? []).map((opt) => (
                  <option key={opt.value} value={opt.value}>
                    {textOf(opt.label)}
                  </option>
                ))}
              </Select>
            ) : f.kind === "number" ? (
              <NumberInput
                id={fieldId}
                min={f.min}
                max={f.max}
                value={Number(value || 0)}
                onChange={(n) => setField(String(n))}
              />
            ) : f.kind === "boolean" ? (
              <Switch
                id={fieldId}
                aria-label={label}
                checked={value === "true"}
                onChange={(e) => setField(e.target.checked ? "true" : "false")}
              />
            ) : (f.secret && f.kind !== "text") || f.key === "endpoint" ? (
              <SecretInput
                id={fieldId}
                value={value}
                placeholder={f.placeholder}
                onChange={setField}
              />
            ) : (
              <Input
                id={fieldId}
                value={value}
                placeholder={f.placeholder}
                onChange={(e) => setField(e.target.value)}
              />
            )}
          </Field>
        );
      })}
    </>
  );
}

/** Create or edit one notification channel. */
export function ChannelDrawer({
  open,
  channel: initial,
  isNew,
  descriptors: providedDescriptors,
  saving,
  error,
  onClose,
  onSave,
}: {
  open: boolean;
  channel: Channel;
  isNew: boolean;
  descriptors?: readonly Descriptor[];
  saving: boolean;
  error: unknown;
  onClose: () => void;
  onSave: (channel: Channel) => void;
}) {
  const id = useId();
  const registry = useRegistry("channelKind", "eventKind");
  const descriptors = providedDescriptors?.length ? providedDescriptors : registry.descriptors;
  const [channel, setChannel] = useState(initial);
  useEffect(() => setChannel(initial), [initial]);
  const patch = (p: Partial<Channel>) => setChannel((v) => ({ ...v, ...p }));

  const channelKinds = channelKindDescriptors(descriptors);
  const selectableKinds = channelKinds.filter((d) => d.meta?.selectable !== false);
  const currentKind = channelKindDescriptor(descriptors, channel.kind);
  const eventKinds = eventKindDescriptors(descriptors);
  const kindLocked =
    (currentKind ? currentKind.meta?.selectable === false : channel.kind === "webpush") || !isNew;

  const submit = () => {
    const settings = defaultSettingsForKind(
      descriptors,
      channel.kind,
      channel.settings,
      channel.secrets,
    );
    onSave({
      ...channel,
      ...(Object.keys(settings).length ? { settings } : {}),
    });
  };

  return (
    <Drawer
      open={open}
      onClose={onClose}
      title={isNew ? t("New notification channel") : t("Edit notification channel")}
      description={t(
        "Saved endpoints and credentials show as [configured]; leave them as they are to keep them.",
      )}
      size="lg"
      footer={
        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose} disabled={saving}>
            {t("Cancel")}
          </Button>
          <Button onClick={submit} disabled={saving}>
            {saving ? t("Saving…") : t("Save channel")}
          </Button>
        </div>
      }
    >
      <div className="space-y-6">
        {error ? <MutationError error={error} /> : null}
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={t("Name")} htmlFor={`${id}-name`}>
            <Input
              id={`${id}-name`}
              value={channel.name}
              placeholder={transportLabel(channel.kind, descriptors)}
              onChange={(e) => patch({ name: e.target.value })}
            />
          </Field>
          <Field label={t("Transport")} htmlFor={`${id}-kind`}>
            <Select
              id={`${id}-kind`}
              aria-label={t("Transport")}
              value={channel.kind}
              disabled={kindLocked}
              onChange={(e) => {
                const nextKind = e.target.value;
                const nextSettings = defaultSettingsForKind(
                  descriptors,
                  nextKind,
                  channel.settings,
                  channel.secrets,
                );
                patch({
                  kind: nextKind,
                  ...(Object.keys(nextSettings).length ? { settings: nextSettings } : {}),
                  events: channel.events.filter((k) =>
                    eventAllowedForKind(eventKindDescriptor(descriptors, k), nextKind),
                  ),
                });
              }}
            >
              {!selectableKinds.some((d) => d.id === channel.kind) ? (
                <option value={channel.kind}>{transportLabel(channel.kind, descriptors)}</option>
              ) : null}
              {selectableKinds.map((d) => (
                <option key={d.id} value={d.id}>
                  {isPluginDescriptor(d)
                    ? t("{label} (from {owner})", { label: textOf(d.label), owner: d.owner })
                    : textOf(d.label)}
                </option>
              ))}
            </Select>
          </Field>
          <p className="text-sm text-fg-muted sm:col-span-2">{textOf(currentKind?.hint)}</p>
          <ChannelSchemaFields
            idPrefix={id}
            fields={currentKind?.meta?.fields ?? []}
            channel={channel}
            onChange={patch}
          />
          <Field label={t("Most deliveries per minute")} htmlFor={`${id}-rate`}>
            <NumberInput
              id={`${id}-rate`}
              min={1}
              max={60}
              value={channel.perMinute}
              onChange={(n) => patch({ perMinute: n })}
            />
          </Field>
          <Field
            label={t("Digest delay (minutes)")}
            hint={t("Collect alerts and send them together. 0 sends each one right away.")}
            htmlFor={`${id}-digest`}
          >
            <NumberInput
              id={`${id}-digest`}
              min={0}
              max={10080}
              value={channel.digestMinutes}
              onChange={(n) => patch({ digestMinutes: n })}
            />
          </Field>
        </div>

        <label className="flex items-center justify-between gap-3 rounded-lg border border-border px-4 py-3">
          <span>
            <span className="block text-sm font-medium text-fg">{t("Enabled")}</span>
            <span className="block text-xs text-fg-muted">
              {t("Turn off to stop sending without deleting the channel.")}
            </span>
          </span>
          <Switch
            checked={channel.enabled}
            onChange={(e) => patch({ enabled: e.target.checked })}
          />
        </label>

        <fieldset className="space-y-2">
          <legend className="text-sm font-medium text-fg">{t("Send these events")}</legend>
          <p className="text-xs text-fg-muted">
            {t(
              "Choose exactly what reaches this destination. Server lifecycle alerts bypass digest delays. Weekly reports are delivered by email.",
            )}
          </p>
          <div className="flex flex-wrap gap-2 py-2">
            <Button
              size="sm"
              variant="outline"
              onClick={() =>
                patch({
                  events: criticalEventIds(descriptors).filter((k) =>
                    eventAllowedForKind(eventKindDescriptor(descriptors, k), channel.kind),
                  ),
                })
              }
            >
              {t("Critical alerts")}
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={() =>
                patch({
                  events: eventKinds
                    .filter(
                      (ev) => Boolean(ev.meta?.automatic) && eventAllowedForKind(ev, channel.kind),
                    )
                    .map((ev) => ev.id),
                })
              }
            >
              {t("All automatic events")}
            </Button>
            <Button size="sm" variant="ghost" onClick={() => patch({ events: [] })}>
              {t("Clear")}
            </Button>
          </div>
          <div className="grid gap-x-6 gap-y-2 pt-1 sm:grid-cols-2">
            {eventKinds.map((ev) => {
              const k = ev.id;
              const lbl = textOf(ev.label);
              const allowed = eventAllowedForKind(ev, channel.kind);
              return (
                <label key={k} className="flex items-center justify-between gap-3 py-1 text-sm">
                  <span className="text-fg">
                    {lbl}
                    <span className="block text-xs text-fg-muted">{textOf(ev.hint)}</span>
                  </span>
                  <Switch
                    aria-label={lbl}
                    disabled={!allowed}
                    checked={
                      allowed && (channel.events.includes(k) || channel.events.includes("*"))
                    }
                    onChange={(e) =>
                      patch({
                        events: e.target.checked
                          ? [...channel.events.filter((v) => v !== "*"), k]
                          : channel.events.includes("*")
                            ? eventKinds
                                .filter(
                                  (other) =>
                                    other.id !== k && eventAllowedForKind(other, channel.kind),
                                )
                                .map((other) => other.id)
                            : channel.events.filter((v) => v !== k),
                      })
                    }
                  />
                </label>
              );
            })}
          </div>
        </fieldset>
      </div>
    </Drawer>
  );
}
