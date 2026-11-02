import { SettingsSection, Spinner, SubtabNav, toast } from "@braedonsaunders/appkit-ui";
import { Segmented } from "../../components/fields";
import { MoreOptions } from "../../components/ConfigSection";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useCallback, useState } from "react";
import { useApp } from "../../app-context";
import { SaveBar, useDraft } from "../../components/SaveBar";
import { ErrorPanel } from "../../components/feedback";
import { secretConfigured } from "../../components/IntegrationAccount";
import { t, tx } from "../../i18n";
import { api, ApiError, errorMessage } from "../../lib/api";
import { useAsync } from "../../lib/hooks";
import { isRuleComplete } from "../../lib/rules";
import { asObj, SchemaField, type FieldGroup } from "../settings/SchemaForm";
import { CatalogPickers, isCatalogPicker } from "./CatalogPickers";
import { CATALOG } from "../settings/schema";
import { HomeReleaseStatus, UpcomingEpisodeSettings } from "./HomeReleaseSettings";
import { WatchlistAlerts } from "../viewers/sections/WatchlistDefaults";
import { NewReleasesSettings, useAvailabilityDraft } from "./NewReleasesSettings";
import { RulesPanel, type VisibilityRule } from "./LibraryVisibility";
import { TrendingStatus } from "./TrendingStatus";
import { LevelBadge } from "../../components/status";
import { SettingsRow } from "../../components/SettingsRow";
import "./catalog.css";

type Obj = Record<string, unknown>;

function TmdbAccount({ apiKey }: { apiKey: unknown }) {
  if (secretConfigured(apiKey)) {
    return (
      <p className="text-sm">
        <Link to="/services/tmdb" className="text-primary underline">
          {t("TMDB connected")}
        </Link>
      </p>
    );
  }
  return (
    <p className="text-sm text-fg-muted">
      {tx("Connect TMDB in {link}.", {
        link: (
          <Link to="/services/tmdb" className="text-primary underline">
            {t("Integrations")}
          </Link>
        ),
      })}
    </p>
  );
}

function briefGroup(g: FieldGroup): FieldGroup {
  return {
    ...g,
    description: undefined,
    fields: g.fields.map((f) =>
      "help" in f && typeof f.help === "string" && f.help.length > 90
        ? { ...f, help: undefined }
        : f,
    ),
  };
}

/** Off / Some / Most for catalog.minPopularity + catalog.minVotes. Some is the default. */
const OBSCURE_PRESETS = {
  off: { minPopularity: 0, minVotes: 0 },
  some: { minPopularity: 5, minVotes: 100 },
  most: { minPopularity: 15, minVotes: 500 },
} as const;
type ObscurePreset = keyof typeof OBSCURE_PRESETS;

function obscurePreset(draft: Obj): ObscurePreset | "custom" {
  const pop = Number(draft.minPopularity ?? 0);
  const votes = Number(draft.minVotes ?? 0);
  for (const [key, v] of Object.entries(OBSCURE_PRESETS))
    if (v.minPopularity === pop && v.minVotes === votes) return key as ObscurePreset;
  return "custom";
}

function ObscureTitles({ draft, onChange }: { draft: Obj; onChange: (next: Obj) => void }) {
  const preset = obscurePreset(draft);
  return (
    <SettingsRow
      title={t("Hide obscure titles")}
      description={preset === "custom" ? t("Custom limits are set under More options.") : undefined}
      control={
        <Segmented
          ariaLabel={t("Hide obscure titles")}
          value={preset === "custom" ? "" : preset}
          onChange={(v) => onChange({ ...draft, ...OBSCURE_PRESETS[v as ObscurePreset] })}
          options={[
            { value: "off", label: t("Off") },
            { value: "some", label: t("Some") },
            { value: "most", label: t("Most") },
          ]}
        />
      }
    />
  );
}

