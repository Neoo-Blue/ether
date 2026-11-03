package main

import (
	"fmt"
	"github.com/evanw/esbuild/pkg/api"
	"os"
)

func main() {
	r := api.Build(api.BuildOptions{EntryPoints: []string{"sdk/examples/subdl/plugin.ts"}, Bundle: true, Format: api.FormatIIFE, GlobalName: "EtherPlugin", Platform: api.PlatformNeutral, Target: api.ES2020, Outfile: "sdk/examples/subdl/dist/plugin.js", Write: true, LogLevel: api.LogLevelInfo})
	if len(r.Errors) > 0 {
		fmt.Fprintln(os.Stderr, "bundle failed")
		os.Exit(1)
	}
}
