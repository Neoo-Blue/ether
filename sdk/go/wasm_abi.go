//go:build wasm

package etherplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"unsafe"
)

var (
	registeredWASMPlugin *Plugin
	liveAllocations      = map[uint32][]byte{}
)

// RegisterWASM registers a Plugin as the active WebAssembly guest handler for
// the exported `handle`, `rank_key`, and `cond_match` guest functions.
func RegisterWASM(p *Plugin) {
	registeredWASMPlugin = p
}

// RegisteredWASMPlugin returns the plugin registered with RegisterWASM.
func RegisteredWASMPlugin() *Plugin {
	return registeredWASMPlugin
}

func defaultHostForRuntime(_ RemoteHostOptions) Host {
	return wasmHost{}
}

//go:wasmimport env host_log
func hostLog(level uint32, ptr uint32, length uint32)

//go:wasmimport env host_abort
func hostAbort(ptr uint32, length uint32)

//go:wasmimport env host_kv_get
func hostKVGet(ptr uint32, length uint32) uint64

//go:wasmimport env host_kv_put
func hostKVPut(kPtr uint32, kLen uint32, vPtr uint32, vLen uint32) uint32

//go:wasmimport env host_kv_delete
func hostKVDelete(ptr uint32, length uint32) uint32

//go:wasmimport env host_kv_list
func hostKVList(ptr uint32, length uint32) uint64

//go:wasmimport env host_secret_get
func hostSecretGet(ptr uint32, length uint32) uint64

//go:wasmimport env host_http_fetch
func hostHTTPFetch(ptr uint32, length uint32) uint64

//go:wasmimport env host_catalog_search
func hostCatalogSearch(ptr uint32, length uint32) uint64

//go:wasmimport env host_catalog_get
func hostCatalogGet(ptr uint32, length uint32) uint64

//go:wasmimport env host_viewers_history
func hostViewersHistory(ptr uint32, length uint32) uint64

//go:wasmimport env host_viewers_set_watch_state
func hostViewersSetWatchState(ptr uint32, length uint32) uint32

//go:wasmimport env host_collections_upsert
func hostCollectionsUpsert(ptr uint32, length uint32) uint32

//go:wasmimport env host_folders_list
func hostFoldersList(ptr uint32, length uint32) uint64

//go:wasmimport env host_folders_stat
func hostFoldersStat(ptr uint32, length uint32) uint64

//go:wasmimport env host_notify_owner
func hostNotifyOwner(ptr uint32, length uint32) uint32

func bytesToPtrLen(b []byte) (uint32, uint32) {
	if len(b) == 0 {
		return 0, 0
	}
	ptr := uint32(uintptr(unsafe.Pointer(&b[0])))
	return ptr, uint32(len(b))
}

func unpackFatPtr(packed uint64) []byte {
	ptr := uint32(packed >> 32)
	length := uint32(packed & 0xFFFFFFFF)
	if ptr == 0 || length == 0 {
		return nil
	}
	src := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), int(length))
	out := make([]byte, len(src))
	copy(out, src)
	delete(liveAllocations, ptr)
	return out
}

func packFatPtr(b []byte) uint64 {
	if len(b) == 0 {
		return 0
	}
	buf := make([]byte, len(b))
	copy(buf, b)
	ptr := uint32(uintptr(unsafe.Pointer(&buf[0])))
	liveAllocations[ptr] = buf
	return (uint64(ptr) << 32) | uint64(uint32(len(buf)))
}

//go:wasmexport alloc
func wasmAlloc(size uint32) uint32 {
	if size == 0 {
		size = 1
	}
	buf := make([]byte, int(size))
	ptr := uint32(uintptr(unsafe.Pointer(&buf[0])))
	liveAllocations[ptr] = buf
	return ptr
}

//go:wasmexport handle
func wasmHandle(ptr uint32, length uint32) uint64 {
	if registeredWASMPlugin == nil {
		return 0
	}
	raw := unpackFatPtr((uint64(ptr) << 32) | uint64(length))
	var req RawRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		resp := errorJSON(400, "invalid_json", err.Error())
		out, _ := json.Marshal(resp)
		return packFatPtr(out)
	}
	resp := registeredWASMPlugin.Dispatch(context.Background(), req)
	if resp.BinaryBody != nil && len(resp.Body) == 0 {
		resp.Body, _ = json.Marshal(string(resp.BinaryBody))
	}
	out, _ := json.Marshal(resp)
	return packFatPtr(out)
}

//go:wasmexport rank_key
func wasmRankKey(ptr uint32, length uint32) int64 {
	if registeredWASMPlugin == nil {
		return 0
	}
	raw := unpackFatPtr((uint64(ptr) << 32) | uint64(length))
	var payload struct {
		Candidate CandidateFacts `json:"candidate"`
		Config    map[string]any `json:"config"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0
	}
	return registeredWASMPlugin.RankKey(payload.Candidate, payload.Config)
}

//go:wasmexport cond_match
func wasmCondMatch(ptr uint32, length uint32) uint32 {
	if registeredWASMPlugin == nil {
		return 0
	}
	raw := unpackFatPtr((uint64(ptr) << 32) | uint64(length))
	var payload struct {
		Candidate CandidateFacts `json:"candidate"`
		Operator  string         `json:"operator"`
		Value     any            `json:"value"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0
	}
	if registeredWASMPlugin.CondMatch(payload.Candidate, payload.Operator, payload.Value) {
		return 1
	}
	return 0
}

