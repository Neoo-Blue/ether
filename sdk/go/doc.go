// Package etherplugin is the Go authoring SDK for Ether Plugin Protocol v1 (EPP/1).
//
// It provides the same declarative router shape, settings decoder, localization
// helper, host service client, test harness, and capability templates as
// `@ether/plugin-sdk` (TypeScript) and `ether_plugin` (Python).
//
// # Execution modes
//
// Go plugins run as native HTTP or WebSocket tunnel sidecars. Hosted Go
// is not supported: the standard Go wasip1 toolchain emits WASI imports,
// which Ether's sandbox rejects. See README.md for details.
//
// Server packages under `internal/` and `cmd/` must not import this package.
package etherplugin
