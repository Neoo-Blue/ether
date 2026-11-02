package wasm

import (
	"fmt"
	"path"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// Diagnostic is one esbuild error or warning, flattened for Studio and
// install reporting.
type Diagnostic struct {
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`   // 1-based
	Column   int    `json:"column,omitempty"` // 1-based
	Text     string `json:"text"`
	Severity string `json:"severity"` // "error" or "warning"
}

// Compile bundles TypeScript (or JavaScript) plugin sources into one IIFE
// JavaScript file plus an external source map, using esbuild's Go API.
// Files maps slash paths ("src/index.ts") to contents; entry is one of
// those paths. Relative imports resolve inside the map only: a plugin
// cannot reach the filesystem or node_modules. Exports land on the
// __plugin global, which the JS runtime normalizes onto globalThis, so
// `export function handle(...)` and `globalThis.handle = ...` both work.
// On failure js is nil and diagnostics carries every error.
func Compile(files map[string]string, entry string) (js []byte, sourcemap []byte, diagnostics []Diagnostic) {
	if _, ok := files[entry]; !ok {
		return nil, nil, []Diagnostic{{Text: fmt.Sprintf("entry file %q is not in the source set", entry), Severity: "error"}}
	}
	vfs := api.Plugin{
		Name: "ether-vfs",
		Setup: func(build api.PluginBuild) {
			build.OnResolve(api.OnResolveOptions{Filter: `.*`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				p := args.Path
				if args.Kind != api.ResolveEntryPoint {
					base := path.Dir(args.Importer)
					p = path.Clean(path.Join(base, p))
				} else {
					p = path.Clean(p)
				}
				for _, cand := range []string{p, p + ".ts", p + ".tsx", p + ".js", p + ".json", p + "/index.ts", p + "/index.js"} {
					if _, ok := files[cand]; ok {
						return api.OnResolveResult{Path: cand, Namespace: "ether-vfs"}, nil
					}
				}
				return api.OnResolveResult{}, fmt.Errorf("cannot resolve %q inside the plugin sources", args.Path)
			})
			build.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "ether-vfs"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				contents, ok := files[args.Path]
				if !ok {
					return api.OnLoadResult{}, fmt.Errorf("missing source %q", args.Path)
				}
				loader := api.LoaderJS
				switch {
				case strings.HasSuffix(args.Path, ".ts"):
					loader = api.LoaderTS
				case strings.HasSuffix(args.Path, ".tsx"):
					loader = api.LoaderTSX
				case strings.HasSuffix(args.Path, ".json"):
					loader = api.LoaderJSON
				}
				return api.OnLoadResult{Contents: &contents, Loader: loader}, nil
			})
		},
	}
	result := api.Build(api.BuildOptions{
		EntryPoints: []string{entry},
		Bundle:      true,
		Write:       false,
		Format:      api.FormatIIFE,
		GlobalName:  "__plugin",
		Platform:    api.PlatformNeutral,
		Target:      api.ES2020,
		Sourcemap:   api.SourceMapExternal,
		Outfile:     "plugin.js",
		Plugins:     []api.Plugin{vfs},
		LogLevel:    api.LogLevelSilent,
	})
	diagnostics = append(diagnostics, convertMessages(result.Errors, "error")...)
	diagnostics = append(diagnostics, convertMessages(result.Warnings, "warning")...)
	if len(result.Errors) > 0 {
		return nil, nil, diagnostics
	}
	for _, f := range result.OutputFiles {
		if strings.HasSuffix(f.Path, ".map") {
			sourcemap = f.Contents
		} else {
			js = f.Contents
		}
	}
	return js, sourcemap, diagnostics
}

func convertMessages(msgs []api.Message, severity string) []Diagnostic {
	out := make([]Diagnostic, 0, len(msgs))
	for _, m := range msgs {
		d := Diagnostic{Text: m.Text, Severity: severity}
		if m.Location != nil {
			d.File = m.Location.File
			d.Line = m.Location.Line
			d.Column = m.Location.Column + 1
		}
		out = append(out, d)
	}
	return out
}