type wasmHost struct{}

func (wasmHost) KVGet(_ context.Context, key string) (string, bool, error) {
	ptr, n := bytesToPtrLen([]byte(key))
	packed := hostKVGet(ptr, n)
	if packed == 0 {
		return "", false, nil
	}
	return string(unpackFatPtr(packed)), true, nil
}

func (wasmHost) KVPut(_ context.Context, key, value string) error {
	kp, kn := bytesToPtrLen([]byte(key))
	vp, vn := bytesToPtrLen([]byte(value))
	if code := hostKVPut(kp, kn, vp, vn); code != 0 {
		return fmt.Errorf("host_kv_put failed with code %d", code)
	}
	return nil
}

func (wasmHost) KVDelete(_ context.Context, key string) error {
	kp, kn := bytesToPtrLen([]byte(key))
	if code := hostKVDelete(kp, kn); code != 0 {
		return fmt.Errorf("host_kv_delete failed with code %d", code)
	}
	return nil
}

func (wasmHost) KVList(_ context.Context, prefix string) ([]string, error) {
	pp, pn := bytesToPtrLen([]byte(prefix))
	raw := unpackFatPtr(hostKVList(pp, pn))
	if len(raw) == 0 {
		return nil, nil
	}
	var keys []string
	err := json.Unmarshal(raw, &keys)
	return keys, err
}

func (wasmHost) SecretGet(_ context.Context, name string) (string, error) {
	np, nn := bytesToPtrLen([]byte(name))
	raw := unpackFatPtr(hostSecretGet(np, nn))
	return string(raw), nil
}

func (wasmHost) Fetch(_ context.Context, req HostFetchRequest) (HostFetchResponse, error) {
	b, _ := json.Marshal(req)
	p, n := bytesToPtrLen(b)
	raw := unpackFatPtr(hostHTTPFetch(p, n))
	var out HostFetchResponse
	err := json.Unmarshal(raw, &out)
	return out, err
}

func (wasmHost) CatalogSearch(_ context.Context, query string, limit, offset int) ([]CatalogTitleSummary, error) {
	b, _ := json.Marshal(map[string]any{"query": query, "limit": limit, "offset": offset})
	p, n := bytesToPtrLen(b)
	raw := unpackFatPtr(hostCatalogSearch(p, n))
	var out struct {
		Titles []CatalogTitleSummary `json:"titles"`
	}
	err := json.Unmarshal(raw, &out)
	return out.Titles, err
}

func (wasmHost) CatalogGet(_ context.Context, id string) (*CatalogTitleSummary, error) {
	p, n := bytesToPtrLen([]byte(id))
	raw := unpackFatPtr(hostCatalogGet(p, n))
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var out CatalogTitleSummary
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (wasmHost) ViewerHistory(_ context.Context, viewerID int64, limit, offset int) ([]ViewerWatchEntry, error) {
	b, _ := json.Marshal(map[string]any{"viewerId": viewerID, "limit": limit, "offset": offset})
	p, n := bytesToPtrLen(b)
	raw := unpackFatPtr(hostViewersHistory(p, n))
	var out []ViewerWatchEntry
	err := json.Unmarshal(raw, &out)
	return out, err
}

func (wasmHost) SetWatchState(_ context.Context, viewerID int64, titleID string, watched bool) error {
	b, _ := json.Marshal(map[string]any{"viewerId": viewerID, "titleId": titleID, "watched": watched})
	p, n := bytesToPtrLen(b)
	if code := hostViewersSetWatchState(p, n); code != 0 {
		return fmt.Errorf("host_viewers_set_watch_state failed with code %d", code)
	}
	return nil
}

func (wasmHost) UpsertCollection(_ context.Context, collection map[string]any) error {
	b, _ := json.Marshal(collection)
	p, n := bytesToPtrLen(b)
	if code := hostCollectionsUpsert(p, n); code != 0 {
		return fmt.Errorf("host_collections_upsert failed with code %d", code)
	}
	return nil
}

func (wasmHost) FoldersList(_ context.Context, root, path string) ([]FolderEntry, error) {
	b, _ := json.Marshal(map[string]string{"root": root, "path": path})
	p, n := bytesToPtrLen(b)
	raw := unpackFatPtr(hostFoldersList(p, n))
	var out []FolderEntry
	err := json.Unmarshal(raw, &out)
	return out, err
}

func (wasmHost) FoldersStat(_ context.Context, root, path string) (FolderEntry, error) {
	b, _ := json.Marshal(map[string]string{"root": root, "path": path})
	p, n := bytesToPtrLen(b)
	raw := unpackFatPtr(hostFoldersStat(p, n))
	var out FolderEntry
	err := json.Unmarshal(raw, &out)
	return out, err
}

func (wasmHost) NotifyOwner(_ context.Context, text string) error {
	p, n := bytesToPtrLen([]byte(text))
	if code := hostNotifyOwner(p, n); code != 0 {
		return fmt.Errorf("host_notify_owner failed with code %d", code)
	}
	return nil
}

func (wasmHost) Log(_ context.Context, level, message string, _ map[string]any) error {
	var lvl uint32 = 1
	switch level {
	case "debug":
		lvl = 0
	case "warn":
		lvl = 2
	case "error":
		lvl = 3
	}
	p, n := bytesToPtrLen([]byte(message))
	hostLog(lvl, p, n)
	return nil
}
