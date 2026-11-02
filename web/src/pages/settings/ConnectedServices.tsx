import { Badge, Button, SettingsSection, Spinner, toast } from "@braedonsaunders/appkit-ui";
import {
  Children,
  Fragment,
  isValidElement,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useApp } from "../../app-context";
import { Drawer } from "../../components/Drawer";
import { ErrorPanel, MutationError } from "../../components/feedback";
import { PasswordInput, TestButton, type TestState } from "../../components/fields";
import { IntegrationAppCard } from "../../components/IntegrationAppCard";
import { secretConfigured } from "../../components/IntegrationAccount";
import { SimklMark, TraktMark } from "../../components/integration-brands";
import { SaveBar } from "../../components/SaveBar";
import { LevelBadge } from "../../components/status";
import { t, tx } from "../../i18n";
import { useLocale } from "../../i18n/react";
import { api, apiFetch, ApiError, type TestFn } from "../../lib/api";
import { textOf, useRegistry, type Descriptor } from "../../lib/extend";
import type { IntegrationMeta } from "../../lib/console-descriptors";
import { useAsync } from "../../lib/hooks";
import type { EtherConfig } from "../../lib/types";
import { MarkerCoverage } from "../playback/Markers";
import { PosterAccounts } from "./PosterAccounts";
import { RatingSources, type RatingSettings } from "../metadata/Ratings";
import { SimklIntegration, type SimklSettings } from "./SimklIntegration";
import { LanguageFeatures } from "./LanguageFeatures";
import { LanguageModelSettings } from "./LanguageModel";
import {
  asObj,
  GenericFields,
  SchemaField,
  useSettingsSchema,
  type FieldGroup,
} from "./SchemaForm";

type Obj = Record<string, unknown>;

export type ServiceTab =
  | "overview"
  | "tmdb"
  | "trakt"
  | "simkl"
  | "llm"
  | "opensubtitles"
  | "ratings"
  | "introdb";

const TAB_ORDER: ServiceTab[] = [
  "overview",
  "tmdb",
  "trakt",
  "simkl",
  "llm",
  "opensubtitles",
  "ratings",
  "introdb",
];

/** Overview plus every enabled integration. A disabled URL does not grow a tab. */
export function integrationTabs(
  enabled: Partial<Record<ServiceTab, boolean>>,
): { key: ServiceTab; label: string }[] {
  const labels: Record<ServiceTab, string> = {
    overview: t("Overview"),
    tmdb: "TMDB",
    trakt: "Trakt",
    simkl: "Simkl",
    llm: t("Language model"),
    opensubtitles: "OpenSubtitles",
    ratings: t("Ratings"),
    introdb: "TheIntroDB",
  };
  return TAB_ORDER.filter(
    (key) => key === "overview" || key === "tmdb" || key === "introdb" || enabled[key],
  ).map((key) => ({
    key,
    label: labels[key],
  }));
}

interface Service {
  tab: Exclude<ServiceTab, "overview" | "ratings">;
  configKey: "trakt" | "simkl" | "llm" | "openSubtitles" | "markers";
  endpoint: string;
  /** Boolean this card's switch writes. Omitted fields stay stored. */
  flag: "enabled" | "theIntroDb";
  label: string;
  groups: FieldGroup[];
  /** Served settingsSchema id whose groups replace `groups` once loaded. */
  schemaId?: string;
  /** Simkl keeps its own account form and save. */
  custom?: boolean;
  form?: (props: {
    value: Obj;
    saved: Obj | undefined;
    onChange: (next: Obj) => void;
    errors: Record<string, string>;
    footer: ReactNode;
  }) => ReactNode;
  testLabel?: string;
  test?: (value: Obj) => ReturnType<TestFn>;
  /** Why enabling must wait. The switch stays off and the setup drawer opens. */
  blockEnable?: (value: Obj) => string | null;
  /** Field the server requires before this integration may be enabled. */
  requiredField?: "clientId" | "model";
}

/**
 * Compiled behaviour for built-in integrations, keyed by descriptor id. The
 * registry names each integration (tab, config key, endpoint, flag, required
 * field); functions and forms stay in the console, like page components.
 */
interface ServiceBehaviour {
  groups: () => FieldGroup[];
  schemaId?: string;
  testLabel?: () => string;
  test?: (value: Obj) => ReturnType<TestFn>;
  form?: Service["form"];
}

const SERVICE_BEHAVIOUR: Record<string, ServiceBehaviour> = {
  trakt: {
    groups: () => [],
    schemaId: "trakt",
    testLabel: () => t("Test Trakt"),
    test: (v) => api.testTrakt(v as EtherConfig["trakt"]),
  },
  simkl: { groups: () => [] },
  llm: {
    groups: () => [],
    form: ({ value, saved, onChange, errors, footer }) => (
      <LanguageModelSettings
        value={value as unknown as EtherConfig["llm"]}
        saved={saved as unknown as EtherConfig["llm"] | undefined}
        onChange={(next) => onChange(next as unknown as Obj)}
        errors={errors}
        footer={footer}
      />
    ),
    testLabel: () => t("Test endpoint"),
    test: (v) => api.testLLM(v as EtherConfig["llm"]),
  },
  opensubtitles: {
    groups: () => [],
    schemaId: "openSubtitles",
    testLabel: () => t("Test OpenSubtitles"),
    test: (v) => api.testOpenSubtitles(v as EtherConfig["openSubtitles"]),
  },
  introdb: {
    groups: () => [],
    schemaId: "markers",
    testLabel: () => t("Test TheIntroDB"),
    test: (v) => api.testIntroDB(String(v.apiKey ?? "")),
  },
};

/** Why enabling must wait, for the field an integration requires. */
export function missingFieldLabel(field: string): string {
  switch (field) {
    case "clientId":
      return t("Needs a client ID");
    case "model":
      return t("Needs a model");
    default:
      return t("Needs an API key");
  }
}

