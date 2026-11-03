package hello_test

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/braedonsaunders/ether/internal/plugins/wasm"
)

// Standard Go's wasip1 target currently imports WASI, which Ether rejects.
// Keep this executable check so a future toolchain improvement is noticed.
func TestStandardGoWASIP1RequiresForbiddenImports(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	out := filepath.Join(dir, "plugin.wasm")
	const program = `package main
//go:wasmexport alloc
func alloc(n uint32) uint32 { return 0 }
//go:wasmexport handle
func handle(p, n uint32) uint64 { return 0 }
func main() {}`
	if err := os.WriteFile(src, []byte(program), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", out, src)
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm", "CGO_ENABLED=0")
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build wasip1 module: %v: %s", err, raw)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	eng := wasm.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer eng.Close()
	p, err := eng.LoadWASM("example.go", "1", raw, nil, wasm.Limits{})
	if p != nil {
		defer p.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "forbidden import wasi_snapshot_preview1.") {
		t.Fatalf("expected forbidden WASI import, got %v", err)
	}
}
