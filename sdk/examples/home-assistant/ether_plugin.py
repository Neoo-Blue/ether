"""Ether Plugin SDK for Python 3.9+ (stdlib-only, zero external dependencies).

Implements Ether Plugin Protocol v1 (EPP/1) for sidecar and remote plugins:
- Declarative router (`plugin(manifest=..., routes=...)`) with identical route
  semantics to `@ether/plugin-sdk` and `sdk/go`.
- Settings schema decoding, validation, and copy-rule linting (SPEC §5.5).
- Localization helper (`create_i18n`, `t_ref`).
- Remote host client (`RemoteHostClient`) for `/ether/plugin-host/*` authenticated
  with `X-Ether-Plugin-Credential` (SPEC §5.1.5, WS8b).
- HTTP sidecar server (`serve(..., mode="http")`) and WebSocket tunnel client
  (`serve(..., mode="tunnel")`, `dispatch_over_tunnel_frame`).
- Local test harness, mock host client, fixtures, and capability starter templates.
"""

from __future__ import annotations

import base64
import hashlib
import http.server
import json
import os
import re
import socket
import socketserver
import ssl
import struct
import threading
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass, field
from typing import Any, Callable, Dict, List, Optional, Tuple, Union

TUNNEL_PROTOCOL = "ether-tunnel-v1"
DEFAULT_MAX_CHUNK_BYTES = 64 * 1024

ALLOWED_SETTING_KINDS = frozenset(
    {
        "group",
        "text",
        "secret",
        "duration",
        "readonly",
        "number",
        "bool",
        "select",
        "list",
        "bytes",
        "hidden",
        "multiSelect",
        "orderedList",
        "language",
        "viewer",
        "collection",
        "folder",
        "url",
        "title",
    }
)

_BANNED_COPY_RE = re.compile(r"\b(simply|easily|just)\b", re.IGNORECASE)
_INTERP_RE = re.compile(r"\{([A-Za-z0-9_]+)\}")


class PluginHttpError(Exception):
    """Structured HTTP error raised by a plugin route handler."""

    def __init__(self, status: int, code: str, message: str) -> None:
        super().__init__(message)
        self.status = int(status)
        self.code = str(code)
        self.message = str(message)


PluginRouteError = PluginHttpError


# ---------------------------------------------------------------------------
# Copy rules & i18n helpers (SPEC §2.7, §2.8, §5.1.1, §5.6)
# ---------------------------------------------------------------------------


def t_ref(key: str) -> str:
    """Returns a `$t:<key>` reference for manifest and ViewSpec fields."""
    return f"$t:{key}"


def lint_copy(raw: str, max_len: int = 0) -> str:
    """Validates a user-visible string against Ether's copy rules."""
    s = raw.strip()
    if max_len > 0 and len(s) > max_len:
        return f"is longer than {max_len} characters"
    if "—" in s or "–" in s:
        return "must not contain em or en dashes"
    if "!" in s:
        return "must not contain exclamation marks"
    if _BANNED_COPY_RE.search(s):
        return "must not use the words simply, just or easily"
    for ch in s:
        if ch.isalpha():
            if ch.islower():
                return "must use sentence case"
            break
    words = [w for w in s.split() if w]
    if len(words) >= 3:
        caps = sum(1 for w in words if w[0].isalpha() and w[0].isupper())
        if caps * 3 >= len(words) * 2:
            return "must use sentence case"
    return ""


class I18nHelper:
    """Resolves localized strings with fallback to base language and 'en'."""

    def __init__(
        self,
        strings: Optional[Dict[str, Dict[str, str]]] = None,
        default_locale: str = "en",
    ) -> None:
        self._strings: Dict[str, Dict[str, str]] = strings or {}
        self._default_locale = default_locale or "en"

    def t(
        self,
        raw_key: str,
        params: Optional[Dict[str, Any]] = None,
        locale: Optional[str] = None,
    ) -> str:
        key = raw_key[3:] if raw_key.startswith("$t:") else raw_key
        loc = locale or self._default_locale
        base = loc.split("-")[0] if loc else ""
        template: Optional[str] = None
        for candidate in (loc, base, "en"):
            if not candidate:
                continue
            table = self._strings.get(candidate)
            if table and key in table:
                template = table[key]
                break
        if template is None:
            template = key
        if not params:
            return template

        def _replace(match: re.Match[str]) -> str:
            name = match.group(1)
            return str(params[name]) if name in params else match.group(0)

        return _INTERP_RE.sub(_replace, template)

    def ref(self, key: str) -> str:
        return t_ref(key)

    def catalog(self) -> Dict[str, Dict[str, str]]:
        return self._strings


def create_i18n(
    strings: Optional[Dict[str, Dict[str, str]]] = None,
    default_locale: str = "en",
) -> I18nHelper:
    return I18nHelper(strings=strings, default_locale=default_locale)


# ---------------------------------------------------------------------------
# Settings schema decoding and linting (SPEC §5.5)
# ---------------------------------------------------------------------------


def collect_setting_fields(schema: Optional[Dict[str, Any]]) -> List[Dict[str, Any]]:
    if not isinstance(schema, dict):
        return []
    out: List[Dict[str, Any]] = []

    def _walk(node: Any) -> None:
        if isinstance(node, list):
            for item in node:
                _walk(item)
            return
        if not isinstance(node, dict):
            return
        key = (
            node.get("key")
            or node.get("name")
            or node.get("id")
            or node.get("field")
            or ""
        )
        kind = node.get("kind", "")
        if isinstance(key, str) and key and isinstance(kind, str) and kind and kind != "group":
            out.append(
                {
                    "key": key,
                    "kind": kind,
                    "label": node.get("label"),
                    "hint": node.get("hint"),
                    "required": node.get("required") is True,
                    "default": node.get("default"),
                    "min": node.get("min"),
                    "max": node.get("max"),
                }
            )
        for v in node.values():
            if isinstance(v, (dict, list)):
                _walk(v)

    _walk(schema)
    return out


def lint_settings_schema(schema: Optional[Dict[str, Any]]) -> List[Dict[str, str]]:
    if not isinstance(schema, dict):
        return []
    problems: List[Dict[str, str]] = []

    def _walk(node: Any, path: str) -> None:
        if isinstance(node, list):
            for idx, item in enumerate(node):
                _walk(item, f"{path}[{idx}]")
            return
        if not isinstance(node, dict):
            return
        kind = node.get("kind")
        if isinstance(kind, str):
            if kind == "json":
                problems.append(
                    {
                        "field": f"{path}.kind",
                        "message": "the json field kind is not allowed in plugin settings",
                    }
                )
            elif kind not in ALLOWED_SETTING_KINDS:
                problems.append(
                    {
                        "field": f"{path}.kind",
                        "message": f'unknown field kind "{kind}"',
                    }
                )
        label = node.get("label")
        if isinstance(label, str) and not label.startswith("$t:"):
            msg = lint_copy(label, 40)
            if msg:
                problems.append({"field": f"{path}.label", "message": msg})
        hint = node.get("hint")
        if isinstance(hint, str) and not hint.startswith("$t:"):
            msg = lint_copy(hint, 120)
            if msg:
                problems.append({"field": f"{path}.hint", "message": msg})
            trimmed = hint.rstrip(".")
            if "." in trimmed:
                problems.append({"field": f"{path}.hint", "message": "must be one sentence"})
        for k, v in node.items():
            if isinstance(v, (dict, list)):
                _walk(v, f"{path}.{k}")

    _walk(schema, "ether.settings")
    return problems


def decode_settings(
    schema: Optional[Dict[str, Any]],
    incoming: Optional[Dict[str, Any]] = None,
    defaults: Optional[Dict[str, Any]] = None,
) -> Tuple[Dict[str, Any], Dict[str, str]]:
    fields = collect_setting_fields(schema)
    result: Dict[str, Any] = dict(defaults or {})
    errors: Dict[str, str] = {}

    for f in fields:
        if f.get("default") is not None and f["key"] not in result:
            result[f["key"]] = f["default"]

    if isinstance(incoming, dict):
        for k, v in incoming.items():
            result[k] = v

    for f in fields:
        key = f["key"]
        kind = f["kind"]
        val = result.get(key)
        ptr = f"/{key}"
        if f.get("required") and (val is None or val == ""):
            errors[ptr] = "is required"
            continue
        if val is None or val == "":
            continue
        if kind in ("number", "bytes", "viewer"):
            if isinstance(val, bool):
                errors[ptr] = "must be a number"
                continue
            try:
                num = float(val) if isinstance(val, str) and "." in val else int(val) if isinstance(val, (int, str)) else float(val)
            except (ValueError, TypeError):
                errors[ptr] = "must be a number"
                continue
            if f.get("min") is not None and num < f["min"]:
                errors[ptr] = f"must be at least {f['min']}"
            if f.get("max") is not None and num > f["max"]:
                errors[ptr] = f"must be at most {f['max']}"
            result[key] = num
        elif kind == "bool":
            if not isinstance(val, bool):
                errors[ptr] = "must be true or false"
        elif kind in ("list", "multiSelect", "orderedList"):
            if not isinstance(val, list):
                errors[ptr] = "must be a list"
        elif kind == "url":
            if not isinstance(val, str) or not (
                val.startswith("http://") or val.startswith("https://")
            ):
                errors[ptr] = "must be an http or https URL"

    return result, errors


