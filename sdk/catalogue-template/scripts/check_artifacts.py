#!/usr/bin/env python3
"""Check every signed-index artifact with Ether's conformance kit."""
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import subprocess
import sys
import tempfile
import urllib.request
import zipfile

MAX_PACKAGE = 64 * 1024 * 1024
MAX_UNPACKED = 128 * 1024 * 1024


def check(index_path):
    with open(index_path, encoding="utf-8") as source:
        index = json.load(source)
    for entry in index["entries"]:
        if entry.get("revoked"):
            continue
        for version in entry["versions"]:
            label = f'{entry["id"]} {version["version"]}'
            request = urllib.request.Request(version["url"], headers={"User-Agent": "Ether catalogue CI"})
            with urllib.request.urlopen(request, timeout=60) as response:
                data = response.read(MAX_PACKAGE + 1)
            if len(data) > MAX_PACKAGE:
                raise ValueError(f"{label}: package exceeds size limit")
            if hashlib.sha256(data).hexdigest() != version["sha256"].lower():
                raise ValueError(f"{label}: package digest mismatch")
            with tempfile.TemporaryDirectory(prefix="ether-catalogue-") as directory:
                root = Path(directory)
                archive_path = root / "package.etherpkg"
                archive_path.write_bytes(data)
                plugin_dir = root / "plugin"
                plugin_dir.mkdir()
                with zipfile.ZipFile(archive_path) as archive:
                    total = 0
                    names = set()
                    for item in archive.infolist():
                        path = PurePosixPath(item.filename)
                        if (path.is_absolute() or ".." in path.parts or "\\" in item.filename
                                or item.filename in names or (item.external_attr >> 16) & 0o170000 == 0o120000):
                            raise ValueError(f"{label}: unsafe package path")
                        names.add(item.filename)
                        total += item.file_size
                        if total > MAX_UNPACKED:
                            raise ValueError(f"{label}: unpacked package exceeds size limit")
                    archive.extractall(plugin_dir)
                manifest = json.loads((plugin_dir / "manifest.json").read_text(encoding="utf-8"))
                if manifest.get("id") != entry["id"] or manifest.get("version") != version["version"]:
                    raise ValueError(f"{label}: package identity does not match index")
                subprocess.run([os.environ.get("ETHER_BIN", "ether"), "plugin", "check", str(plugin_dir)], check=True)
                print(f"Checked {label}", flush=True)


if __name__ == "__main__":
    try:
        check(sys.argv[1] if len(sys.argv) > 1 else "index.json")
    except (OSError, ValueError, KeyError, zipfile.BadZipFile, subprocess.CalledProcessError) as exc:
        print(f"Catalogue check failed: {exc}", file=sys.stderr)
        sys.exit(1)
