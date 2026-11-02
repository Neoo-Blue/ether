/**
 * Ether tunnel client for TypeScript sidecars (Node >= 22 and Bun).
 *
 * A sidecar connects OUT to Ether over one websocket, so it needs no
 * inbound ports and works behind NAT. Ether then sends HTTP-shaped
 * requests (SPEC section 5.1.2) multiplexed over that socket; this
 * module dispatches them to a handler and can serve byte ranges of
 * local files for `file` streams.
 *
 *   import { Tunnel, json } from "./ether_tunnel";
 *
 *   const tunnel = new Tunnel({
 *     url: "wss://ether.example/ether/plugins/tunnel",
 *     credential: process.env.ETHER_PLUGIN_CREDENTIAL!,
 *     fileRoot: "/media/movies",            // optional
 *     handler: (req) => {
 *       if (req.path === "/manifest.json") return json(MANIFEST);
 *       if (req.path === "/ether/health") return json({ status: "ok" });
 *       return { status: 404, body: new Uint8Array() };
 *     },
 *   });
 *   await tunnel.serveForever();            // reconnects with backoff
 *
 * The credential is minted when the owner pairs the plugin in the
 * Ether console (an identity_grants row on surface "plugin"). Keep
 * it secret. Node's global WebSocket cannot set upgrade headers, so
 * on Node the credential travels in a second subprotocol entry,
 * "ether-credential.<token>", which Ether also accepts; under Bun
 * the header is used when available.
 *
 * Wire protocol "ether-tunnel-v1": JSON text frames, base64 bodies.
 * Requests carry an id; replies may be one "response" frame or a
 * streamed response_start/response_chunk/response_end sequence,
 * which this module uses automatically for file ranges.
 */

export interface TunnelRequest {
  method: string;
  path: string;
  query: Record<string, string[]>;
  headers: Record<string, string[]>;
  body: Uint8Array;
  requestId: string;
  deadline?: string;
  header(name: string): string;
  json(): unknown;
}

export interface TunnelResponse {
  status?: number;
  headers?: Record<string, string[]>;
  body: Uint8Array;
}

export type TunnelHandler = (
  req: TunnelRequest,
) => TunnelResponse | Promise<TunnelResponse>;

export interface TunnelOptions {
  url: string;
  credential: string;
  plugin?: string;
  handler: TunnelHandler;
  /** When set, GET /ether/file/<path> under this root is served with ranges. */
  fileRoot?: string;
  log?: (msg: string) => void;
}

const CHUNK = 64 * 1024;

export function json(value: unknown, status = 200): TunnelResponse {
  return {
    status,
    headers: { "Content-Type": ["application/json"] },
    body: new TextEncoder().encode(JSON.stringify(value)),
  };
}

function b64encode(bytes: Uint8Array): string {
  const buf = (globalThis as { Buffer?: typeof Buffer }).Buffer;
  if (buf) return buf.from(bytes).toString("base64");
  let s = "";
  for (let i = 0; i < bytes.length; i += 0x8000) {
    s += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  }
  return btoa(s);
}