# ---------------------------------------------------------------------------
# Host client for sidecar plugins (`/ether/plugin-host/*`)
# ---------------------------------------------------------------------------


class _KVClient:
    def __init__(self, call_json: Callable[..., Any]) -> None:
        self._call = call_json

    def get(self, key: str) -> Optional[str]:
        encoded = urllib.parse.quote(key, safe="")
        res = self._call("GET", f"/ether/plugin-host/kv/{encoded}", allow_404=True)
        return res.get("value") if isinstance(res, dict) else None

    def put(self, key: str, value: str) -> None:
        encoded = urllib.parse.quote(key, safe="")
        self._call("PUT", f"/ether/plugin-host/kv/{encoded}", {"value": str(value)})

    def delete(self, key: str) -> None:
        encoded = urllib.parse.quote(key, safe="")
        self._call("DELETE", f"/ether/plugin-host/kv/{encoded}")

    def list(self, prefix: str = "") -> List[str]:
        q = f"?prefix={urllib.parse.quote(prefix, safe='')}" if prefix else ""
        res = self._call("GET", f"/ether/plugin-host/kv{q}")
        return list(res.get("keys", [])) if isinstance(res, dict) else []


class _HttpClient:
    def fetch(self, req: Dict[str, Any]) -> Dict[str, Any]:
        method = str(req.get("method") or "GET").upper()
        url = str(req["url"])
        headers = dict(req.get("headers") or {})
        body_str = req.get("body")
        data = body_str.encode("utf-8") if isinstance(body_str, str) else None
        request = urllib.request.Request(url, data=data, headers=headers, method=method)
        try:
            with urllib.request.urlopen(request, timeout=15) as resp:
                raw = resp.read().decode("utf-8", errors="replace")
                out_headers = {k.lower(): v for k, v in resp.headers.items()}
                return {"status": resp.status, "headers": out_headers, "body": raw}
        except urllib.error.HTTPError as exc:
            raw = exc.read().decode("utf-8", errors="replace")
            out_headers = {k.lower(): v for k, v in exc.headers.items()} if exc.headers else {}
            return {"status": exc.code, "headers": out_headers, "body": raw}


class _CatalogClient:
    def __init__(self, call_json: Callable[..., Any]) -> None:
        self._call = call_json

    def search(self, query: str, limit: int = 50, offset: int = 0) -> Dict[str, Any]:
        qs = urllib.parse.urlencode({"q": query, "limit": str(limit), "offset": str(offset)})
        res = self._call("GET", f"/ether/plugin-host/catalog/search?{qs}")
        return res if isinstance(res, dict) else {"titles": []}

    def get(self, title_id: str) -> Optional[Dict[str, Any]]:
        encoded = urllib.parse.quote(title_id, safe="")
        res = self._call("GET", f"/ether/plugin-host/catalog/{encoded}", allow_404=True)
        return res if isinstance(res, dict) else None


class _ViewersClient:
    def __init__(self, call_json: Callable[..., Any]) -> None:
        self._call = call_json

    def history(self, viewer_id: int, limit: int = 50, offset: int = 0) -> List[Dict[str, Any]]:
        qs = urllib.parse.urlencode({"limit": str(limit), "offset": str(offset)})
        res = self._call("GET", f"/ether/plugin-host/viewers/{int(viewer_id)}/history?{qs}")
        return list(res) if isinstance(res, list) else []

    def set_watch_state(self, viewer_id: int, title_id: str, watched: bool) -> None:
        self._call(
            "POST",
            "/ether/plugin-host/viewers/watch-state",
            {"viewerId": int(viewer_id), "titleId": str(title_id), "watched": bool(watched)},
        )

    setWatchState = set_watch_state


class _CollectionsClient:
    def __init__(self, call_json: Callable[..., Any]) -> None:
        self._call = call_json

    def upsert(self, collection: Dict[str, Any]) -> None:
        self._call("POST", "/ether/plugin-host/collections/upsert", collection)


class _NotifyClient:
    def __init__(self, call_json: Callable[..., Any]) -> None:
        self._call = call_json

    def owner(self, text: str) -> None:
        self._call("POST", "/ether/plugin-host/notify", {"text": str(text)})


class RemoteHostClient:
    """Host client for remote/sidecar plugins talking to `/ether/plugin-host/*`."""

    def __init__(
        self,
        base_url: Optional[str] = None,
        credential: Optional[str] = None,
    ) -> None:
        raw_base = base_url or os.environ.get("ETHER_HOST_URL") or "http://127.0.0.1:32450"
        self.base_url = raw_base.rstrip("/")
        self.credential = (
            credential if credential is not None else os.environ.get("ETHER_PLUGIN_CREDENTIAL", "")
        )
        self.kv = _KVClient(self._call_json)
        self.http = _HttpClient()
        self.catalog = _CatalogClient(self._call_json)
        self.viewers = _ViewersClient(self._call_json)
        self.collections = _CollectionsClient(self._call_json)
        self.notify = _NotifyClient(self._call_json)

    def _call_json(
        self,
        method: str,
        path: str,
        body: Optional[Any] = None,
        allow_404: bool = False,
    ) -> Any:
        headers: Dict[str, str] = {
            "X-Ether-Plugin-Credential": self.credential,
            "Accept": "application/json",
        }
        data: Optional[bytes] = None
        if body is not None:
            headers["Content-Type"] = "application/json"
            data = json.dumps(body).encode("utf-8")
        req = urllib.request.Request(
            f"{self.base_url}{path}",
            data=data,
            headers=headers,
            method=method,
        )
        try:
            with urllib.request.urlopen(req, timeout=15) as resp:
                raw = resp.read().decode("utf-8")
                return json.loads(raw) if raw else None
        except urllib.error.HTTPError as exc:
            if allow_404 and exc.code == 404:
                return None
            raw = exc.read().decode("utf-8", errors="replace")
            try:
                parsed = json.loads(raw)
                msg = parsed.get("error", {}).get("message")
            except Exception:
                msg = None
            raise RuntimeError(
                msg or f"Host call {method} {path} failed with HTTP {exc.code}"
            ) from exc

    def log(
        self,
        level: str,
        message: str,
        attrs: Optional[Dict[str, Any]] = None,
    ) -> None:
        payload: Dict[str, Any] = {"level": str(level), "message": str(message)}
        if attrs is not None:
            payload["attrs"] = attrs
        self._call_json("POST", "/ether/plugin-host/log", payload)

    def abort(self, message: str = "aborted") -> None:
        raise RuntimeError(message)


def create_remote_host_client(
    base_url: Optional[str] = None,
    credential: Optional[str] = None,
) -> RemoteHostClient:
    return RemoteHostClient(base_url=base_url, credential=credential)


# ---------------------------------------------------------------------------
# Plugin context & router
# ---------------------------------------------------------------------------


@dataclass
class PluginContext:
    manifest: Dict[str, Any]
    settings: Dict[str, Any]
    locale: str
    pick_token: Optional[str]
    request_id: Optional[str]
    headers: Dict[str, Any]
    query: Dict[str, str]
    ether: Any
    i18n: I18nHelper

    @property
    def pickToken(self) -> Optional[str]:
        return self.pick_token

    @property
    def requestId(self) -> Optional[str]:
        return self.request_id

    def t(self, key: str, params: Optional[Dict[str, Any]] = None) -> str:
        return self.i18n.t(key, params=params, locale=self.locale)


def _get_header(headers: Optional[Dict[str, Any]], name: str) -> Optional[str]:
    if not headers:
        return None
    target = name.lower()
    for k, v in headers.items():
        if k.lower() == target:
            if isinstance(v, list):
                return str(v[0]) if v else None
            if v is not None:
                return str(v)
    return None


def _parse_url_path_and_query(
    raw_path: str,
    raw_query: Optional[Dict[str, Any]] = None,
) -> Tuple[str, Dict[str, str]]:
    path_only = raw_path or "/"
    query: Dict[str, str] = {}
    if isinstance(raw_query, dict):
        for k, v in raw_query.items():
            if isinstance(v, list):
                if v:
                    query[str(k)] = str(v[0])
            elif v is not None:
                query[str(k)] = str(v)
    if "?" in path_only:
        path_only, qs = path_only.split("?", 1)
        path_only = path_only or "/"
        for k, vals in urllib.parse.parse_qs(qs, keep_blank_values=True).items():
            if k not in query and vals:
                query[k] = vals[0]
    if not path_only.startswith("/"):
        path_only = "/" + path_only
    return path_only, query


