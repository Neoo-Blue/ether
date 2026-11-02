// Package etherplugin is the Go authoring SDK for Ether Plugin Protocol v1 (EPP/1).
//
// It provides the same declarative router shape, settings decoder, localization
// helper, host service client, test harness, and capability templates as
// `@ether/plugin-sdk` (TypeScript) and `ether_plugin` (Python).
//
// # Execution modes
//
// Go plugins can run in two modes:
//
//  1. Native HTTP or WebSocket tunnel sidecar (`ether.runtime.kind: "remote"`):
//     Build a standard Go binary and call [Plugin.Serve] (or mount [Plugin] as an
//     [net/http.Handler]). In sidecar mode, host calls go over HTTP to
//     `/ether/plugin-host/*` authenticated with `X-Ether-Plugin-Credential`.
//  2. Hosted WebAssembly guest (`ether.runtime.kind: "hosted"`):
//     Register the plugin with [RegisterWASM] and compile as a WebAssembly
//     reactor module (`alloc`, `handle`, `rank_key`, `cond_match` exports, with
//     `env.host_*` imports). Note that standard Go's `GOOS=wasip1 GOARCH=wasm`
//     target emits a command module whose `_start` entry point exits the runtime
//     when `main` returns, clearing exported memory in wazero before subsequent
//     calls. For hosted WASM modules, compile with TinyGo (`tinygo build -target=wasip1 -buildmode=c-shared`)
//     or run standard Go plugins as HTTP/tunnel sidecars.
//
// Server packages under `internal/` and `cmd/` must not import this package.
package etherplugin
