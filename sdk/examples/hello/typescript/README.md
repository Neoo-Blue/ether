# Hello plugin (TypeScript)

Minimal reference plugin built with `@ether/plugin-sdk`. Runs both hosted inside Ether's QuickJS + wazero runtime (`ether.runtime.kind: "hosted"`) and as a Node/Bun HTTP or tunnel sidecar.

## Test

```bash
node --test sdk/examples/hello/typescript/plugin.test.ts
```