def _parse_json_body(body: Any) -> Dict[str, Any]:
    if body is None or body == "" or body == b"":
        return {}
    if isinstance(body, (bytes, bytearray)):
        body = body.decode("utf-8")
    if isinstance(body, str):
        trimmed = body.strip()
        if not trimmed:
            return {}
        parsed = json.loads(trimmed)
        return parsed if isinstance(parsed, dict) else {}
    if isinstance(body, dict):
        return body
    return {}


def _ok_response(
    body: Any,
    headers: Optional[Dict[str, str]] = None,
    status: int = 200,
) -> Dict[str, Any]:
    merged = {"Content-Type": "application/json"}
    if headers:
        merged.update(headers)
    return {"status": status, "headers": merged, "body": body}


def _error_response(status: int, code: str, message: str) -> Dict[str, Any]:
    return {
        "status": status,
        "headers": {"Content-Type": "application/json"},
        "body": {"error": {"code": code, "message": message}},
    }


def _match_stremio_route(path: str, query: Dict[str, str]) -> Optional[Dict[str, Any]]:
    if not path.endswith(".json"):
        return None
    trimmed = path[1:-5]
    parts = trimmed.split("/")
    if len(parts) < 3 or len(parts) > 4:
        return None
    resource = parts[0]
    if resource not in ("catalog", "meta", "stream", "subtitles"):
        return None
    media_type = urllib.parse.unquote(parts[1])
    item_id = urllib.parse.unquote(parts[2])
    extra: Dict[str, str] = {}
    if len(parts) == 4 and parts[3]:
        raw_extra = parts[3]
        for k, vals in urllib.parse.parse_qs(raw_extra, keep_blank_values=True).items():
            if vals:
                extra[k] = vals[0]
    for k, v in query.items():
        if k not in extra:
            extra[k] = v
    return {
        "resource": resource,
        "type": media_type,
        "id": item_id,
        "extra": extra,
    }


