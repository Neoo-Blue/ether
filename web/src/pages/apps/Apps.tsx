import {
  Badge,
  Button,
  Input,
  SettingsSection,
  Switch,
  confirmDialog,
} from "@braedonsaunders/appkit-ui";
import { useEffect, useState, type ReactNode } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useApp } from "../../app-context";
import { ErrorPanel, LoadingRows } from "../../components/feedback";
import { IntegrationAppCard } from "../../components/IntegrationAppCard";
import {
  EmbyMark,
  householdIcon,
  JellyfinMark,
  PlexMark,
  SiloMark,
} from "../../components/integration-brands";
import { StremioMark } from "../sources/marks";
import { MoreOptions } from "../../components/ConfigSection";
import { DomainPage, useRouteTab } from "../../components/layout";
import { SaveBar, useDraft } from "../../components/SaveBar";
import { SettingsRow } from "../../components/SettingsRow";
import { t } from "../../i18n";
import { API_BASE, api, apiBlob, apiFetch } from "../../lib/api";
import { isPlainObject } from "../../lib/format";
import { useAsync, useInterval } from "../../lib/hooks";
import { viewerStatus } from "../viewers/ViewerStatus";
import { PlexApp } from "./PlexApp";
import { appLabel, appPath, appPurpose, appRank } from "./catalog";
import { HomeStatusRow } from "./HomeStatus";
import { awaySummary, usableAddresses } from "./reachability";

/** One field of a frontend's served settings schema (WS1d). */
export interface FrontendSettingsField {
  key: string;
  kind: string;
  more?: boolean;
  min?: number;
  max?: number;
  options?: string[];
}

/** A frontend's served settings schema: internal/frontend.SettingsSchema. */
export interface FrontendSettingsSchema {
  sections: { key: string; fields: FrontendSettingsField[] }[];
  accountConnect?: boolean;
  signInHelp?: boolean;
}

type FrontendInfo = {
  descriptor: {
    id: string;
    name: string;
    version?: string;
    certifiedApps?: { name: string; steps: string[] }[];
    defaultPort?: number;
    settingsSchema?: FrontendSettingsSchema;
  };
  enabled: boolean;
  status: {
    addresses: string[] | null;
    connectUrls?: string[];
    lan: string;
    wan: string;
    clients24h: number;
    clients24hKnown?: boolean;
    liveStreams: number;
    liveStreamsKnown?: boolean;
    targetVersion: string;
    conformance: string;
    error?: string;
    warnings?: string[];
  };
};
type Settings = Record<string, unknown>;

/** Labels for the personalized row kinds a schema can offer. */
const HOME_ROW_LABELS: Record<string, () => string> = {
  "because-you-watched": () => t("Because you watched"),
  trending: () => t("Trending"),
  "top-10": () => t("Top 10 for you"),
  "fans-also-loved": () => t("Fans also loved"),
  "new-releases": () => t("New releases"),
  "hidden-gems": () => t("Hidden gems"),
  "more-from-person": () => t("People you watch"),
  "popular-on-server": () => t("Popular on this server"),
};

/**
 * Copy and defaults for the setting keys a schema can name. The schema says
 * which settings a frontend has; the console keeps their wording.
 */
const FIELD_COPY: Record<string, () => { label: string; help?: string; fallback?: unknown }> = {
  discovery: () => ({
    label: t("Let apps find this server"),
    help: t("Apps on your network can find this server without typing an address."),
  }),
  quickConnect: () => ({
    label: t("Quick Connect"),
    help: t("A viewer approves a short code in the Jellyfin app instead of typing a password."),
  }),
  publicViewerPicker: () => ({
    label: t("Show local viewer names on the sign-in screen"),
    help: t("Leave this off on a network you do not trust."),
  }),
  homeRows: () => ({
    label: t("Personalized rows"),
    help: t("Each viewer gets their own rows, filled from what they watch."),
    fallback: true,
  }),
  homeRowCount: () => ({
    label: t("How many rows"),
    help: t("About four is enough for a home screen. The most relevant rows come first."),
    fallback: 4,
  }),
};

/** Connect-note copy by the key a schema note field carries. */
const CONNECT_NOTES: Record<string, () => string> = {
  addon: () =>
    t("Stremio uses an addon address from your account page. It does not have its own port."),
  files: () => t("Other apps open this library as files. The address is on your account page."),
  playlist: () => t("Live TV apps use a playlist from your account page."),
};

