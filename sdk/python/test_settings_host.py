import unittest
from ether_plugin import plugin, create_mock_host_client

class SettingsHostTest(unittest.TestCase):
    def test_current_settings_each_request(self):
        host = create_mock_host_client()
        value = {"word": "one"}
        host.settings_get = lambda: value.copy()
        manifest = {"id": "test.settings", "name": "Settings", "version": "1", "ether": {"settings": {"fields": [{"key": "word", "kind": "text"}]}}}
        instance = plugin(manifest, {"test": lambda ctx: {"ok": True, "message": ctx.settings["word"]}}, host=host)
        self.assertEqual(instance.handle({"method": "POST", "path": "/ether/test"})["body"]["message"], "one")
        value["word"] = "two"
        self.assertEqual(instance.handle({"method": "POST", "path": "/ether/test"})["body"]["message"], "two")
