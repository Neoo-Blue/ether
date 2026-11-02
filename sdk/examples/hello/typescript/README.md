# Hello plugin (TypeScript)

Minimal reference plugin built with `@ether/plugin-sdk`. Runs both hosted inside Ether's QuickJS + wazero runtime (`ether.runtime.kind: "hosted"`) and as a Node/Bun HTTP or tunnel sidecar.

## Test

```bash
node --test sdk/examples/hello/typescript/plugin.test.ts
```

## Run a sidecar

With Node 23.6 or newer (or Bun), from the repository root:

```bash
ETHER_PLUGIN_PORT=8080 node sdk/examples/hello/typescript/sidecar.ts
```

Set `ETHER_TUNNEL_URL` and `ETHER_PLUGIN_CREDENTIAL` for a tunnel instead.
The hosted entry point is `plugin.ts`.