const pathFor = appPath;
const nameFor = (info: FrontendInfo) => appLabel(info.descriptor.id, info.descriptor.name);

/** Vendored marks only. Files, DLNA, and M3U have none. */
function appLogo(id: string): ReactNode | undefined {
  switch (id) {
    case "plex":
      return <PlexMark />;
    case "jellyfin":
      return <JellyfinMark />;
    case "emby":
      return <EmbyMark />;
    case "silo":
      return <SiloMark />;
    case "stremio":
      return <StremioMark />;
    default:
      return householdIcon(id);
  }
}

function frontendWarning(warning: string): string {
  const port =
    /^Another server on this computer is using port (\d+); Ether still works at (.+)$/.exec(
      warning,
    );
  if (port) {
    return t(
      "Another server on this computer is using port {port}; Ether still works at {address}",
      { port: port[1], address: port[2] },
    );
  }
  const discovery =
    /^Another server on this computer is using discovery; Ether still works at (.+)$/.exec(warning);
  if (discovery) {
    return t("Another server on this computer is using discovery; Ether still works at {address}", {
      address: discovery[1],
    });
  }
  return warning;
}

function connectionStep(code: string) {
  switch (code) {
    case "openApp":
      return t("Open the app on your device.");
    case "addServer":
      return t("Add the server address shown above.");
    case "signIn":
      return t("Sign in with your viewer account.");
    case "installAddon":
      return t("Install the addon using the address shown above.");
    default:
      return null;
  }
}

function viewerPortalAddress(chosen?: string): string {
  if (chosen) {
    try {
      const u = new URL(chosen);
      const prefix = u.pathname.replace(/\/(jellyfin|emby|silo)\/?$/, "");
      if (prefix !== u.pathname) {
        return `${u.origin}${prefix}/ether/me`;
      }
    } catch {
      // ignore invalid URL
    }
  }
  if (
    typeof window !== "undefined" &&
    window.location?.origin &&
    window.location.origin !== "null"
  ) {
    return `${window.location.origin}/ether/me`;
  }
  return "/ether/me";
}

function ConnectDevice({
  info,
  startOpen = false,
  selectedURL: selectedURLProp,
  onSelectedURL,
}: {
  info: FrontendInfo;
  startOpen?: boolean;
  selectedURL?: string;
  onSelectedURL?: (url: string) => void;
}) {
  const [open, setOpen] = useState(startOpen);
  const [showOther, setShowOther] = useState(false);
  const [qr, setQR] = useState<string>();
  const [selectedURLState, setSelectedURLState] = useState("");
  const selectedURL = selectedURLProp ?? selectedURLState;
  const setSelectedURL = onSelectedURL ?? setSelectedURLState;
  const urls = info.status.connectUrls ?? [];
  const likely = urls.slice(0, Math.min(2, urls.length));
  const other = urls.slice(likely.length);
  const shown = showOther ? urls : likely;
  const url = urls.includes(selectedURL) ? selectedURL : likely[0];
  useEffect(() => {
    if (!open || !url) return;
    const controller = new AbortController();
    let objectURL: string | undefined;
    void apiBlob(`${API_BASE}/frontends/qr?url=${encodeURIComponent(url)}`, controller.signal)
      .then((blob) => {
        if (!controller.signal.aborted) {
          objectURL = URL.createObjectURL(blob);
          setQR(objectURL);
        }
      })
      .catch(() => setQR(undefined));
    return () => {
      controller.abort();
      if (objectURL) URL.revokeObjectURL(objectURL);
      setQR(undefined);
    };
  }, [open, url]);
  if (!info.enabled) return null;
  if (info.descriptor.settingsSchema?.accountConnect) {
    return (
      <Link className="text-primary underline" to="/me">
        {t("Connect a device")}
      </Link>
    );
  }
  if (!url) return null;
  return (
    <div>
      {open ? null : <Button onClick={() => setOpen(true)}>{t("Connect a device")}</Button>}
      {open ? (
        <div className="space-y-2 pt-3">
          <p>{t("Enter this server address in a supported app:")}</p>
          {shown.length > 1 ? (
            <label>
              {t("Server address")}
              <select value={url} onChange={(event) => setSelectedURL(event.currentTarget.value)}>
                {shown.map((address) => (
                  <option key={address} value={address}>
                    {address}
                  </option>
                ))}
              </select>
            </label>
          ) : (
            <code>{url}</code>
          )}
          {other.length > 0 ? (
            <div>
              <Button variant="ghost" size="sm" onClick={() => setShowOther((value) => !value)}>
                {showOther ? t("Likely addresses") : t("Other addresses")}
              </Button>
            </div>
          ) : null}
          {qr && (
            <img src={qr} alt={t("QR code for the server address")} width={192} height={192} />
          )}
          {info.descriptor.certifiedApps?.length ? (
            <>
              <p>{t("Certified native apps:")}</p>
              {info.descriptor.certifiedApps.map((app) => (
                <div key={app.name}>
                  <p>{app.name}</p>
                  <ol>
                    {app.steps.map((step, index) => {
                      const label = connectionStep(step);
                      return label ? <li key={index}>{label}</li> : null;
                    })}
                  </ol>
                </div>
              ))}
            </>
          ) : (
            <p>
              {t(
                "Open the app, add the server address above, and sign in. Infuse, Swiftfin, Findroid, and Streamyfin are the usual places to start.",
              )}
            </p>
          )}
        </div>
      ) : null}
    </div>
  );
}

