from __future__ import annotations

import os
import sys
import urllib.parse
from typing import Any, Dict

_SDK_DIR = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", "..", "python"))
if _SDK_DIR not in sys.path:
    sys.path.insert(0, _SDK_DIR)

from ether_plugin import PluginContext, plugin, serve  # noqa: E402

HELLO_MANIFEST: Dict[str, Any] = {
    "id": "org.ether.examples.hello",
    "version": "0.1.0",
    "name": "Hello plugin",
    "description": "Minimal greeting plugin for Ether.",
    "types": ["movie"],
    "idPrefixes": ["tt"],
    "catalogs": [
        {
            "type": "movie",
            "id": "hello",
            "name": "Hello movies",
            "extra": [{"name": "search", "isRequired": False}],
        }
    ],
    "resources": ["catalog", "meta", "subtitles"],
    "ether": {
        "protocol": 1,
        "kind": "extension",
        "capabilities": [
            "list_source",
            "subtitles",
            "ui",
            "ranking_step",
            "rule_condition",
        ],
        "summary": "Minimal greeting plugin for Ether.",
        "runtime": {"kind": "remote", "entry": "plugin.py"},
        "permissions": {"catalog": "none"},
        "settings": {
            "fields": [
                {
                    "key": "greeting",
                    "kind": "text",
                    "label": "Greeting word",
                    "hint": "Word used to greet viewers in the catalog and view",
                    "default": "Hello",
                }
            ]
        },
        "contributes": {
            "listSources": [
                {
                    "key": "hello",
                    "label": "Hello movies",
                    "hint": "Sample movie list from the hello plugin",
                    "catalog": "hello",
                    "refresh": "1h",
                }
            ],
            "subtitles": {"languages": ["en"], "formats": ["vtt"]},
            "pages": [
                {
                    "key": "home",
                    "label": "Hello overview",
                    "route": "/ether/view/home",
                }
            ],
            "rankingSteps": [
                {
                    "key": "cached_boost",
                    "label": "Boost cached releases",
                    "hint": "Adds a score bonus when a release is cached",
                }
            ],
            "ruleConditions": [
                {
                    "key": "is_cached",
                    "label": "Release is cached",
                    "operators": ["eq"],
                }
            ],
        },
    },
}


def _catalog(req: Dict[str, Any], ctx: PluginContext) -> Dict[str, Any]:
    greeting = str(ctx.settings.get("greeting") or "Hello")
    target = str(req.get("search") or "from Ether")
    return {
        "metas": [
            {
                "id": "tt0133093",
                "type": req["type"],
                "name": f"{greeting} {target}",
                "releaseInfo": "1999",
                "description": "A computer hacker learns about the true nature of reality.",
            }
        ]
    }


def _meta(req: Dict[str, Any], _ctx: PluginContext) -> Dict[str, Any]:
    return {
        "meta": {
            "id": req["id"],
            "type": req["type"],
            "name": "The Matrix",
            "releaseInfo": "1999",
            "description": "A computer hacker learns about the true nature of reality.",
        }
    }


def _subtitles(req: Dict[str, Any], _ctx: PluginContext) -> Dict[str, Any]:
    encoded = urllib.parse.quote(req["id"], safe="")
    return {
        "subtitles": [
            {
                "id": f"{req['id']}:en",
                "url": f"https://subtitles.example.invalid/{encoded}/en.vtt",
                "lang": "en",
                "label": "English",
                "format": "vtt",
            }
        ]
    }


def _view(_req: Dict[str, Any], ctx: PluginContext) -> Dict[str, Any]:
    greeting = str(ctx.settings.get("greeting") or "Hello")
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
                    "title": "Greeting",
                    "blocks": [{"kind": "text", "content": {"$": "greetingText"}}],
                }
            ],
        },
        "data": {
            "title": "Hello plugin",
            "subtitle": "Greeting view",
            "greetingText": f"{greeting} from Ether",
        },
    }


def _action(_req: Dict[str, Any], ctx: PluginContext) -> Dict[str, Any]:
    greeting = str(ctx.settings.get("greeting") or "Hello")
    return {"toast": f"{greeting} from Ether", "refresh": True}


hello_plugin = plugin(
    manifest=HELLO_MANIFEST,
    default_settings={"greeting": "Hello"},
    routes={
        "test": lambda _ctx: {"ok": True, "message": "Hello plugin is healthy."},
        "catalog": _catalog,
        "meta": _meta,
        "subtitles": _subtitles,
        "view": _view,
        "action": _action,
        "rank_key": lambda cand, _cfg: 1000 if cand.get("cached") else 100,
        "cond_match": lambda cand, _op, val: bool(cand.get("cached"))
        is (val if isinstance(val, bool) else True),
    },
)


if __name__ == "__main__":
    serve(hello_plugin, background=False)