/** Service records for the built-in integration descriptors. */
function servicesFrom(descriptors: Descriptor<IntegrationMeta>[]): Service[] {
  const out: Service[] = [];
  for (const d of descriptors) {
    const behaviour = SERVICE_BEHAVIOUR[d.id];
    const meta = d.meta;
    if (!behaviour || !meta?.tab || !meta.configKey || !meta.endpoint || !meta.flag) continue;
    const required = meta.requiredField;
    out.push({
      tab: meta.tab as Service["tab"],
      configKey: meta.configKey as Service["configKey"],
      endpoint: meta.endpoint,
      flag: meta.flag as Service["flag"],
      label: textOf(d.label),
      groups: behaviour.groups(),
      schemaId: behaviour.schemaId,
      custom: meta.form === "simkl" || undefined,
      form: behaviour.form,
      testLabel: behaviour.testLabel?.(),
      test: behaviour.test,
      blockEnable: required
        ? (v) => (String(v[required] ?? "").trim() ? null : missingFieldLabel(required))
        : undefined,
      requiredField: (required as Service["requiredField"]) || undefined,
    });
  }
  return out;
}

function sectionBases(stored: Record<string, Obj> | null): Record<string, string> {
  if (!stored) return {};
  return Object.fromEntries(
    Object.entries(stored).map(([key, value]) => [key, JSON.stringify(value)]),
  );
}

/**
 * Per-section draft. A saved update adopts only sections whose draft still
 * matched the previous save, so toggling one integration cannot wipe edits
 * on another. This is not a change to the shared useDraft hook.
 */