function sectionCopy(
  key: string,
  appName: string,
  note?: string,
): { title: string; description: string } {
  switch (key) {
    case "home":
      return {
        title: t("Home"),
        description: t("{app} apps show this viewer's For You rows.", { app: appName }),
      };
    case "tv":
      return {
        title: t("What the TV shows"),
        description: t("DLNA players browse the library as one viewer."),
      };
    case "connect":
      return { title: t("Connect"), description: (note && CONNECT_NOTES[note]?.()) || "" };
    default:
      return {
        title: t("Find this server"),
        description: t("How {app} apps find this server.", { app: appName }),
      };
  }
}

/** A frontend's settings, rendered from its served schema (WS1d). */
function ProtocolSettings({ info, hidden }: { info: FrontendInfo; hidden: boolean }) {
  const { boot } = useApp();
  const id = info.descriptor.id;
  const schema = info.descriptor.settingsSchema;
  const state = useAsync(() => apiFetch<Settings>(`/frontends/${id}/settings`), [id]);
  const { draft, setDraft, dirty, reset } = useDraft(state.data);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<unknown>();
  async function save() {
    if (!draft) return;
    setSaving(true);
    setError(undefined);
    try {
      const { serverName: _serverName, ...body } = draft;
      await apiFetch(`/frontends/${id}/settings`, { method: "PUT", body: JSON.stringify(body) });
      await state.reload();
    } catch (err) {
      setError(err);
    } finally {
      setSaving(false);
    }
  }
  if (state.error)
    return hidden ? null : <ErrorPanel error={state.error} onRetry={() => void state.reload()} />;
  if (!draft) return hidden ? null : <LoadingRows />;
  const appName = info.descriptor.name;

  function renderField(field: FrontendSettingsField): ReactNode {
    if (!draft) return null;
    const copy = FIELD_COPY[field.key]?.();
    switch (field.kind) {
      case "serverName":
        return (
          <SettingsRow
            key={field.key}
            title={t("Server name")}
            description={
              <>
                <span>{boot.serverName?.trim() || "Ether"}</span>
                {" · "}
                <Link className="underline" to="/server">
                  {t("General settings")}
                </Link>
              </>
            }
          />
        );
      case "port": {
        if (draft.port === undefined) return null;
        return (
          <SettingsRow
            key={field.key}
            title={<label htmlFor={`${id}-port`}>{t("Port")}</label>}
            description={portHelp(info.descriptor.defaultPort)}
            control={
              <Input
                id={`${id}-port`}
                aria-label={t("Port")}
                type="number"
                min={field.min ?? 1}
                max={field.max ?? 65535}
                value={Number(draft.port)}
                onChange={(e) => setDraft({ ...draft, port: Number(e.currentTarget.value) })}
              />
            }
          />
        );
      }
      case "bool": {
        if (!copy) return null;
        const checked =
          draft[field.key] === undefined ? copy.fallback === true : Boolean(draft[field.key]);
        return (
          <SettingsRow
            key={field.key}
            title={copy.label}
            description={copy.help}
            control={
              <Switch
                checked={checked}
                aria-label={copy.label}
                onChange={(e) => setDraft({ ...draft, [field.key]: e.currentTarget.checked })}
              />
            }
          />
        );
      }
      case "number": {
        if (!copy) return null;
        const value =
          typeof draft[field.key] === "number"
            ? (draft[field.key] as number)
            : Number(copy.fallback ?? 0);
        return (
          <SettingsRow
            key={field.key}
            title={<label htmlFor={`${id}-${field.key}`}>{copy.label}</label>}
            description={copy.help}
            control={
              <Input
                id={`${id}-${field.key}`}
                aria-label={copy.label}
                type="number"
                min={field.min}
                max={field.max}
                value={value}
                onChange={(e) => setDraft({ ...draft, [field.key]: Number(e.currentTarget.value) })}
              />
            }
          />
        );
      }
      case "rowKinds": {
        const options = field.options ?? [];
        const current = draft[field.key];
        const selected = Array.isArray(current) ? (current as string[]) : options;
        return (
          <FragmentRows key={field.key}>
            <SettingsRow
              title={t("Which rows")}
              description={t(
                "Leave them all on to use every personalized row. Turn one off to hide that kind.",
              )}
            />
            {options.map((kind) => {
              const label = HOME_ROW_LABELS[kind]?.() ?? kind;
              return (
                <SettingsRow
                  key={kind}
                  title={label}
                  control={
                    <Switch
                      checked={selected.includes(kind)}
                      aria-label={label}
                      onChange={(e) => {
                        if (!draft) return;
                        const now = new Set(selected);
                        if (e.currentTarget.checked) now.add(kind);
                        else now.delete(kind);
                        const next = options.filter((o) => now.has(o));
                        setDraft({
                          ...draft,
                          [field.key]: next.length === options.length ? undefined : next,
                        });
                      }}
                    />
                  }
                />
              );
            })}
          </FragmentRows>
        );
      }
      case "viewer":
        return (
          <DLNAViewer
            key={field.key}
            value={typeof draft[field.key] === "number" ? (draft[field.key] as number) : undefined}
            onChange={(viewerId) =>
              setDraft((old) => (old ? { ...old, [field.key]: viewerId } : old))
            }
          />
        );
      case "note":
        return (
          <div key={field.key} className="px-5 py-3">
            <Link className="text-primary underline" to="/me">
              {t("Open your account")}
            </Link>
          </div>
        );
      default:
        return null;
    }
  }

  return (
    <>
      <div hidden={hidden}>
        {(schema?.sections ?? []).map((section) => {
          const note = section.fields.find((f) => f.kind === "note")?.key;
          const { title, description } = sectionCopy(section.key, appName, note);
          const main = section.fields.filter((f) => !f.more);
          const more = section.fields.filter((f) => f.more);
          return (
            <SettingsSection key={section.key} title={title} description={description}>
              {main.map(renderField)}
              {more.length ? (
                <MoreOptions label={t("More options: {title}", { title })}>
                  {more.map(renderField)}
                </MoreOptions>
              ) : null}
            </SettingsSection>
          );
        })}
      </div>
      <SaveBar dirty={dirty} saving={saving} onSave={save} onDiscard={reset} error={error} />
    </>
  );
}

