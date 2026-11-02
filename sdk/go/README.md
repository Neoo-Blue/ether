# `etherplugin` (Go SDK)

Go authoring package (`github.com/braedonsaunders/ether/sdk/go`) for Ether Plugin Protocol v1 (EPP/1).

## Execution modes

1. **Native HTTP or WebSocket tunnel sidecar (`ether.runtime.kind: "remote"`)**:
   Compile a standard Go binary and call `p.Serve(ctx, etherplugin.ServeOptions{})` (or mount `p` as an `http.Handler`). Host calls go over HTTP to `/ether/plugin-host/*` authenticated with `X-Ether-Plugin-Credential`.
2. **Hosted WebAssembly guest (`ether.runtime.kind: "hosted"`)**:
   Register the plugin with `etherplugin.RegisterWASM(p)` and compile a reactor module exporting `alloc`, `handle`, `rank_key`, and `cond_match` against the `env.host_*` imports.
   - Standard Go's `GOOS=wasip1 GOARCH=wasm` target emits a command module (`_start`) that exits the runtime when `main` returns, clearing exported memory in wazero before subsequent calls.
   - For hosted WASM modules, compile with TinyGo (`tinygo build -target=wasip1 -buildmode=c-shared -o plugin.wasm .`) or run standard Go plugins as native HTTP/tunnel sidecars.

## Usage

```go
package main

import (
	"context"
	"os/signal"
	"syscall"

	etherplugin "github.com/braedonsaunders/ether/sdk/go"
)

func main() {
	p := etherplugin.New(etherplugin.Definition{
		Manifest: etherplugin.Manifest{
			ID:      "org.ether.examples.hello",
			Name:    "Hello plugin",
			Version: "0.1.0",
			Ether: &etherplugin.EtherManifestBlock{
				Protocol:     1,
				Kind:         etherplugin.KindExtension,
				Capabilities: []etherplugin.Capability{etherplugin.CapListSource, etherplugin.CapUI},
				Summary:      "Minimal greeting plugin for Ether.",
				Runtime:      etherplugin.RuntimeDecl{Kind: "remote"},
			},
		},
		Routes: etherplugin.Routes{
			Catalog: func(ctx *etherplugin.Context, req etherplugin.CatalogRequest) (etherplugin.CatalogResponse, error) {
				return etherplugin.CatalogResponse{
					Metas: []etherplugin.CatalogItem{{
						ID:          "tt0133093",
						Type:        req.Type,
						Name:        "Hello from " + req.ID,
						ReleaseInfo: "1999",
					}},
				}, nil
			},
		},
	})

	etherplugin.RegisterWASM(p)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	_ = p.Serve(ctx, etherplugin.ServeOptions{})
}
```