export function CatalogSettings() {
  const { tab } = useParams();
  const navigate = useNavigate();
  const section = tab === "visibility" ? "rules" : "library";
  const { health, refreshDashboard } = useApp();
  const config = useAsync(() => api.getConfig(), []);
  const stored = config.data ? asObj(config.data.catalog) : null;
  const { draft, setDraft, dirty, reset } = useDraft<Obj>(stored);
  const [error, setError] = useState<unknown>(null);
  const [saving, setSaving] = useState(false);
  const rules = (Array.isArray(draft?.exclusions) ? draft.exclusions : []) as VisibilityRule[];
  const fallback = draft?.visibilityDefault === "hide" ? "hide" : "show";
  const rulesReady = rules.every((g) => g.rules.length > 0 && g.rules.every(isRuleComplete));
  const save = useCallback(async () => {
    if (!draft) return;
    setSaving(true);
    setError(null);
    try {
      await api.putSection("catalog", draft);
      toast.success(t("Settings saved"));
      await config.reload();
      void refreshDashboard();
    } catch (e) {
      setError(e);
      if (!(e instanceof ApiError)) toast.error(errorMessage(e));
    } finally {
      setSaving(false);
    }
  }, [config, draft, refreshDashboard]);
  const availability = useAvailabilityDraft();
  const groups = [...CATALOG, UpcomingEpisodeSettings].map(briefGroup);
  return (
    <div className="catalog-scroll app-scroll">
      <div className="space-y-5 p-5 lg:p-7">
        <SubtabNav
          ariaLabel={t("Catalog settings")}
          active={section}
          onSelect={(key) =>
            navigate(key === "rules" ? "/catalog/visibility" : "/catalog/settings")
          }
          tabs={[
            { key: "library", label: t("General") },
            { key: "rules", label: t("Visibility") },
          ]}
        />
        {config.error ? (
          <ErrorPanel error={config.error} onRetry={() => void config.reload()} />
        ) : null}
        {!draft ? (
          <div className="py-10 text-center">
            <Spinner size={20} label={t("Loading settings")} />
          </div>
        ) : section === "rules" ? (
          <RulesPanel
            rules={rules}
            fallback={fallback}
            region={String(draft.region ?? "")}
            onRules={(next) => setDraft({ ...draft, exclusions: next })}
            onFallback={(value) => setDraft({ ...draft, visibilityDefault: value })}
          />
        ) : (
          <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_300px]">
            <div className="space-y-5">
              <TmdbAccount apiKey={draft.tmdbKey} />
              {availability.draft ? (
                <NewReleasesSettings
                  availability={availability.draft}
                  onAvailability={availability.setDraft}
                  gate={Boolean(draft.gateMoviesByHomeRelease)}
                  onGate={(v) => setDraft({ ...draft, gateMoviesByHomeRelease: v })}
                />
              ) : null}
              {groups.map((g, i) => (
                <SettingsSection key={g.id ?? i} title={g.title}>
                  {g.id === "browsable" ? (
                    <ObscureTitles draft={draft} onChange={setDraft} />
                  ) : null}
                  {g.fields.map((f, j) =>
                    f.kind === "group" || isCatalogPicker(f.key) ? null : (
                      <SchemaField
                        key={`${f.key}-${j}`}
                        def={f}
                        value={draft[f.key]}
                        path={`catalog.${f.key}`}
                        errors={{}}
                        onChange={(v) => setDraft({ ...draft, [f.key]: v })}
                      />
                    ),
                  )}
                  {g.fields.some((f) => isCatalogPicker(f.key)) ? (
                    <CatalogPickers
                      language={draft.language}
                      region={draft.region}
                      refreshInterval={draft.refreshInterval}
                      onChange={(key, value) => setDraft({ ...draft, [key]: value })}
                    />
                  ) : null}
                  {g.more?.length ? (
                    <MoreOptions label={t("More options: {title}", { title: g.title ?? "" })}>
                      {g.more.map((f, j) => (
                        <SchemaField
                          key={`more-${f.key}-${j}`}
                          def={f}
                          value={draft[f.key]}
                          path={`catalog.${f.key}`}
                          errors={{}}
                          onChange={(v) => setDraft({ ...draft, [f.key]: v })}
                        />
                      ))}
                    </MoreOptions>
                  ) : null}
                </SettingsSection>
              ))}
              <WatchlistAlerts />
            </div>
            <aside className="space-y-5">
              <HomeReleaseStatus />
              <TrendingStatus />
              {health.find((h) => h.id === "catalog") ? (
                <SettingsSection title={t("Catalog health")}>
                  <SettingsRow
                    title={t("TMDB catalog")}
                    control={
                      <LevelBadge
                        level={health.find((h) => h.id === "catalog")!.level}
                        label={health.find((h) => h.id === "catalog")!.summary}
                      />
                    }
                  />
                </SettingsSection>
              ) : null}
            </aside>
          </div>
        )}
        <SaveBar
          dirty={dirty}
          saving={saving}
          error={error}
          disabled={dirty && !rulesReady}
          onSave={() => void save()}
          onDiscard={() => {
            reset();
            setError(null);
          }}
        />
        {availability.saveBar}
      </div>
    </div>
  );
}