/** Rows with no wrapper element of their own. */
function FragmentRows({ children }: { children: ReactNode }) {
  return <>{children}</>;
}

function portHelp(defaultPort?: number): string {
  return defaultPort
    ? t(
        "Use the address on this page. The port is part of that address. Leave this at {port} unless another server already uses it.",
        { port: defaultPort },
      )
    : t("Change this only if the usual port is already in use.");
}

function DLNAViewer({ value, onChange }: { value?: number; onChange: (id?: number) => void }) {
  const viewers = useAsync(() => api.viewers(), []);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  async function createGuest() {
    if (
      !(await confirmDialog({
        title: t("Create a living-room guest"),
        message: t("Starts with the Kids preset (PG). You can change it on the guest's profile."),
        confirmLabel: t("Create a living-room guest"),
      }))
    )
      return;
    setBusy(true);
    setError(undefined);
    try {
      const viewer = await apiFetch<{ id: number }>("/frontends/dlna/guest", {
        method: "POST",
        body: "{}",
      });
      await viewers.reload();
      onChange(viewer.id);
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="space-y-2">
      <label className="flex flex-col gap-1 text-sm">
        {t("Browse as")}
        <select
          value={value ?? ""}
          onChange={(e) =>
            onChange(e.currentTarget.value ? Number(e.currentTarget.value) : undefined)
          }
        >
          <option value="">{t("No viewer selected")}</option>
          {(viewers.data ?? [])
            .filter((v) => ["owner", "active"].includes(viewerStatus(v)))
            .map((v) => (
              <option key={v.id} value={v.id}>
                {v.title || v.username}
              </option>
            ))}
        </select>
      </label>
      <Button disabled={busy} onClick={() => void createGuest()}>
        {t("Create a living-room guest")}
      </Button>
      {error != null && <ErrorPanel error={error} />}
    </div>
  );
}

function AppStatus({ info }: { info: FrontendInfo }) {
  return (
    <>
      <Badge variant={info.enabled ? "success" : "secondary"}>
        {info.enabled ? t("On") : t("Off")}
      </Badge>
      {info.status.error ? <span className="text-danger">{info.status.error}</span> : null}
    </>
  );
}

function AppDetail({ info, startOpen = false }: { info: FrontendInfo; startOpen?: boolean }) {
  const addresses = usableAddresses(info.status);
  const [selectedURL, setSelectedURL] = useState("");
  const chosen = addresses.includes(selectedURL) ? selectedURL : addresses[0];
  const away = awaySummary(info.status, { homeNetwork: true });
  const portalURL = viewerPortalAddress(chosen);
  return (
    <div className="space-y-6 pb-6">
      <SettingsSection title={t("Status")}>
        <HomeStatusRow status={info.status} address={chosen} />
        <SettingsRow title={away.title} description={away.detail} />
        {info.status.error ? (
          <p className="px-5 py-3 text-sm text-danger">{info.status.error}</p>
        ) : null}
        {info.status.warnings?.map((warning) => (
          <p key={warning} className="px-5 py-3 text-sm text-fg-muted">
            {frontendWarning(warning)}
          </p>
        ))}
        <div className="px-5 py-3">
          <ConnectDevice
            info={info}
            startOpen={startOpen}
            selectedURL={chosen}
            onSelectedURL={setSelectedURL}
          />
        </div>
      </SettingsSection>
      {info.descriptor.settingsSchema?.signInHelp ? (
        <SettingsSection
          title={t("How people sign in")}
          description={t("Infuse needs an app password. The Emby for TV app isn't supported.")}
          footer={
            <div className="flex flex-wrap items-center gap-3">
              <Link className="text-sm font-medium text-primary underline" to="/profile">
                {t("Open my account")}
              </Link>
              <Link className="text-sm text-fg-muted underline" to="/me">
                {t("Viewer portal")}
              </Link>
            </div>
          }
        >
          <SettingsRow
            title={t("Quick Connect")}
            description={t("Quick Connect: approve the code at {url}", { url: portalURL })}
          />
          <SettingsRow
            title={t("App password")}
            description={t("App password: from Connect apps at {url}", { url: portalURL })}
          />
        </SettingsSection>
      ) : null}
    </div>
  );
}

export default function Apps() {
  const { dashboard } = useApp();
  const state = useAsync((signal) => apiFetch<FrontendInfo[]>("/frontends", { signal }), []);
  const { tab: routeTab, section: routeSection } = useParams();
  const [searchParams] = useSearchParams();
  const connectOpen = searchParams.get("connect") === "1";
  const navigate = useNavigate();
  const querySection = searchParams.get("section");
  const apps = [...(state.data ?? [])].sort(
    (a, b) => appRank(a.descriptor.id) - appRank(b.descriptor.id),
  );
  const plexInfo = apps.find((info) => info.descriptor.id === "plex");
  const plexOn = plexInfo?.enabled === true;
  const showPlex = plexOn && routeTab === "plex";
  const tabs = [
    { key: "overview", label: t("Overview") },
    ...apps
      .filter((info) => info.enabled)
      .map((info) => ({ key: pathFor(info.descriptor.id), label: nameFor(info) })),
  ];
  const routed = useRouteTab(tabs, "overview");
  const tab = showPlex ? "plex" : routed;
  const [visited, setVisited] = useState(() => new Set([tab]));
  useEffect(() => {
    setVisited((old) => (old.has(tab) ? old : new Set([...old, tab])));
  }, [tab]);
  useEffect(() => {
    if (!state.data || !routeTab || routeTab === "overview") return;
    const plexEnabled = state.data.some((info) => info.descriptor.id === "plex" && info.enabled);
    if (routeTab === "plex") {
      if (!plexEnabled) navigate("/apps/overview", { replace: true });
      return;
    }
    const match = state.data.find((info) => pathFor(info.descriptor.id) === routeTab);
    if (!match?.enabled) navigate("/apps/overview", { replace: true });
  }, [state.data, routeTab, navigate]);
  useEffect(() => {
    if (!state.data || routeTab !== "plex") return;
    const plexEnabled = state.data.some((info) => info.descriptor.id === "plex" && info.enabled);
    if (!plexEnabled) return;
    const asked = querySection || routeSection;
    if (asked === "remote" || asked === "secure") {
      if (routeSection !== "remote" || querySection)
        navigate("/apps/plex/remote", { replace: true });
      return;
    }
    if (asked === "compatibility") {
      if (routeSection !== "compatibility" || querySection)
        navigate("/apps/plex/compatibility", { replace: true });
      return;
    }
    if (querySection === "account" || (routeSection && routeSection !== "account")) {
      navigate("/apps/plex/account", { replace: true });
    }
  }, [state.data, routeTab, routeSection, querySection, navigate]);
  useInterval(() => void state.reload(), 10_000);
  const [error, setError] = useState<unknown>();
  const [busy, setBusy] = useState<string>();
  async function toggle(info: FrontendInfo, enabled: boolean) {
    setBusy(info.descriptor.id);
    setError(undefined);
    try {
      await apiFetch(`/frontends/${info.descriptor.id}/enabled`, {
        method: "PUT",
        body: JSON.stringify({ enabled }),
      });
      await state.reload();
      if (!enabled && routeTab === pathFor(info.descriptor.id)) {
        navigate("/apps/overview", { replace: true });
      }
    } catch (err) {
      setError(err);
    } finally {
      setBusy(undefined);
    }
  }
  return (
    <DomainPage title={t("Apps")} base="/apps" tabs={tabs} active={tab}>
      {tab === "overview" &&
        (state.error ? (
          <ErrorPanel error={state.error} onRetry={() => void state.reload()} />
        ) : !state.data ? (
          <LoadingRows />
        ) : (
          <div className="space-y-4">
            {plexOn &&
            isPlainObject(dashboard?.sections.remote) &&
            dashboard.sections.remote.claimed === false ? (
              <p className="text-sm text-fg">
                <Link className="text-primary underline" to="/apps/plex/account">
                  {t("Finish connecting Plex")}
                </Link>
              </p>
            ) : null}
            <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
              {state.data.map((info) => (
                <IntegrationAppCard
                  key={info.descriptor.id}
                  identity={{ id: info.descriptor.id, name: nameFor(info) }}
                  logo={appLogo(info.descriptor.id)}
                  description={appPurpose(info.descriptor.id)}
                  status={<AppStatus info={info} />}
                  enabled={info.enabled}
                  busy={busy === info.descriptor.id}
                  disabled={busy != null && busy !== info.descriptor.id}
                  toggleLabel={t("{app} enabled", { app: nameFor(info) })}
                  onToggle={(enabled) => void toggle(info, enabled)}
                  configureHref={info.enabled ? `/apps/${pathFor(info.descriptor.id)}` : undefined}
                  configureLabel={t("Settings")}
                />
              ))}
            </div>
          </div>
        ))}
      {(visited.has("plex") || showPlex) && (
        <PlexApp hidden={tab !== "plex"} section={routeSection} status={plexInfo?.status} />
      )}
      {tabs
        .filter((item) => !["overview", "plex"].includes(item.key) && visited.has(item.key))
        .map((item) => {
          const info = apps.find((app) => pathFor(app.descriptor.id) === item.key);
          return (
            <div key={item.key}>
              {tab === item.key && info ? <AppDetail info={info} startOpen={connectOpen} /> : null}
              {info ? <ProtocolSettings info={info} hidden={tab !== item.key} /> : null}
            </div>
          );
        })}
      {error != null && <ErrorPanel error={error} />}
    </DomainPage>
  );
}
