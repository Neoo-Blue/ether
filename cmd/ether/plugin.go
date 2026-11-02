package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/braedonsaunders/ether/internal/plugins/kit"
)

const pluginUsage = `usage:
  ether plugin new --kind <kind> [--lang ts|js|python] <dir>
      Scaffold a new plugin project.
      Kinds: livetv_source, source, integration, notification_channel, extension

  ether plugin check [--json] <dir|pkg>
      Run the offline conformance kit and copy linter against a plugin directory
      or .etherpkg archive. Exits non-zero on any conformance error.

  ether plugin test <dir>
      Run recorded fixtures, conformance checks, and (when $ETHER_BIN is set)
      an integration check against a headless Ether instance.

  ether plugin pack [-o <out.etherpkg>] <dir>
      Compile TypeScript, verify conformance, and bundle a verified .etherpkg.

  ether plugin dev <dir> --server <url> [--token <tok>] [--once]
      Watch files in <dir>, hot-reload into a running Ether server via the
      Studio API, and stream plugin logs.

  ether plugin publish [<dir|pkg>]
      Validate the plugin and print instructions for opening a pull request
      against the Ether community catalogue repository.
`

func runPlugin(args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return runPluginWithContext(ctx, args, stdout, stderr)
}

func runPluginWithContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(stderr, pluginUsage)
		return 2
	}
	sub := args[0]
	rest := args[1:]

	switch sub {
	case "new":
		return runPluginNew(rest, stdout, stderr)
	case "check":
		return runPluginCheck(ctx, rest, stdout, stderr)
	case "test":
		return runPluginTest(ctx, rest, stdout, stderr)
	case "pack":
		return runPluginPack(ctx, rest, stdout, stderr)
	case "dev":
		return runPluginDev(ctx, rest, stdout, stderr)
	case "publish":
		return runPluginPublish(ctx, rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "ether plugin: unknown subcommand %q\n\n%s", sub, pluginUsage)
		return 2
	}
}

func runPluginNew(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ether plugin new", flag.ContinueOnError)
	fs.SetOutput(stderr)
	kind := fs.String("kind", "", "plugin kind: livetv_source, source, integration, notification_channel, extension")
	lang := fs.String("lang", "ts", "language: ts, js, python")
	id := fs.String("id", "", "optional plugin ID")
	name := fs.String("name", "", "optional display name")

	pos := parseInterleaved(fs, args)
	if len(pos) != 1 || *kind == "" {
		fmt.Fprint(stderr, pluginUsage)
		return 2
	}
	dir := pos[0]
	if err := kit.Scaffold(kit.TemplateOptions{
		Dir:  dir,
		Kind: *kind,
		Lang: *lang,
		ID:   *id,
		Name: *name,
	}); err != nil {
		fmt.Fprintln(stderr, "ether plugin new:", err)
		return 1
	}
	fmt.Fprintf(stdout, "Created %s (%s) plugin in %s\n", *kind, *lang, dir)
	return 0
}

func runPluginCheck(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ether plugin check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "output structured JSON report")

	pos := parseInterleaved(fs, args)
	if len(pos) != 1 {
		fmt.Fprint(stderr, pluginUsage)
		return 2
	}
	pkg, err := kit.Load(pos[0])
	if err != nil && pkg == nil {
		fmt.Fprintln(stderr, "ether plugin check:", err)
		return 1
	}
	rep := kit.Check(ctx, pkg)
	if *asJSON {
		b, err := rep.JSON()
		if err != nil {
			fmt.Fprintln(stderr, "ether plugin check:", err)
			return 1
		}
		fmt.Fprintln(stdout, string(b))
	} else {
		fmt.Fprint(stdout, rep.Text())
	}
	if !rep.OK {
		return 1
	}
	return 0
}

func runPluginTest(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ether plugin test", flag.ContinueOnError)
	fs.SetOutput(stderr)
	pos := parseInterleaved(fs, args)
	if len(pos) != 1 {
		fmt.Fprint(stderr, pluginUsage)
		return 2
	}
	pkg, err := kit.Load(pos[0])
	if err != nil && pkg == nil {
		fmt.Fprintln(stderr, "ether plugin test:", err)
		return 1
	}
	rep := kit.Check(ctx, pkg)
	fmt.Fprint(stdout, rep.Text())
	if !rep.OK {
		return 1
	}
	tr, err := kit.RunTests(ctx, pkg, os.Getenv("ETHER_BIN"))
	if err != nil {
		fmt.Fprintln(stderr, "ether plugin test:", err)
		return 1
	}
	fmt.Fprint(stdout, tr.Text())
	if !tr.OK {
		return 1
	}
	return 0
}

