import { Badge, Button, Input, SettingsSection, toast } from "@braedonsaunders/appkit-ui";
import { useEffect, useState, type ReactNode } from "react";
import { Drawer } from "../../components/Drawer";
import { MutationError } from "../../components/feedback";
import { Field, SecretInput } from "../../components/fields";
import { IntegrationAppCard } from "../../components/IntegrationAppCard";
import { SaveBar } from "../../components/SaveBar";
import { localized, t } from "../../i18n";
import { api, apiFetch } from "../../lib/api";
import { useAsync } from "../../lib/hooks";
import type { PosterProviderConfig as Posters } from "../../lib/types";
export const emptyPoster: Posters = {
  enabled: false,
  source: "tmdb",
  baseUrl: "",
  apiKey: "",
  urlTemplate: "",
  profile: "",
};
export const POSTER_SERVICES = localized(() => [
  {
    id: "postersplus",
    name: "PostersPlus",
    description: t("Alternate posters from your PostersPlus server."),
    key: true,
    template: false,
  },
  {
    id: "xrdb",
    name: "XRDB",
    description: t("Posters from an XRDB server."),
    key: true,
    template: false,
  },
  {
    id: "betterposters",
    name: "BetterPosters",
    description: t("Styled posters from BetterPosters."),
    key: false,
    template: false,
  },
  {
    id: "rpdb",
    name: "RPDB",
    description: t("Rating posters from Rating Poster Database."),
    key: true,
    template: false,
  },
  {
    id: "custom",
    name: t("Custom posters"),
    description: t("Posters from a URL template you provide."),
    key: false,
    template: true,
  },
]);

export function normalizePosters(value?: Posters | null): Posters {
  const p = { ...emptyPoster, ...value };
  if (!p.version)
    return {
      ...emptyPoster,
      version: 1,
      source: p.enabled ? p.source : "tmdb",
      accounts: p.enabled ? { [p.source]: p } : {},
    };
  return {
    ...p,
    accounts: Object.fromEntries(
      Object.entries(p.accounts ?? {}).map(([id, a]) => [id, { ...emptyPoster, ...a, source: id }]),
    ),
  };
}
export function PosterAccountForm({
  value,
  onChange,
}: {
  value: Posters;
  onChange: (p: Posters) => void;
}) {
  const service = POSTER_SERVICES.find((s) => s.id === value.source)!;
  return (
    <SettingsSection title={service.name}>
      <div className="space-y-4 p-5">
        {["postersplus", "xrdb"].includes(value.source) && (
          <Field label={t("Server URL")} htmlFor="poster-url">
            <Input
              id="poster-url"
              value={value.baseUrl}
              onChange={(e) => onChange({ ...value, baseUrl: e.target.value })}
            />
          </Field>
        )}
        {value.source === "xrdb" && (
          <Field label={t("Profile")} htmlFor="poster-profile">
            <Input
              id="poster-profile"
              value={value.profile ?? ""}
              onChange={(e) => onChange({ ...value, profile: e.target.value })}
            />
          </Field>
        )}
        {(service.key || service.template) && (
          <Field label={t("API key")} htmlFor="poster-key">
            <SecretInput
              id="poster-key"
              value={value.apiKey}
              onChange={(apiKey) => onChange({ ...value, apiKey })}
            />
          </Field>
        )}
        {(service.template || value.source === "betterposters") && (
          <Field label={t("Poster URL template")} htmlFor="poster-template">
            <SecretInput
              id="poster-template"
              value={value.urlTemplate}
              onChange={(urlTemplate) => onChange({ ...value, urlTemplate })}
            />
          </Field>
        )}
      </div>
    </SettingsSection>
  );
}
export function PosterAccounts({
  locked,
  children,
  tab = "overview",
  onEnabled,
}: {
  locked: boolean;
  children?: (cards: ReactNode) => ReactNode;
  tab?: string;
  onEnabled?: (flags: Record<string, boolean>) => void;
}) {
  const config = useAsync(async () => normalizePosters((await api.getConfig()).posters), []);
  const [draft, setDraft] = useState<Posters | null>(null);
  const [editing, setEditing] = useState<Posters | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [saving, setSaving] = useState(false);
  useEffect(() => {
    if (config.data)
      onEnabled?.(
        Object.fromEntries(
          POSTER_SERVICES.map((s) => [s.id, !!config.data?.accounts?.[s.id]?.enabled]),
        ),
      );
  }, [config.data, onEnabled]);
  useEffect(() => {
    setEditing(config.data?.accounts?.[tab] ?? null);
  }, [tab, config.data]);
  async function save(account: Posters) {
    if (!config.data) return;
    setSaving(true);
    setError(null);
    try {
      const next = {
        ...config.data,
        accounts: { ...config.data.accounts, [account.source]: account },
      };
      if (!account.enabled && next.source === account.source) next.source = "tmdb";
      const result = await apiFetch<{ value: Posters }>("/config/posters", {
        method: "PUT",
        body: JSON.stringify(posterPayload(next)),
      });
      config.setData(normalizePosters(result.value));
      setDraft(null);
      toast.success(t("Poster provider saved"));
    } catch (e) {
      setError(e);
    } finally {
      setSaving(false);
    }
  }
  const cards = POSTER_SERVICES.map((s) => (
    <IntegrationAppCard
      key={s.id}
      identity={{ id: s.id, name: s.name }}
      description={s.description}
      enabled={!!config.data?.accounts?.[s.id]?.enabled}
      disabled={locked || saving || !config.data}
      status={
        <Badge variant={config.data?.accounts?.[s.id]?.enabled ? "success" : "secondary"}>
          {config.data?.accounts?.[s.id]?.enabled ? t("On") : t("Off")}
        </Badge>
      }
      onToggle={(on) => {
        const value = {
          ...emptyPoster,
          ...config.data?.accounts?.[s.id],
          source: s.id,
          enabled: on,
        };
        if (on) setDraft(value);
        else void save(value);
      }}
      configureHref={config.data?.accounts?.[s.id]?.enabled ? "/services/" + s.id : undefined}
    />
  ));
  return (
    <>
      {children?.(cards)}
      {editing && (
        <>
          <PosterAccountForm value={editing} onChange={setEditing} />
          <SaveBar
            dirty={JSON.stringify(editing) !== JSON.stringify(config.data?.accounts?.[tab])}
            saving={saving}
            error={error}
            onSave={() => save(editing)}
            onDiscard={() => setEditing(config.data?.accounts?.[tab] ?? null)}
          />
        </>
      )}
      <Drawer
        open={!!draft}
        onClose={() => {
          if (!saving) setDraft(null);
        }}
        title={t("Set up {app}", {
          app: POSTER_SERVICES.find((s) => s.id === draft?.source)?.name ?? "",
        })}
        footer={
          <Button disabled={saving} onClick={() => draft && void save(draft)}>
            {t("Save and enable")}
          </Button>
        }
      >
        {error ? <MutationError error={error} /> : null}
        {draft && <PosterAccountForm value={draft} onChange={setDraft} />}
      </Drawer>
    </>
  );
}

export function posterPayload(p: Posters) {
  return {
    ...p,
    accounts: Object.fromEntries(
      Object.entries(p.accounts ?? {}).map(([id, a]) => [
        id,
        {
          enabled: a.enabled,
          baseUrl: a.baseUrl,
          apiKey: a.apiKey,
          urlTemplate: a.urlTemplate,
          profile: a.profile ?? "",
        },
      ]),
    ),
  };
}