class PluginInstance:
    """Runnable Ether plugin instance returned by `plugin(...)`."""

    def __init__(
        self,
        manifest: Dict[str, Any],
        routes: Optional[Dict[str, Callable[..., Any]]] = None,
        default_settings: Optional[Dict[str, Any]] = None,
        strings: Optional[Dict[str, Dict[str, str]]] = None,
        default_locale: str = "en",
        host: Optional[Any] = None,
    ) -> None:
        if not isinstance(manifest, dict):
            raise ValueError("Plugin manifest is required.")
        if not manifest.get("id") or not manifest.get("name") or not manifest.get("version"):
            raise ValueError("Plugin manifest requires id, name, and version.")

        self.manifest = manifest
        self.routes: Dict[str, Callable[..., Any]] = dict(routes or {})
        self._default_settings = dict(default_settings or {})
        self._default_locale = default_locale or "en"
        self._host = host

        inline_strings: Dict[str, Dict[str, str]] = {}
        ether_block = manifest.get("ether")
        if isinstance(ether_block, dict) and isinstance(ether_block.get("strings"), dict):
            for loc, val in ether_block["strings"].items():
                if isinstance(val, dict):
                    inline_strings[loc] = dict(val)
        if strings:
            for loc, table in strings.items():
                inline_strings.setdefault(loc, {}).update(table)
        self.i18n = create_i18n(inline_strings, self._default_locale)

        self._settings_schema = (
            ether_block.get("settings") if isinstance(ether_block, dict) else None
        )
        self._current_settings, _ = decode_settings(
            self._settings_schema,
            None,
            self._default_settings,
        )

        self._page_routes: Dict[str, str] = {}
        if isinstance(ether_block, dict):
            contributes = ether_block.get("contributes")
            if isinstance(contributes, dict):
                for p in contributes.get("pages") or []:
                    if isinstance(p, dict) and p.get("route") and p.get("key"):
                        self._page_routes[str(p["route"])] = str(p["key"])
                for s in contributes.get("slots") or []:
                    if isinstance(s, dict) and s.get("route") and s.get("key"):
                        self._page_routes[str(s["route"])] = str(s["key"])

    def get_settings(self) -> Dict[str, Any]:
        return dict(self._current_settings)

    def set_settings(
        self, incoming: Dict[str, Any]
    ) -> Tuple[Dict[str, Any], Dict[str, str]]:
        decoded, errors = decode_settings(
            self._settings_schema,
            incoming,
            self._default_settings,
        )
        if not errors:
            self._current_settings = decoded
        return decoded, errors

    def _build_context(self, req: Dict[str, Any]) -> PluginContext:
        _, query = _parse_url_path_and_query(
            str(req.get("path") or "/"),
            req.get("query"),
        )
        headers = req.get("headers") or {}
        locale_raw = (
            req.get("locale")
            or query.get("locale")
            or _get_header(headers, "X-Ether-Locale")
            or _get_header(headers, "Accept-Language")
            or self._default_locale
        )
        locale = str(locale_raw).split(",")[0].strip() or "en"
        pick_token = _get_header(headers, "X-Ether-Pick")
        request_id = _get_header(headers, "X-Ether-Request-ID")
        host_client = self._host if self._host is not None else create_remote_host_client()
        return PluginContext(
            manifest=self.manifest,
            settings=dict(self._current_settings),
            locale=locale,
            pick_token=pick_token,
            request_id=request_id,
            headers=headers,
            query=query,
            ether=host_client,
            i18n=self.i18n,
        )

    def handle(self, req: Dict[str, Any]) -> Dict[str, Any]:
        try:
            ctx = self._build_context(req)
            return self._dispatch_core(req, ctx)
        except PluginHttpError as exc:
            return _error_response(exc.status, exc.code, exc.message)
        except Exception as exc:
            return _error_response(500, "internal_error", str(exc) or "Internal plugin error.")

    def dispatch(self, req: Dict[str, Any]) -> Dict[str, Any]:
        return self.handle(req)

    def rank_key(
        self,
        candidate: Dict[str, Any],
        config: Optional[Dict[str, Any]] = None,
    ) -> float:
        fn = self.routes.get("rank_key") or self.routes.get("rankKey")
        if not fn:
            return 0
        cfg = config if config is not None else self._current_settings
        return float(fn(candidate, cfg) or 0)

    def cond_match(
        self,
        candidate: Dict[str, Any],
        operator: str = "eq",
        value: Any = None,
    ) -> bool:
        fn = self.routes.get("cond_match") or self.routes.get("condMatch")
        if not fn:
            return False
        return bool(fn(candidate, operator, value))

    def _dispatch_core(self, req: Dict[str, Any], ctx: PluginContext) -> Dict[str, Any]:
        method = str(req.get("method") or "GET").upper()
        path, query = _parse_url_path_and_query(
            str(req.get("path") or "/"),
            req.get("query"),
        )

        if method == "GET" and path == "/manifest.json":
            return _ok_response(self.manifest)

        if method == "GET" and path == "/ether/health":
            fn = self.routes.get("health")
            if fn:
                return _ok_response(fn(ctx))
            return _ok_response({"status": "ok"})

        if method == "POST" and path == "/ether/configure":
            try:
                body_obj = _parse_json_body(req.get("body"))
            except Exception:
                return _error_response(400, "invalid_json", "Request body must be valid JSON.")
            incoming = (
                body_obj["settings"]
                if isinstance(body_obj.get("settings"), dict)
                else body_obj
            )
            decoded, errors = decode_settings(
                self._settings_schema,
                incoming,
                self._default_settings,
            )
            if errors:
                return _ok_response({"errors": errors})
            fn = self.routes.get("configure")
            if fn:
                ctx.settings = dict(decoded)
                res = fn(decoded, ctx) or {}
                custom_errors = res.get("errors") if isinstance(res, dict) else {}
                if not custom_errors:
                    self._current_settings = decoded
                return _ok_response({"errors": custom_errors or {}})
            self._current_settings = decoded
            return _ok_response({"errors": {}})

        if method == "POST" and path == "/ether/test":
            fn = self.routes.get("test")
            if fn:
                return _ok_response(fn(ctx))
            return _ok_response({"ok": True})

        if method == "GET" and path.startswith("/ether/options/"):
            field_name = urllib.parse.unquote(path[len("/ether/options/") :])
            fn = self.routes.get("options")
            if not fn:
                return _ok_response([])
            return _ok_response(fn(field_name, ctx))

        if method == "POST" and path == "/ether/stream/open":
            fn = self.routes.get("open_stream") or self.routes.get("openStream")
            if not fn:
                return _error_response(404, "not_found", "Stream open handler is not registered.")
            body_obj = _parse_json_body(req.get("body"))
            return _ok_response(fn({"streamId": str(body_obj.get("streamId") or "")}, ctx))

        if method == "GET" and path.startswith("/ether/markers/") and path.endswith(".json"):
            fn = self.routes.get("markers")
            if not fn:
                return _error_response(404, "not_found", "Markers handler is not registered.")
            trimmed = path[len("/ether/markers/") : -5]
            parts = trimmed.split("/")
            if len(parts) < 2:
                return _error_response(400, "invalid_path", "Markers route requires type and id.")
            return _ok_response(
                fn(
                    {
                        "type": urllib.parse.unquote(parts[0]),
                        "id": urllib.parse.unquote("/".join(parts[1:])),
                    },
                    ctx,
                )
            )

        if method == "GET" and path == "/ether/channels.json":
            fn = self.routes.get("channels")
            if not fn:
                return _error_response(404, "not_found", "Channels handler is not registered.")
            return _ok_response(fn(ctx))

        if method == "GET" and path.startswith("/ether/guide/") and path.endswith(".json"):
            fn = self.routes.get("guide")
            if not fn:
                return _error_response(404, "not_found", "Guide handler is not registered.")
            channel_id = urllib.parse.unquote(path[len("/ether/guide/") : -5])
            return _ok_response(
                fn(
                    {
                        "channelId": channel_id,
                        "from": query.get("from"),
                        "to": query.get("to"),
                    },
                    ctx,
                )
            )

        if method == "POST" and path == "/ether/notify":
            fn = self.routes.get("notify")
            if not fn:
                return _error_response(404, "not_found", "Notify handler is not registered.")
            body_obj = _parse_json_body(req.get("body"))
            return _ok_response(
                fn(
                    {
                        "title": body_obj.get("title"),
                        "message": str(body_obj.get("message") or body_obj.get("text") or ""),
                        "event": body_obj.get("event"),
                        "settings": body_obj.get("settings"),
                    },
                    ctx,
                )
            )

        if method == "POST" and path == "/ether/event":
            fn = self.routes.get("event")
            if not fn:
                return _error_response(404, "not_found", "Event handler is not registered.")
            body_obj = _parse_json_body(req.get("body"))
            events = body_obj.get("events") if isinstance(body_obj.get("events"), list) else []
            res = fn(events, ctx)
            if isinstance(res, dict) and isinstance(res.get("ack"), list):
                return _ok_response({"ack": res["ack"]})
            return _ok_response({"ack": [e.get("id") for e in events if isinstance(e, dict)]})

        if method == "GET" and (
            path == "/ether/view"
            or path.startswith("/ether/view/")
            or path in self._page_routes
        ):
            fn = self.routes.get("view")
            if not fn:
                return _error_response(404, "not_found", "View handler is not registered.")
            page = self._page_routes.get(path)
            if not page:
                page = (
                    "home"
                    if path == "/ether/view"
                    else urllib.parse.unquote(path[len("/ether/view/") :])
                )
            return _ok_response(fn({"page": page, "query": query}, ctx))

        if method == "POST" and path.startswith("/ether/action/"):
            fn = self.routes.get("action")
            if not fn:
                return _error_response(404, "not_found", "Action handler is not registered.")
            action_name = urllib.parse.unquote(path[len("/ether/action/") :])
            input_obj = _parse_json_body(req.get("body"))
            return _ok_response(fn({"action": action_name, "input": input_obj}, ctx))

        if method == "POST" and path.startswith("/ether/tool/"):
            fn = self.routes.get("tool")
            if not fn:
                return _error_response(404, "not_found", "Tool handler is not registered.")
            tool_name = urllib.parse.unquote(path[len("/ether/tool/") :])
            input_obj = _parse_json_body(req.get("body"))
            return _ok_response(fn({"name": tool_name, "input": input_obj}, ctx))

        if method == "POST" and path.startswith("/ether/job/"):
            fn = self.routes.get("job")
            if not fn:
                return _error_response(404, "not_found", "Job handler is not registered.")
            job_name = urllib.parse.unquote(path[len("/ether/job/") :])
            return _ok_response(fn({"name": job_name}, ctx))

        if method == "POST" and path == "/ether/rank-key":
            fn = self.routes.get("rank_key") or self.routes.get("rankKey")
            if not fn:
                return _error_response(404, "not_found", "Rank key handler is not registered.")
            body_obj = _parse_json_body(req.get("body"))
            candidate = (
                body_obj["candidate"]
                if isinstance(body_obj.get("candidate"), dict)
                else body_obj
            )
            cfg = (
                body_obj["config"]
                if isinstance(body_obj.get("config"), dict)
                else self._current_settings
            )
            score = float(fn(candidate, cfg) or 0)
            return _ok_response({"score": int(score) if score.is_integer() else score})

        if method == "POST" and path == "/ether/cond-match":
            fn = self.routes.get("cond_match") or self.routes.get("condMatch")
            if not fn:
                return _error_response(
                    404, "not_found", "Condition match handler is not registered."
                )
            body_obj = _parse_json_body(req.get("body"))
            candidate = (
                body_obj["candidate"]
                if isinstance(body_obj.get("candidate"), dict)
                else body_obj
            )
            operator = str(body_obj.get("operator") or "eq")
            value = body_obj.get("value")
            return _ok_response({"match": bool(fn(candidate, operator, value))})

        if method in ("GET", "HEAD") and path.startswith("/ether/file/"):
            fn = self.routes.get("file")
            if not fn:
                return _error_response(404, "not_found", "File handler is not registered.")
            rel_path = urllib.parse.unquote(path[len("/ether/file/") :])
            if not rel_path or rel_path.startswith("/") or ".." in rel_path.split("/"):
                return _error_response(
                    400, "invalid_path", "File path must be a clean relative path."
                )
            range_header = _get_header(req.get("headers"), "Range")
            file_res = fn(
                {"path": rel_path, "range": range_header, "headOnly": method == "HEAD"},
                ctx,
            )
            headers: Dict[str, str] = {
                "Content-Type": file_res.get("contentType") or "application/octet-stream",
                "Accept-Ranges": "bytes",
            }
            if isinstance(file_res.get("headers"), dict):
                headers.update(file_res["headers"])
            content_range = file_res.get("contentRange")
            if content_range:
                headers["Content-Range"] = str(content_range)
            elif file_res.get("totalSize") is not None:
                headers["Content-Length"] = str(file_res["totalSize"])
            status = int(file_res.get("status") or (206 if content_range else 200))
            raw_body = file_res.get("body", b"")
            if isinstance(raw_body, str):
                raw_body = raw_body.encode("utf-8")
            return {
                "status": status,
                "headers": headers,
                "body": b"" if method == "HEAD" else bytes(raw_body),
            }

        if method == "GET":
            stremio = _match_stremio_route(path, query)
            if stremio:
                resource = stremio["resource"]
                if resource == "subtitles":
                    if not ctx.pick_token or not ctx.pick_token.strip():
                        return _error_response(
                            403,
                            "pick_required",
                            "Subtitles requests require an X-Ether-Pick token.",
                        )
                    fn = self.routes.get("subtitles")
                    if not fn:
                        return _error_response(
                            404, "not_found", "Subtitles handler is not registered."
                        )
                    extra = stremio["extra"]
                    video_size = (
                        int(extra["videoSize"])
                        if "videoSize" in extra and str(extra["videoSize"]).isdigit()
                        else None
                    )
                    return _ok_response(
                        fn(
                            {
                                "type": stremio["type"],
                                "id": stremio["id"],
                                "extra": extra,
                                "pickToken": ctx.pick_token,
                                "videoHash": extra.get("videoHash"),
                                "videoSize": video_size,
                                "filename": extra.get("filename"),
                            },
                            ctx,
                        )
                    )

                if resource == "catalog":
                    fn = self.routes.get("catalog")
                    if not fn:
                        return _error_response(
                            404, "not_found", "Catalog handler is not registered."
                        )
                    extra = stremio["extra"]
                    skip = (
                        int(extra["skip"])
                        if "skip" in extra and str(extra["skip"]).isdigit()
                        else 0
                    )
                    return _ok_response(
                        fn(
                            {
                                "type": stremio["type"],
                                "id": stremio["id"],
                                "extra": extra,
                                "skip": skip,
                                "search": extra.get("search"),
                                "genre": extra.get("genre"),
                            },
                            ctx,
                        )
                    )

                if resource == "meta":
                    fn = self.routes.get("meta")
                    if not fn:
                        return _error_response(
                            404, "not_found", "Meta handler is not registered."
                        )
                    return _ok_response(
                        fn({"type": stremio["type"], "id": stremio["id"]}, ctx)
                    )

                if resource == "stream":
                    fn = self.routes.get("stream")
                    if not fn:
                        return _error_response(
                            404, "not_found", "Stream handler is not registered."
                        )
                    return _ok_response(
                        fn(
                            {
                                "type": stremio["type"],
                                "id": stremio["id"],
                                "extra": stremio["extra"],
                            },
                            ctx,
                        )
                    )

        return _error_response(404, "not_found", f"No route matched {method} {path}.")


