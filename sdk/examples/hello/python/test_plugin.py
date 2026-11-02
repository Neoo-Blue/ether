import json
import os
import sys
import unittest
import urllib.request

_HERE = os.path.dirname(os.path.abspath(__file__))
_SDK_DIR = os.path.abspath(os.path.join(_HERE, "..", "..", "..", "python"))
for _p in (_HERE, _SDK_DIR):
    if _p not in sys.path:
        sys.path.insert(0, _p)

from ether_plugin import create_test_harness, fixtures, serve  # noqa: E402
from plugin import hello_plugin  # noqa: E402


class HelloPythonPluginTest(unittest.TestCase):
    def test_hello_plugin_routes_and_sidecar(self) -> None:
        harness = create_test_harness(hello_plugin)

        health = harness.health()
        self.assertEqual(health["status"], 200)
        self.assertEqual(health["body"], {"status": "ok"})

        cat = harness.catalog("movie", "hello")
        self.assertEqual(cat["status"], 200)
        self.assertEqual(cat["body"]["metas"][0]["name"], "Hello from Ether")

        meta = harness.meta("movie", "tt0133093")
        self.assertEqual(meta["status"], 200)
        self.assertEqual(meta["body"]["meta"]["name"], "The Matrix")

        sub_denied = harness.subtitles("movie", "tt0133093")
        self.assertEqual(sub_denied["status"], 403)

        sub_ok = harness.subtitles("movie", "tt0133093", fixtures.pick_token("tt0133093"))
        self.assertEqual(sub_ok["status"], 200)
        self.assertEqual(sub_ok["body"]["subtitles"][0]["lang"], "en")

        view = harness.view("home")
        self.assertEqual(view["status"], 200)
        self.assertEqual(view["body"]["data"]["greetingText"], "Hello from Ether")

        handle = serve(hello_plugin, mode="http", port=0, background=True)
        try:
            assert handle.url is not None
            with urllib.request.urlopen(f"{handle.url}/catalog/movie/hello.json") as resp:
                self.assertEqual(resp.status, 200)
                body = json.loads(resp.read().decode("utf-8"))
                self.assertEqual(body, cat["body"])
        finally:
            handle.close()


if __name__ == "__main__":
    unittest.main()