function b64decode(text: string): Uint8Array {
  const buf = (globalThis as { Buffer?: typeof Buffer }).Buffer;
  if (buf) return new Uint8Array(buf.from(text, "base64"));
  const bin = atob(text);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

function parseRange(
  header: string,
  size: number,
): { start: number; end: number; ranged: boolean } {
  if (!header) return { start: 0, end: size - 1, ranged: false };
  if (!header.startsWith("bytes=") || header.includes(",") || size === 0) {
    throw new Error("unsupported range");
  }
  const [lo, hi] = header.slice(6).split("-", 2);
  if (lo === "" && (hi === "" || hi === undefined)) throw new Error("unsatisfiable range");
  if (lo === "") {
    const suffix = Number(hi);
    if (!Number.isFinite(suffix) || suffix <= 0) throw new Error("unsatisfiable range");
    const n = Math.min(suffix, size);
    return { start: size - n, end: size - 1, ranged: true };
  }
  const start = Number(lo);
  if (!Number.isFinite(start) || start < 0 || start >= size) {
    throw new Error("unsatisfiable range");
  }
  let end = size - 1;
  if (hi !== "" && hi !== undefined) {
    end = Number(hi);
    if (!Number.isFinite(end) || end < start) throw new Error("unsatisfiable range");
    if (end >= size) end = size - 1;
  }
  return { start, end, ranged: true };
}

interface Frame {
  type: string;
  id?: string;
  requestId?: string;
  protocol?: string;
  plugin?: string;
  pluginId?: string;
  connectionId?: string;
  features?: string[];
  maxChunkBytes?: number;
  method?: string;
  path?: string;
  query?: Record<string, string[]>;
  headers?: Record<string, string[]>;
  body?: string;
  data?: string;
  deadline?: string;
  status?: number;
  error?: { code: string; message: string };
}

export class Tunnel {
  private stopped = false;
  private ws?: WebSocket;
  private chunkBytes = CHUNK;

  constructor(private opts: TunnelOptions) {}

  stop(): void {
    this.stopped = true;
    this.ws?.close();
  }

  /** Serve until stop() is called, reconnecting with capped backoff. */
  async serveForever(): Promise<void> {
    let backoff = 500;
    while (!this.stopped) {
      const started = Date.now();
      try {
        await this.serveOnce();
      } catch (err) {
        if (err instanceof Error && err.message.includes("401")) throw err;
        this.opts.log?.(`tunnel dropped: ${String(err)}`);
      }
      if (this.stopped) return;
      if (Date.now() - started > 60_000) backoff = 500;
      await new Promise((r) => setTimeout(r, backoff));
      backoff = Math.min(backoff * 2, 30_000);
    }
  }

  /** Serve one tunnel connection until it ends. */
  serveOnce(): Promise<void> {
    return new Promise((resolve, reject) => {
      if (typeof WebSocket === "undefined") {
        reject(new Error("global WebSocket is unavailable (need Node >= 22 or Bun)"));
        return;
      }
      // Header auth under Bun (its WebSocket takes an options
      // object), subprotocol auth under Node: Ether accepts both.
      // The subprotocol token alphabet is base64url-safe for Ether
      // grant credentials.
      const protocols = ["ether-tunnel-v1", `ether-credential.${this.opts.credential}`];
      const isBun = "Bun" in globalThis;
      const ws = isBun
        ? new WebSocket(this.opts.url, {
            protocols,
            headers: { "X-Ether-Plugin-Credential": this.opts.credential },
          } as never)
        : new WebSocket(this.opts.url, protocols);
      this.ws = ws;
      let opened = false;
      ws.addEventListener("open", () => {
        opened = true;
        this.send({
          type: "hello",
          protocol: "ether-tunnel-v1",
          plugin: this.opts.plugin ?? "",
          features: ["range", "stream"],
        });
      });
      ws.addEventListener("message", (ev: MessageEvent) => {
        const text = typeof ev.data === "string" ? ev.data : "";
        if (!text) return;
        let frame: Frame;
        try {
          frame = JSON.parse(text) as Frame;
        } catch {
          return;
        }
        if (frame.type === "welcome") {
          if (frame.maxChunkBytes && frame.maxChunkBytes > 0) {
            this.chunkBytes = frame.maxChunkBytes;
          }
          this.opts.log?.(`tunnel connected as ${frame.pluginId ?? "?"}`);
          return;
        }
        if (frame.type === "request") {
          void this.serveRequest(frame);
        }
        // "cancel" needs no action: a late reply is dropped by Ether.
      });
      ws.addEventListener("close", (ev: CloseEvent) => {
        if (!opened && ev.code === 1006) {
          reject(new Error("tunnel refused (HTTP 401 if the credential is wrong)"));
        } else {
          resolve();
        }
      });
      ws.addEventListener("error", () => {
        if (!opened) reject(new Error("tunnel connect failed"));
      });
    });
  }

  private send(frame: Frame): void {
    this.ws?.send(JSON.stringify(frame));
  }

  private async serveRequest(frame: Frame): Promise<void> {
    const id = frame.id ?? "";
    try {
      const headers = frame.headers ?? {};
      const req: TunnelRequest = {
        method: frame.method || "GET",
        path: frame.path || "/",
        query: frame.query ?? {},
        headers,
        body: frame.body ? b64decode(frame.body) : new Uint8Array(),
        requestId: frame.requestId ?? "",
        deadline: frame.deadline,
        header(name: string): string {
          for (const [k, v] of Object.entries(headers)) {
            if (k.toLowerCase() === name.toLowerCase() && v.length > 0) return v[0];
          }
          return "";
        },
        json(): unknown {
          return JSON.parse(new TextDecoder().decode(this.body) || "null");
        },
      };
      if (this.opts.fileRoot && req.path.startsWith("/ether/file/")) {
        await this.serveFile(req, id);
        return;
      }
      const resp = await this.opts.handler(req);
      this.send({
        type: "response",
        id,
        status: resp.status ?? 200,
        headers: resp.headers ?? {},
        body: resp.body.length ? b64encode(resp.body) : "",
      });
    } catch (err) {
      // A handler bug fails one call, never the tunnel.
      try {
        this.send({
          type: "error",
          id,
          error: { code: "error", message: err instanceof Error ? err.message : String(err) },
        });
      } catch {
        /* socket already gone */
      }
    }
  }

  private async serveFile(req: TunnelRequest, id: string): Promise<void> {
    // Node and Bun both expose node:fs; load it lazily so the module
    // still parses in a plain type check without @types/node.
    const fs = (await import("node:fs" as string)) as typeof import("fs");
    const pathMod = (await import("node:path" as string)) as typeof import("path");
    const root = pathMod.resolve(this.opts.fileRoot!);
    const rel = decodeURIComponent(req.path.slice("/ether/file/".length));
    const full = pathMod.resolve(root, "." + pathMod.sep + rel.split("/").join(pathMod.sep));
    if (full !== root && !full.startsWith(root + pathMod.sep)) {
      throw new Error("path escapes the file root");
    }
    let size: number;
    try {
      const stat = fs.statSync(full);
      if (!stat.isFile()) throw new Error("not a file");
      size = stat.size;
    } catch {
      this.send({ type: "error", id, error: { code: "not_found", message: "no such file" } });
      return;
    }
    let range: { start: number; end: number; ranged: boolean };
    try {
      range = parseRange(req.header("Range"), size);
    } catch {
      this.send({
        type: "response",
        id,
        status: 416,
        headers: { "Content-Range": [`bytes */${size}`] },
        body: "",
      });
      return;
    }
    const { start, end, ranged } = range;
    const headers: Record<string, string[]> = {
      "Accept-Ranges": ["bytes"],
      "Content-Length": [String(end - start + 1)],
    };
    if (ranged) headers["Content-Range"] = [`bytes ${start}-${end}/${size}`];
    this.send({ type: "response_start", id, status: ranged ? 206 : 200, headers });
    if (req.method !== "HEAD") {
      const stream = fs.createReadStream(full, {
        start,
        end,
        highWaterMark: this.chunkBytes,
      });
      for await (const chunk of stream) {
        const bytes = chunk as Uint8Array;
        this.send({ type: "response_chunk", id, data: b64encode(bytes) });
      }
    }
    this.send({ type: "response_end", id });
  }
}