func runPluginPack(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ether plugin pack", flag.ContinueOnError)
	fs.SetOutput(stderr)
	outPath := fs.String("o", "", "output .etherpkg path")

	pos := parseInterleaved(fs, args)
	if len(pos) != 1 {
		fmt.Fprint(stderr, pluginUsage)
		return 2
	}
	dir := pos[0]
	pkg, err := kit.Load(dir)
	if err != nil {
		fmt.Fprintln(stderr, "ether plugin pack:", err)
		return 1
	}
	rep := kit.Check(ctx, pkg)
	if !rep.OK {
		fmt.Fprint(stderr, rep.Text())
		return 1
	}
	pkgBytes, err := kit.Pack(pkg)
	if err != nil {
		fmt.Fprintln(stderr, "ether plugin pack:", err)
		return 1
	}
	dest := *outPath
	if dest == "" {
		base := filepath.Base(filepath.Clean(dir))
		if base == "." || base == "/" || base == "" {
			base = pkg.Manifest.ID
		}
		if !strings.HasSuffix(base, ".etherpkg") {
			base += ".etherpkg"
		}
		dest = base
	}
	if err := os.WriteFile(dest, pkgBytes, 0o644); err != nil {
		fmt.Fprintln(stderr, "ether plugin pack:", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s\n", dest)
	return 0
}

func runPluginDev(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ether plugin dev", flag.ContinueOnError)
	fs.SetOutput(stderr)
	serverURL := fs.String("server", "http://localhost:32400", "Ether server URL")
	token := fs.String("token", os.Getenv("ETHER_TOKEN"), "owner admin token")
	once := fs.Bool("once", false, "reload once, stream current logs, and exit")
	interval := fs.Duration("interval", 750*time.Millisecond, "watch poll interval")

	pos := parseInterleaved(fs, args)
	if len(pos) != 1 {
		fmt.Fprint(stderr, pluginUsage)
		return 2
	}
	dir := pos[0]
	base := strings.TrimRight(strings.TrimSpace(*serverURL), "/")
	if base == "" {
		fmt.Fprintln(stderr, "ether plugin dev: --server is required")
		return 2
	}

	client := &http.Client{Timeout: 10 * time.Second}
	seenLogs := map[string]bool{}

	reload := func() (string, error) {
		pkg, err := kit.LoadDir(dir)
		if err != nil {
			return "", err
		}
		pkgBytes, err := kit.Pack(pkg)
		if err != nil {
			return "", err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/plugins/install", bytes.NewReader(pkgBytes))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("X-Ether-Request", "1")
		if *token != "" {
			req.Header.Set("X-Ether-Admin-Token", *token)
			req.Header.Set("Authorization", "Bearer "+*token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("install returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return pkg.Manifest.ID, nil
	}

	pollLogs := func(pluginID string) {
		if pluginID == "" {
			return
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/plugins/"+pluginID+"/logs", nil)
		if err != nil {
			return
		}
		req.Header.Set("X-Ether-Request", "1")
		if *token != "" {
			req.Header.Set("X-Ether-Admin-Token", *token)
			req.Header.Set("Authorization", "Bearer "+*token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return
		}
		var payload struct {
			Lines []string `json:"lines"`
		}
		if json.NewDecoder(resp.Body).Decode(&payload) != nil {
			return
		}
		for _, line := range payload.Lines {
			if !seenLogs[line] {
				seenLogs[line] = true
				fmt.Fprintf(stdout, "[log] %s\n", line)
			}
		}
	}

	pluginID, err := reload()
	if err != nil {
		fmt.Fprintln(stderr, "ether plugin dev:", err)
		return 1
	}
	fmt.Fprintf(stdout, "Reloaded %s on %s\n", pluginID, base)
	pollLogs(pluginID)
	if *once {
		return 0
	}

	lastSnap := dirSnapshot(dir)
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return 0
		case <-ticker.C:
			snap := dirSnapshot(dir)
			if snap != lastSnap {
				lastSnap = snap
				if id, err := reload(); err != nil {
					fmt.Fprintf(stderr, "reload error: %v\n", err)
				} else {
					pluginID = id
					fmt.Fprintf(stdout, "Reloaded %s on %s\n", pluginID, base)
				}
			}
			pollLogs(pluginID)
		}
	}
}

func dirSnapshot(dir string) string {
	var b strings.Builder
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if info, err := d.Info(); err == nil {
			fmt.Fprintf(&b, "%s:%d:%d;", p, info.Size(), info.ModTime().UnixNano())
		}
		return nil
	})
	return b.String()
}

func runPluginPublish(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ether plugin publish", flag.ContinueOnError)
	fs.SetOutput(stderr)
	pos := parseInterleaved(fs, args)
	target := "."
	if len(pos) == 1 {
		target = pos[0]
	} else if len(pos) > 1 {
		fmt.Fprint(stderr, pluginUsage)
		return 2
	}
	pkg, err := kit.Load(target)
	if err != nil {
		fmt.Fprintln(stderr, "ether plugin publish:", err)
		return 1
	}
	rep := kit.Check(ctx, pkg)
	if !rep.OK {
		fmt.Fprint(stderr, rep.Text())
		return 1
	}
	pkgBytes, err := kit.Pack(pkg)
	if err != nil {
		fmt.Fprintln(stderr, "ether plugin publish:", err)
		return 1
	}
	unpacked, err := kit.LoadPkgBytes(pkgBytes)
	if err != nil {
		fmt.Fprintln(stderr, "ether plugin publish:", err)
		return 1
	}
	digest := strings.TrimSpace(string(unpacked.Files["digest"]))

	fmt.Fprintf(stdout, "Verified %s %s (%s)\n", pkg.Manifest.ID, pkg.Manifest.Version, pkg.Manifest.Ether.Kind)
	fmt.Fprintf(stdout, "Package digest: %s\n\n", digest)
	fmt.Fprintf(stdout, "To publish %s to the Ether community catalogue:\n", pkg.Manifest.Name)
	fmt.Fprintf(stdout, "  1. Pack the release archive:\n")
	fmt.Fprintf(stdout, "       ether plugin pack -o %s-%s.etherpkg %s\n", pkg.Manifest.ID, pkg.Manifest.Version, target)
	fmt.Fprintf(stdout, "  2. Attach %s-%s.etherpkg to a GitHub release in your plugin repository.\n", pkg.Manifest.ID, pkg.Manifest.Version)
	fmt.Fprintf(stdout, "  3. Open a pull request against https://github.com/ether-media/catalogue adding or updating:\n")
	fmt.Fprintf(stdout, "       plugins/%s/manifest.json (version %s, sha256 %s)\n", pkg.Manifest.ID, pkg.Manifest.Version, digest)
	fmt.Fprintf(stdout, "  4. Catalogue CI will run `ether plugin check` automatically on your pull request.\n")
	return 0
}
