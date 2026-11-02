# `etherplugin` (Go SDK)

Go authoring package (`github.com/braedonsaunders/ether/sdk/go`) for Ether Plugin Protocol v1 (EPP/1).

## Execution modes

1. **Native HTTP or WebSocket tunnel sidecar (`ether.runtime.kind: "remote"`)**:
   Compile a standard Go binary and call `p.Serve(ctx, etherplugin.ServeOptions{})` (or mount `p` as an `http.Handler`). Host calls go over HTTP to `/ether/plugin-host/*` authenticated with `X-Ether-Plugin-Credential`.
2. **Hosted WebAssembly guest**: not yet supported. The experimental `RegisterWASM` implementation uses imports that Ether's hosted runtime rejects. Standard Go `wasip1` also emits WASI imports, which the runtime forbids. Use a sidecar until a guest is built and tested against Ether's raw WASM ABI.

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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	_ = p.Serve(ctx, etherplugin.ServeOptions{})
}
```
