//go:build !wasm

package etherplugin

var registeredWASMPlugin *Plugin

// RegisterWASM registers a Plugin as the active WebAssembly guest handler.
// When compiled for a non-WASM target, it records the plugin for inspection
// while [Plugin.Serve] or [Plugin.ServeHTTP] handles HTTP/tunnel requests.
func RegisterWASM(p *Plugin) {
	registeredWASMPlugin = p
}

// RegisteredWASMPlugin returns the plugin registered with RegisterWASM, if any.
func RegisteredWASMPlugin() *Plugin {
	return registeredWASMPlugin
}

func defaultHostForRuntime(opts RemoteHostOptions) Host {
	return NewRemoteHostClient(opts)
}