function useSectionDrafts(stored: Record<string, Obj> | null) {
  const storedKey = useMemo(() => JSON.stringify(stored ?? null), [stored]);
  const [adopted, setAdopted] = useState(storedKey);
  const [bases, setBases] = useState(() => sectionBases(stored));
  const [draft, setDraft] = useState<Record<string, Obj> | null>(() =>
    stored ? structuredClone(stored) : null,
  );
  if (adopted !== storedKey) {
    const next = (() => {
      if (!stored) return null;
      if (!draft) return structuredClone(stored);
      const reconciled: Record<string, Obj> = {};
      for (const [key, value] of Object.entries(stored)) {
        const now = JSON.stringify(draft[key] ?? null);
        if (bases[key] === undefined || now === bases[key])
          reconciled[key] = structuredClone(value);
        else reconciled[key] = draft[key]!;
      }
      return reconciled;
    })();
    setAdopted(storedKey);
    setBases(sectionBases(stored));
    setDraft(next);
  }
  const dirty = draft !== null && JSON.stringify(draft) !== storedKey;
  const reset = useCallback(() => {
    setDraft(stored ? structuredClone(stored) : null);
    setBases(sectionBases(stored));
    // storedKey stands in for the stored snapshot.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [storedKey]);
  return { draft, setDraft, dirty, reset };
}

function flagOf(value: Obj | undefined, flag: Service["flag"]) {
  return value?.[flag] === true;
}

/** One persistence tail. A save that started first cannot finish over a later toggle. */
function useExclusive() {
  const tail = useRef(Promise.resolve());
  const depth = useRef(0);
  const [locked, setLocked] = useState(false);
  const enqueue = useCallback(<T,>(task: () => Promise<T>): Promise<T> => {
    depth.current += 1;
    setLocked(true);
    const run = tail.current.then(task, task);
    const finish = () => {
      depth.current -= 1;
      if (depth.current === 0) setLocked(false);
    };
    tail.current = run.then(finish, finish);
    return run;
  }, []);
  return { locked, enqueue };
}

function simklSettings(value: Obj | undefined): SimklSettings {
  return {
    enabled: value?.enabled === true,
    clientId: String(value?.clientId ?? ""),
    clientSecret: String(value?.clientSecret ?? ""),
  };
}

function textField(section: Obj, key: string, fallback: string): string {
  const value = section[key];
  return typeof value === "string" ? value : fallback;
}

/** A saved language model can translate and answer search. */
export function languageModelReady(section: Obj | undefined): boolean {
  if (!section || section.enabled !== true) return false;
  const model = textField(section, "model", "").trim();
  if (!model) return false;
  const provider = textField(section, "provider", "");
  const base = textField(section, "baseUrl", "").trim();
  return base !== "" || (provider !== "" && provider !== "custom");
}

/** Speech-to-text is a separate endpoint from the chat model. */
export function speechToTextReady(section: Obj | undefined): boolean {
  if (!section) return false;
  return (
    textField(section, "speechBaseUrl", "").trim() !== "" &&
    textField(section, "speechModel", "").trim() !== ""
  );
}

function boolField(section: Obj, key: string, fallback: boolean): boolean {
  const value = section[key];
  return typeof value === "boolean" ? value : fallback;
}

/** Overlay a saved section onto the loaded config. Missing fields keep the loaded values. */
function mergeSection(
  base: EtherConfig,
  key: Service["configKey"] | "catalog",
  section: Obj,
): EtherConfig {
  switch (key) {
    case "trakt":
      return {
        ...base,
        trakt: {
          enabled: boolField(section, "enabled", base.trakt.enabled),
          clientId: textField(section, "clientId", base.trakt.clientId),
          clientSecret: textField(section, "clientSecret", base.trakt.clientSecret),
        },
      };
    case "simkl": {
      const prev = base.simkl ?? {
        enabled: false,
        clientId: "",
        clientSecret: "",
      };
      return {
        ...base,
        simkl: {
          enabled: boolField(section, "enabled", prev.enabled),
          clientId: textField(section, "clientId", prev.clientId),
          clientSecret: textField(section, "clientSecret", prev.clientSecret),
        },
      };
    }
    case "llm":
      return {
        ...base,
        llm: {
          enabled: boolField(section, "enabled", base.llm.enabled),
          provider: textField(section, "provider", base.llm.provider),
          baseUrl: textField(section, "baseUrl", base.llm.baseUrl),
          apiKey: textField(section, "apiKey", base.llm.apiKey),
          model: textField(section, "model", base.llm.model),
          speechBaseUrl: textField(section, "speechBaseUrl", base.llm.speechBaseUrl ?? ""),
          speechApiKey: textField(section, "speechApiKey", base.llm.speechApiKey ?? ""),
          speechModel: textField(section, "speechModel", base.llm.speechModel ?? ""),
        },
      };
    case "openSubtitles":
      return {
        ...base,
        openSubtitles: {
          enabled: boolField(section, "enabled", base.openSubtitles.enabled),
          apiKey: textField(section, "apiKey", base.openSubtitles.apiKey),
          username: textField(section, "username", base.openSubtitles.username),
          password: textField(section, "password", base.openSubtitles.password),
        },
      };
    case "markers": {
      const prev = base.markers ?? { theIntroDb: false, apiKey: "" };
      return {
        ...base,
        markers: {
          theIntroDb: boolField(section, "theIntroDb", prev.theIntroDb),
          apiKey: textField(section, "apiKey", prev.apiKey),
        },
      };
    }
    case "catalog":
      return {
        ...base,
        catalog: {
          ...base.catalog,
          tmdbKey: textField(section, "tmdbKey", base.catalog.tmdbKey),
        },
      };
  }
}

/** On, off, or switched on but missing what it needs. */
function switchStatus(enabled: boolean, ready: boolean, missing: string) {
  if (!enabled) return <Badge variant="secondary">{t("Off")}</Badge>;
  return ready ? (
    <Badge variant="success">{t("On")}</Badge>
  ) : (
    <Badge variant="warning">{missing}</Badge>
  );
}

/**
 * Integrations overview and one configuration tab per enabled service.
 * Card switches persist the enable flag. A dirty form keeps its other unsaved
 * fields; turning it off stores only the flag. Tabs and card status follow the
 * saved flag. Forms share one draft, reconciled per section.
 */
export function ConnectedServices({
  tab,
  onEnabled,
}: {
  tab: string;
  /** Enabled flags only. The page derives translated labels while rendering. */
  onEnabled: (enabled: Partial<Record<ServiceTab, boolean>>) => void;
}) {
  const { refreshDashboard } = useApp();
  const navigate = useNavigate();
  const { tab: requested } = useParams();
  const config = useAsync(() => api.getConfig(), []);
  const ratings = useAsync(() => apiFetch<RatingSettings>("/discovery/ratings/settings"), []);
  const registry = useRegistry<IntegrationMeta>("integration");
  const { locale } = useLocale();
  // Services are copy-bearing (labels, test buttons), so locale is a dep.
  const services = useMemo(
    () => servicesFrom(registry.descriptors),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [registry.descriptors, locale],
  );
  const cfg = config.data;
  // i18n: memo holds no copy — only stable config keys and stored values.
  const stored = useMemo(() => {
    if (!cfg) return null;
    const raw = cfg as unknown as Obj;
    return {
      ...Object.fromEntries(services.map((s) => [s.configKey, asObj(raw[s.configKey])])),
      catalog: { tmdbKey: cfg.catalog?.tmdbKey ?? "" },
    } as Record<string, Obj>;
  }, [cfg, services]);
  const { draft, setDraft, dirty, reset } = useSectionDrafts(stored);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [error, setError] = useState<unknown>(null);
  const [saving, setSaving] = useState(false);
  const [tests, setTests] = useState<Record<string, TestState>>({});
  const [busyId, setBusyId] = useState<string | null>(null);
  const [visited, setVisited] = useState<Set<string>>(() => new Set([tab]));
  const [sectionSetup, setSectionSetup] = useState<{
    service: Service;
    draft: Obj;
    errors: Record<string, string>;
    failure: unknown;
  } | null>(null);
  const [ratingsSetup, setRatingsSetup] = useState<{
    draft: RatingSettings;
    failure: unknown;
  } | null>(null);
  const { locked, enqueue } = useExclusive();
  const skipSetupOffer = useRef(false);
  const model = useRef({
    draft: null as Record<string, Obj> | null,
    stored: null as Record<string, Obj> | null,
    ratings: undefined as RatingSettings | undefined,
    services: [] as Service[],
    tab,
  });
  model.current.draft = draft;
  model.current.stored = stored;
  model.current.ratings = ratings.data;
  model.current.services = services;
  model.current.tab = tab;

  useEffect(() => {
    setVisited((current) => (current.has(tab) ? current : new Set(current).add(tab)));
  }, [tab]);

  // i18n: memo holds no copy — only stable tab keys and boolean flags.
  const enabled = useMemo(() => {
    const flags: Partial<Record<ServiceTab, boolean>> = {};
    for (const service of services) {
      flags[service.tab] = flagOf(stored?.[service.configKey], service.flag);
    }
    flags.ratings = ratings.data?.enabled === true;
    flags.tmdb = true;
    flags.introdb = true;
    return flags;
  }, [stored, ratings.data?.enabled, services]);

  const flagSig = TAB_ORDER.map((key) => (enabled[key] ? "1" : "0")).join("");
  const reported = useRef("");
  useEffect(() => {
    if (reported.current === flagSig) return;
    reported.current = flagSig;
    onEnabled(enabled);
  }, [flagSig, enabled, onEnabled]);

  useEffect(() => {
    if (!draft) return;
    if (!requested || requested === "overview") {
      skipSetupOffer.current = false;
      return;
    }
    if (requested === "ratings" && ratings.data === undefined && !ratings.error) return;
    const key = TAB_ORDER.find((item) => item === requested);
    if (key && enabled[key]) return;
    navigate("/services/overview", { replace: true });
    if (skipSetupOffer.current) return;
    if (key === "ratings" && ratings.data && !ratings.data.enabled && !ratings.data.apiKey.trim()) {
      openRatingsSetup(ratings.data);
      return;
    }
    const service = model.current.services.find((item) => item.tab === key);
    if (service && service.blockEnable?.(draft[service.configKey] ?? {})) {
      openSectionSetup(service, draft[service.configKey] ?? {});
    }
    // Setup openers are stable setState wrappers. Re-running would reset a drawer mid-edit.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [draft, requested, enabled, ratings.data, ratings.error, navigate]);

  const runTest = useCallback((s: Service, value: Obj) => {
    if (!s.test) return;
    setTests((prev) => ({ ...prev, [s.configKey]: { status: "running" } }));
    s.test(value).then(
      (r) =>
        setTests((prev) => ({
          ...prev,
          [s.configKey]: r.ok
            ? { status: "ok", durationMs: r.durationMs }
            : { status: "fail", message: r.error ?? t("failed") },
        })),
      (e) =>
        setTests((prev) => ({
          ...prev,
          [s.configKey]: { status: "fail", message: (e as Error).message },
        })),
    );
  }, []);

  const applySection = (
    key: Service["configKey"] | "catalog",
    section: Obj,
    draftNext: Record<string, Obj>,
  ) => {
    const currentConfig = config.data;
    if (!currentConfig) return;
    const storedNext = { ...(model.current.stored ?? {}), [key]: section };
    model.current.stored = storedNext;
    model.current.draft = draftNext;
    config.setData((prev) => mergeSection(prev ?? currentConfig, key, section));
    setDraft(draftNext);
  };

  const openSectionSetup = (service: Service, seed: Obj) => {
    setRatingsSetup(null);
    setSectionSetup({
      service,
      draft: { ...seed, [service.flag]: true },
      errors: {},
      failure: null,
    });
  };

  const openRatingsSetup = (seed: RatingSettings) => {
    setSectionSetup(null);
    setRatingsSetup({ draft: { ...seed, enabled: true }, failure: null });
  };

  const save = () =>
    enqueue(async () => {
      const draftNow = model.current.draft;
      const storedNow = model.current.stored;
      if (!draftNow || !storedNow) return;
      const changed = model.current.services.filter(
        (s) =>
          !s.custom &&
          JSON.stringify(draftNow[s.configKey]) !== JSON.stringify(storedNow[s.configKey]),
      );
      const catalogChanged = JSON.stringify(draftNow.catalog) !== JSON.stringify(storedNow.catalog);
      if (!changed.length && !catalogChanged) return;
      setSaving(true);
      setError(null);
      setErrors({});
      const savedLabels: string[] = [];
      try {
        if (catalogChanged && config.data) {
          const sent = draftNow.catalog ?? {};
          const res = await api.putSection("catalog", { ...config.data.catalog, ...sent });
          const latest = model.current.draft ?? draftNow;
          const section = {
            tmdbKey: textField(asObj(res.value), "tmdbKey", String(sent.tmdbKey ?? "")),
          };
          const editedSinceSend = JSON.stringify(latest.catalog) !== JSON.stringify(sent);
          const draftNext = editedSinceSend ? latest : { ...latest, catalog: section };
          applySection("catalog", section, draftNext);
          savedLabels.push("TMDB");
        }
        for (const s of changed) {
          const sent = draftNow[s.configKey]!;
          const res = await api.putSection(s.endpoint, sent);
          const latest = model.current.draft ?? draftNow;
          const storedLatest = model.current.stored ?? storedNow;
          const section = { ...asObj(storedLatest[s.configKey]), ...asObj(res.value) };
          const editedSinceSend = JSON.stringify(latest[s.configKey]) !== JSON.stringify(sent);
          // The server accepted this body. Adopt its normalized document when
          // the user has not typed over it; otherwise keep the newer draft dirty.
          const draftNext = editedSinceSend ? latest : { ...latest, [s.configKey]: section };
          applySection(s.configKey, section, draftNext);
          savedLabels.push(s.label);
        }
        toast.success(t("Settings saved"), { description: savedLabels.join(", ") });
        void refreshDashboard();
      } catch (e) {
        setError(e);
        if (e instanceof ApiError && e.fields) {
          setErrors(Object.fromEntries(e.fields.map((f) => [f.field, f.message])));
          const failed = model.current.services.find((svc) =>
            e.fields!.some((f) => f.field.startsWith(`${svc.configKey}.`)),
          );
          if (failed) navigate(`/services/${failed.tab}`);
        }
      } finally {
        setSaving(false);
      }
    });

  const toggleService = (service: Service, next: boolean) =>
    enqueue(async () => {
      const draftNow = model.current.draft;
      const storedNow = model.current.stored;
      if (!draftNow || !storedNow) return;
      const current = draftNow[service.configKey] ?? {};
      if (!next) skipSetupOffer.current = true;
      const blocked = next ? service.blockEnable?.(current) : null;
      if (blocked) {
        openSectionSetup(service, current);
        return;
      }
      const saved = storedNow[service.configKey] ?? {};
      const sectionDirty = JSON.stringify(current) !== JSON.stringify(saved);
      const enablingDraft = next && sectionDirty;
      // Off persists the flag alone so unsaved fields stay in the draft.
      const sent = enablingDraft ? { ...current, [service.flag]: true } : { [service.flag]: next };
      setBusyId(service.tab);
      try {
        const res = await api.putSection(service.endpoint, sent);
        const storedLatest = model.current.stored ?? storedNow;
        const draftLatest = model.current.draft ?? draftNow;
        const section = { ...asObj(storedLatest[service.configKey]), ...asObj(res.value) };
        section[service.flag] = next;
        const baseline = enablingDraft ? current : saved;
        const unchanged =
          JSON.stringify(draftLatest[service.configKey] ?? null) === JSON.stringify(baseline);
        const draftNext =
          !next && sectionDirty
            ? {
                ...draftLatest,
                [service.configKey]: {
                  ...asObj(draftLatest[service.configKey]),
                  [service.flag]: false,
                },
              }
            : unchanged
              ? { ...draftLatest, [service.configKey]: section }
              : draftLatest;
        applySection(service.configKey, section, draftNext);
        if (!next && model.current.tab === service.tab) navigate("/services/overview");
      } catch (e) {
        if (next) {
          const fields =
            e instanceof ApiError && e.fields
              ? Object.fromEntries(e.fields.map((f) => [f.field, f.message]))
              : {};
          setRatingsSetup(null);
          setSectionSetup({
            service,
            draft: { ...current, [service.flag]: true },
            errors: fields,
            failure: e,
          });
        } else {
          toast.error(t("Couldn't save the change"), {
            description: (e as Error).message,
          });
        }
      } finally {
        setBusyId(null);
      }
    });

  const toggleRatings = (next: boolean) =>
    enqueue(async () => {
      const current = model.current.ratings;
      if (!current) return;
      if (!next) skipSetupOffer.current = true;
      if (next && !current.apiKey.trim()) {
        openRatingsSetup(current);
        return;
      }
      setBusyId("ratings");
      try {
        const saved = await apiFetch<RatingSettings>("/discovery/ratings/settings", {
          method: "PUT",
          body: JSON.stringify({ ...current, enabled: next }),
        });
        model.current.ratings = saved;
        ratings.setData(saved);
        if (!next && model.current.tab === "ratings") navigate("/services/overview");
      } catch (e) {
        if (next) openRatingsSetup({ ...current, enabled: true });
        setRatingsSetup((setup) => (setup ? { ...setup, failure: e } : setup));
        if (!next)
          toast.error(t("Couldn't save the change"), {
            description: (e as Error).message,
          });
      } finally {
        setBusyId(null);
      }
    });

  const saveSectionSetup = () => {
    const setup = sectionSetup;
    if (!setup) return;
    const body = { ...setup.draft, [setup.service.flag]: true };
    const blocked = setup.service.blockEnable?.(body);
    if (blocked) {
      const field = setup.service.requiredField ?? "clientId";
      setSectionSetup({
        ...setup,
        draft: body,
        failure: null,
        errors: { [`${setup.service.configKey}.${field}`]: blocked },
      });
      return;
    }
    void enqueue(async () => {
      setBusyId(setup.service.tab);
      try {
        const res = await api.putSection(setup.service.endpoint, body);
        const storedLatest = model.current.stored ?? {};
        const draftLatest = model.current.draft ?? {};
        const section = {
          ...asObj(storedLatest[setup.service.configKey]),
          ...asObj(res.value),
        };
        section[setup.service.flag] = true;
        applySection(setup.service.configKey, section, {
          ...draftLatest,
          [setup.service.configKey]: section,
        });
        setSectionSetup(null);
      } catch (e) {
        const fields =
          e instanceof ApiError && e.fields
            ? Object.fromEntries(e.fields.map((f) => [f.field, f.message]))
            : {};
        setSectionSetup((current) =>
          current && current.service.tab === setup.service.tab
            ? { ...current, errors: fields, failure: e }
            : current,
        );
      } finally {
        setBusyId(null);
      }
    });
  };

  const saveRatingsSetup = () => {
    const setup = ratingsSetup;
    if (!setup) return;
    if (!setup.draft.apiKey.trim()) {
      setRatingsSetup({ ...setup, failure: new Error(t("Needs an API key")) });
      return;
    }
    void enqueue(async () => {
      setBusyId("ratings");
      try {
        const saved = await apiFetch<RatingSettings>("/discovery/ratings/settings", {
          method: "PUT",
          body: JSON.stringify({ ...setup.draft, enabled: true }),
        });
        model.current.ratings = saved;
        ratings.setData(saved);
        setRatingsSetup(null);
      } catch (e) {
        setRatingsSetup((current) => (current ? { ...current, failure: e } : current));
      } finally {
        setBusyId(null);
      }
    });
  };

  if (registry.error)
    return <ErrorPanel error={registry.error} onRetry={() => registry.reload()} />;
  if (config.error) return <ErrorPanel error={config.error} onRetry={() => void config.reload()} />;
  if (registry.loading)
    return (
      <div className="py-10 text-center">
        <Spinner size={20} label={t("Loading settings")} />
      </div>
    );
  if (!cfg || !draft)
    return (
      <div className="py-10 text-center">
        <Spinner size={20} label={t("Loading settings")} />
      </div>
    );

  const setupService = sectionSetup?.service;
  return (
    <>
      <Drawer
        open={sectionSetup !== null}
        onClose={() => {
          if (busyId) return;
          setSectionSetup(null);
        }}
        title={t("Set up {app}", { app: setupService?.label ?? "" })}
        description={t("Save and enable turns this on. Cancel leaves it off.")}
        footer={
          <div className="flex w-full flex-wrap items-center gap-2">
            <span className="flex-1" />
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setSectionSetup(null)}
              disabled={busyId !== null}
            >
              {t("Cancel")}
            </Button>
            <Button size="sm" onClick={saveSectionSetup} disabled={busyId !== null || locked}>
              {busyId ? t("Saving…") : t("Save and enable")}
            </Button>
          </div>
        }
      >
        {sectionSetup && setupService ? (
          <div className="space-y-4">
            {sectionSetup.failure ? <MutationError error={sectionSetup.failure} /> : null}
            {setupService.custom ? (
              <div className="space-y-3">
                <label className="block">
                  {t("Client ID")}{" "}
                  <input
                    className="w-full rounded border border-border bg-surface px-2 py-1"
                    value={String(sectionSetup.draft.clientId ?? "")}
                    onChange={(e) =>
                      setSectionSetup({
                        ...sectionSetup,
                        draft: {
                          ...sectionSetup.draft,
                          clientId: e.target.value,
                        },
                      })
                    }
                  />
                </label>
                {sectionSetup.errors[`${setupService.configKey}.clientId`] ? (
                  <p role="alert" className="text-sm text-danger">
                    {sectionSetup.errors[`${setupService.configKey}.clientId`]}
                  </p>
                ) : null}
                <label className="block" htmlFor="setup-client-secret">
                  {t("Client secret")}
                </label>
                <PasswordInput
                  id="setup-client-secret"
                  autoComplete="off"
                  value={String(sectionSetup.draft.clientSecret ?? "")}
                  onChange={(e) =>
                    setSectionSetup({
                      ...sectionSetup,
                      draft: {
                        ...sectionSetup.draft,
                        clientSecret: e.target.value,
                      },
                    })
                  }
                />
              </div>
            ) : setupService.form ? (
              <setupService.form
                value={sectionSetup.draft}
                saved={stored?.[setupService.configKey]}
                onChange={(next) =>
                  setSectionSetup({
                    ...sectionSetup,
                    draft: { ...next, [setupService.flag]: true },
                  })
                }
                errors={sectionSetup.errors}
                footer={null}
              />
            ) : (
              <SchemaGroups
                service={setupService}
                value={sectionSetup.draft}
                set={(next) => setSectionSetup({ ...sectionSetup, draft: next })}
                errors={sectionSetup.errors}
                footer={null}
              />
            )}
          </div>
        ) : null}
      </Drawer>
      <Drawer
        open={ratingsSetup !== null}
        onClose={() => {
          if (busyId) return;
          setRatingsSetup(null);
        }}
        title={t("Set up {app}", { app: t("OMDb and MDBList") })}
        description={t("Save and enable turns this on. Cancel leaves it off.")}
        footer={
          <div className="flex w-full flex-wrap items-center gap-2">
            <span className="flex-1" />
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setRatingsSetup(null)}
              disabled={busyId !== null}
            >
              {t("Cancel")}
            </Button>
            <Button size="sm" onClick={saveRatingsSetup} disabled={busyId !== null || locked}>
              {busyId ? t("Saving…") : t("Save and enable")}
            </Button>
          </div>
        }
      >
        {ratingsSetup ? (
          <div className="space-y-3">
            {ratingsSetup.failure ? <MutationError error={ratingsSetup.failure} /> : null}
            <label className="block">
              {t("API key")}{" "}
              <input
                className="w-full rounded border border-border bg-surface px-2 py-1"
                value={ratingsSetup.draft.apiKey}
                onChange={(e) =>
                  setRatingsSetup({
                    ...ratingsSetup,
                    draft: { ...ratingsSetup.draft, apiKey: e.target.value },
                  })
                }
              />
            </label>
          </div>
        ) : null}
      </Drawer>
      {tab === "tmdb" || visited.has("tmdb") ? (
        <div hidden={tab !== "tmdb"}>
          <SettingsSection
            title="TMDB"
            description={t(
              "Ether uses this key to name titles, fetch artwork and fill the catalogue.",
            )}
            footer={
              <TestButton
                label={t("Test TMDB key")}
                state={tests.catalog ?? { status: "idle" }}
                onTest={() => {
                  setTests((prev) => ({ ...prev, catalog: { status: "running" } }));
                  api.testTMDB(String(draft.catalog?.tmdbKey ?? "")).then(
                    (r) =>
                      setTests((prev) => ({
                        ...prev,
                        catalog: r.ok
                          ? { status: "ok", durationMs: r.durationMs }
                          : { status: "fail", message: r.error ?? t("failed") },
                      })),
                    (e) =>
                      setTests((prev) => ({
                        ...prev,
                        catalog: { status: "fail", message: (e as Error).message },
                      })),
                  );
                }}
              />
            }
          >
            <p className="mb-3 text-sm text-fg-muted">
              {tx("Create a free API key in your {link} account settings, then paste it below.", {
                link: (
                  <a
                    href="https://www.themoviedb.org/settings/api"
                    target="_blank"
                    rel="noreferrer"
                    className="font-medium text-primary underline"
                  >
                    themoviedb.org
                  </a>
                ),
              })}
            </p>
            <SchemaField
              def={{
                key: "tmdbKey",
                label: t("TMDB API key"),
                kind: "secret",
                help: t("Your personal TMDB v3 API key or v4 read-access token."),
              }}
              value={draft.catalog?.tmdbKey}
              path="catalog.tmdbKey"
              error={errors["catalog.tmdbKey"]}
              onChange={(v) => setDraft({ ...draft, catalog: { ...draft.catalog, tmdbKey: v } })}
            />
          </SettingsSection>
        </div>
      ) : null}
      <div hidden={tab !== "overview"}>
        <ServiceOverview
          descriptors={registry.descriptors}
          services={services}
          draft={draft}
          stored={stored}
          ratings={ratings.data}
          busyId={busyId}
          locked={locked}
          onToggle={(service, next) => void toggleService(service, next)}
          onToggleRatings={(next) => void toggleRatings(next)}
        />
      </div>
      {services
        .filter((s) => !s.custom)
        .map((s) => {
          if (tab !== s.tab && !visited.has(s.tab)) return null;
          const value = draft[s.configKey] ?? {};
          const set = (next: Obj) => setDraft({ ...draft, [s.configKey]: next });
          const testButton =
            s.test && s.testLabel ? (
              <TestButton
                label={s.testLabel}
                state={tests[s.configKey] ?? { status: "idle" }}
                onTest={() => runTest(s, value)}
              />
            ) : undefined;
          return (
            <div key={s.configKey} hidden={tab !== s.tab}>
              {s.form ? (
                <s.form
                  value={value}
                  saved={stored?.[s.configKey]}
                  onChange={set}
                  errors={errors}
                  footer={testButton}
                />
              ) : (
                <SchemaGroups
                  service={s}
                  value={value}
                  set={set}
                  errors={errors}
                  footer={testButton}
                />
              )}
              {s.tab === "llm" ? (
                <LanguageFeatures
                  textModelReady={languageModelReady(stored?.llm)}
                  speechReady={speechToTextReady(stored?.llm)}
                />
              ) : null}
              {s.tab === "introdb" ? <MarkerCoverage /> : null}
            </div>
          );
        })}
      {tab === "simkl" || visited.has("simkl") ? (
        <div hidden={tab !== "simkl"}>
          <SimklIntegration
            settings={simklSettings(draft.simkl)}
            onSettings={(next) => {
              const draftNow = model.current.draft;
              if (!draftNow) return;
              const draftNext = { ...draftNow, simkl: next as unknown as Obj };
              model.current.draft = draftNext;
              setDraft(draftNext);
            }}
            onPersisted={(next) => {
              const storedNow = model.current.stored;
              const draftNow = model.current.draft;
              if (!storedNow || !draftNow) return;
              const section = { ...asObj(storedNow.simkl), ...(next as unknown as Obj) };
              applySection("simkl", section, { ...draftNow, simkl: section });
            }}
            persist={enqueue}
            disabled={locked}
          />
        </div>
      ) : null}
      {tab === "ratings" || visited.has("ratings") ? (
        <div hidden={tab !== "ratings"}>
          <RatingSources
            saved={ratings.data ?? null}
            disabled={locked}
            persist={enqueue}
            onPersisted={(next) => {
              model.current.ratings = next;
              ratings.setData(next);
            }}
          />
        </div>
      ) : null}
      <SaveBar
        dirty={dirty}
        saving={saving}
        disabled={locked}
        error={error}
        onSave={save}
        onDiscard={() => {
          reset();
          setErrors({});
          setError(null);
        }}
      />
    </>
  );
}

function SchemaGroups({
  service,
  value,
  set,
  errors,
  footer,
}: {
  service: Service;
  value: Obj;
  set: (next: Obj) => void;
  errors: Record<string, string>;
  footer?: ReactNode;
}) {
  const served = useSettingsSchema(service.schemaId ?? "");
  const groups = service.schemaId ? served.groups : service.groups;
  if (!groups) {
    return (
      <div className="py-10 text-center">
        <Spinner size={20} label={t("Loading settings")} />
      </div>
    );
  }
  const known = new Set(groups.flatMap((g) => g.fields.map((f) => f.key)));
  const extra = Object.keys(value).filter((k) => !known.has(k));
  return groups.map((g, i) => {
    const last = i === groups.length - 1;
    return (
      <SettingsSection
        key={`${service.configKey}-${g.title ?? i}`}
        title={g.title ?? service.label}
        description={g.description}
        footer={last ? footer : undefined}
      >
        {g.fields.map((f) => (
          <SchemaField
            key={f.key}
            def={f}
            value={value[f.key]}
            path={`${service.configKey}.${f.key}`}
            error={errors[`${service.configKey}.${f.key}`]}
            onChange={(v) => set({ ...value, [f.key]: v })}
          />
        ))}
        {last && extra.length ? (
          <GenericFields
            value={value}
            keys={extra}
            onChange={set}
            errors={errors}
            prefix={service.configKey}
          />
        ) : null}
      </SettingsSection>
    );
  });
}

/** Card categories in display order, with their headings. */
const INTEGRATION_CATEGORIES: { key: string; title: () => string }[] = [
  { key: "required", title: () => t("Required") },
  { key: "watch-tracking", title: () => t("Watch tracking") },
  { key: "artwork-ratings", title: () => t("Artwork and ratings") },
  { key: "playback-extras", title: () => t("Playback extras") },
  { key: "smart-features", title: () => t("Smart features") },
];

/** Built-in brand marks by the logo name integration descriptors carry. */
function integrationLogo(logo: string | undefined): ReactNode | undefined {
  switch (logo) {
    case "trakt":
      return <TraktMark />;
    case "simkl":
      return <SimklMark />;
    default:
      if (logo && /^(https?:)?\/\//.test(logo))
        return <img src={logo} alt="" width={40} height={40} className="rounded-md" />;
      return undefined;
  }
}

/** Configure-link copy that predates the generic Manage/Set up wording. */
function configureLabelFor(id: string, name: string, on: boolean): string {
  switch (id) {
    case "llm":
      return on ? t("Manage language model") : t("Set up a language model");
    case "introdb":
      return t("Markers");
    case "ratings":
      return t("Ratings");
    default:
      return on ? t("Manage {app}", { app: name }) : t("Set up {app}", { app: name });
  }
}

function ServiceOverview({
  descriptors,
  services,
  stored,
  ratings,
  busyId,
  locked,
  onToggle,
  onToggleRatings,
}: {
  descriptors: Descriptor<IntegrationMeta>[];
  services: Service[];
  draft: Record<string, Obj>;
  stored: Record<string, Obj> | null;
  ratings: RatingSettings | undefined;
  busyId: string | null;
  locked: boolean;
  onToggle: (service: Service, next: boolean) => void;
  onToggleRatings: (next: boolean) => void;
}) {
  const { health } = useApp();
  const switchesHeld = locked || busyId !== null;

  function card(d: Descriptor<IntegrationMeta>) {
    const meta = d.meta!;
    const name = textOf(d.label);
    const tab = meta.tab ?? d.id;
    const href = meta.tab ? `/services/${meta.tab}` : undefined;
    const logo = integrationLogo(meta.logo);
    const description = textOf(d.hint);
    if (meta.form === "ratings") {
      return (
        <IntegrationAppCard
          key={d.id}
          identity={{ id: tab, name }}
          logo={logo}
          description={description}
          status={ratingsStatus(ratings)}
          enabled={ratings?.enabled}
          busy={locked || busyId === tab}
          disabled={switchesHeld || !ratings}
          onToggle={ratings ? onToggleRatings : undefined}
          configureHref={href}
          configureLabel={configureLabelFor(d.id, name, ratings?.enabled === true)}
        />
      );
    }
    const service = services.find((s) => s.tab === meta.tab);
    if (!service) {
      // No switch: a core service (TMDB) or a plugin integration.
      const readyField = meta.readyField ?? meta.requiredField;
      const ready = readyField
        ? secretConfigured(stored?.[meta.configKey ?? ""]?.[readyField])
        : false;
      const catalogHealth = health.find((item) => item.id === "catalog");
      return (
        <IntegrationAppCard
          key={d.id}
          identity={{ id: tab, name }}
          logo={logo}
          description={description}
          status={
            d.id === "tmdb" ? (
              ready ? (
                catalogHealth ? (
                  <LevelBadge level={catalogHealth.level} />
                ) : (
                  <Badge variant="success">{t("Key saved")}</Badge>
                )
              ) : (
                <Badge variant="warning">{t("Needs an API key")}</Badge>
              )
            ) : undefined
          }
          configureHref={href}
          configureLabel={configureLabelFor(d.id, name, ready)}
        />
      );
    }
    const saved = stored?.[service.configKey] ?? {};
    const on = flagOf(saved, service.flag);
    const readyField = meta.readyField ?? meta.requiredField;
    const ready = readyField ? secretConfigured(saved[readyField]) : true;
    return (
      <IntegrationAppCard
        key={d.id}
        identity={{ id: tab, name }}
        logo={logo}
        description={description}
        status={
          meta.publicFallback
            ? introStatus(on, readyField ? saved[readyField] : undefined)
            : switchStatus(
                on,
                ready,
                readyField ? missingFieldLabel(readyField) : t("Needs an API key"),
              )
        }
        enabled={on}
        busy={locked || busyId === tab}
        disabled={switchesHeld}
        onToggle={(next) => onToggle(service, next)}
        configureHref={href}
        configureLabel={configureLabelFor(d.id, name, on)}
      />
    );
  }

  const byCategory = new Map<string, Descriptor<IntegrationMeta>[]>();
  for (const d of descriptors) {
    const category = d.meta?.category ?? d.group ?? "";
    byCategory.set(category, [...(byCategory.get(category) ?? []), d]);
  }
  const extra = [...byCategory.keys()].filter(
    (key) => !INTEGRATION_CATEGORIES.some((c) => c.key === key),
  );

  return (
    <>
      {INTEGRATION_CATEGORIES.map(({ key, title }) => {
        const cards = (byCategory.get(key) ?? [])
          .slice()
          .sort((a, b) => (a.order ?? 0) - (b.order ?? 0))
          .map(card);
        if (key === "artwork-ratings") {
          return (
            <PosterAccounts key={key} locked={switchesHeld}>
              {(posterCards) => (
                <IntegrationGroup title={title()}>
                  {posterCards}
                  {cards}
                </IntegrationGroup>
              )}
            </PosterAccounts>
          );
        }
        return (
          <IntegrationGroup key={key} title={title()}>
            {cards}
          </IntegrationGroup>
        );
      })}
      {extra.map((key) => (
        <IntegrationGroup key={key} title={key}>
          {(byCategory.get(key) ?? []).map(card)}
        </IntegrationGroup>
      ))}
    </>
  );
}

function introStatus(enabled: boolean, apiKey: unknown) {
  if (!enabled) return <Badge variant="secondary">{t("Off")}</Badge>;
  if (secretConfigured(apiKey)) return <Badge variant="success">{t("Key saved")}</Badge>;
  return <Badge variant="secondary">{t("Public lookup")}</Badge>;
}

function ratingsStatus(ratings: RatingSettings | undefined) {
  if (!ratings) return undefined;
  if (!ratings.enabled) return <Badge variant="secondary">{t("Off")}</Badge>;
  if (!ratings.apiKey.trim()) return <Badge variant="warning">{t("Needs an API key")}</Badge>;
  return <Badge variant="success">{t("Configured")}</Badge>;
}

function flattenCards(children: ReactNode): ReactNode[] {
  const out: ReactNode[] = [];
  Children.forEach(children, (child) => {
    if (isValidElement<{ children?: ReactNode }>(child) && child.type === Fragment)
      out.push(...flattenCards(child.props.children));
    else out.push(child);
  });
  return out;
}

function IntegrationGroup({ title, children }: { title: string; children: ReactNode }) {
  const cards = flattenCards(children).filter(
    (child) => isValidElement<{ identity?: { name?: string } }>(child) && child.props.identity,
  );
  const rest = flattenCards(children).filter(
    (child) => !(isValidElement<{ identity?: { name?: string } }>(child) && child.props.identity),
  );
  return (
    <section aria-label={title} className="space-y-3 pt-6 first:pt-0">
      <h2 className="text-sm font-semibold text-fg">{title}</h2>
      <ul className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
        {cards.map((child) => {
          const name = isValidElement<{ identity?: { name?: string } }>(child)
            ? child.props.identity?.name
            : undefined;
          return (
            <li key={name} aria-label={name} className="h-full">
              {child}
            </li>
          );
        })}
      </ul>
      {rest}
    </section>
  );
}
