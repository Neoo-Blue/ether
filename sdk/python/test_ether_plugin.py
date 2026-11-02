import http.server
import json
import socketserver
import socket
from unittest import mock
import ether_plugin
import threading
import unittest
import urllib.request

from ether_plugin import (
    PluginRouteError,
    assert_valid_view_spec,
    create_catalog_template,
    create_condition_template,
    create_i18n,
    create_mock_host_client,
    create_ranker_template,
    create_remote_host_client,
    create_scrobbler_template,
    create_subtitles_template,
    create_test_harness,
    create_view_template,
    decode_settings,
    dispatch_over_tunnel_frame,
    fixtures,
    lint_copy,
    lint_settings_schema,
    plugin,
    serve,
)


class EtherPluginSDKTest(unittest.TestCase):
    def test_tunnel_uses_tls_and_pong_preserves_payload(self) -> None:
        instance = plugin({"id": "test", "name": "test", "version": "1"}, {})
        fake_socket = mock.Mock()
        tls_context = mock.Mock()
        tls_context.wrap_socket.side_effect = OSError("TLS failed")
        with mock.patch.object(ether_plugin.socket, "create_connection", return_value=fake_socket), mock.patch.object(ether_plugin.ssl, "create_default_context", return_value=tls_context):
            with self.assertRaisesRegex(OSError, "TLS failed"):
                serve(instance, mode="tunnel", tunnel_url="wss://example.test/tunnel")
        tls_context.wrap_socket.assert_called_once_with(fake_socket, server_hostname="example.test")
        fake_socket.close.assert_called_once()
        a, b = socket.socketpair()
        try:
            ether_plugin._ws_send_frame(a, 0x0A, b"ping payload")
            self.assertEqual(ether_plugin._ws_recv_frame(b), (0x0A, b"ping payload"))
        finally:
            a.close()
            b.close()

    def test_settings_decode_and_lint(self) -> None:
        schema = {
            "fields": [
                {
                    "key": "apiKey",
                    "kind": "secret",
                    "label": "Access token",
                    "hint": "Token used to query the upstream service",
                    "required": True,
                },
                {
                    "key": "maxResults",
                    "kind": "number",
                    "label": "Maximum results",
                    "default": 20,
                    "min": 1,
                    "max": 100,
                },
                {
                    "key": "includeAdult",
                    "kind": "bool",
                    "label": "Include adult titles",
                    "default": False,
                },
            ]
        }
        self.assertEqual(lint_settings_schema(schema), [])
        self.assertEqual(lint_copy("Valid sentence case label", 40), "")
        self.assertNotEqual(lint_copy("Invalid — em dash", 40), "")
        self.assertNotEqual(lint_copy("Invalid exclamation!", 40), "")
        self.assertNotEqual(lint_copy("Simply invalid word", 40), "")

        defaults, errs = decode_settings(schema, {})
        self.assertEqual(defaults["maxResults"], 20)
        self.assertEqual(defaults["includeAdult"], False)
        self.assertEqual(errs.get("/apiKey"), "is required")

        valid, errs2 = decode_settings(
            schema,
            {"apiKey": "tok_1234", "maxResults": 50, "includeAdult": True},
        )
        self.assertEqual(errs2, {})
        self.assertEqual(valid["apiKey"], "tok_1234")
        self.assertEqual(valid["maxResults"], 50)
        self.assertTrue(valid["includeAdult"])

    def test_i18n_helper(self) -> None:
        i18n = create_i18n(
            {
                "en": {"greeting": "Hello {name}", "fallbackOnly": "Base text"},
                "fr-CA": {"greeting": "Bonjour {name}"},
            },
            "en",
        )
        self.assertEqual(i18n.t("greeting", {"name": "Ada"}, "fr-CA"), "Bonjour Ada")
        self.assertEqual(i18n.t("$t:fallbackOnly", None, "fr-CA"), "Base text")
        self.assertEqual(i18n.t("missing.key", None, "fr-CA"), "missing.key")

    def test_router_all_capabilities(self) -> None:
        host = create_mock_host_client()
        inst = plugin(
            manifest={
                "id": "org.ether.test.all",
                "name": "Test all capabilities",
                "version": "1.2.3",
                "description": "Test plugin covering all capabilities.",
                "types": ["movie", "series"],
                "idPrefixes": ["tt"],
                "ether": {
                    "protocol": 1,
                    "kind": "extension",
                    "capabilities": [
                        "list_source",
                        "stream_source",
                        "subtitles",
                        "markers",
                        "events",
                        "ui",
                        "tools",
                        "ranking_step",
                        "rule_condition",
                    ],
                    "summary": "Test plugin covering all capabilities.",
                    "runtime": {"kind": "remote"},
                    "settings": {
                        "fields": [
                            {
                                "key": "prefix",
                                "kind": "text",
                                "label": "Title prefix",
                                "default": "Ether",
                                "required": True,
                            }
                        ]
                    },
                    "contributes": {
                        "pages": [
                            {"key": "home", "label": "Overview", "route": "/ether/view/home"}
                        ]
                    },
                },
            },
            strings={"en": {"title": "{prefix} overview"}},
            routes={
                "options": lambda field, _ctx: (
                    [{"value": "Ether", "label": "Ether"}] if field == "prefix" else []
                ),
                "catalog": lambda req, ctx: {
                    "metas": [
                        fixtures.catalog_item(
                            id=f"{req['id']}:1",
                            type=req["type"],
                            name=f"{ctx.settings['prefix']} {req.get('search') or 'Featured'}",
                        )
                    ]
                },
                "meta": lambda req, _ctx: {
                    "meta": fixtures.movie(id=req["id"], type=req["type"])
                },
                "stream": lambda req, _ctx: {
                    "streams": [
                        {
                            "name": "Direct 1080p",
                            "url": f"https://streams.example.invalid/{req['id']}.mp4",
                        }
                    ]
                },
                "subtitles": lambda req, _ctx: {
                    "subtitles": [
                        {
                            "id": f"{req['id']}:en",
                            "url": f"https://subs.example.invalid/{req['id']}.vtt?pick={req['pickToken']}",
                            "lang": "en",
                        }
                    ]
                },
                "markers": lambda _req, _ctx: {
                    "markers": [{"kind": "intro", "start": 10, "end": 65, "label": "Skip intro"}]
                },
                "event": lambda events, ctx: (
                    [ctx.ether.kv.put(f"evt:{ev['id']}", ev["name"]) for ev in events],
                    {"ack": [ev["id"] for ev in events]},
                )[1],
                "view": lambda _req, ctx: {
                    "spec": {
                        "specVersion": 1,
                        "layout": "list",
                        "header": [{"kind": "page-header", "title": {"$": "title"}}],
                        "body": [{"kind": "text", "content": "Ready."}],
                    },
                    "data": {"title": ctx.t("title", {"prefix": ctx.settings["prefix"]})},
                },
                "action": lambda req, ctx: (
                    ctx.ether.notify.owner(f"Action {req['action']}: {req['input'].get('target', '')}"),
                    {"toast": "Done.", "refresh": True},
                )[1],
                "tool": lambda req, _ctx: {"executed": req["name"], "echo": req["input"]},
                "rank_key": lambda cand, _cfg: 100 if cand.get("cached") else 10,
                "cond_match": lambda cand, _op, val: bool(cand.get("verified")) is bool(val),
                "file": lambda req, _ctx: (
                    {
                        "status": 206,
                        "contentType": "text/plain",
                        "contentRange": f"bytes 0-3/{len('file:' + req['path'])}",
                        "totalSize": len("file:" + req["path"]),
                        "body": ("file:" + req["path"]).encode("utf-8")[:4],
                    }
                    if req.get("range") == "bytes=0-3"
                    else {
                        "status": 200,
                        "contentType": "text/plain",
                        "totalSize": len("file:" + req["path"]),
                        "body": ("file:" + req["path"]).encode("utf-8"),
                    }
                ),
            },
            host=host,
        )
        harness = create_test_harness(inst, host)

        self.assertEqual(harness.manifest()["id"], "org.ether.test.all")
        self.assertEqual(harness.health()["status"], 200)
        self.assertEqual(harness.health()["body"], {"status": "ok"})

        cfg_res = harness.configure({"prefix": "Custom"})
        self.assertEqual(cfg_res["status"], 200)
        self.assertEqual(cfg_res["body"]["errors"], {})

        opts_res = harness.options("prefix")
        self.assertEqual(opts_res["status"], 200)
        self.assertEqual(len(opts_res["body"]), 1)

        cat_res = harness.catalog("movie", "top", {"search": "Matrix"})
        self.assertEqual(cat_res["status"], 200)
        self.assertEqual(cat_res["body"]["metas"][0]["name"], "Custom Matrix")

        meta_res = harness.meta("movie", "tt0133093")
        self.assertEqual(meta_res["status"], 200)
        self.assertEqual(meta_res["body"]["meta"]["id"], "tt0133093")

        stream_res = harness.stream("movie", "tt0133093")
        self.assertEqual(stream_res["status"], 200)
        self.assertEqual(len(stream_res["body"]["streams"]), 1)

        # Subtitles without X-Ether-Pick must fail with 403 pick_required
        sub_denied = harness.subtitles("movie", "tt0133093")
        self.assertEqual(sub_denied["status"], 403)
        self.assertEqual(sub_denied["body"]["error"]["code"], "pick_required")

        # Subtitles with X-Ether-Pick succeeds
        pick = fixtures.pick_token("tt0133093")
        sub_ok = harness.subtitles("movie", "tt0133093", pick)
        self.assertEqual(sub_ok["status"], 200)
        self.assertEqual(len(sub_ok["body"]["subtitles"]), 1)

        markers_res = harness.markers("movie", "tt0133093")
        self.assertEqual(markers_res["status"], 200)
        self.assertEqual(markers_res["body"]["markers"][0]["kind"], "intro")

        ev = fixtures.playback_event()
        ev_res = harness.event([ev])
        self.assertEqual(ev_res["status"], 200)
        self.assertEqual(ev_res["body"]["ack"], [ev["id"]])
        self.assertEqual(host.kv.get(f"evt:{ev['id']}"), "playback.completed")

        view_res = harness.view("home")
        self.assertEqual(view_res["status"], 200)
        self.assertEqual(view_res["body"]["data"]["title"], "Custom overview")

        act_res = harness.action("sync", {"target": "library"})
        self.assertEqual(act_res["status"], 200)
        self.assertEqual(act_res["body"]["toast"], "Done.")
        self.assertEqual(host.notifications, ["Action sync: library"])

        tool_res = harness.tool("lookup", {"q": "Arrival"})
        self.assertEqual(tool_res["status"], 200)
        self.assertEqual(tool_res["body"]["executed"], "lookup")

        cand = fixtures.candidate(cached=True, verified=True)
        self.assertEqual(harness.rank_key(cand), 100)
        self.assertTrue(harness.cond_match(cand, "eq", True))
        self.assertFalse(harness.cond_match(cand, "eq", False))

        file_full = harness.file("clips/intro.txt")
        self.assertEqual(file_full["status"], 200)
        self.assertEqual(file_full["body"], b"file:clips/intro.txt")

        file_range = harness.file("clips/intro.txt", "bytes=0-3")
        self.assertEqual(file_range["status"], 206)
        self.assertEqual(file_range["body"], b"file")

        bad_file = harness.request({"method": "GET", "path": "/ether/file/../secrets.txt"})
        self.assertEqual(bad_file["status"], 400)

    def test_view_spec_rejects_classname(self) -> None:
        with self.assertRaises(AssertionError):
            assert_valid_view_spec(
                {
                    "specVersion": 1,
                    "layout": "list",
                    "header": [],
                    "body": [
                        {
                            "kind": "panel",
                            "title": "Bad",
                            "blocks": [],
                            "containerClassName": "bg-red-500",
                        }
                    ],
                }
            )

    def test_serve_http_and_tunnel_frame_dispatch(self) -> None:
        inst = create_catalog_template("org.ether.py.sidecar", "Python sidecar")
        inst.routes["file"] = lambda _req, _ctx: {
            "status": 206,
            "contentType": "video/mp4",
            "contentRange": "bytes 0-7/16",
            "body": bytes([1, 2, 3, 4, 5, 6, 7, 8]),
        }

        handle = serve(inst, mode="http", port=0, background=True)
        try:
            self.assertEqual(handle.mode, "http")
            assert handle.url is not None
            with urllib.request.urlopen(f"{handle.url}/manifest.json") as resp:
                self.assertEqual(resp.status, 200)
                data = json.loads(resp.read().decode("utf-8"))
                self.assertEqual(data["id"], "org.ether.py.sidecar")

            with urllib.request.urlopen(
                f"{handle.url}/catalog/movie/featured/search=Arrival.json"
            ) as resp:
                self.assertEqual(resp.status, 200)
                cat = json.loads(resp.read().decode("utf-8"))
                self.assertEqual(cat["metas"][0]["name"], "Result for Arrival")
        finally:
            handle.close()

        json_frames = dispatch_over_tunnel_frame(
            inst,
            {"type": "request", "id": "req-1", "method": "GET", "path": "/ether/health"},
        )
        self.assertEqual(len(json_frames), 1)
        self.assertEqual(json_frames[0]["type"], "response")
        self.assertEqual(json_frames[0]["status"], 200)

        file_frames = dispatch_over_tunnel_frame(
            inst,
            {
                "type": "request",
                "id": "req-file",
                "method": "GET",
                "path": "/ether/file/sample.mp4",
                "headers": {"Range": ["bytes=0-7"]},
            },
            1024,
        )
        self.assertEqual(len(file_frames), 3)
        self.assertEqual(file_frames[0]["type"], "response_start")
        self.assertEqual(file_frames[0]["status"], 206)
        self.assertEqual(file_frames[1]["type"], "response_chunk")
        self.assertEqual(file_frames[2]["type"], "response_end")

    def test_remote_host_client(self) -> None:
        received: list[dict[str, str]] = []

        class _HostHandler(http.server.BaseHTTPRequestHandler):
            def _handle(self) -> None:
                length = int(self.headers.get("Content-Length", "0") or "0")
                raw = self.rfile.read(length).decode("utf-8") if length > 0 else ""
                received.append(
                    {
                        "method": self.command,
                        "path": self.path,
                        "cred": self.headers.get("X-Ether-Plugin-Credential", ""),
                        "body": raw,
                    }
                )
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                if self.path == "/ether/plugin-host/kv/greeting" and self.command == "GET":
                    self.wfile.write(json.dumps({"key": "greeting", "value": "hello"}).encode("utf-8"))
                elif self.path.startswith("/ether/plugin-host/kv") and self.command == "GET":
                    self.wfile.write(json.dumps({"keys": ["greeting"]}).encode("utf-8"))
                else:
                    self.wfile.write(json.dumps({"ok": True}).encode("utf-8"))

            def do_GET(self) -> None:  # noqa: N802
                self._handle()

            def do_PUT(self) -> None:  # noqa: N802
                self._handle()

            def do_POST(self) -> None:  # noqa: N802
                self._handle()

            def log_message(self, format: str, *args: object) -> None:  # noqa: A002
                return

        server = socketserver.TCPServer(("127.0.0.1", 0), _HostHandler)
        port = server.server_address[1]
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            client = create_remote_host_client(
                base_url=f"http://127.0.0.1:{port}",
                credential="cred-py-test",
            )
            self.assertEqual(client.kv.get("greeting"), "hello")
            client.kv.put("greeting", "updated")
            self.assertEqual(client.kv.list(""), ["greeting"])
            client.collections.upsert({"name": "Top picks", "itemIds": ["tt0133093"]})
            client.notify.owner("Sync complete.")
            self.assertTrue(all(r["cred"] == "cred-py-test" for r in received))
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=2.0)

    def test_starter_templates(self) -> None:
        cat_h = create_test_harness(
            create_catalog_template("org.ether.tpl.cat", "Catalog template")
        )
        self.assertEqual(cat_h.catalog("movie", "featured")["status"], 200)

        sub_h = create_test_harness(
            create_subtitles_template("org.ether.tpl.sub", "Subtitles template")
        )
        self.assertEqual(
            sub_h.subtitles("movie", "tt0133093", fixtures.pick_token())["status"], 200
        )

        scrobble_h = create_test_harness(
            create_scrobbler_template("org.ether.tpl.scrobble", "Scrobbler template")
        )
        self.assertEqual(scrobble_h.event([fixtures.playback_event()])["status"], 200)

        view_h = create_test_harness(
            create_view_template("org.ether.tpl.view", "View template")
        )
        self.assertEqual(view_h.view("home")["status"], 200)
        self.assertEqual(view_h.action("refresh")["status"], 200)

        rank_h = create_test_harness(
            create_ranker_template("org.ether.tpl.rank", "Ranker template")
        )
        self.assertEqual(
            rank_h.rank_key(fixtures.candidate(cached=True, resolution=2160)), 1500
        )

        cond_h = create_test_harness(
            create_condition_template("org.ether.tpl.cond", "Condition template")
        )
        self.assertTrue(cond_h.cond_match(fixtures.candidate(cached=True), "eq", True))
        self.assertFalse(cond_h.cond_match(fixtures.candidate(cached=False), "eq", True))


if __name__ == "__main__":
    unittest.main()
