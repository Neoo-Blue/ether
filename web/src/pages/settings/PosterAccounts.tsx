import { Badge, Button, buttonVariants, toast } from "@braedonsaunders/appkit-ui";
import { Link } from "react-router-dom";
import { useState, type ReactNode } from "react";
import { Drawer } from "../../components/Drawer";
import { MutationError } from "../../components/feedback";
import { Field, SecretInput } from "../../components/fields";
import { IntegrationAppCard } from "../../components/IntegrationAppCard";
import { secretConfigured } from "../../components/IntegrationAccount";
import { localized, t } from "../../i18n";
import { api, apiFetch } from "../../lib/api";
import { useAsync } from "../../lib/hooks";
import type { PosterProviderConfig as Posters } from "../../lib/types";

const empty: Posters = {
  enabled: false,
  source: "postersplus",
  baseUrl: "",
  apiKey: "",
  urlTemplate: "",
  profile: "",
};

const SERVICES = localized(() => [
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

function normalize(value?: Posters | null): Posters {
  return { ...empty, ...value, source: value?.source || "postersplus" };
}

function active(posters: Posters, id: string) {
  return posters.enabled && posters.source === id;
}

/**
 * Poster-service keys. Artwork keeps which style to use and links here.
 * `children` receives one card per service so the parent can place them in
 * the same grid as the other cards in the group.
 */
export function PosterAccounts({
  locked,
  children,
}: {
  locked: boolean;
  children: (cards: ReactNode) => ReactNode;
}) {
  const config = useAsync(async () => normalize((await api.getConfig()).posters), []);
  const posters = config.data ? normalize(config.data) : null;
  const [draft, setDraft] = useState<Posters | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [saving, setSaving] = useState(false);

  const save = async (next: Posters) => {
    setSaving(true);
    setError(null);
    try {
      const result = await apiFetch<{ value: Posters }>("/config/posters", {
        method: "PUT",
        body: JSON.stringify(next),
      });
      config.setData(normalize(result.value));
      setDraft(null);
      toast.success(t("Poster provider saved"));
    } catch (e) {
      setError(e);
    } finally {
      setSaving(false);
    }
  };

  if (!posters) return children(null);
  const service = SERVICES.find((item) => item.id === draft?.source);
  return (
    <>
      {children(
        SERVICES.map((item) => {
          const on = active(posters, item.id);
          const keyed = secretConfigured(posters.apiKey) || secretConfigured(posters.urlTemplate);
          return (
            <IntegrationAppCard
              key={item.id}
              identity={{ id: item.id, name: item.name }}
              description={item.description}
              status={
                !on ? (
                  <Badge variant="secondary">{t("Not in use")}</Badge>
                ) : keyed || !item.key ? (
                  <Badge variant="success">{keyed ? t("Key saved") : t("In use")}</Badge>
                ) : (
                  <Badge variant="warning">{t("Needs an API key")}</Badge>
                )
              }
              action={
                on && !item.key && !item.template ? undefined : on ? (
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={locked || saving}
                    onClick={() => {
                      setError(null);
                      setDraft({ ...posters });
                    }}
                  >
                    {t("Manage")}
                  </Button>
                ) : (
                  <Link
                    to="/metadata/artwork"
                    className={buttonVariants({ variant: "outline", size: "sm" })}
                  >
                    {t("Choose in Artwork")}
                  </Link>
                )
              }
            />
          );
        }),
      )}
      <Drawer
        open={draft !== null}
        onClose={() => {
          if (saving) return;
          setDraft(null);
        }}
        title={service?.name ?? t("Poster service")}
        description={t("The key for the poster service Artwork uses.")}
        footer={
          <div className="flex w-full flex-wrap items-center gap-2">
            <span className="flex-1" />
            <Button variant="ghost" size="sm" onClick={() => setDraft(null)} disabled={saving}>
              {t("Cancel")}
            </Button>
            <Button size="sm" disabled={saving || !draft} onClick={() => draft && void save(draft)}>
              {saving ? t("Saving…") : t("Save")}
            </Button>
          </div>
        }
      >
        {draft && service ? (
          <div className="space-y-3">
            {error ? <MutationError error={error} /> : null}
            {service.key ? (
              <Field label={t("API key")} htmlFor="poster-account-key">
                <SecretInput
                  id="poster-account-key"
                  value={draft.apiKey}
                  onChange={(apiKey) => setDraft({ ...draft, apiKey })}
                />
              </Field>
            ) : null}
            {service.template ? (
              <Field
                label={t("Poster URL template")}
                htmlFor="poster-account-template"
                hint={t("Put the key in the template where the service expects it.")}
              >
                <SecretInput
                  id="poster-account-template"
                  value={draft.urlTemplate}
                  onChange={(urlTemplate) => setDraft({ ...draft, urlTemplate })}
                />
              </Field>
            ) : null}
          </div>
        ) : null}
      </Drawer>
    </>
  );
}
