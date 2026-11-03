import hashlib
import http.server
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import unittest
import zipfile

SCRIPT = Path(__file__).with_name("check_artifacts.py")


class CatalogueCheckTest(unittest.TestCase):
    def test_package_check_and_rejection(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            package = io.BytesIO()
            with zipfile.ZipFile(package, "w") as archive:
                archive.writestr("manifest.json", json.dumps({"id": "example.plugin", "version": "1.0.0"}))
            data = package.getvalue()
            (root / "plugin.etherpkg").write_bytes(data)
            class Handler(http.server.SimpleHTTPRequestHandler):
                def __init__(self, *args, **kwargs):
                    super().__init__(*args, directory=tmp, **kwargs)
                def log_message(self, *args):
                    pass
            server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
            thread = threading.Thread(target=server.serve_forever)
            thread.start()
            self.addCleanup(thread.join)
            self.addCleanup(server.server_close)
            self.addCleanup(server.shutdown)
            marker = root / "checked"
            stub = root / "ether"
            stub.write_text('#!/bin/sh\n[ "$1" = plugin ] && [ "$2" = check ] && [ -f "$3/manifest.json" ] || exit 1\nprintf checked >> "$MARKER"\n')
            stub.chmod(0o755)
            env = dict(os.environ, ETHER_BIN=str(stub), MARKER=str(marker))
            index = {"entries": [{"id": "example.plugin", "versions": [{"version": "1.0.0", "url": f"http://127.0.0.1:{server.server_port}/plugin.etherpkg", "sha256": hashlib.sha256(data).hexdigest()}]}]}
            index_file = root / "index.json"
            sign_bin = os.environ.get("SIGN_BIN")
            if sign_bin:
                entries = root / "entries"
                entries.mkdir()
                (entries / "example.yaml").write_text(
                    "id: example.plugin\nname: Example\nsummary: Example plugin.\nkind: extension\n"
                    "versions:\n  - version: 1.0.0\n    url: "
                    + index["entries"][0]["versions"][0]["url"] + "\n    sha256: "
                    + index["entries"][0]["versions"][0]["sha256"] + "\n")
                compiled = subprocess.run([sign_bin, "index", "-check", "-dir", str(entries), "-out", str(index_file)], env=env, capture_output=True, text=True)
                self.assertEqual(compiled.returncode, 0, compiled.stderr)
            else:
                index_file.write_text(json.dumps(index))
            result = subprocess.run([sys.executable, str(SCRIPT), str(index_file)], env=env, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(marker.read_text(), "checked")
            index["entries"][0]["versions"][0]["sha256"] = "0" * 64
            index_file.write_text(json.dumps(index))
            result = subprocess.run([sys.executable, str(SCRIPT), str(index_file)], env=env, capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("digest mismatch", result.stderr)
            self.assertEqual(marker.read_text(), "checked")


if __name__ == "__main__":
    unittest.main()