def plugin(
    manifest: Dict[str, Any],
    routes: Optional[Dict[str, Callable[..., Any]]] = None,
    default_settings: Optional[Dict[str, Any]] = None,
    strings: Optional[Dict[str, Dict[str, str]]] = None,
    default_locale: str = "en",
    host: Optional[Any] = None,
) -> PluginInstance:
    return PluginInstance(
        manifest=manifest,
        routes=routes,
        default_settings=default_settings,
        strings=strings,
        default_locale=default_locale,
        host=host,
    )


# ---------------------------------------------------------------------------
# Sidecar HTTP server & WebSocket tunnel (`ether-tunnel-v1`)
# ---------------------------------------------------------------------------


def _serialize_response_body(body: Any) -> Tuple[bytes, bool]:
    if isinstance(body, (bytes, bytearray)):
        return bytes(body), True
    if isinstance(body, str):
        return body.encode("utf-8"), False
    return json.dumps(body if body is not None else {}, separators=(",", ":")).encode("utf-8"), False


def dispatch_over_tunnel_frame(
    instance: PluginInstance,
    frame: Dict[str, Any],
    max_chunk_bytes: int = DEFAULT_MAX_CHUNK_BYTES,
) -> List[Dict[str, Any]]:
    if frame.get("type") != "request" or not frame.get("id"):
        return []
    frame_id = str(frame["id"])
    raw_b64 = frame.get("body") or ""
    decoded_bytes = base64.b64decode(raw_b64) if raw_b64 else b""
    body_text = decoded_bytes.decode("utf-8", errors="replace") if decoded_bytes else None

    headers: Dict[str, Any] = {}
    if isinstance(frame.get("headers"), dict):
        headers.update(frame["headers"])
    if frame.get("requestId") and "X-Ether-Request-ID" not in headers:
        headers["X-Ether-Request-ID"] = frame["requestId"]

    req = {
        "method": frame.get("method") or "GET",
        "path": frame.get("path") or "/",
        "query": frame.get("query"),
        "headers": headers,
        "body": body_text,
    }
    res = instance.dispatch(req)
    payload_bytes, is_binary = _serialize_response_body(res.get("body"))
    multi_headers = {k: [str(v)] for k, v in (res.get("headers") or {}).items()}

    if is_binary and str(req["path"]).startswith("/ether/file/"):
        out: List[Dict[str, Any]] = [
            {
                "type": "response_start",
                "id": frame_id,
                "status": res["status"],
                "headers": multi_headers,
            }
        ]
        chunk_limit = max(1024, int(max_chunk_bytes))
        for offset in range(0, len(payload_bytes), chunk_limit):
            chunk = payload_bytes[offset : offset + chunk_limit]
            out.append(
                {
                    "type": "response_chunk",
                    "id": frame_id,
                    "data": base64.b64encode(chunk).decode("ascii"),
                }
            )
        out.append({"type": "response_end", "id": frame_id})
        return out

    reply: Dict[str, Any] = {
        "type": "response",
        "id": frame_id,
        "status": res["status"],
        "headers": multi_headers,
    }
    if payload_bytes:
        reply["body"] = base64.b64encode(payload_bytes).decode("ascii")
    return [reply]


class _ThreadingHTTPServer(socketserver.ThreadingMixIn, http.server.HTTPServer):
    daemon_threads = True
    allow_reuse_address = True


@dataclass
class ServeHandle:
    mode: str
    url: Optional[str] = None
    port: Optional[int] = None
    _close_fn: Callable[[], None] = field(default=lambda: None, repr=False)

    def close(self) -> None:
        self._close_fn()


def _serve_http(
    instance: PluginInstance,
    host: str,
    port: int,
    background: bool,
) -> ServeHandle:
    class _Handler(http.server.BaseHTTPRequestHandler):
        def _handle_all(self) -> None:
            length = int(self.headers.get("Content-Length", "0") or "0")
            raw_body = self.rfile.read(length).decode("utf-8", errors="replace") if length > 0 else None
            hdrs = {k: v for k, v in self.headers.items()}
            res = instance.dispatch(
                {
                    "method": self.command,
                    "path": self.path,
                    "headers": hdrs,
                    "body": raw_body,
                }
            )
            payload_bytes, _ = _serialize_response_body(res.get("body"))
            self.send_response(int(res.get("status", 200)))
            out_headers = {"Content-Type": "application/json"}
            out_headers.update(res.get("headers") or {})
            out_headers["Content-Length"] = str(len(payload_bytes))
            for k, v in out_headers.items():
                self.send_header(k, str(v))
            self.end_headers()
            if self.command.upper() != "HEAD":
                self.wfile.write(payload_bytes)

        def do_GET(self) -> None:  # noqa: N802
            self._handle_all()

        def do_POST(self) -> None:  # noqa: N802
            self._handle_all()

        def do_PUT(self) -> None:  # noqa: N802
            self._handle_all()

        def do_DELETE(self) -> None:  # noqa: N802
            self._handle_all()

        def do_HEAD(self) -> None:  # noqa: N802
            self._handle_all()

        def log_message(self, format: str, *args: Any) -> None:  # noqa: A002
            return

    server = _ThreadingHTTPServer((host, port), _Handler)
    actual_port = int(server.server_address[1])
    url = f"http://{host}:{actual_port}"

    if background:
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()

        def _close() -> None:
            server.shutdown()
            server.server_close()
            thread.join(timeout=2.0)

        return ServeHandle(mode="http", url=url, port=actual_port, _close_fn=_close)

    try:
        server.serve_forever()
    finally:
        server.server_close()
    return ServeHandle(mode="http", url=url, port=actual_port)


def _ws_send_frame(sock: socket.socket, opcode: int, payload: bytes) -> None:
    mask_key = os.urandom(4)
    header = bytearray([0x80 | opcode])
    length = len(payload)
    if length < 126:
        header.append(0x80 | length)
    elif length < 65536:
        header.append(0x80 | 126)
        header.extend(struct.pack("!H", length))
    else:
        header.append(0x80 | 127)
        header.extend(struct.pack("!Q", length))
    header.extend(mask_key)
    masked = bytearray(length)
    for i, b in enumerate(payload):
        masked[i] = b ^ mask_key[i & 3]
    sock.sendall(bytes(header) + bytes(masked))


def _ws_send_text(sock: socket.socket, text: str) -> None:
    _ws_send_frame(sock, 0x01, text.encode("utf-8"))


def _ws_recv_exact(sock: socket.socket, n: int) -> bytes:
    buf = bytearray()
    while len(buf) < n:
        chunk = sock.recv(n - len(buf))
        if not chunk:
            raise ConnectionError("WebSocket closed unexpectedly")
        buf.extend(chunk)
    return bytes(buf)


def _ws_recv_frame(sock: socket.socket) -> Tuple[int, bytes]:
    first_two = _ws_recv_exact(sock, 2)
    opcode = first_two[0] & 0x0F
    masked = (first_two[1] & 0x80) != 0
    length = first_two[1] & 0x7F
    if length == 126:
        length = struct.unpack("!H", _ws_recv_exact(sock, 2))[0]
    elif length == 127:
        length = struct.unpack("!Q", _ws_recv_exact(sock, 8))[0]
    mask_key = _ws_recv_exact(sock, 4) if masked else b""
    payload = bytearray(_ws_recv_exact(sock, length))
    if masked:
        for i in range(length):
            payload[i] ^= mask_key[i & 3]
    return opcode, bytes(payload)


