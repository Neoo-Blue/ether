# Hello plugin (Python)

Minimal reference plugin built with `ether_plugin` (Python 3.9+ stdlib). Runs as an HTTP or WebSocket tunnel sidecar.

## Test

```bash
python3 -m unittest discover -s sdk/examples/hello/python -p "test_*.py"
```

## Run a sidecar

```bash
ETHER_PLUGIN_PORT=8080 python3 sdk/examples/hello/python/plugin.py
```

Set `ETHER_TUNNEL_URL` and `ETHER_PLUGIN_CREDENTIAL` for a tunnel instead.
