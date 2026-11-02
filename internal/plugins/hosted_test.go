package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/braedonsaunders/ether/internal/plugins/wasm"
)

type memHost struct {
	kv     map[string][]byte
	denied bool
}

func (h *memHost) KVGet(_ context.Context, k string) ([]byte, bool, error) {
	v, ok := h.kv[k]
	return v, ok, nil
}
func (h *memHost) KVPut(_ context.Context, k string, v []byte) error {
	if h.denied {
		return ErrPermissionDenied
	}
	h.kv[k] = v
	return nil
}
func (h *memHost) KVDelete(_ context.Context, k string) error { delete(h.kv, k); return nil }
func (h *memHost) KVList(context.Context, string) ([]string, error) {
	return nil, nil
}
func (h *memHost) SecretGet(context.Context, string) (string, error) { return "", ErrPermissionDenied }
func (h *memHost) HTTPFetch(context.Context, FetchRequest) (FetchResponse, error) {
	return FetchResponse{}, ErrPermissionDenied
}
func (h *memHost) CatalogSearch(context.Context, string, int, int) (json.RawMessage, error) {
	return json.RawMessage(`{"titles":[]}`), nil
}
func (h *memHost) CatalogGet(context.Context, string) (json.RawMessage, error) { return nil, nil }
func (h *memHost) ViewerHistory(context.Context, int64, int, int) (json.RawMessage, error) {
	return nil, ErrPermissionDenied
}
func (h *memHost) SetWatchState(context.Context, int64, string, bool) error {
	return ErrPermissionDenied
}
func (h *memHost) CollectionsUpsert(context.Context, json.RawMessage) error {
	return ErrPermissionDenied
}
func (h *memHost) FoldersList(context.Context, string, string) ([]FolderEntry, error) {
	return nil, ErrPermissionDenied
}
func (h *memHost) FoldersStat(context.Context, string, string) (FolderEntry, error) {
	return FolderEntry{}, ErrPermissionDenied
}
func (h *memHost) NotifyOwner(context.Context, string) error { return nil }
func (h *memHost) Log(context.Context, string, string, map[string]any) error {
	return nil
}

const hostedTS = `
type Req = { path: string; query?: Record<string, string[]> };
function handle(req: Req): { status: number; body: string } {
  const prev = ether.kv.get("count");
  const next = String(Number(prev ?? "0") + 1);
  ether.kv.put("count", next);
  return { status: 200, body: JSON.stringify({ path: req.path, count: next }) };
}
function rank_key(c: { name: string }): number { return c.name.includes("DUB") ? 10 : 0; }
(globalThis as any).handle = handle;
(globalThis as any).rank_key = rank_key;
`

func TestHostedRuntimeFactoryRunsTypeScriptThroughHost(t *testing.T) {
	eng := wasm.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { _ = eng.Close() })
	host := &memHost{kv: map[string][]byte{}}
	inst := Installed{
		ID: "dev.example.hello",
		Manifest: Manifest{
			Version: "1.0.0",
			Ether:   &EtherBlock{Runtime: RuntimeDecl{Kind: "hosted", Entry: "plugin.ts"}},
		},
		Files: func(p string) ([]byte, bool) {
			if p == "plugin.ts" {
				return []byte(hostedTS), true
			}
			return nil, false
		},
		Host: host,
	}
	rt, err := HostedRuntimeFactory(eng)(inst)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	ctx := context.Background()
	for want := 1; want <= 2; want++ {
		resp, err := rt.Call(ctx, Request{Method: "GET", Path: "/ether/health"})
		if err != nil {
			t.Fatal(err)
		}
		var body string
		if err := json.Unmarshal(resp.Body, &body); err != nil {
			t.Fatalf("body %s: %v", resp.Body, err)
		}
		if resp.Status != 200 || !strings.Contains(body, `"count":"`+string(rune('0'+want))+`"`) {
			t.Fatalf("call %d: status %d body %s", want, resp.Status, body)
		}
	}
	hot, ok := rt.(HotRuntime)
	if !ok || !hot.HasRankKey() {
		t.Fatal("hosted runtime should expose rank_key")
	}
	keys, _, err := hot.RankBatch(ctx, []CandidateFacts{{ReleaseID: "a", Name: "Movie.DUB.1080p"}, {ReleaseID: "b", Name: "Movie.1080p"}}, nil)
	if err != nil || len(keys) != 2 || keys[0] != 10 || keys[1] != 0 {
		t.Fatalf("rank keys = %v, %v", keys, err)
	}

	host.denied = true
	if _, err := rt.Call(ctx, Request{Method: "GET", Path: "/"}); err == nil {
		t.Fatal("a revoked grant must fail the call")
	} else {
		var we *wasm.Error
		if !errors.As(err, &we) || we.Status != wasm.StatusForbidden {
			t.Fatalf("revoked grant error = %v", err)
		}
	}
}

func TestHostedRuntimeFactoryReportsCompileErrors(t *testing.T) {
	eng := wasm.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { _ = eng.Close() })
	inst := Installed{
		ID:       "dev.example.broken",
		Manifest: Manifest{Version: "1.0.0", Ether: &EtherBlock{Runtime: RuntimeDecl{Kind: "hosted", Entry: "plugin.ts"}}},
		Files:    func(string) ([]byte, bool) { return []byte("function (("), true },
		Host:     &memHost{kv: map[string][]byte{}},
	}
	if _, err := HostedRuntimeFactory(eng)(inst); err == nil || !strings.Contains(err.Error(), "compile") {
		t.Fatalf("compile error = %v", err)
	}
}
