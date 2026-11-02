package kit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/plugins/wasm"
)

// FixtureCase is one recorded test fixture in tests/fixtures.json.
type FixtureCase struct {
	Name       string            `json:"name"`
	Method     string            `json:"method"`
	Path       string            `json:"path"`
	Query      map[string]string `json:"query,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       json.RawMessage   `json:"body,omitempty"`
	WantStatus int               `json:"wantStatus"`
	WantFields map[string]any    `json:"wantFields,omitempty"`
}

// FixtureSuite is the schema of tests/fixtures.json.
type FixtureSuite struct {
	Cases []FixtureCase `json:"cases"`
}

// CaseResult is the outcome of one fixture test case.
type CaseResult struct {
	Name       string  `json:"name"`
	OK         bool    `json:"ok"`
	Status     int     `json:"status"`
	DurationMS float64 `json:"durationMs"`
	Error      string  `json:"error,omitempty"`
}

// TestReport is the outcome of RunTests.
type TestReport struct {
	PluginID    string       `json:"pluginId"`
	OK          bool         `json:"ok"`
	Cases       []CaseResult `json:"cases"`
	Integration *CaseResult  `json:"integration,omitempty"`
}

// Text formats the test report as plain human-readable lines.
func (tr TestReport) Text() string {
	var b strings.Builder
	status := "PASS"
	if !tr.OK {
		status = "FAIL"
	}
	fmt.Fprintf(&b, "%s: %s (%d cases)\n", status, tr.PluginID, len(tr.Cases))
	for _, c := range tr.Cases {
		mark := "ok"
		if !c.OK {
			mark = "fail"
		}
		if c.Error != "" {
			fmt.Fprintf(&b, "  %s %s (%.2fms): %s\n", mark, c.Name, c.DurationMS, c.Error)
		} else {
			fmt.Fprintf(&b, "  %s %s -> %d (%.2fms)\n", mark, c.Name, c.Status, c.DurationMS)
		}
	}
	if tr.Integration != nil {
		mark := "ok"
		if !tr.Integration.OK {
			mark = "fail"
		}
		if tr.Integration.Error != "" {
			fmt.Fprintf(&b, "  %s integration (%s): %s\n", mark, tr.Integration.Name, tr.Integration.Error)
		} else {
			fmt.Fprintf(&b, "  %s integration (%s, %.1fms)\n", mark, tr.Integration.Name, tr.Integration.DurationMS)
		}
	}
	return b.String()
}

// RunTests runs the plugin's recorded fixtures through the hosted runtime
// (or conformal check when no fixtures file is present), and when etherBin
// is non-empty, runs an integration check against a headless Ether process.
func RunTests(ctx context.Context, pkg *Package, etherBin string) (TestReport, error) {
	if pkg == nil || len(pkg.ManifestRaw) == 0 {
		return TestReport{}, fmt.Errorf("kit: test: empty package")
	}
	man, err := plugins.ParseManifest(pkg.ManifestRaw)
	if err != nil {
		return TestReport{}, err
	}
	pkg.Manifest = man
	out := TestReport{PluginID: man.ID, OK: true}

	var suite FixtureSuite
	if raw, ok := pkg.Files["tests/fixtures.json"]; ok {
		if err := json.Unmarshal(raw, &suite); err != nil {
			return out, fmt.Errorf("kit: parse tests/fixtures.json: %w", err)
		}
	}

	if man.Ether != nil && man.Ether.Runtime.Kind == "hosted" {
		eng := sharedEngine()

		host := newRecordingHost(man)
		var plug *wasm.Plugin
		if man.Ether.Runtime.Module != "" {
			mod, ok := pkg.Files[man.Ether.Runtime.Module]
			if !ok {
				return out, fmt.Errorf("kit: missing WASM module %q", man.Ether.Runtime.Module)
			}
			plug, err = eng.LoadWASM(man.ID, man.Version, mod, host, wasm.Limits{})
		} else {
			jsSrc, compErr := loadOrCompileEntry(man.Ether.Runtime.Entry, pkg.Files)
			if compErr != nil {
				return out, compErr
			}
			plug, err = eng.LoadJS(man.ID, man.Version, jsSrc, host, wasm.Limits{})
		}
		if err != nil {
			return out, err
		}
		defer func() { _ = plug.Close() }()

		for _, tc := range suite.Cases {
			res := runFixtureCase(ctx, plug, tc)
			if !res.OK {
				out.OK = false
			}
			out.Cases = append(out.Cases, res)
		}
	}

	if len(out.Cases) == 0 {
		// Fall back to conformance check as the baseline test suite.
		rep := Check(ctx, pkg)
		cr := CaseResult{Name: "conformance", OK: rep.OK, Status: 200, DurationMS: rep.DurationMS}
		if !rep.OK && len(rep.Errors) > 0 {
			cr.Error = rep.Errors[0].String()
			out.OK = false
		}
		out.Cases = append(out.Cases, cr)
	}

	if strings.TrimSpace(etherBin) != "" {
		ir := runHeadlessEtherIntegration(ctx, pkg, etherBin)
		out.Integration = &ir
		if !ir.OK {
			out.OK = false
		}
	}

	return out, nil
}

func runFixtureCase(ctx context.Context, plug *wasm.Plugin, tc FixtureCase) CaseResult {
	method := tc.Method
	if method == "" {
		method = "GET"
	}
	wantStatus := tc.WantStatus
	if wantStatus == 0 {
		wantStatus = 200
	}
	q := map[string][]string{}
	for k, v := range tc.Query {
		q[k] = []string{v}
	}
	t0 := time.Now()
	resp, _, err := plug.CallDetailed(ctx, wasm.Request{
		Method:  method,
		Path:    tc.Path,
		Query:   q,
		Headers: tc.Headers,
		Body:    tc.Body,
	})
	dur := float64(time.Since(t0).Microseconds()) / 1000.0
	res := CaseResult{Name: tc.Name, Status: resp.Status, DurationMS: dur, OK: true}
	if err != nil {
		res.OK = false
		res.Error = err.Error()
		return res
	}
	if resp.Status != wantStatus {
		res.OK = false
		res.Error = fmt.Sprintf("status = %d, want %d", resp.Status, wantStatus)
		return res
	}
	if len(tc.WantFields) > 0 {
		var doc map[string]any
		if err := decodeResponseBody(resp.Body, &doc); err != nil {
			res.OK = false
			res.Error = "decode body: " + err.Error()
			return res
		}
		for k, want := range tc.WantFields {
			got := doc[k]
			wJSON, _ := json.Marshal(want)
			gJSON, _ := json.Marshal(got)
			if !bytes.Equal(wJSON, gJSON) {
				res.OK = false
				res.Error = fmt.Sprintf("field %q = %s, want %s", k, gJSON, wJSON)
				return res
			}
		}
	}
	return res
}

func runHeadlessEtherIntegration(ctx context.Context, pkg *Package, etherBin string) CaseResult {
	t0 := time.Now()
	res := CaseResult{Name: "headless-ether", OK: true}
	defer func() {
		res.DurationMS = float64(time.Since(t0).Microseconds()) / 1000.0
	}()

	pkgBytes, err := Pack(pkg)
	if err != nil {
		res.OK = false
		res.Error = "pack: " + err.Error()
		return res
	}

	tmpDir, err := os.MkdirTemp("", "ether-plugin-test-*")
	if err != nil {
		res.OK = false
		res.Error = err.Error()
		return res
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	port, err := freePort()
	if err != nil {
		res.OK = false
		res.Error = err.Error()
		return res
	}
	token := "test-admin-token-conformance"
	cfg := map[string]any{
		"server": map[string]any{
			"listen":     fmt.Sprintf("127.0.0.1:%d", port),
			"adminToken": token,
			"logLevel":   "error",
		},
	}
	cfgBytes, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), cfgBytes, 0o600); err != nil {
		res.OK = false
		res.Error = err.Error()
		return res
	}

	runCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(runCtx, etherBin, "--data-dir", tmpDir)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		res.OK = false
		res.Error = "start ether: " + err.Error()
		return res
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 3 * time.Second}
	ready := false
	for i := 0; i < 40; i++ {
		req, _ := http.NewRequestWithContext(runCtx, http.MethodGet, baseURL+"/api/plugins", nil)
		req.Header.Set("X-Ether-Admin-Token", token)
		req.Header.Set("X-Ether-Request", "1")
		if resp, err := client.Do(req); err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	if !ready {
		res.OK = false
		res.Error = "headless ether did not become ready"
		return res
	}

	req, err := http.NewRequestWithContext(runCtx, http.MethodPost, baseURL+"/api/plugins/install", bytes.NewReader(pkgBytes))
	if err != nil {
		res.OK = false
		res.Error = err.Error()
		return res
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Ether-Admin-Token", token)
	req.Header.Set("X-Ether-Request", "1")
	resp, err := client.Do(req)
	if err != nil {
		res.OK = false
		res.Error = "install request: " + err.Error()
		return res
	}
	defer resp.Body.Close()
	res.Status = resp.StatusCode
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		res.OK = false
		res.Error = fmt.Sprintf("install returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		return res
	}
	return res
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}
