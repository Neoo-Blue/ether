var __plugin = (() => {
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

  // sdk/examples/ether-wrapped/plugin.ts
  var plugin_exports = {};
  __export(plugin_exports, {
    handle: () => handle,
    wrappedDefinition: () => wrappedDefinition,
    wrappedPlugin: () => wrappedPlugin
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
            body: req.body ?? ""
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
        granted() {
          return callHost("viewers.granted", []) ?? [];
        },
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
        async granted() {
          return await callJson("GET", "/ether/plugin-host/viewers/granted") ?? [];
        },
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
  function isEtherHostFault(err) {
    if (!err) return false;
    const msg = err instanceof Error ? err.message : String(err);
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
            if (!ctx.pickToken || ctx.pickToken.trim() === "") {
              return errorResponse(
                403,
                "pick_required",
                "Subtitles requests require an X-Ether-Pick token."
              );
            }
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
    function handleRouteError(err) {
      if (isEtherHostFault(err)) {
        throw err;
      }
      if (err instanceof PluginHttpError) {
        return errorResponse(err.status, err.code, err.message);
      }
      if (err && typeof err === "object" && "status" in err && typeof err.status === "number") {
        const e = err;
        return errorResponse(e.status, e.code || "plugin_error", e.message || "Request failed.");
      }
      const message = err instanceof Error ? err.message : String(err);
      return errorResponse(500, "internal_error", message || "Internal plugin error.");
    }
    function handleRequest(req) {
      try {
        const ctx = buildContext(req);
        const res = dispatchCore(req, ctx);
        return res instanceof Promise ? res.catch(handleRouteError) : res;
      } catch (err) {
        return handleRouteError(err);
      }
    }
    async function dispatch(req) {
      try {
        const ctx = buildContext(req);
        return await dispatchCore(req, ctx);
      } catch (err) {
        return handleRouteError(err);
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

  // sdk/typescript/src/serve.ts
  var DEFAULT_MAX_CHUNK_BYTES = 64 * 1024;

  // sdk/examples/ether-wrapped/manifest.json
  var manifest_default = { id: "org.ether.examples.wrapped", version: "1.0.0", name: "Ether Wrapped", description: "Explore your year in titles.", types: ["movie", "series"], idPrefixes: ["tt"], resources: ["catalog"], catalogs: [{ type: "movie", id: "year", name: "Your year in titles" }, { type: "series", id: "year", name: "Your year in titles" }], ether: { protocol: 1, kind: "extension", capabilities: ["ui", "list_source"], summary: "$t:summary", icon: "assets/icon.svg", runtime: { kind: "hosted", entry: "dist/plugin.js" }, permissions: { catalog: "read", viewers: { watch_history: "granted_viewers" } }, settings: { $ref: "settings.schema.json" }, strings: { en: "i18n/en.json", fr: "i18n/fr.json" }, contributes: { pages: [{ key: "year", label: "$t:page", route: "/ether/view/year" }], listSources: [{ key: "year", label: "$t:list", hint: "$t:list_hint", catalog: "year", maxItems: 200, refresh: "1h", params: { fields: [{ key: "viewerId", kind: "viewer", label: "$t:viewer", required: true }, { key: "year", kind: "number", label: "$t:year" }] } }] } } };

  // sdk/examples/ether-wrapped/i18n/en.json
  var en_default = {
    summary: "Explore a viewer's year in titles.",
    page: "Ether Wrapped",
    description: "Your year in titles, one story at a time.",
    list: "Your year in titles",
    list_hint: "Build a collection from a viewer's year.",
    viewer: "Viewer",
    year: "Year",
    plays: "Completed plays",
    titles: "Titles",
    top_titles: "Top titles",
    empty: "No completed titles found.",
    genres: "Top genres",
    activity: "Activity by month",
    previous: "Previous viewers",
    next: "Next viewers"
  };

  // sdk/examples/ether-wrapped/i18n/fr.json
  var fr_default = {
    summary: "Explorez l'ann\xE9e d'un spectateur en titres.",
    page: "Ether Wrapped",
    description: "Votre ann\xE9e en titres, une histoire \xE0 la fois.",
    list: "Votre ann\xE9e en titres",
    list_hint: "Cr\xE9er une collection \xE0 partir de l'ann\xE9e d'un spectateur.",
    viewer: "Spectateur",
    year: "Ann\xE9e",
    plays: "Lectures termin\xE9es",
    titles: "Titres",
    top_titles: "Titres pr\xE9f\xE9r\xE9s",
    empty: "Aucun titre termin\xE9 trouv\xE9.",
    genres: "Genres pr\xE9f\xE9r\xE9s",
    activity: "Activit\xE9 par mois",
    previous: "Spectateurs pr\xE9c\xE9dents",
    next: "Spectateurs suivants"
  };

  // sdk/examples/ether-wrapped/plugin.ts
  async function yearData(ctx, viewer, year) {
    if (!Number.isSafeInteger(viewer) || viewer <= 0 || year < 1900 || year > 9999) return { titles: [], plays: 0, genres: [], months: Array(12).fill(0) };
    const viewers = await ctx.ether.viewers.granted();
    if (!viewers.some((v) => v.id === viewer)) return { titles: [], plays: 0, genres: [], months: Array(12).fill(0) };
    const history = await ctx.ether.viewers.history(viewer, 200, 0);
    const counts = /* @__PURE__ */ new Map();
    const months = Array(12).fill(0);
    for (const entry of history) {
      const date = new Date(entry.watchedAt);
      if (!entry.completed || date.getUTCFullYear() !== year) continue;
      let titleId = entry.titleId;
      if (entry.type === "episode") {
        const key = Number(titleId);
        if (Number.isSafeInteger(key) && key >= 4e15 && key < 5e15) titleId = String(2e15 + Math.floor((key - 4e15) / 1e7));
      }
      counts.set(titleId, (counts.get(titleId) || 0) + 1);
      months[date.getUTCMonth()]++;
    }
    const titles = [];
    for (const [id, count] of counts) {
      try {
        const title = await ctx.ether.catalog.get(id);
        if (title) titles.push({ ...title, count });
      } catch {
      }
    }
    titles.sort((a, b) => b.count - a.count || a.title.localeCompare(b.title));
    const genreCounts = /* @__PURE__ */ new Map();
    for (const title of titles) for (const genre of title.genres || []) genreCounts.set(genre, (genreCounts.get(genre) || 0) + title.count);
    const genres = [...genreCounts].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0])).slice(0, 5);
    return { titles, plays: months.reduce((a, b) => a + b, 0), genres, months };
  }
  var wrappedDefinition = { manifest: manifest_default, strings: { en: en_default, fr: fr_default }, routes: {
    async view(req, ctx) {
      const viewers = await ctx.ether.viewers.granted();
      const viewer = Number(req.query.viewer || viewers[0]?.id || 0);
      const year = Number(req.query.year || (/* @__PURE__ */ new Date()).getUTCFullYear());
      const result = await yearData(ctx, viewer, year);
      const data = { year: String(year), plays: String(result.plays), titleCount: String(result.titles.length), genres: result.genres.map(([g, n]) => `${g} (${n})`).join(", "), titles: result.titles.map((t) => ({ ratingKey: t.id, title: t.title, type: t.type, poster: t.posterPath ? `https://image.tmdb.org/t/p/w300${t.posterPath}` : void 0 })), series: [{ key: "plays", label: ctx.t("plays"), values: result.months }], times: result.months.map((_, i) => `${year}-${String(i + 1).padStart(2, "0")}-01T00:00:00Z`) };
      const page = Math.max(0, Math.min(Math.ceil(viewers.length / 6) - 1, Number(req.query.viewerPage) || 0));
      const links = viewers.slice(page * 6, page * 6 + 6).map((v, i) => {
        data[`viewer${i}`] = v.name;
        data[`active${i}`] = v.id === viewer;
        return { href: `?viewer=${v.id}&viewerPage=${page}`, label: { $: `viewer${i}` }, activeWhen: { $: `active${i}` } };
      });
      data.neverActive = false;
      if (page > 0) links.unshift({ href: `?viewerPage=${page - 1}`, label: "$t:previous", activeWhen: { $: "neverActive" } });
      if ((page + 1) * 6 < viewers.length) links.push({ href: `?viewerPage=${page + 1}`, label: "$t:next", activeWhen: { $: "neverActive" } });
      return { spec: { specVersion: 1, layout: "list", header: [{ kind: "page-header", title: "$t:page", description: "$t:description" }], body: [{ kind: "filter-bar", controls: {}, leading: { kind: "toggle-links", links } }, { kind: "stat-tile", iconKey: "calendar", accent: "violet", label: "$t:year", value: { $: "year" } }, { kind: "stat-tile", iconKey: "play", accent: "teal", label: "$t:plays", value: { $: "plays" } }, { kind: "stat-tile", iconKey: "film", accent: "blue", label: "$t:titles", value: { $: "titleCount" } }, { kind: "heading", level: 2, content: "$t:top_titles" }, { kind: "widget", widget: "ether.posterGrid", props: { items: { $: "titles" }, empty: "$t:empty" } }, { kind: "panel", title: "$t:genres", blocks: [{ kind: "text", content: { $: "genres" } }] }, { kind: "heading", level: 2, content: "$t:activity" }, { kind: "widget", widget: "ether.liveChart", props: { series: { $: "series" }, times: { $: "times" }, label: "$t:plays" } }] }, data };
    },
    async catalog(req, ctx) {
      const viewer = Number(req.extra.viewerId || 0), year = Number(req.extra.year || (/* @__PURE__ */ new Date()).getUTCFullYear());
      const data = await yearData(ctx, viewer, year);
      return { metas: data.titles.filter((t) => (req.type === "series" ? t.type === "show" : t.type === req.type) && t.imdbId).slice(req.skip, req.skip + 100).map((t) => ({ id: t.imdbId, type: req.type, name: t.title, releaseInfo: String(t.year || ""), description: t.summary })) };
    }
  } };
  var wrappedPlugin = plugin(wrappedDefinition);
  var handle = wrappedPlugin.handle;
  return __toCommonJS(plugin_exports);
})();
