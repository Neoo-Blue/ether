/**
 * Connect an AI app: MCP pairing inside a drawer nested over Ask Ether.
 * Shows a short-lived pairing code and the revocable list of paired apps.
 */
import { Button, Spinner, toast } from "@braedonsaunders/appkit-ui";
import { Check, Copy, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { t } from "../../i18n";
import { Drawer } from "../Drawer";
import { ErrorPanel } from "../feedback";
import {
  createMcpPairing,
  fetchMcpClients,
  revokeMcpClient,
  type McpClient,
  type McpPairing as Pairing,
} from "./agentApi";

function absoluteEndpoint(endpoint: string): string {
  if (/^https?:\/\//.test(endpoint)) return endpoint;
  return `${window.location.origin}${endpoint.startsWith("/") ? endpoint : `/${endpoint}`}`;
}

function formatTime(iso?: string): string {
  if (!iso) return "";
  const date = new Date(iso);
  return Number.isNaN(date.getTime())
    ? ""
    : date.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });
}

function formatDate(iso?: string): string {
  if (!iso) return "";
  const date = new Date(iso);
  return Number.isNaN(date.getTime())
    ? ""
    : date.toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });
}

function CopyButton({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1600);
    } catch {
      toast.error(t("Copy failed"), { description: t("Copy the text by hand instead.") });
    }
  };
  return (
    <Button
      type="button"
      variant="outline"
      size="sm"
      onClick={() => void copy()}
      aria-label={label}
    >
      {copied ? <Check size={14} /> : <Copy size={14} />}
      {copied ? t("Copied") : t("Copy")}
    </Button>
  );
}

export function McpPairingDrawer({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [pairing, setPairing] = useState<Pairing | null>(null);
  const [clients, setClients] = useState<McpClient[] | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [pairingBusy, setPairingBusy] = useState(false);
  const [revoking, setRevoking] = useState<string | null>(null);

  const loadClients = useCallback(async () => {
    setClients(await fetchMcpClients());
  }, []);

  const newCode = useCallback(async () => {
    setPairingBusy(true);
    setError(null);
    try {
      setPairing(await createMcpPairing());
    } catch (e) {
      setError(e);
    } finally {
      setPairingBusy(false);
    }
  }, []);

  useEffect(() => {
    if (!open) return;
    setError(null);
    void loadClients().catch((e: unknown) => setError(e));
    void newCode();
  }, [open, loadClients, newCode]);

  const revoke = async (client: McpClient) => {
    setRevoking(client.id);
    setError(null);
    try {
      await revokeMcpClient(client.id);
      setClients((current) => (current ?? []).filter((item) => item.id !== client.id));
      toast.success(t("Access revoked"), {
        description: t("{name} can no longer connect to this server.", { name: client.name }),
      });
    } catch (e) {
      setError(e);
    } finally {
      setRevoking(null);
    }
  };

  const endpoint = pairing ? absoluteEndpoint(pairing.endpoint) : "";
  const expires = formatTime(pairing?.expiresAt);

  return (
    <Drawer open={open} onClose={onClose} title={t("Connect an AI app")} size="sm">
      <div className="space-y-6">
        <p className="text-sm leading-relaxed text-fg-muted">
          {t(
            "Pair an AI app so it can ask Ether questions with the same tools as Ask Ether. Anything that changes your server still needs your approval here.",
          )}
        </p>

        {error ? <ErrorPanel error={error} /> : null}

        <section aria-label={t("Pairing code")} className="space-y-3">
          <h3 className="text-sm font-semibold text-fg">{t("Pairing code")}</h3>
          {pairing ? (
            <div className="rounded-xl border border-border bg-bg-subtle p-4">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <span
                  className="font-mono text-2xl font-semibold tracking-[0.2em] text-fg"
                  aria-label={t("Pairing code {code}", { code: pairing.code })}
                >
                  {pairing.code}
                </span>
                <CopyButton value={pairing.code} label={t("Copy pairing code")} />
              </div>
              {expires ? (
                <p className="mt-2 text-xs text-fg-muted">
                  {t("This code stops working at {time}.", { time: expires })}
                </p>
              ) : null}
              <div className="mt-3 flex flex-wrap items-center justify-between gap-3 border-t border-border pt-3">
                <span className="min-w-0 break-all font-mono text-xs text-fg-muted">
                  {endpoint}
                </span>
                <CopyButton value={endpoint} label={t("Copy server address")} />
              </div>
            </div>
          ) : (
            <div className="grid place-items-center rounded-xl border border-border bg-bg-subtle p-6">
              <Spinner size={20} label={t("Creating a pairing code")} />
            </div>
          )}
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={pairingBusy}
            onClick={() => void newCode()}
          >
            <RefreshCw size={14} className={pairingBusy ? "animate-spin" : undefined} />
            {t("New code")}
          </Button>

          <ol className="list-decimal space-y-1.5 pl-5 text-sm leading-relaxed text-fg-muted">
            <li>{t("Open your AI app and add a custom MCP server.")}</li>
            <li>{t("Paste the server address above as the server URL.")}</li>
            <li>{t("Enter the pairing code when the app asks for it.")}</li>
          </ol>
        </section>

        <section aria-label={t("Paired apps")} className="space-y-3">
          <h3 className="text-sm font-semibold text-fg">{t("Paired apps")}</h3>
          {clients === null ? (
            <div className="grid place-items-center p-4">
              <Spinner size={18} label={t("Loading paired apps")} />
            </div>
          ) : clients.length === 0 ? (
            <p className="text-sm text-fg-muted">{t("No AI apps are paired with this server.")}</p>
          ) : (
            <ul className="divide-y divide-border rounded-xl border border-border">
              {clients.map((client) => {
                const pairedOn = formatDate(client.createdAt);
                return (
                  <li key={client.id} className="flex items-center gap-3 px-3 py-2.5">
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-sm font-medium text-fg">
                        {client.name}
                      </span>
                      {pairedOn ? (
                        <span className="block text-xs text-fg-muted">
                          {t("Paired on {date}", { date: pairedOn })}
                        </span>
                      ) : null}
                    </span>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      disabled={revoking === client.id}
                      onClick={() => void revoke(client)}
                    >
                      {t("Revoke")}
                    </Button>
                  </li>
                );
              })}
            </ul>
          )}
        </section>
      </div>
    </Drawer>
  );
}
