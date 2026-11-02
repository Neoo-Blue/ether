var EtherPlugin = (() => {
  var __defProp = Object.defineProperty;
  var __getOwnPropDesc = Object.getOwnPropertyDescriptor;
  var __getOwnPropNames = Object.getOwnPropertyNames;
  var __hasOwnProp = Object.prototype.hasOwnProperty;
  var __defNormalProp = (obj, key, value) => key in obj ? __defProp(obj, key, { enumerable: true, configurable: true, writable: true, value }) : obj[key] = value;
  var __export = (target, all) => {
    for (var name in all)
      __defProp(target, name, { get: all[name], enumerable: true });
  };
  var __copyProps = (to, from, except, desc) => {
    if (from && typeof from === "object" || typeof from === "function") {
      for (let key of __getOwnPropNames(from))
        if (!__hasOwnProp.call(to, key) && key !== except)
          __defProp(to, key, { get: () => from[key], enumerable: !(desc = __getOwnPropDesc(from, key)) || desc.enumerable });
    }
    return to;
  };
  var __toCommonJS = (mod) => __copyProps(__defProp({}, "__esModule", { value: true }), mod);
  var __publicField = (obj, key, value) => __defNormalProp(obj, typeof key !== "symbol" ? key + "" : key, value);

  // sdk/examples/subdl/plugin.ts
  var plugin_exports = {};
  __export(plugin_exports, {
    handle: () => handle,
    subdl: () => subdl
  });

  // sdk/typescript/src/types.ts
  var PluginHttpError = class extends Error {
    constructor(status, code, message) {
      super(message);
      __publicField(this, "status");
      __publicField(this, "code");
      this.name = "PluginHttpError";
      this.status = status;
      this.code = code;
    }
  };

  // sdk/typescript/src/host.ts
  function isHostedRuntime() {
    const g = globalThis;
    return typeof g.__ether_host === "function" || typeof g.ether === "object";
  }
  function createHostedClient() {
    const g = globalThis;
    const callHost = (name, args) => {
      if (typeof g.__ether_host === "function") {
        const raw = g.__ether_host(name, JSON.stringify(args));
        return raw ? JSON.parse(raw) : null;
      }
      throw new Error(`Host service ${name} is not available outside the Ether runtime`);
    };
    return {
      kv: {
        get(key) {
          if (g.ether?.kv) return g.ether.kv.get(key) ?? null;
          return callHost("kv.get", [key]);
        },
        put(key, value) {
          if (g.ether?.kv) {
            g.ether.kv.put(key, String(value));
            return;
          }
          callHost("kv.put", [key, String(value)]);
        },
        delete(key) {
          if (g.ether?.kv) {
            g.ether.kv.delete(key);
            return;
          }
          callHost("kv.delete", [key]);
        },
        list(prefix = "") {
          if (g.ether?.kv) return g.ether.kv.list(prefix) ?? [];
          return callHost("kv.list", [prefix]) ?? [];
        }
      },
      secret: {
        get(name) {
          if (g.ether?.secret) return g.ether.secret.get(name);
          return callHost("secret.get", [name]);
        }
      },
      http: {
        fetch(req) {
          const normalized = {
            method: req.method ?? "GET",
            url: req.url,
            headers: req.headers ?? {},
            ...req.bodyBase64 !== void 0 ? { bodyBase64: req.bodyBase64 } : { body: req.body ?? "" }
          };
          if (g.ether?.http) return g.ether.http.fetch(normalized);
          return callHost("http.fetch", [normalized]);
        }
      },
      catalog: {
        search(query, limit = 50, offset = 0) {
          if (typeof g.__ether_host === "function") {
            return callHost("catalog.search", [
              query,
              limit,
              offset
            ]);
          }
          if (g.ether?.catalog) return g.ether.catalog.search(query, limit, offset);
          return { titles: [] };
        },
        get(id) {
          if (g.ether?.catalog) return g.ether.catalog.get(id);
          return callHost("catalog.get", [id]);
        }
      },
      viewers: {
        history(viewerId, limit = 50, offset = 0) {
          if (typeof g.__ether_host === "function") {
            return callHost("viewers.history", [viewerId, limit, offset]) ?? [];
          }
          if (g.ether?.viewers) return g.ether.viewers.history(viewerId, limit, offset) ?? [];
          return [];
        },
        setWatchState(viewerId, titleId, watched) {
          if (typeof g.__ether_host === "function") {
            callHost("viewers.setWatchState", [viewerId, titleId, watched]);
            return;
          }
          if (g.ether?.viewers) {
            g.ether.viewers.setWatchState(viewerId, titleId, watched);
            return;
          }
          callHost("viewers.setWatchState", [viewerId, titleId, watched]);
        }
      },
      collections: {
        upsert(collection) {
          if (g.ether?.collections) {
            g.ether.collections.upsert(collection);
            return;
          }
          callHost("collections.upsert", [collection]);
        }
      },
      folders: {
        list(root, path = "") {
          if (g.ether?.folders) return g.ether.folders.list(root, path) ?? [];
          return callHost("folders.list", [root, path]) ?? [];
        },
        stat(root, path) {
          if (g.ether?.folders) return g.ether.folders.stat(root, path);
          return callHost("folders.stat", [root, path]);
        }
      },
      notify: {
        owner(text) {
          if (g.ether?.notify) {
            g.ether.notify.owner(text);
            return;
          }
          callHost("notify.owner", [text]);
        }
      },
      log(level, message, attrs) {
        if (typeof g.__ether_host === "function") {
          try {
            g.__ether_host(
              "log",
              JSON.stringify(attrs ? [level, message, attrs] : [level, message])
            );
          } catch {
          }
          return;
        }
        if (g.ether?.log) {
          g.ether.log(level, message, attrs ? JSON.stringify(attrs) : "");
        }
      },
      abort(message = "aborted") {
        if (g.ether?.abort) {
          return g.ether.abort(message);
        }
        throw new Error(
          "__ETHER_FAULT__" + JSON.stringify({ code: "aborted", message: String(message) })
        );
      }
    };
  }
  var ether = isHostedRuntime() ? createHostedClient() : createRemoteHostClient();
  function readEnv(name) {
    const g = globalThis;
    return g.process?.env?.[name];
  }
  function createRemoteHostClient(opts = {}) {
    const rawBase = opts.baseUrl ?? readEnv("ETHER_HOST_URL") ?? "http://127.0.0.1:32450";
    const base = rawBase.replace(/\/+$/, "");
    const credential = opts.credential ?? readEnv("ETHER_PLUGIN_CREDENTIAL") ?? "";
    const doFetch = opts.fetchImpl ?? fetch;
    const callJson = async (method, path, body, allow404 = false) => {
      const headers = {
        "X-Ether-Plugin-Credential": credential,
        Accept: "application/json"
      };
      let payload;
      if (body !== void 0) {
        headers["Content-Type"] = "application/json";
        payload = JSON.stringify(body);
      }
      const resp = await doFetch(`${base}${path}`, {
        method,
        headers,
        body: payload
      });
      if (allow404 && resp.status === 404) {
        return null;
      }
      const text = await resp.text();
      const parsed = text ? JSON.parse(text) : null;
      if (!resp.ok) {
        const errObj = parsed && typeof parsed.error === "object" ? parsed.error : null;
        throw new Error(
          errObj?.message ?? `Host call ${method} ${path} failed with HTTP ${resp.status}`
        );
      }
      return parsed;
    };
    return {
      kv: {
        async get(key) {
          const res = await callJson(
            "GET",
            `/ether/plugin-host/kv/${encodeURIComponent(key)}`,
            void 0,
            true
          );
          return res ? res.value : null;
        },
        async put(key, value) {
          await callJson("PUT", `/ether/plugin-host/kv/${encodeURIComponent(key)}`, {
            value: String(value)
          });
        },
        async delete(key) {
          await callJson("DELETE", `/ether/plugin-host/kv/${encodeURIComponent(key)}`);
        },
        async list(prefix = "") {
          const q = prefix ? `?prefix=${encodeURIComponent(prefix)}` : "";
          const res = await callJson("GET", `/ether/plugin-host/kv${q}`);
          return res?.keys ?? [];
        }
      },
      secret: {
        get(_name) {
          return Promise.reject(
            new Error(
              "secret.get is only available in hosted plugins; remote plugins receive secrets via /ether/configure"
            )
          );
        }
      },
      http: {
        async fetch(req) {
          const resp = await doFetch(req.url, {
            method: req.method ?? "GET",
            headers: req.headers,
            body: req.body
          });
          const outHeaders = {};
          resp.headers.forEach((v, k) => {
            outHeaders[k] = v;
          });
          return {
            status: resp.status,
            headers: outHeaders,
            body: await resp.text()
          };
        }
      },
      catalog: {
        async search(query, limit = 50, offset = 0) {
          const params = new URLSearchParams({
            q: query,
            limit: String(limit),
            offset: String(offset)
          });
          const res = await callJson(
            "GET",
            `/ether/plugin-host/catalog/search?${params.toString()}`
          );
          return res ?? { titles: [] };
        },
        async get(id) {
          return callJson(
            "GET",
            `/ether/plugin-host/catalog/${encodeURIComponent(id)}`,
            void 0,
            true
          );
        }
      },
      viewers: {
        async history(viewerId, limit = 50, offset = 0) {
          const params = new URLSearchParams({
            limit: String(limit),
            offset: String(offset)
          });
          const res = await callJson(
            "GET",
            `/ether/plugin-host/viewers/${viewerId}/history?${params.toString()}`
          );
          return res ?? [];
        },
        async setWatchState(viewerId, titleId, watched) {
          await callJson("POST", "/ether/plugin-host/viewers/watch-state", {
            viewerId,
            titleId,
            watched
          });
        }
      },
      collections: {
        async upsert(collection) {
          await callJson("POST", "/ether/plugin-host/collections/upsert", collection);
        }
      },
      folders: {
        list() {
          return Promise.reject(
            new Error(
              "folders.list is for hosted plugins; sidecar plugins read their local fileRoot directly"
            )
          );
        },
        stat() {
          return Promise.reject(
            new Error(
              "folders.stat is for hosted plugins; sidecar plugins read their local fileRoot directly"
            )
          );
        }
      },
      notify: {
        async owner(text) {
          await callJson("POST", "/ether/plugin-host/notify", { text });
        }
      },
      async log(level, message, attrs) {
        await callJson("POST", "/ether/plugin-host/log", { level, message, attrs });
      },
      abort(message = "aborted") {
        throw new Error(message);
      }
    };
  }

  // sdk/typescript/src/i18n.ts
  function tRef(key) {
    return `$t:${key}`;
  }
  function createI18n(strings = {}, defaultLocale = "en") {
    const resolveKey = (rawKey, params, locale = defaultLocale) => {
      const key = rawKey.startsWith("$t:") ? rawKey.slice(3) : rawKey;
      const base = locale ? locale.split("-")[0] ?? "" : "";
      const candidates = [locale, base, "en"];
      let template;
      for (const loc of candidates) {
        if (!loc) continue;
        const table = strings[loc];
        if (table && Object.prototype.hasOwnProperty.call(table, key)) {
          template = table[key];
          break;
        }
      }
      if (template === void 0) {
        template = key;
      }
      if (!params) return template;
      return template.replace(/\{([A-Za-z0-9_]+)\}/g, (match, name) => {
        const val = params[name];
        return val !== void 0 ? String(val) : match;
      });
    };
    return {
      t: resolveKey,
      ref: tRef,
      catalog: () => strings
    };
  }

  // sdk/typescript/src/settings.ts
  function collectSettingFields(schema) {
    if (!schema || typeof schema !== "object") return [];
    const out = [];
    const walk = (node) => {
      if (!node || typeof node !== "object") return;
      if (Array.isArray(node)) {
        for (const item of node) walk(item);
        return;
      }
      const obj = node;
      const key = typeof obj.key === "string" ? obj.key : typeof obj.name === "string" ? obj.name : typeof obj.id === "string" ? obj.id : typeof obj.field === "string" ? obj.field : "";
      const kind = typeof obj.kind === "string" ? obj.kind : "";
      if (key && kind && kind !== "group") {
        out.push({
          key,
          kind,
          label: typeof obj.label === "string" ? obj.label : void 0,
          hint: typeof obj.hint === "string" ? obj.hint : void 0,
          required: obj.required === true,
          default: obj.default,
          min: typeof obj.min === "number" ? obj.min : void 0,
          max: typeof obj.max === "number" ? obj.max : void 0
        });
      }
      for (const v of Object.values(obj)) {
        if (v && typeof v === "object") walk(v);
      }
    };
    walk(schema);
    return out;
  }
  function decodeSettings(schema, incoming, defaults) {
    const fields = collectSettingFields(schema);
    const result = { ...defaults };
    const errors = {};
    for (const field of fields) {
      if (field.default !== void 0 && result[field.key] === void 0) {
        result[field.key] = field.default;
      }
    }
    if (incoming && typeof incoming === "object") {
      for (const [k, v] of Object.entries(incoming)) {
        result[k] = v;
      }
    }
    for (const field of fields) {
      const val = result[field.key];
      const ptr = `/${field.key}`;
      if (field.required && (val === void 0 || val === null || val === "")) {
        errors[ptr] = "is required";
        continue;
      }
      if (val === void 0 || val === null || val === "") {
        continue;
      }
      switch (field.kind) {
        case "number":
        case "bytes":
        case "viewer": {
          const num = typeof val === "number" ? val : Number(val);
          if (!Number.isFinite(num)) {
            errors[ptr] = "must be a number";
          } else {
            if (field.min !== void 0 && num < field.min) {
              errors[ptr] = `must be at least ${field.min}`;
            }
            if (field.max !== void 0 && num > field.max) {
              errors[ptr] = `must be at most ${field.max}`;
            }
            result[field.key] = num;
          }
          break;
        }
        case "bool": {
          if (typeof val !== "boolean") {
            errors[ptr] = "must be true or false";
          }
          break;
        }
        case "list":
        case "multiSelect":
        case "orderedList": {
          if (!Array.isArray(val)) {
            errors[ptr] = "must be a list";
          }
          break;
        }
        case "url": {
          if (typeof val !== "string" || !val.startsWith("http://") && !val.startsWith("https://")) {
            errors[ptr] = "must be an http or https URL";
          }
          break;
        }
        default:
          break;
      }
    }
    return {
      settings: result,
      errors
    };
  }

  // sdk/typescript/src/router.ts
  function getHeader(headers, name) {
    if (!headers) return void 0;
    const target = name.toLowerCase();
    for (const [k, v] of Object.entries(headers)) {
      if (k.toLowerCase() === target) {
        return Array.isArray(v) ? v[0] : v;
      }
    }
    return void 0;
  }
  function parseUrlPathAndQuery(rawPath, rawQuery) {
    let pathOnly = rawPath || "/";
    const query = {};
    if (rawQuery) {
      for (const [k, v] of Object.entries(rawQuery)) {
        if (Array.isArray(v)) {
          if (v.length > 0) query[k] = String(v[0]);
        } else if (v !== void 0 && v !== null) {
          query[k] = String(v);
        }
      }
    }
    const qIndex = pathOnly.indexOf("?");
    if (qIndex >= 0) {
      const qs = pathOnly.slice(qIndex + 1);
      pathOnly = pathOnly.slice(0, qIndex) || "/";
      const params = parseParams(qs);
      Object.entries(params).forEach(([k, v]) => {
        if (query[k] === void 0) {
          query[k] = v;
        }
      });
    }
    if (!pathOnly.startsWith("/")) {
      pathOnly = "/" + pathOnly;
    }
    return { path: pathOnly, query };
  }
  function parseJsonBody(body) {
    if (body === void 0 || body === null || body === "") {
      return {};
    }
    if (typeof body === "string") {
      const trimmed = body.trim();
      if (!trimmed) return {};
      const parsed = JSON.parse(trimmed);
      if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
        return parsed;
      }
      return {};
    }
    if (typeof body === "object" && !Array.isArray(body)) {
      return body;
    }
    return {};
  }
  function okResponse(body, headers, status = 200) {
    return {
      status,
      headers: {
        "Content-Type": "application/json",
        ...headers ?? {}
      },
      body
    };
  }
  function errorResponse(status, code, message) {
    return {
      status,
      headers: { "Content-Type": "application/json" },
      body: { error: { code, message } }
    };
  }
  function isEtherHostFault(err2) {
    if (!err2) return false;
    const msg = err2 instanceof Error ? err2.message : String(err2);
    return msg.includes("__ETHER_FAULT__");
  }
  function parseParams(input) {
    const result = {};
    for (const part of input.split("&")) {
      if (!part) continue;
      const index = part.indexOf("=");
      const decode = (value) => decodeURIComponent(value.replace(/\+/g, " "));
      result[decode(index < 0 ? part : part.slice(0, index))] = decode(index < 0 ? "" : part.slice(index + 1));
    }
    return result;
  }
  function parseExtraSegment(rawExtra) {
    const out = {};
    if (!rawExtra) return out;
    const params = parseParams(rawExtra);
    Object.entries(params).forEach(([k, v]) => {
      out[k] = v;
    });
    return out;
  }
  function matchStremioRoute(path, query) {
    if (!path.endsWith(".json")) return null;
    const trimmed = path.slice(1, -5);
    const parts = trimmed.split("/");
    if (parts.length < 3 || parts.length > 4) return null;
    const resource = parts[0];
    if (resource !== "catalog" && resource !== "meta" && resource !== "stream" && resource !== "subtitles") {
      return null;
    }
    const type = decodeURIComponent(parts[1] ?? "");
    const id = decodeURIComponent(parts[2] ?? "");
    const extra = parseExtraSegment(parts[3]);
    for (const [k, v] of Object.entries(query)) {
      if (extra[k] === void 0) {
        extra[k] = v;
      }
    }
    return { resource, type, id, extra };
  }
  function buildInlineStrings(manifest, explicit) {
    const catalog = {};
    if (manifest.ether?.strings) {
      for (const [loc, val] of Object.entries(manifest.ether.strings)) {
        if (val && typeof val === "object") {
          catalog[loc] = { ...val };
        }
      }
    }
    if (explicit) {
      for (const [loc, table] of Object.entries(explicit)) {
        catalog[loc] = { ...catalog[loc] ?? {}, ...table };
      }
    }
    return catalog;
  }
  function plugin(def) {
    if (!def || !def.manifest) {
      throw new Error("Plugin manifest is required.");
    }
    const manifest = def.manifest;
    if (!manifest.id || !manifest.name || !manifest.version) {
      throw new Error("Plugin manifest requires id, name, and version.");
    }
    const routes = def.routes ?? {};
    const i18n = createI18n(
      buildInlineStrings(manifest, def.strings),
      def.defaultLocale ?? "en"
    );
    const settingsSchema = manifest.ether?.settings;
    let currentSettings = decodeSettings(
      settingsSchema,
      void 0,
      def.defaultSettings
    ).settings;
    const pageRoutes = /* @__PURE__ */ new Map();
    if (manifest.ether?.contributes?.pages) {
      for (const p of manifest.ether.contributes.pages) {
        if (p.route) {
          pageRoutes.set(p.route, p.key);
        }
      }
    }
    if (manifest.ether?.contributes?.slots) {
      for (const s of manifest.ether.contributes.slots) {
        if (s.route) {
          pageRoutes.set(s.route, s.key);
        }
      }
    }
    function buildContext(req) {
      const { query } = parseUrlPathAndQuery(req.path, req.query);
      const localeHeader = req.locale ?? query.locale ?? getHeader(req.headers, "X-Ether-Locale") ?? getHeader(req.headers, "Accept-Language") ?? def.defaultLocale ?? "en";
      const locale = localeHeader.split(",")[0]?.trim() || "en";
      const pickToken = getHeader(req.headers, "X-Ether-Pick");
      const requestId = getHeader(req.headers, "X-Ether-Request-ID");
      const hostClient = def.host ?? (isHostedRuntime() ? createHostedClient() : createRemoteHostClient(def.remoteHostOptions));
      return {
        manifest,
        settings: currentSettings,
        locale,
        pickToken,
        requestId,
        headers: req.headers ?? {},
        query,
        ether: hostClient,
        i18n,
        t: (key, params) => i18n.t(key, params, locale)
      };
    }
    function mapResult(value, transform = (r) => okResponse(r)) {
      if (value instanceof Promise) {
        return value.then(transform);
      }
      return transform(value);
    }
    function dispatchCore(req, ctx) {
      const method = (req.method || "GET").toUpperCase();
      const { path, query } = parseUrlPathAndQuery(req.path, req.query);
      if (method === "GET" && path === "/manifest.json") {
        return okResponse(manifest);
      }
      if (method === "GET" && path === "/ether/health") {
        if (routes.health) {
          return mapResult(routes.health(ctx));
        }
        const fallback = { status: "ok" };
        return okResponse(fallback);
      }
      if (method === "POST" && path === "/ether/configure") {
        let bodyObj;
        try {
          bodyObj = parseJsonBody(req.body);
        } catch {
          return errorResponse(400, "invalid_json", "Request body must be valid JSON.");
        }
        const incoming = bodyObj.settings && typeof bodyObj.settings === "object" && !Array.isArray(bodyObj.settings) ? bodyObj.settings : bodyObj;
        const decoded = decodeSettings(settingsSchema, incoming, def.defaultSettings);
        if (Object.keys(decoded.errors).length > 0) {
          return okResponse({ errors: decoded.errors });
        }
        if (routes.configure) {
          const nextCtx = { ...ctx, settings: decoded.settings };
          return mapResult(routes.configure(decoded.settings, nextCtx), (res) => {
            const errors = res?.errors ?? {};
            if (Object.keys(errors).length === 0) {
              currentSettings = decoded.settings;
            }
            return okResponse({ errors });
          });
        }
        currentSettings = decoded.settings;
        return okResponse({ errors: {} });
      }
      if (method === "POST" && path === "/ether/subtitles/download") {
        if (!ctx.pickToken?.trim()) return errorResponse(403, "pick_required", "A subtitle pick is required.");
        if (!routes.subtitlesDownload) return errorResponse(404, "not_found", "Subtitle download handler is not registered.");
        const body = parseJsonBody(req.body);
        return mapResult(routes.subtitlesDownload({ id: String(body.id || ""), lang: String(body.lang || ""), format: String(body.format || "") }, ctx));
      }
      if (method === "POST" && path === "/ether/test") {
        if (routes.test) {
          return mapResult(routes.test(ctx));
        }
        const fallback = { ok: true };
        return okResponse(fallback);
      }
      if (method === "GET" && path.startsWith("/ether/options/")) {
        const field = decodeURIComponent(path.slice("/ether/options/".length));
        if (!routes.options) {
          return okResponse([]);
        }
        return mapResult(routes.options(field, ctx));
      }
      if (method === "POST" && path === "/ether/stream/open") {
        if (!routes.openStream) {
          return errorResponse(404, "not_found", "Stream open handler is not registered.");
        }
        const bodyObj = parseJsonBody(req.body);
        return mapResult(
          routes.openStream({ streamId: String(bodyObj.streamId ?? "") }, ctx)
        );
      }
      if (method === "GET" && path.startsWith("/ether/markers/") && path.endsWith(".json")) {
        if (!routes.markers) {
          return errorResponse(404, "not_found", "Markers handler is not registered.");
        }
        const trimmed = path.slice("/ether/markers/".length, -5);
        const parts = trimmed.split("/");
        if (parts.length < 2) {
          return errorResponse(400, "invalid_path", "Markers route requires type and id.");
        }
        return mapResult(
          routes.markers(
            {
              type: decodeURIComponent(parts[0] ?? ""),
              id: decodeURIComponent(parts.slice(1).join("/"))
            },
            ctx
          )
        );
      }
      if (method === "GET" && path === "/ether/channels.json") {
        if (!routes.channels) {
          return errorResponse(404, "not_found", "Channels handler is not registered.");
        }
        return mapResult(routes.channels(ctx));
      }
      if (method === "GET" && path.startsWith("/ether/guide/") && path.endsWith(".json")) {
        if (!routes.guide) {
          return errorResponse(404, "not_found", "Guide handler is not registered.");
        }
        const channelId = decodeURIComponent(path.slice("/ether/guide/".length, -5));
        return mapResult(
          routes.guide({ channelId, from: query.from, to: query.to }, ctx)
        );
      }
      if (method === "POST" && path === "/ether/notify") {
        if (!routes.notify) {
          return errorResponse(404, "not_found", "Notify handler is not registered.");
        }
        const bodyObj = parseJsonBody(req.body);
        return mapResult(
          routes.notify(
            {
              title: typeof bodyObj.title === "string" ? bodyObj.title : void 0,
              message: String(bodyObj.message ?? bodyObj.text ?? ""),
              event: typeof bodyObj.event === "string" ? bodyObj.event : void 0,
              settings: bodyObj.settings && typeof bodyObj.settings === "object" ? bodyObj.settings : void 0
            },
            ctx
          )
        );
      }
      if (method === "POST" && path === "/ether/event") {
        if (!routes.event) {
          return errorResponse(404, "not_found", "Event handler is not registered.");
        }
        const bodyObj = parseJsonBody(req.body);
        const events = Array.isArray(bodyObj.events) ? bodyObj.events : [];
        return mapResult(routes.event(events, ctx), (res) => {
          if (res && Array.isArray(res.ack)) {
            return okResponse({ ack: res.ack });
          }
          return okResponse({ ack: events.map((e) => e.id) });
        });
      }
      if (method === "GET" && (path === "/ether/view" || path.startsWith("/ether/view/") || pageRoutes.has(path))) {
        if (!routes.view) {
          return errorResponse(404, "not_found", "View handler is not registered.");
        }
        let page = pageRoutes.get(path);
        if (!page) {
          page = path === "/ether/view" ? "home" : decodeURIComponent(path.slice("/ether/view/".length));
        }
        return mapResult(routes.view({ page, query }, ctx));
      }
      if (method === "POST" && path.startsWith("/ether/action/")) {
        if (!routes.action) {
          return errorResponse(404, "not_found", "Action handler is not registered.");
        }
        const action = decodeURIComponent(path.slice("/ether/action/".length));
        const input = parseJsonBody(req.body);
        return mapResult(routes.action({ action, input }, ctx));
      }
      if (method === "POST" && path.startsWith("/ether/tool/")) {
        if (!routes.tool) {
          return errorResponse(404, "not_found", "Tool handler is not registered.");
        }
        const name = decodeURIComponent(path.slice("/ether/tool/".length));
        const input = parseJsonBody(req.body);
        return mapResult(routes.tool({ name, input }, ctx));
      }
      if (method === "POST" && path.startsWith("/ether/job/")) {
        if (!routes.job) {
          return errorResponse(404, "not_found", "Job handler is not registered.");
        }
        const name = decodeURIComponent(path.slice("/ether/job/".length));
        return mapResult(routes.job({ name }, ctx));
      }
      if (method === "POST" && path === "/ether/rank-key") {
        if (!routes.rankKey) {
          return errorResponse(404, "not_found", "Rank key handler is not registered.");
        }
        const bodyObj = parseJsonBody(req.body);
        const candidate = bodyObj.candidate ?? bodyObj;
        const cfg = bodyObj.config ?? currentSettings;
        const score = Number(routes.rankKey(candidate, cfg)) || 0;
        return okResponse({ score });
      }
      if (method === "POST" && path === "/ether/cond-match") {
        if (!routes.condMatch) {
          return errorResponse(404, "not_found", "Condition match handler is not registered.");
        }
        const bodyObj = parseJsonBody(req.body);
        const candidate = bodyObj.candidate ?? bodyObj;
        const operator = String(bodyObj.operator ?? "eq");
        const value = bodyObj.value;
        const matched = Boolean(routes.condMatch(candidate, operator, value));
        return okResponse({ match: matched });
      }
      if ((method === "GET" || method === "HEAD") && path.startsWith("/ether/file/")) {
        if (!routes.file) {
          return errorResponse(404, "not_found", "File handler is not registered.");
        }
        const relPath = decodeURIComponent(path.slice("/ether/file/".length));
        if (!relPath || relPath.startsWith("/") || relPath.split("/").includes("..")) {
          return errorResponse(400, "invalid_path", "File path must be a clean relative path.");
        }
        const rangeHeader = getHeader(req.headers, "Range");
        return mapResult(
          routes.file({ path: relPath, range: rangeHeader, headOnly: method === "HEAD" }, ctx),
          (fileRes) => {
            const headers = {
              "Content-Type": fileRes.contentType || "application/octet-stream",
              "Accept-Ranges": "bytes",
              ...fileRes.headers ?? {}
            };
            if (fileRes.contentRange) {
              headers["Content-Range"] = fileRes.contentRange;
            }
            if (fileRes.totalSize !== void 0 && !fileRes.contentRange) {
              headers["Content-Length"] = String(fileRes.totalSize);
            }
            return {
              status: fileRes.status ?? (fileRes.contentRange ? 206 : 200),
              headers,
              body: method === "HEAD" ? new Uint8Array(0) : fileRes.body
            };
          }
        );
      }
      if (method === "GET") {
        const stremio = matchStremioRoute(path, query);
        if (stremio) {
          if (stremio.resource === "subtitles") {
            if (!routes.subtitles) {
              return errorResponse(404, "not_found", "Subtitles handler is not registered.");
            }
            const videoSize = stremio.extra.videoSize ? Number(stremio.extra.videoSize) : void 0;
            return mapResult(
              routes.subtitles(
                {
                  type: stremio.type,
                  id: stremio.id,
                  extra: stremio.extra,
                  pickToken: ctx.pickToken,
                  videoHash: stremio.extra.videoHash,
                  videoSize: Number.isFinite(videoSize) ? videoSize : void 0,
                  filename: stremio.extra.filename
                },
                ctx
              )
            );
          }
          if (stremio.resource === "catalog") {
            if (!routes.catalog) {
              return errorResponse(404, "not_found", "Catalog handler is not registered.");
            }
            const skip = stremio.extra.skip ? Number(stremio.extra.skip) || 0 : 0;
            return mapResult(
              routes.catalog(
                {
                  type: stremio.type,
                  id: stremio.id,
                  extra: stremio.extra,
                  skip,
                  search: stremio.extra.search,
                  genre: stremio.extra.genre
                },
                ctx
              )
            );
          }
          if (stremio.resource === "meta") {
            if (!routes.meta) {
              return errorResponse(404, "not_found", "Meta handler is not registered.");
            }
            return mapResult(routes.meta({ type: stremio.type, id: stremio.id }, ctx));
          }
          if (stremio.resource === "stream") {
            if (!routes.stream) {
              return errorResponse(404, "not_found", "Stream handler is not registered.");
            }
            return mapResult(
              routes.stream({ type: stremio.type, id: stremio.id, extra: stremio.extra }, ctx)
            );
          }
        }
      }
      return errorResponse(404, "not_found", `No route matched ${method} ${path}.`);
    }
    function handleRouteError(err2) {
      if (isEtherHostFault(err2)) {
        throw err2;
      }
      if (err2 instanceof PluginHttpError) {
        return errorResponse(err2.status, err2.code, err2.message);
      }
      if (err2 && typeof err2 === "object" && "status" in err2 && typeof err2.status === "number") {
        const e = err2;
        return errorResponse(e.status, e.code || "plugin_error", e.message || "Request failed.");
      }
      const message = err2 instanceof Error ? err2.message : String(err2);
      return errorResponse(500, "internal_error", message || "Internal plugin error.");
    }
    function handleRequest(req) {
      try {
        const ctx = buildContext(req);
        const res = dispatchCore(req, ctx);
        return res instanceof Promise ? res.catch(handleRouteError) : res;
      } catch (err2) {
        return handleRouteError(err2);
      }
    }
    async function dispatch(req) {
      try {
        const ctx = buildContext(req);
        return await dispatchCore(req, ctx);
      } catch (err2) {
        return handleRouteError(err2);
      }
    }
    function rankKey(candidate, config) {
      if (!routes.rankKey) return 0;
      return Number(routes.rankKey(candidate, config ?? currentSettings)) || 0;
    }
    function condMatch(candidate, operator, value) {
      if (!routes.condMatch) return false;
      return Boolean(routes.condMatch(candidate, operator, value));
    }
    const instance = {
      manifest,
      routes,
      getSettings: () => currentSettings,
      setSettings: (incoming) => {
        const decoded = decodeSettings(settingsSchema, incoming, def.defaultSettings);
        if (Object.keys(decoded.errors).length === 0) {
          currentSettings = decoded.settings;
        }
        return decoded;
      },
      handle: handleRequest,
      dispatch,
      rank_key: rankKey,
      cond_match: condMatch
    };
    if (isHostedRuntime()) {
      const g = globalThis;
      g.handle = handleRequest;
      g.rank_key = rankKey;
      g.cond_match = condMatch;
    }
    return instance;
  }

  // sdk/examples/subdl/vendor/fflate.js
  var u8 = Uint8Array;
  var u16 = Uint16Array;
  var i32 = Int32Array;
  var fleb = new u8([
    0,
    0,
    0,
    0,
    0,
    0,
    0,
    0,
    1,
    1,
    1,
    1,
    2,
    2,
    2,
    2,
    3,
    3,
    3,
    3,
    4,
    4,
    4,
    4,
    5,
    5,
    5,
    5,
    0,
    /* unused */
    0,
    0,
    /* impossible */
    0
  ]);
  var fdeb = new u8([
    0,
    0,
    0,
    0,
    1,
    1,
    2,
    2,
    3,
    3,
    4,
    4,
    5,
    5,
    6,
    6,
    7,
    7,
    8,
    8,
    9,
    9,
    10,
    10,
    11,
    11,
    12,
    12,
    13,
    13,
    /* unused */
    0,
    0
  ]);
  var clim = new u8([16, 17, 18, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15]);
  var freb = function(eb, start) {
    var b = new u16(31);
    for (var i2 = 0; i2 < 31; ++i2) {
      b[i2] = start += 1 << eb[i2 - 1];
    }
    var r = new i32(b[30]);
    for (var i2 = 1; i2 < 30; ++i2) {
      for (var j = b[i2]; j < b[i2 + 1]; ++j) {
        r[j] = j - b[i2] << 5 | i2;
      }
    }
    return { b, r };
  };
  var _a = freb(fleb, 2);
  var fl = _a.b;
  var revfl = _a.r;
  fl[28] = 258, revfl[258] = 28;
  var _b = freb(fdeb, 0);
  var fd = _b.b;
  var revfd = _b.r;
  var rev = new u16(32768);
  for (i = 0; i < 32768; ++i) {
    x = (i & 43690) >> 1 | (i & 21845) << 1;
    x = (x & 52428) >> 2 | (x & 13107) << 2;
    x = (x & 61680) >> 4 | (x & 3855) << 4;
    rev[i] = ((x & 65280) >> 8 | (x & 255) << 8) >> 1;
  }
  var x;
  var i;
  var hMap = (function(cd, mb, r) {
    var s = cd.length;
    var i2 = 0;
    var l = new u16(mb);
    for (; i2 < s; ++i2) {
      if (cd[i2])
        ++l[cd[i2] - 1];
    }
    var le = new u16(mb);
    for (i2 = 1; i2 < mb; ++i2) {
      le[i2] = le[i2 - 1] + l[i2 - 1] << 1;
    }
    var co;
    if (r) {
      co = new u16(1 << mb);
      var rvb = 15 - mb;
      for (i2 = 0; i2 < s; ++i2) {
        if (cd[i2]) {
          var sv = i2 << 4 | cd[i2];
          var r_1 = mb - cd[i2];
          var v = le[cd[i2] - 1]++ << r_1;
          for (var m = v | (1 << r_1) - 1; v <= m; ++v) {
            co[rev[v] >> rvb] = sv;
          }
        }
      }
    } else {
      co = new u16(s);
      for (i2 = 0; i2 < s; ++i2) {
        if (cd[i2]) {
          co[i2] = rev[le[cd[i2] - 1]++] >> 15 - cd[i2];
        }
      }
    }
    return co;
  });
  var flt = new u8(288);
  for (i = 0; i < 144; ++i)
    flt[i] = 8;
  var i;
  for (i = 144; i < 256; ++i)
    flt[i] = 9;
  var i;
  for (i = 256; i < 280; ++i)
    flt[i] = 7;
  var i;
  for (i = 280; i < 288; ++i)
    flt[i] = 8;
  var i;
  var fdt = new u8(32);
  for (i = 0; i < 32; ++i)
    fdt[i] = 5;
  var i;
  var flrm = /* @__PURE__ */ hMap(flt, 9, 1);
  var fdrm = /* @__PURE__ */ hMap(fdt, 5, 1);
  var max = function(a) {
    var m = a[0];
    for (var i2 = 1; i2 < a.length; ++i2) {
      if (a[i2] > m)
        m = a[i2];
    }
    return m;
  };
  var bits = function(d, p, m) {
    var o = p / 8 | 0;
    return (d[o] | d[o + 1] << 8) >> (p & 7) & m;
  };
  var bits16 = function(d, p) {
    var o = p / 8 | 0;
    return (d[o] | d[o + 1] << 8 | d[o + 2] << 16) >> (p & 7);
  };
  var shft = function(p) {
    return (p + 7) / 8 | 0;
  };
  var slc = function(v, s, e) {
    if (s == null || s < 0)
      s = 0;
    if (e == null || e > v.length)
      e = v.length;
    return new u8(v.subarray(s, e));
  };
  var ec = [
    "unexpected EOF",
    "invalid block type",
    "invalid length/literal",
    "invalid distance",
    "stream finished",
    "no stream handler",
    ,
    "no callback",
    "invalid UTF-8 data",
    "extra field too long",
    "date not in range 1980-2099",
    "filename too long",
    "stream finishing",
    "invalid zip data"
    // determined by unknown compression method
  ];
  var err = function(ind, msg, nt) {
    var e = new Error(msg || ec[ind]);
    e.code = ind;
    if (Error.captureStackTrace)
      Error.captureStackTrace(e, err);
    if (!nt)
      throw e;
    return e;
  };
  var inflt = function(dat, st, buf, dict) {
    var sl = dat.length, dl = dict ? dict.length : 0;
    if (!sl || st.f && !st.l)
      return buf || new u8(0);
    var noBuf = !buf;
    var resize = noBuf || st.i != 2;
    var noSt = st.i;
    if (noBuf)
      buf = new u8(sl * 3);
    var cbuf = function(l2) {
      var bl = buf.length;
      if (l2 > bl) {
        var nbuf = new u8(Math.max(bl * 2, l2));
        nbuf.set(buf);
        buf = nbuf;
      }
    };
    var final = st.f || 0, pos = st.p || 0, bt = st.b || 0, lm = st.l, dm = st.d, lbt = st.m, dbt = st.n;
    var tbts = sl * 8;
    do {
      if (!lm) {
        final = bits(dat, pos, 1);
        var type = bits(dat, pos + 1, 3);
        pos += 3;
        if (!type) {
          var s = shft(pos) + 4, l = dat[s - 4] | dat[s - 3] << 8, t = s + l;
          if (t > sl) {
            if (noSt)
              err(0);
            break;
          }
          if (resize)
            cbuf(bt + l);
          buf.set(dat.subarray(s, t), bt);
          st.b = bt += l, st.p = pos = t * 8, st.f = final;
          continue;
        } else if (type == 1)
          lm = flrm, dm = fdrm, lbt = 9, dbt = 5;
        else if (type == 2) {
          var hLit = bits(dat, pos, 31) + 257, hcLen = bits(dat, pos + 10, 15) + 4;
          var tl = hLit + bits(dat, pos + 5, 31) + 1;
          pos += 14;
          var ldt = new u8(tl);
          var clt = new u8(19);
          for (var i2 = 0; i2 < hcLen; ++i2) {
            clt[clim[i2]] = bits(dat, pos + i2 * 3, 7);
          }
          pos += hcLen * 3;
          var clb = max(clt), clbmsk = (1 << clb) - 1;
          var clm = hMap(clt, clb, 1);
          for (var i2 = 0; i2 < tl; ) {
            var r = clm[bits(dat, pos, clbmsk)];
            pos += r & 15;
            var s = r >> 4;
            if (s < 16) {
              ldt[i2++] = s;
            } else {
              var c = 0, n = 0;
              if (s == 16)
                n = 3 + bits(dat, pos, 3), pos += 2, c = ldt[i2 - 1];
              else if (s == 17)
                n = 3 + bits(dat, pos, 7), pos += 3;
              else if (s == 18)
                n = 11 + bits(dat, pos, 127), pos += 7;
              while (n--)
                ldt[i2++] = c;
            }
          }
          var lt = ldt.subarray(0, hLit), dt = ldt.subarray(hLit);
          lbt = max(lt);
          dbt = max(dt);
          lm = hMap(lt, lbt, 1);
          dm = hMap(dt, dbt, 1);
        } else
          err(1);
        if (pos > tbts) {
          if (noSt)
            err(0);
          break;
        }
      }
      if (resize)
        cbuf(bt + 131072);
      var lms = (1 << lbt) - 1, dms = (1 << dbt) - 1;
      var lpos = pos;
      for (; ; lpos = pos) {
        var c = lm[bits16(dat, pos) & lms], sym = c >> 4;
        pos += c & 15;
        if (pos > tbts) {
          if (noSt)
            err(0);
          break;
        }
        if (!c)
          err(2);
        if (sym < 256)
          buf[bt++] = sym;
        else if (sym == 256) {
          lpos = pos, lm = null;
          break;
        } else {
          var add = sym - 254;
          if (sym > 264) {
            var i2 = sym - 257, b = fleb[i2];
            add = bits(dat, pos, (1 << b) - 1) + fl[i2];
            pos += b;
          }
          var d = dm[bits16(dat, pos) & dms], dsym = d >> 4;
          if (!d)
            err(3);
          pos += d & 15;
          var dt = fd[dsym];
          if (dsym > 3) {
            var b = fdeb[dsym];
            dt += bits16(dat, pos) & (1 << b) - 1, pos += b;
          }
          if (pos > tbts) {
            if (noSt)
              err(0);
            break;
          }
          if (resize)
            cbuf(bt + 131072);
          var end = bt + add;
          if (bt < dt) {
            var shift = dl - dt, dend = Math.min(dt, end);
            if (shift + bt < 0)
              err(3);
            for (; bt < dend; ++bt)
              buf[bt] = dict[shift + bt];
          }
          for (; bt < end; ++bt)
            buf[bt] = buf[bt - dt];
        }
      }
      st.l = lm, st.p = lpos, st.b = bt, st.f = final;
      if (lm)
        final = 1, st.m = lbt, st.d = dm, st.n = dbt;
    } while (!final);
    return bt != buf.length && noBuf ? slc(buf, 0, bt) : buf.subarray(0, bt);
  };
  var et = /* @__PURE__ */ new u8(0);
  var b2 = function(d, b) {
    return d[b] | d[b + 1] << 8;
  };
  var b4 = function(d, b) {
    return (d[b] | d[b + 1] << 8 | d[b + 2] << 16 | d[b + 3] << 24) >>> 0;
  };
  var b8 = function(d, b) {
    return b4(d, b) + b4(d, b + 4) * 4294967296;
  };
  function inflateSync(data, opts) {
    return inflt(data, { i: 2 }, opts && opts.out, opts && opts.dictionary);
  }
  var td = typeof TextDecoder != "undefined" && /* @__PURE__ */ new TextDecoder();
  var tds = 0;
  try {
    td.decode(et, { stream: true });
    tds = 1;
  } catch (e) {
  }
  var dutf8 = function(d) {
    for (var r = "", i2 = 0; ; ) {
      var c = d[i2++];
      var eb = (c > 127) + (c > 223) + (c > 239);
      if (i2 + eb > d.length)
        return { s: r, r: slc(d, i2 - 1) };
      if (!eb)
        r += String.fromCharCode(c);
      else if (eb == 3) {
        c = ((c & 15) << 18 | (d[i2++] & 63) << 12 | (d[i2++] & 63) << 6 | d[i2++] & 63) - 65536, r += String.fromCharCode(55296 | c >> 10, 56320 | c & 1023);
      } else if (eb & 1)
        r += String.fromCharCode((c & 31) << 6 | d[i2++] & 63);
      else
        r += String.fromCharCode((c & 15) << 12 | (d[i2++] & 63) << 6 | d[i2++] & 63);
    }
  };
  function strFromU8(dat, latin1) {
    if (latin1) {
      var r = "";
      for (var i2 = 0; i2 < dat.length; i2 += 16384)
        r += String.fromCharCode.apply(null, dat.subarray(i2, i2 + 16384));
      return r;
    } else if (td) {
      return td.decode(dat);
    } else {
      var _a2 = dutf8(dat), s = _a2.s, r = _a2.r;
      if (r.length)
        err(8);
      return s;
    }
  }
  var slzh = function(d, b) {
    return b + 30 + b2(d, b + 26) + b2(d, b + 28);
  };
  var zh = function(d, b, z) {
    var fnl = b2(d, b + 28), fn = strFromU8(d.subarray(b + 46, b + 46 + fnl), !(b2(d, b + 8) & 2048)), es = b + 46 + fnl, bs = b4(d, b + 20);
    var _a2 = z && bs == 4294967295 ? z64e(d, es) : [bs, b4(d, b + 24), b4(d, b + 42)], sc = _a2[0], su = _a2[1], off = _a2[2];
    return [b2(d, b + 10), sc, su, fn, es + b2(d, b + 30) + b2(d, b + 32), off];
  };
  var z64e = function(d, b) {
    for (; b2(d, b) != 1; b += 4 + b2(d, b + 2))
      ;
    return [b8(d, b + 12), b8(d, b + 4), b8(d, b + 20)];
  };
  function unzipSync(data, opts) {
    var files = {};
    var e = data.length - 22;
    for (; b4(data, e) != 101010256; --e) {
      if (!e || data.length - e > 65558)
        err(13);
    }
    ;
    var c = b2(data, e + 8);
    if (!c)
      return {};
    var o = b4(data, e + 16);
    var z = o == 4294967295 || c == 65535;
    if (z) {
      var ze = b4(data, e - 12);
      z = b4(data, ze) == 101075792;
      if (z) {
        c = b4(data, ze + 32);
        o = b4(data, ze + 48);
      }
    }
    var fltr = opts && opts.filter;
    for (var i2 = 0; i2 < c; ++i2) {
      var _a2 = zh(data, o, z), c_2 = _a2[0], sc = _a2[1], su = _a2[2], fn = _a2[3], no = _a2[4], off = _a2[5], b = slzh(data, off);
      o = no;
      if (!fltr || fltr({
        name: fn,
        size: sc,
        originalSize: su,
        compression: c_2
      })) {
        if (!c_2)
          files[fn] = slc(data, b, b + sc);
        else if (c_2 == 8)
          files[fn] = inflateSync(data.subarray(b, b + sc), { out: new u8(su) });
        else
          err(14, "unknown compression type " + c_2);
      }
    }
    return files;
  }

  // sdk/examples/subdl/manifest.json
  var manifest_default = {
    id: "org.ether.examples.subdl",
    version: "1.0.0",
    name: "SubDL",
    description: "Find subtitles with your SubDL account.",
    types: [
      "movie",
      "series"
    ],
    idPrefixes: [
      "tt"
    ],
    resources: [
      "subtitles"
    ],
    ether: {
      protocol: 1,
      kind: "integration",
      capabilities: [
        "subtitles"
      ],
      summary: "$t:summary",
      author: {
        name: "Ether"
      },
      license: "MIT",
      icon: "assets/icon.svg",
      runtime: {
        kind: "hosted",
        entry: "dist/plugin.js"
      },
      permissions: {
        network: [
          "api.subdl.com",
          "dl.subdl.com"
        ],
        secrets: [
          "apiKey"
        ]
      },
      settings: {
        $ref: "settings.schema.json"
      },
      strings: {
        en: "i18n/en.json",
        fr: "i18n/fr.json"
      },
      contributes: {
        integration: {
          category: "playback-extras",
          adds: [
            "$t:adds"
          ],
          connect: "fields",
          test: true
        },
        subtitles: {
          languages: "any",
          formats: [
            "srt"
          ]
        }
      }
    }
  };

  // sdk/examples/subdl/settings.schema.json
  var settings_schema_default = {
    fields: [
      {
        key: "apiKey",
        kind: "secret",
        label: "$t:api_key",
        required: true
      }
    ]
  };

  // sdk/examples/subdl/plugin.ts
  var MAX = 2 * 1024 * 1024;
  var fail = (message) => {
    throw new PluginHttpError(502, "subdl_error", message);
  };
  function bytes64(s) {
    const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    if (!/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(s) || s.length > 4 * Math.ceil(MAX / 3)) return fail("Invalid subtitle archive.");
    const out = [];
    let bits2 = 0, value = 0;
    for (const c of s.replace(/=+$/, "")) {
      value = value << 6 | alphabet.indexOf(c);
      bits2 += 6;
      if (bits2 >= 8) {
        bits2 -= 8;
        out.push(value >> bits2 & 255);
      }
    }
    return new Uint8Array(out);
  }
  function archive(data) {
    const bytes = bytes64(data);
    let end = -1;
    for (let i2 = bytes.length - 22; i2 >= Math.max(0, bytes.length - 65557); i2--) if (bytes[i2] === 80 && bytes[i2 + 1] === 75 && bytes[i2 + 2] === 5 && bytes[i2 + 3] === 6) {
      end = i2;
      break;
    }
    if (end < 0) return fail("Invalid subtitle archive.");
    const view = new DataView(bytes.buffer);
    const count = view.getUint16(end + 10, true);
    let offset = view.getUint32(end + 16, true);
    let total = 0;
    if (!count || count > 100 || offset >= end || view.getUint16(end + 8, true) !== count || view.getUint16(end + 4, true) !== 0 || view.getUint16(end + 6, true) !== 0 || end + 22 + view.getUint16(end + 20, true) !== bytes.length) return fail("Invalid subtitle archive.");
    for (let i2 = 0; i2 < count; i2++) {
      if (offset + 46 > end || view.getUint32(offset, true) !== 33639248) return fail("Invalid subtitle archive.");
      const size = view.getUint32(offset + 24, true);
      total += size;
      if (total > MAX || view.getUint16(offset + 8, true) & 1) return fail("Subtitle archive is too large or encrypted.");
      offset += 46 + view.getUint16(offset + 28, true) + view.getUint16(offset + 30, true) + view.getUint16(offset + 32, true);
    }
    if (offset > end) return fail("Invalid subtitle archive.");
    try {
      const files = unzipSync(bytes);
      const names = Object.keys(files).filter((n) => /\.srt$/i.test(n) && !n.startsWith("__MACOSX/")).sort();
      if (names.length !== 1 || files[names[0]].length > MAX) return fail("The archive must contain one SRT subtitle.");
      return strFromU8(files[names[0]]);
    } catch {
      return fail("Could not read the subtitle archive.");
    }
  }
  var settingsManifest = { ...manifest_default, ether: { ...manifest_default.ether, settings: settings_schema_default } };
  var subdl = plugin({ manifest: settingsManifest, routes: {
    subtitles(req, ctx) {
      const id = req.id.split(":");
      if (!/^tt\d+$/.test(id[0]) || !ctx.settings.apiKey) return { subtitles: [] };
      let url = "https://api.subdl.com/api/v1/subtitles?api_key=" + encodeURIComponent(ctx.settings.apiKey) + "&imdb_id=" + encodeURIComponent(id[0]) + "&subs_per_page=30&client=custom_integration";
      if (req.type === "series" && /^\d+$/.test(id[1] || "") && /^\d+$/.test(id[2] || "")) url += "&type=tv&season_number=" + id[1] + "&episode_number=" + id[2];
      const response = ctx.ether.http.fetch({ url });
      if (response.status !== 200) return fail("Subtitle search failed.");
      const body = JSON.parse(response.body);
      if (!body.status || !Array.isArray(body.subtitles)) return fail("Subtitle search failed.");
      return { subtitles: body.subtitles.slice(0, 30).filter((s) => /^\/subtitle\/[0-9]+-[0-9]+\.zip$/.test(s.url || "") && /^[a-z]{2,3}$/i.test(s.language || s.lang || "")).map((s) => ({ id: s.url, lang: (s.language || s.lang).toLowerCase(), format: "srt", name: String(s.release_name || s.name || "SubDL").slice(0, 200) })) };
    },
    subtitlesDownload(req, ctx) {
      if (!/^\/subtitle\/[0-9]+-[0-9]+\.zip$/.test(req.id) || req.format !== "srt") return fail("Invalid subtitle selection.");
      const response = ctx.ether.http.fetch({ url: "https://dl.subdl.com" + req.id, headers: { "x-api-key": ctx.settings.apiKey } });
      if (response.status !== 200 || !response.bodyBase64) return fail("Subtitle download failed.");
      return { format: "srt", encoding: "utf8", data: archive(response.bodyBase64) };
    },
    test(ctx) {
      return { ok: !!ctx.settings.apiKey, message: ctx.settings.apiKey ? "API key saved." : "Enter an API key." };
    }
  } });
  var handle = subdl.handle;
  return __toCommonJS(plugin_exports);
})();
