package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/safefetch"
)

type fakeHot struct{ *fakeRuntime }

func (fakeHot) RankBatch(context.Context, []CandidateFacts, json.RawMessage) ([]int64, HotOutcome, error) {
	return nil, HotOutcome{Status: "ok"}, nil
}
func (fakeHot) CondMatch(context.Context, CandidateFacts, string, json.RawMessage) (bool, HotOutcome, error) {
	return false, HotOutcome{Status: "ok"}, nil
}
func (fakeHot) HasRankKey() bool    { return true }
func (fakeHot) HasCondMatch() bool  { return false }
func (fakeHot) Budgets() HotBudgets { return HotBudgets{} }

func TestHotReadyNeverBlocksAndWarmsInBackground(t *testing.T) {
	env := newTestEnv(t)
	release := make(chan struct{})
	env.m.RegisterRuntime("hosted", func(inst Installed) (Runtime, error) {
		<-release // a slow load: compiling, reading artifacts
		return fakeHot{env.fake}, nil
	})
	rec := env.installHello(t, "1.0.0")

	got := make(chan bool, 1)
	go func() {
		_, ok := env.m.HotReady(rec.ID)
		got <- ok
	}()
	select {
	case ok := <-got:
		if ok {
			t.Fatal("runtime cannot be ready before it has loaded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("HotReady blocked on a runtime load")
	}

	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if hot, ok := env.m.HotReady(rec.ID); ok {
			if !hot.HasRankKey() {
				t.Fatal("warmed runtime lost its capabilities")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("runtime never finished warming")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := env.m.SetShadow(context.Background(), rec.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, ok := env.m.HotReady(rec.ID); ok {
		t.Fatal("a plugin in shadow mode must not change real picks")
	}
	if err := env.m.SetShadow(context.Background(), rec.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := env.m.HotReady(rec.ID); !ok {
		t.Fatal("leaving shadow mode restores the plugin")
	}

	if err := env.m.SetEnabled(context.Background(), rec.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := env.m.HotReady(rec.ID); ok {
		t.Fatal("a disabled plugin must not serve the hot path")
	}
}

func TestPlainStremioManifestUsesTheRemoteRuntime(t *testing.T) {
	env := newTestEnv(t)
	plain := Record{ID: "community.stremio.addon", Manifest: Manifest{ID: "community.stremio.addon", Version: "1.0.0", Name: "Add-on"}}
	if got := plain.Manifest.RuntimeKind(); got != "remote" {
		t.Fatalf("runtime kind = %q, want remote", got)
	}
	rt, err := env.m.runtimeFor(context.Background(), plain)
	if err != nil || rt == nil {
		t.Fatalf("a plain Stremio add-on must open on the remote runtime: %v", err)
	}
	if env.fake.inst.ID != plain.ID {
		t.Fatalf("remote factory opened %q", env.fake.inst.ID)
	}
}

func TestFetchErrorsNeverExposeTheRequestURL(t *testing.T) {
	secretURL := "https://api.example.test/search?api_key=sk-SECRET-123&q=x"
	for _, inner := range []error{
		&url.Error{Op: "Get", URL: secretURL, Err: errors.New("dial tcp 203.0.113.1:443: connect: connection refused " + secretURL)},
		&url.Error{Op: "Get", URL: secretURL, Err: context.DeadlineExceeded},
		&url.Error{Op: "Get", URL: secretURL, Err: safefetch.ErrBlocked},
	} {
		err := fetchError("api.example.test", inner)
		if strings.Contains(err.Error(), "sk-SECRET-123") || strings.Contains(err.Error(), "api_key") {
			t.Fatalf("fetch error leaks the URL: %v", err)
		}
		if !errors.Is(err, ErrFetchFailed) {
			t.Fatalf("fetch error must wrap ErrFetchFailed: %v", err)
		}
	}
	if err := fetchError("h", &url.Error{Op: "Get", URL: secretURL, Err: safefetch.ErrBlocked}); !errors.Is(err, safefetch.ErrBlocked) {
		t.Fatalf("blocked fetches keep the ErrBlocked classification: %v", err)
	}
}

func TestNewRuntimeReceivesStoredSettings(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	rec := env.installHello(t, "1.0.0")
	if _, err := env.m.Configure(ctx, rec.ID, map[string]any{"api_token": "sk-1", "greeting": "Hi"}); err != nil {
		t.Fatal(err)
	}
	env.m.closeRuntime(rec.ID) // a restart: the next runtime starts empty
	before := len(env.fake.calls)
	if _, err := env.m.Call(ctx, rec.ID, Request{Method: "GET", Path: "/ether/health"}); err != nil {
		t.Fatal(err)
	}
	var configured bool
	for _, c := range env.fake.calls[before:] {
		if c.Path == "/ether/configure" && strings.Contains(string(c.Body), "sk-1") && strings.Contains(string(c.Body), "Hi") {
			configured = true
		}
	}
	if !configured {
		t.Fatal("a new runtime must receive the stored settings through /ether/configure")
	}
	if got := DeadlineFor("/ether/subtitles/download"); got != 5*time.Second {
		t.Fatalf("subtitle download deadline = %v", got)
	}
}

func TestKVPrefixListingSeesPastOtherKeys(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	rec := env.installHello(t, "1.0.0")
	h := env.m.HostFor(rec.ID)
	for i := 0; i < 1100; i++ {
		if err := h.KVPut(ctx, fmt.Sprintf("a/%04d", i), []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		if err := h.KVPut(ctx, fmt.Sprintf("z/%d", i), []byte("y")); err != nil {
			t.Fatal(err)
		}
	}
	keys, err := h.KVList(ctx, "z/")
	if err != nil || len(keys) != 3 {
		t.Fatalf("prefix list after 1,100 other keys = %v, %v", keys, err)
	}
}

func TestFolderCallsStayInsideTheGrantThroughSymlinks(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "movie.mkv"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "leak.txt")); err != nil {
		t.Fatal(err)
	}
	h := &host{}
	if _, err := h.resolveFolderPath(root, "escape/secret.txt"); err == nil {
		t.Fatal("a symlinked directory must not reach outside the grant")
	}
	if _, err := h.resolveFolderPath(root, "leak.txt"); err == nil {
		t.Fatal("a symlinked file must not reach outside the grant")
	}
	if got, err := h.resolveFolderPath(root, "movie.mkv"); err != nil || filepath.Base(got) != "movie.mkv" {
		t.Fatalf("a real file inside the grant must resolve: %q %v", got, err)
	}
}

func TestActionResultStartsABackgroundJobOnce(t *testing.T) {
	env := newTestEnv(t)
	rec := env.installHello(t, "1.0.0")
	release := make(chan struct{})
	started := make(chan string, 4)
	env.fake.mu.Lock()
	env.fake.routes = map[string]func(Request) (Response, error){
		"/ether/job/": func(r Request) (Response, error) {
			started <- r.Path
			<-release
			return Response{Status: 200}, nil
		},
	}
	env.fake.mu.Unlock()
	body := json.RawMessage(`{"toast":"Scanning","job":"scan"}`)
	env.m.startActionJob(rec.ID, body)
	env.m.startActionJob(rec.ID, body) // a second click while running
	select {
	case p := <-started:
		if p != "/ether/job/scan" {
			t.Fatalf("job path = %q", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the job never started")
	}
	select {
	case <-started:
		t.Fatal("the same job must not run twice at once")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	env.m.startActionJob(rec.ID, json.RawMessage(`{"job":"../../ether/configure"}`))
	select {
	case p := <-started:
		t.Fatalf("a path outside /ether/job/ must not run: %q", p)
	case <-time.After(200 * time.Millisecond):
	}
}
