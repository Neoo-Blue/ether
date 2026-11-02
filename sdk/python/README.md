# `ether_plugin` (Python SDK)

Single-file, standard-library-only Python 3.9+ SDK for Ether Plugin Protocol v1 (EPP/1).

## Features

- Zero external pip dependencies (`http.server`, `urllib.request`, `json`, `socket`, `base64`).
- Same declarative `plugin(manifest=..., routes=...)` router shape as `@ether/plugin-sdk` and `sdk/go`.
- Runs as an HTTP sidecar (`serve(p, mode="http")`) or an outbound WebSocket tunnel sidecar (`serve(p, mode="tunnel")`).
- Includes `RemoteHostClient` for `/ether/plugin-host/*` authenticated with `X-Ether-Plugin-Credential`, plus `create_test_harness`, `create_mock_host_client`, `fixtures`, and starter capability templates.

## Usage

```python
from ether_plugin import plugin, serve

hello = plugin(
    manifest={
        "id": "org.ether.examples.hello",
        "name": "Hello plugin",
        "version": "0.1.0",
        "description": "Minimal greeting plugin for Ether.",
        "types": ["movie"],
        "idPrefixes": ["tt"],
        "catalogs": [{"type": "movie", "id": "hello", "name": "Hello movies"}],
        "resources": ["catalog", "meta"],
        "ether": {
            "protocol": 1,
            "kind": "extension",
            "capabilities": ["list_source", "ui"],
            "summary": "Minimal greeting plugin for Ether.",
            "runtime": {"kind": "remote"},
        },
    },
    routes={
        "catalog": lambda req, ctx: {
            "metas": [
                {
                    "id": "tt0133093",
                    "type": req["type"],
                    "name": f"Hello from {req['id']}",
                    "releaseInfo": "1999",
                }
            ]
        },
    },
)

if __name__ == "__main__":
    serve(hello, background=False)
```

Tunnel mode makes one connection and returns when it drops. Use a process
supervisor to restart the sidecar after a disconnection.