def _serve_tunnel(
    instance: PluginInstance,
    tunnel_url: str,
    credential: str,
    background: bool,
) -> ServeHandle:
    parsed = urllib.parse.urlparse(tunnel_url)
    if parsed.scheme not in ("ws", "wss") or not parsed.hostname:
        raise ValueError("Tunnel URL must use ws:// or wss:// and include a host")
    host = parsed.hostname
    port = parsed.port or (443 if parsed.scheme == "wss" else 80)
    path = parsed.path or "/ether/plugins/tunnel"
    if parsed.query:
        path = f"{path}?{parsed.query}"

    sock = socket.create_connection((host, port), timeout=10)
    if parsed.scheme == "wss":
        try:
            sock = ssl.create_default_context().wrap_socket(sock, server_hostname=host)
        except Exception:
            sock.close()
            raise
    ws_key = base64.b64encode(os.urandom(16)).decode("ascii")
    lines = [
        f"GET {path} HTTP/1.1",
        f"Host: {host}:{port}",
        "Upgrade: websocket",
        "Connection: Upgrade",
        f"Sec-WebSocket-Key: {ws_key}",
        "Sec-WebSocket-Version: 13",
        f"Sec-WebSocket-Protocol: {TUNNEL_PROTOCOL}",
    ]
    if credential:
        lines.append(f"X-Ether-Plugin-Credential: {credential}")
    lines.append("\r\n")
    sock.sendall("\r\n".join(lines).encode("ascii"))

    resp_buf = b""
    while b"\r\n\r\n" not in resp_buf:
        chunk = sock.recv(1)
        if not chunk or len(resp_buf) >= 16384:
            sock.close()
            raise ConnectionError("Failed WebSocket tunnel handshake")
        resp_buf += chunk
    status_line = resp_buf.split(b"\r\n", 1)[0].decode("ascii", errors="replace")
    if " 101 " not in status_line:
        sock.close()
        raise ConnectionError(f"Tunnel handshake rejected: {status_line}")

    expected_accept = base64.b64encode(
        hashlib.sha1((ws_key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode("ascii")).digest()
    ).decode("ascii")
    if expected_accept.encode("ascii") not in resp_buf:
        sock.close()
        raise ConnectionError("Invalid Sec-WebSocket-Accept in tunnel handshake")

    sock.settimeout(None)
    hello = {
        "type": "hello",
        "protocol": TUNNEL_PROTOCOL,
        "plugin": instance.manifest["id"],
        "features": ["range", "stream"],
    }
    _ws_send_text(sock, json.dumps(hello))

    opcode, welcome_bytes = _ws_recv_frame(sock)
    if opcode != 0x01:
        sock.close()
        raise ConnectionError("Expected welcome text frame on tunnel")
    welcome = json.loads(welcome_bytes.decode("utf-8"))
    if welcome.get("type") != "welcome":
        sock.close()
        raise ConnectionError(f"Unexpected tunnel frame: {welcome.get('type')}")
    max_chunk_bytes = int(welcome.get("maxChunkBytes") or DEFAULT_MAX_CHUNK_BYTES)

    stop_event = threading.Event()
    write_lock = threading.Lock()
    active_lock = threading.Lock()
    active: Dict[str, threading.Event] = {}

    def _reply(frame: Dict[str, Any], cancelled: threading.Event) -> None:
        frame_id = str(frame.get("id", ""))
        try:
            replies = dispatch_over_tunnel_frame(instance, frame, max_chunk_bytes)
            for reply in replies:
                if cancelled.is_set() or stop_event.is_set():
                    return
                with write_lock:
                    _ws_send_text(sock, json.dumps(reply))
        except Exception:
            if not cancelled.is_set() and not stop_event.is_set():
                with write_lock:
                    _ws_send_text(sock, json.dumps({"type": "error", "id": frame_id,
                        "error": {"code": "internal_error", "message": "Plugin request failed"}}))
        finally:
            with active_lock:
                active.pop(frame_id, None)

    def _loop() -> None:
        try:
            while not stop_event.is_set():
                op, raw = _ws_recv_frame(sock)
                if op == 0x08:
                    break
                if op == 0x09:
                    with write_lock:
                        _ws_send_frame(sock, 0x0A, raw)
                    continue
                if op != 0x01:
                    continue
                frame = json.loads(raw.decode("utf-8"))
                frame_id = str(frame.get("id", ""))
                if frame.get("type") == "cancel":
                    with active_lock:
                        cancelled = active.get(frame_id)
                    if cancelled:
                        cancelled.set()
                elif frame.get("type") == "request":
                    cancelled = threading.Event()
                    with active_lock:
                        active[frame_id] = cancelled
                    threading.Thread(target=_reply, args=(frame, cancelled), daemon=True).start()
        except (OSError, ConnectionError, ValueError):
            pass
        finally:
            stop_event.set()
            with active_lock:
                for cancelled in active.values():
                    cancelled.set()
            sock.close()

    def _close() -> None:
        stop_event.set()
        try:
            sock.shutdown(socket.SHUT_RDWR)
        except OSError:
            pass
        sock.close()

    if background:
        thread = threading.Thread(target=_loop, daemon=True)
        thread.start()
        return ServeHandle(mode="tunnel", url=tunnel_url, _close_fn=_close)

    try:
        _loop()
    finally:
        _close()
    return ServeHandle(mode="tunnel", url=tunnel_url)


def serve(
    instance: PluginInstance,
    *,
    mode: Optional[str] = None,
    host: Optional[str] = None,
    port: Optional[int] = None,
    tunnel_url: Optional[str] = None,
    credential: Optional[str] = None,
    background: bool = True,
) -> ServeHandle:
    resolved_tunnel = tunnel_url or os.environ.get("ETHER_TUNNEL_URL")
    resolved_mode = mode or ("tunnel" if resolved_tunnel else "http")
    if resolved_mode == "tunnel":
        if not resolved_tunnel:
            raise ValueError("Tunnel mode requires tunnel_url or ETHER_TUNNEL_URL.")
        cred = credential if credential is not None else os.environ.get("ETHER_PLUGIN_CREDENTIAL", "")
        return _serve_tunnel(instance, resolved_tunnel, cred, background)

    resolved_host = host or os.environ.get("ETHER_PLUGIN_HOST") or "127.0.0.1"
    resolved_port = (
        port
        if port is not None
        else int(os.environ["ETHER_PLUGIN_PORT"])
        if os.environ.get("ETHER_PLUGIN_PORT")
        else 0
    )
    return _serve_http(instance, resolved_host, resolved_port, background)


# ---------------------------------------------------------------------------
# Testing harness, mock host client, and fixtures
# ---------------------------------------------------------------------------


class MockHostClient:
    """In-memory mock implementation of Ether host services for unit tests."""

    def __init__(self) -> None:
        self.kv_store: Dict[str, str] = {}
        self.secrets_store: Dict[str, str] = {}
        self.catalog_store: Dict[str, Dict[str, Any]] = {}
        self.history_store: Dict[int, List[Dict[str, Any]]] = {}
        self.watch_state: Dict[str, bool] = {}
        self.collections_list: List[Dict[str, Any]] = []
        self.notifications: List[str] = []
        self.logs: List[Dict[str, Any]] = []
        self.http_handler: Callable[[Dict[str, Any]], Dict[str, Any]] = lambda req: {
            "status": 200,
            "headers": {"Content-Type": "application/json"},
            "body": "{}",
        }

        self.kv = self._KV(self)
        self.secret = self._Secret(self)
        self.http = self._Http(self)
        self.catalog = self._Catalog(self)
        self.viewers = self._Viewers(self)
        self.collections = self._Collections(self)
        self.notify = self._Notify(self)

    class _KV:
        def __init__(self, parent: "MockHostClient") -> None:
            self._p = parent

        def get(self, key: str) -> Optional[str]:
            return self._p.kv_store.get(key)

        def put(self, key: str, value: str) -> None:
            self._p.kv_store[key] = str(value)

        def delete(self, key: str) -> None:
            self._p.kv_store.pop(key, None)

        def list(self, prefix: str = "") -> List[str]:
            return sorted(k for k in self._p.kv_store if k.startswith(prefix))

    class _Secret:
        def __init__(self, parent: "MockHostClient") -> None:
            self._p = parent

        def get(self, name: str) -> str:
            return self._p.secrets_store.get(name, "")

    class _Http:
        def __init__(self, parent: "MockHostClient") -> None:
            self._p = parent

        def fetch(self, req: Dict[str, Any]) -> Dict[str, Any]:
            return self._p.http_handler(req)

    class _Catalog:
        def __init__(self, parent: "MockHostClient") -> None:
            self._p = parent

        def search(self, query: str, limit: int = 50, offset: int = 0) -> Dict[str, Any]:
            needle = query.lower()
            matches = [
                v
                for v in self._p.catalog_store.values()
                if needle in str(v.get("title", "")).lower()
            ]
            return {"titles": matches[offset : offset + limit]}

        def get(self, title_id: str) -> Optional[Dict[str, Any]]:
            return self._p.catalog_store.get(title_id)

    class _Viewers:
        def __init__(self, parent: "MockHostClient") -> None:
            self._p = parent

        def history(self, viewer_id: int, limit: int = 50, offset: int = 0) -> List[Dict[str, Any]]:
            items = self._p.history_store.get(int(viewer_id), [])
            return items[offset : offset + limit]

        def set_watch_state(self, viewer_id: int, title_id: str, watched: bool) -> None:
            self._p.watch_state[f"{viewer_id}:{title_id}"] = bool(watched)

        setWatchState = set_watch_state

    class _Collections:
        def __init__(self, parent: "MockHostClient") -> None:
            self._p = parent

        def upsert(self, collection: Dict[str, Any]) -> None:
            self._p.collections_list.append(collection)

    class _Notify:
        def __init__(self, parent: "MockHostClient") -> None:
            self._p = parent

        def owner(self, text: str) -> None:
            self._p.notifications.append(str(text))

    def log(
        self,
        level: str,
        message: str,
        attrs: Optional[Dict[str, Any]] = None,
    ) -> None:
        self.logs.append({"level": level, "message": message, "attrs": attrs})

    def abort(self, message: str = "aborted") -> None:
        raise RuntimeError(f'__ETHER_FAULT__{{"code":"aborted","message":{json.dumps(message)}}}')


def create_mock_host_client() -> MockHostClient:
    return MockHostClient()


def assert_valid_view_spec(doc: Dict[str, Any]) -> None:
    if not isinstance(doc, dict):
        raise AssertionError("ViewSpecDocument must be a dict.")
    if doc.get("specVersion") != 1:
        raise AssertionError(f"ViewSpecDocument specVersion must be 1 (got {doc.get('specVersion')}).")
    if doc.get("layout") not in ("list", "detail", "bare"):
        raise AssertionError(f"ViewSpecDocument layout must be list, detail, or bare (got {doc.get('layout')}).")
    if not isinstance(doc.get("header"), list):
        raise AssertionError("ViewSpecDocument header must be a list.")
    if not isinstance(doc.get("body"), list):
        raise AssertionError("ViewSpecDocument body must be a list.")

    def _check_no_classname(val: Any, path: str) -> None:
        if isinstance(val, list):
            for idx, entry in enumerate(val):
                _check_no_classname(entry, f"{path}[{idx}]")
            return
        if isinstance(val, dict):
            for k, v in val.items():
                if k == "className" or k.endswith("ClassName"):
                    raise AssertionError(f"ViewSpec forbids {k} at {path}.{k}.")
                _check_no_classname(v, f"{path}.{k}")

    _check_no_classname(doc, "spec")


class fixtures:  # noqa: N801
    @staticmethod
    def pick_token(item_id: str = "ether:movie:1") -> str:
        return f"pick-v1:{item_id}:sig-test"

    @staticmethod
    def movie(**overrides: Any) -> Dict[str, Any]:
        base: Dict[str, Any] = {
            "id": "tt0133093",
            "type": "movie",
            "name": "The Matrix",
            "releaseInfo": "1999",
            "description": "A computer hacker learns about the true nature of reality.",
            "genres": ["Action", "Sci-Fi"],
            "imdbRating": "8.7",
        }
        base.update(overrides)
        return base

    @staticmethod
    def catalog_item(**overrides: Any) -> Dict[str, Any]:
        base: Dict[str, Any] = {
            "id": "tt0133093",
            "type": "movie",
            "name": "The Matrix",
            "releaseInfo": "1999",
        }
        base.update(overrides)
        return base

    @staticmethod
    def playback_event(
        name: str = "playback.completed", **overrides: Any
    ) -> Dict[str, Any]:
        base: Dict[str, Any] = {
            "id": "evt_01",
            "name": name,
            "at": "2026-11-02T12:00:00Z",
            "viewerId": 1,
            "payload": {
                "titleId": "tt0133093",
                "title": "The Matrix",
                "progress": 0.95,
            },
        }
        base.update(overrides)
        return base

    @staticmethod
    def candidate(**overrides: Any) -> Dict[str, Any]:
        base: Dict[str, Any] = {
            "releaseId": "rel_01",
            "name": "The.Matrix.1999.2160p.UHD.BluRay.x265",
            "resolution": 2160,
            "hdr": ["DV", "HDR10"],
            "videoCodec": "hevc",
            "cached": True,
            "verified": True,
        }
        base.update(overrides)
        return base


def _encode_extra_path(extra: Optional[Dict[str, str]]) -> str:
    if not extra:
        return ""
    return "/" + urllib.parse.urlencode(extra)


class TestHarness:
    """Synchronous test harness for exercising an Ether `PluginInstance`."""

    def __init__(
        self,
        instance: PluginInstance,
        host: Optional[MockHostClient] = None,
    ) -> None:
        self.host = host or (
            instance._host if isinstance(instance._host, MockHostClient) else create_mock_host_client()
        )
        instance._host = self.host
        self.instance = instance

    def request(self, req: Dict[str, Any]) -> Dict[str, Any]:
        return self.instance.dispatch(req)

    def manifest(self) -> Dict[str, Any]:
        return self.instance.dispatch({"method": "GET", "path": "/manifest.json"})["body"]

    def health(self) -> Dict[str, Any]:
        return self.instance.dispatch({"method": "GET", "path": "/ether/health"})

    def configure(self, settings: Dict[str, Any]) -> Dict[str, Any]:
        return self.instance.dispatch(
            {"method": "POST", "path": "/ether/configure", "body": json.dumps(settings)}
        )

    def test(self) -> Dict[str, Any]:
        return self.instance.dispatch({"method": "POST", "path": "/ether/test"})

    def options(self, field_name: str) -> Dict[str, Any]:
        encoded = urllib.parse.quote(field_name, safe="")
        return self.instance.dispatch({"method": "GET", "path": f"/ether/options/{encoded}"})

    def catalog(
        self,
        media_type: str,
        catalog_id: str,
        extra: Optional[Dict[str, str]] = None,
    ) -> Dict[str, Any]:
        suffix = _encode_extra_path(extra)
        return self.instance.dispatch(
            {
                "method": "GET",
                "path": f"/catalog/{urllib.parse.quote(media_type)}/{urllib.parse.quote(catalog_id)}{suffix}.json",
            }
        )

    def meta(self, media_type: str, item_id: str) -> Dict[str, Any]:
        return self.instance.dispatch(
            {
                "method": "GET",
                "path": f"/meta/{urllib.parse.quote(media_type)}/{urllib.parse.quote(item_id)}.json",
            }
        )

    def stream(
        self,
        media_type: str,
        item_id: str,
        extra: Optional[Dict[str, str]] = None,
    ) -> Dict[str, Any]:
        suffix = _encode_extra_path(extra)
        return self.instance.dispatch(
            {
                "method": "GET",
                "path": f"/stream/{urllib.parse.quote(media_type)}/{urllib.parse.quote(item_id)}{suffix}.json",
            }
        )

    def subtitles(
        self,
        media_type: str,
        item_id: str,
        pick_token: Optional[str] = None,
        extra: Optional[Dict[str, str]] = None,
    ) -> Dict[str, Any]:
        suffix = _encode_extra_path(extra)
        headers: Dict[str, str] = {}
        if pick_token is not None:
            headers["X-Ether-Pick"] = pick_token
        return self.instance.dispatch(
            {
                "method": "GET",
                "path": f"/subtitles/{urllib.parse.quote(media_type)}/{urllib.parse.quote(item_id)}{suffix}.json",
                "headers": headers,
            }
        )

    def markers(self, media_type: str, item_id: str) -> Dict[str, Any]:
        return self.instance.dispatch(
            {
                "method": "GET",
                "path": f"/ether/markers/{urllib.parse.quote(media_type)}/{urllib.parse.quote(item_id)}.json",
            }
        )

    def event(self, events: List[Dict[str, Any]]) -> Dict[str, Any]:
        return self.instance.dispatch(
            {
                "method": "POST",
                "path": "/ether/event",
                "body": json.dumps({"events": events}),
            }
        )

    def view(
        self,
        page: str = "home",
        query: Optional[Dict[str, str]] = None,
    ) -> Dict[str, Any]:
        clean = page.lstrip("/") or "home"
        res = self.instance.dispatch(
            {
                "method": "GET",
                "path": f"/ether/view/{urllib.parse.quote(clean)}",
                "query": query,
            }
        )
        if res["status"] == 200:
            assert_valid_view_spec(res["body"]["spec"])
        return res

    def action(
        self,
        action_name: str,
        input_payload: Optional[Dict[str, Any]] = None,
    ) -> Dict[str, Any]:
        return self.instance.dispatch(
            {
                "method": "POST",
                "path": f"/ether/action/{urllib.parse.quote(action_name)}",
                "body": json.dumps(input_payload or {}),
            }
        )

    def tool(
        self,
        name: str,
        input_payload: Optional[Dict[str, Any]] = None,
    ) -> Dict[str, Any]:
        return self.instance.dispatch(
            {
                "method": "POST",
                "path": f"/ether/tool/{urllib.parse.quote(name)}",
                "body": json.dumps(input_payload or {}),
            }
        )

    def rank_key(
        self,
        candidate: Dict[str, Any],
        config: Optional[Dict[str, Any]] = None,
    ) -> float:
        return self.instance.rank_key(candidate, config)

    def cond_match(
        self,
        candidate: Dict[str, Any],
        operator: str = "eq",
        value: Any = None,
    ) -> bool:
        return self.instance.cond_match(candidate, operator, value)

    def file(self, path: str, range_header: Optional[str] = None) -> Dict[str, Any]:
        headers: Dict[str, str] = {}
        if range_header:
            headers["Range"] = range_header
        clean = path.lstrip("/")
        return self.instance.dispatch(
            {"method": "GET", "path": f"/ether/file/{clean}", "headers": headers}
        )


def create_test_harness(
    instance: PluginInstance,
    host: Optional[MockHostClient] = None,
) -> TestHarness:
    return TestHarness(instance=instance, host=host)


# ---------------------------------------------------------------------------
# Capability starter templates
# ---------------------------------------------------------------------------


def _base_manifest(
    plugin_id: str,
    name: str,
    kind: str,
    capabilities: List[str],
    version: str = "0.1.0",
    summary: Optional[str] = None,
) -> Dict[str, Any]:
    resolved_summary = summary or f"{name} plugin for Ether."
    return {
        "id": plugin_id,
        "name": name,
        "version": version,
        "description": resolved_summary,
        "types": ["movie", "series"],
        "idPrefixes": ["tt"],
        "ether": {
            "protocol": 1,
            "kind": kind,
            "capabilities": capabilities,
            "summary": resolved_summary,
            "runtime": {"kind": "remote"},
            "permissions": {"catalog": "none"},
        },
    }


def create_catalog_template(
    plugin_id: str,
    name: str,
    version: str = "0.1.0",
) -> PluginInstance:
    man = _base_manifest(plugin_id, name, "extension", ["list_source"], version=version)
    man["catalogs"] = [
        {
            "type": "movie",
            "id": "featured",
            "name": "Featured movies",
            "extra": [{"name": "search", "isRequired": False}],
        }
    ]
    man["resources"] = ["catalog", "meta"]
    man["ether"]["settings"] = {
        "fields": [
            {
                "key": "apiKey",
                "kind": "secret",
                "label": "Access token",
                "hint": "Token used to query the upstream catalog",
            }
        ]
    }
    man["ether"]["contributes"] = {
        "listSources": [
            {
                "key": "featured",
                "label": "Featured movies",
                "hint": "Curated movies from the upstream list",
                "catalog": "featured",
                "refresh": "1h",
            }
        ]
    }
    return plugin(
        manifest=man,
        routes={
            "catalog": lambda req, _ctx: {
                "metas": [
                    {
                        "id": "tt0133093",
                        "type": req["type"],
                        "name": (
                            f"Result for {req['search']}"
                            if req.get("search")
                            else f"Featured in {req['id']}"
                        ),
                        "releaseInfo": "1999",
                    }
                ]
            },
            "meta": lambda req, _ctx: {
                "meta": {
                    "id": req["id"],
                    "type": req["type"],
                    "name": f"Title {req['id']}",
                    "releaseInfo": "1999",
                    "description": "Catalog metadata entry.",
                }
            },
        },
    )


def create_subtitles_template(
    plugin_id: str,
    name: str,
    version: str = "0.1.0",
) -> PluginInstance:
    man = _base_manifest(plugin_id, name, "extension", ["subtitles"], version=version)
    man["resources"] = ["subtitles"]
    man["ether"]["settings"] = {
        "fields": [
            {
                "key": "preferredLanguage",
                "kind": "select",
                "label": "Preferred language",
                "default": "en",
                "options": [
                    {"value": "en", "label": "English"},
                    {"value": "es", "label": "Spanish"},
                    {"value": "fr", "label": "French"},
                ],
            }
        ]
    }
    man["ether"]["contributes"] = {
        "subtitles": {"languages": ["en", "es", "fr"], "formats": ["vtt", "srt"]}
    }

    def _subtitles(req: Dict[str, Any], ctx: PluginContext) -> Dict[str, Any]:
        lang = str(ctx.settings.get("preferredLanguage") or "en")
        encoded = urllib.parse.quote(req["id"], safe="")
        return {
            "subtitles": [
                {
                    "id": f"{req['id']}:{lang}",
                    "url": f"https://subtitles.example.invalid/{encoded}/{lang}.vtt",
                    "lang": lang,
                    "label": lang.upper(),
                    "format": "vtt",
                }
            ]
        }

    return plugin(
        manifest=man,
        default_settings={"preferredLanguage": "en"},
        routes={"subtitles": _subtitles},
    )


def create_scrobbler_template(
    plugin_id: str,
    name: str,
    version: str = "0.1.0",
) -> PluginInstance:
    man = _base_manifest(plugin_id, name, "integration", ["events"], version=version)
    man["ether"]["contributes"] = {
        "integration": {"category": "scrobbler", "adds": ["events"], "connect": "none"},
        "events": {
            "subscribe": [
                "playback.started",
                "playback.paused",
                "playback.stopped",
                "playback.completed",
            ]
        },
    }

    def _event(events: List[Dict[str, Any]], ctx: PluginContext) -> Dict[str, Any]:
        ack: List[str] = []
        for ev in events:
            viewer_id = ev.get("viewerId") or 0
            ctx.ether.kv.put(f"last_event:{viewer_id}", json.dumps(ev))
            ack.append(str(ev["id"]))
        return {"ack": ack}

    return plugin(manifest=man, routes={"event": _event})


def create_view_template(
    plugin_id: str,
    name: str,
    version: str = "0.1.0",
) -> PluginInstance:
    man = _base_manifest(plugin_id, name, "extension", ["ui"], version=version)
    man["ether"]["contributes"] = {
        "pages": [{"key": "home", "label": name, "route": "/ether/view/home"}]
    }

    def _view(_req: Dict[str, Any], ctx: PluginContext) -> Dict[str, Any]:
        raw = ctx.ether.kv.get("refresh_count")
        count = int(raw) if raw and str(raw).isdigit() else 0
        return {
            "spec": {
                "specVersion": 1,
                "layout": "list",
                "header": [
                    {
                        "kind": "page-header",
                        "title": {"$": "title"},
                        "description": {"$": "subtitle"},
                    }
                ],
                "body": [
                    {
                        "kind": "panel",
                        "title": "Status",
                        "blocks": [{"kind": "text", "content": {"$": "statusText"}}],
                    }
                ],
            },
            "data": {
                "title": name,
                "subtitle": f"Refreshed {count} times",
                "statusText": "Plugin view is ready.",
            },
        }

    def _action(req: Dict[str, Any], ctx: PluginContext) -> Dict[str, Any]:
        if req.get("action") == "refresh":
            raw = ctx.ether.kv.get("refresh_count")
            current = (int(raw) if raw and str(raw).isdigit() else 0) + 1
            ctx.ether.kv.put("refresh_count", str(current))
            return {"toast": "State refreshed.", "refresh": True}
        return {"toast": "Unknown action."}

    return plugin(manifest=man, routes={"view": _view, "action": _action})


def create_ranker_template(
    plugin_id: str,
    name: str,
    version: str = "0.1.0",
) -> PluginInstance:
    man = _base_manifest(plugin_id, name, "extension", ["ranking_step"], version=version)
    man["ether"]["settings"] = {
        "fields": [
            {
                "key": "boost4k",
                "kind": "bool",
                "label": "Boost 2160p releases",
                "default": True,
            }
        ]
    }
    man["ether"]["contributes"] = {
        "rankingSteps": [
            {
                "key": "resolution_boost",
                "label": "Boost 2160p releases",
                "hint": "Adds a score bonus when a candidate is 2160p",
            }
        ]
    }

    def _rank_key(candidate: Dict[str, Any], config: Dict[str, Any]) -> int:
        res = candidate.get("resolution")
        is_4k = res == 2160 or res == "2160p"
        bonus = 1000 if config.get("boost4k") and is_4k else 0
        return (500 if candidate.get("cached") else 0) + bonus

    return plugin(
        manifest=man,
        default_settings={"boost4k": True},
        routes={"rank_key": _rank_key},
    )


def create_condition_template(
    plugin_id: str,
    name: str,
    version: str = "0.1.0",
) -> PluginInstance:
    man = _base_manifest(plugin_id, name, "extension", ["rule_condition"], version=version)
    man["ether"]["contributes"] = {
        "ruleConditions": [
            {
                "key": "cached_release",
                "label": "Release is cached",
                "operators": ["eq"],
            }
        ]
    }

    def _cond_match(candidate: Dict[str, Any], _op: str, value: Any) -> bool:
        want = value if isinstance(value, bool) else True
        return bool(candidate.get("cached")) is want

    return plugin(manifest=man, routes={"cond_match": _cond_match})
