package catalogue

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/agent/tools"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/store"
)

func makePkg(t *testing.T, id, name, version string, networkHosts []string) ([]byte, string) {
	t.Helper()
	man := map[string]any{
		"id":          id,
		"name":        name,
		"version":     version,
		"description": "Free live television channels.",
		"resources":   []string{"catalog", "stream"},
		"types":       []string{"tv"},
		"ether": map[string]any{
			"protocol":     1,
			"kind":         "livetv_source",
			"summary":      "Free live television channels.",
			"capabilities": []string{"livetv_channels"},
			"runtime": map[string]any{
				"kind":  "hosted",
				"entry": "plugin.js",
			},
			"permissions": map[string]any{
				"network": networkHosts,
			},
			"contributes": map[string]any{
				"channels": map[string]any{
					"test": true,
				},
			},
		},
	}
	manBytes, err := json.Marshal(man)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	jsBytes := []byte("export default {};\n")
	files := map[string][]byte{
		"manifest.json": manBytes,
		"plugin.js":     jsBytes,
	}
	digest := plugins.PkgDigest(files)
	files["digest"] = []byte(digest + "\n")

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, k := range []string{"manifest.json", "plugin.js", "digest"} {
		w, err := zw.Create(k)
		if err != nil {
			t.Fatalf("zip create: %v", err)
		}
		if _, err := w.Write(files[k]); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	pkgBytes := buf.Bytes()
	sum := sha256.Sum256(pkgBytes)
	return pkgBytes, hex.EncodeToString(sum[:])
}

func TestSignAndVerifyDetached(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	_, sha := makePkg(t, "community.plutotv.ether", "Pluto TV", "1.0.0", []string{"api.pluto.tv"})
	idx := Index{
		Schema:      1,
		GeneratedAt: "2026-11-02T00:00:00Z",
		Entries: []Entry{
			{
				ID:           "community.plutotv.ether",
				Name:         "Pluto TV",
				Summary:      "Free live television channels.",
				Kind:         "livetv_source",
				Category:     "Live TV",
				Capabilities: []string{"livetv_channels"},
				Permissions: plugins.Permissions{
					Network: []string{"api.pluto.tv"},
				},
				Versions: []Version{
					{
						Version: "1.0.0",
						URL:     "https://catalogue.example.invalid/pluto-1.0.0.etherpkg",
						SHA256:  sha,
					},
				},
			},
		},
	}
	raw, err := json.Marshal(idx)
	if err != nil {
		t.Fatalf("marshal index: %v", err)
	}
	sig := SignDetached(raw, priv)

	parsed, err := ParseAndVerify(raw, []byte(sig), pub)
	if err != nil {
		t.Fatalf("ParseAndVerify failed: %v", err)
	}
	if len(parsed.Entries) != 1 || parsed.Entries[0].ID != "community.plutotv.ether" {
		t.Fatalf("unexpected parsed entries: %+v", parsed.Entries)
	}

	// Tampered index payload must be rejected.
	tampered := bytes.Replace(raw, []byte("Pluto TV"), []byte("Evil  TV"), 1)
	if _, err := ParseAndVerify(tampered, []byte(sig), pub); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("expected ErrSignatureInvalid on tampered index, got %v", err)
	}

	// Wrong public key must be rejected.
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := ParseAndVerify(raw, []byte(sig), otherPub); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("expected ErrSignatureInvalid with wrong public key, got %v", err)
	}
}

func TestLoadCatalogueTemplateDir(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "sdk", "catalogue-template", "plugins")
	idx, err := LoadEntriesDir(dir)
	if err != nil {
		t.Fatalf("LoadEntriesDir(%s): %v", dir, err)
	}
	if len(idx.Entries) < 2 {
		t.Fatalf("expected at least 2 template entries, got %d", len(idx.Entries))
	}
}

func TestCatalogueInstallRevokeAndAutoUpdate(t *testing.T) {
	ctx := context.Background()
	db := store.OpenTest(t)

	var noticesMu sync.Mutex
	var notices []string

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	pubB64 := base64.StdEncoding.EncodeToString(pub)

	pkgV1, shaV1 := makePkg(t, "community.plutotv.ether", "Pluto TV", "1.0.0", []string{"api.pluto.tv"})
	pkgV2SamePerms, shaV2Same := makePkg(t, "community.plutotv.ether", "Pluto TV", "1.1.0", []string{"api.pluto.tv"})
	pkgV3MorePerms, shaV3More := makePkg(t, "community.plutotv.ether", "Pluto TV", "1.2.0", []string{"api.pluto.tv", "ads.evil.example"})

	var srvMu sync.Mutex
	var currentIndex []byte
	var currentSig string
	tamperPkg := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srvMu.Lock()
		defer srvMu.Unlock()
		switch r.URL.Path {
		case "/index.json":
			_, _ = w.Write(currentIndex)
		case "/index.json.sig":
			_, _ = w.Write([]byte(currentSig))
		case "/pluto-1.0.0.etherpkg":
			if tamperPkg {
				bad := append([]byte(nil), pkgV1...)
				bad[len(bad)-1] ^= 0xff
				_, _ = w.Write(bad)
				return
			}
			_, _ = w.Write(pkgV1)
		case "/pluto-1.1.0.etherpkg":
			_, _ = w.Write(pkgV2SamePerms)
		case "/pluto-1.2.0.etherpkg":
			_, _ = w.Write(pkgV3MorePerms)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	publishIndex := func(idx Index) {
		t.Helper()
		raw, err := json.Marshal(idx)
		if err != nil {
			t.Fatalf("marshal index: %v", err)
		}
		sig := SignDetached(raw, priv)
		srvMu.Lock()
		currentIndex = raw
		currentSig = sig
		srvMu.Unlock()
	}

	publishIndex(Index{
		Schema:      1,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Entries: []Entry{
			{
				ID:           "community.plutotv.ether",
				Name:         "Pluto TV",
				Summary:      "Free live television channels.",
				Kind:         "livetv_source",
				Category:     "Live TV",
				Capabilities: []string{"livetv_channels"},
				Permissions: plugins.Permissions{
					Network: []string{"api.pluto.tv"},
				},
				Versions: []Version{
					{
						Version: "1.0.0",
						URL:     srv.URL + "/pluto-1.0.0.etherpkg",
						SHA256:  shaV1,
					},
				},
			},
		},
	})

	mgr, err := plugins.New(plugins.Deps{
		DB:          db,
		Registry:    extend.New(),
		Tools:       tools.NewCatalogue(),
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		FetchClient: srv.Client(),
		Backends: plugins.Backends{
			Notice: func(text string) {
				noticesMu.Lock()
				notices = append(notices, text)
				noticesMu.Unlock()
			},
		},
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	cat, err := New(Options{
		DB:        db,
		Manager:   mgr,
		IndexURL:  srv.URL + "/index.json",
		PublicKey: pubB64,
		Client:    srv.Client(),
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("new catalogue: %v", err)
	}
	mgr.SetCatalogue(cat)

	// 1. Refresh and query via HTTP handler.
	if _, err := cat.Refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	apiSrv := httptest.NewServer(mgr.Handler())
	defer apiSrv.Close()

	resp, err := http.Get(apiSrv.URL + "/catalogue?q=pluto&kind=livetv_source")
	if err != nil {
		t.Fatalf("GET /catalogue: %v", err)
	}
	var catResp struct {
		Configured bool            `json:"configured"`
		Entries    []CatalogueItem `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&catResp); err != nil {
		t.Fatalf("decode /catalogue: %v", err)
	}
	_ = resp.Body.Close()
	if !catResp.Configured || len(catResp.Entries) != 1 {
		t.Fatalf("expected 1 configured catalogue entry, got %+v", catResp)
	}
	if catResp.Entries[0].LatestVersion != "1.0.0" || catResp.Entries[0].Installed {
		t.Fatalf("unexpected entry state: %+v", catResp.Entries[0])
	}

	// 2. Digest mismatch at install must be rejected.
	srvMu.Lock()
	tamperPkg = true
	srvMu.Unlock()
	_, err = mgr.PreviewInstall(ctx, plugins.Source{Kind: "catalogue", ID: "community.plutotv.ether"})
	if !errors.Is(err, plugins.ErrDigestMismatch) {
		t.Fatalf("expected ErrDigestMismatch when artifact is tampered, got %v", err)
	}
	srvMu.Lock()
	tamperPkg = false
	srvMu.Unlock()

	// 3. Install from catalogue via HTTP API sets Verified: true and AutoUpdate: true.
	installBody := `{"kind":"catalogue","id":"community.plutotv.ether","autoUpdate":true}`
	instResp, err := http.Post(apiSrv.URL+"/install", "application/json", strings.NewReader(installBody))
	if err != nil {
		t.Fatalf("POST /install: %v", err)
	}
	if instResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", instResp.StatusCode)
	}
	var summary plugins.Summary
	if err := json.NewDecoder(instResp.Body).Decode(&summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	_ = instResp.Body.Close()
	if !summary.Verified {
		t.Fatalf("expected catalogue install to mark plugin Verified=true")
	}
	if summary.Version != "1.0.0" || !summary.AutoUpdate {
		t.Fatalf("unexpected summary: %+v", summary)
	}

	// 4. Publish 1.1.0 with identical permissions; Refresh auto-updates to 1.1.0 and records PrevVersion=1.0.0.
	publishIndex(Index{
		Schema:      1,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Entries: []Entry{
			{
				ID:           "community.plutotv.ether",
				Name:         "Pluto TV",
				Summary:      "Free live television channels.",
				Kind:         "livetv_source",
				Category:     "Live TV",
				Capabilities: []string{"livetv_channels"},
				Permissions: plugins.Permissions{
					Network: []string{"api.pluto.tv"},
				},
				Versions: []Version{
					{Version: "1.0.0", URL: srv.URL + "/pluto-1.0.0.etherpkg", SHA256: shaV1},
					{Version: "1.1.0", URL: srv.URL + "/pluto-1.1.0.etherpkg", SHA256: shaV2Same},
				},
			},
		},
	})
	if _, err := cat.Refresh(ctx); err != nil {
		t.Fatalf("refresh 1.1.0: %v", err)
	}
	rec, err := mgr.Get(ctx, "community.plutotv.ether")
	if err != nil {
		t.Fatalf("get after auto-update: %v", err)
	}
	if rec.Version != "1.1.0" || rec.PrevVersion != "1.0.0" {
		t.Fatalf("expected auto-update to 1.1.0 with prev 1.0.0, got version=%s prev=%s", rec.Version, rec.PrevVersion)
	}
	if !rec.AutoUpdate || !rec.Verified {
		t.Fatalf("expected AutoUpdate and Verified preserved after auto-update, got %+v", rec)
	}

	// 5. One-step rollback returns to 1.0.0; then re-apply update to 1.1.0.
	rolled, err := mgr.Rollback(ctx, "community.plutotv.ether")
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if rolled.Version != "1.0.0" {
		t.Fatalf("expected rollback to 1.0.0, got %s", rolled.Version)
	}
	if _, err := cat.Refresh(ctx); err != nil {
		t.Fatalf("refresh after rollback: %v", err)
	}

	// 6. Publish 1.2.0 that adds a network host; auto-update must pause and wait for owner confirmation.
	publishIndex(Index{
		Schema:      1,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Entries: []Entry{
			{
				ID:           "community.plutotv.ether",
				Name:         "Pluto TV",
				Summary:      "Free live television channels.",
				Kind:         "livetv_source",
				Category:     "Live TV",
				Capabilities: []string{"livetv_channels"},
				Permissions: plugins.Permissions{
					Network: []string{"api.pluto.tv", "ads.evil.example"},
				},
				Versions: []Version{
					{Version: "1.0.0", URL: srv.URL + "/pluto-1.0.0.etherpkg", SHA256: shaV1},
					{Version: "1.1.0", URL: srv.URL + "/pluto-1.1.0.etherpkg", SHA256: shaV2Same},
					{Version: "1.2.0", URL: srv.URL + "/pluto-1.2.0.etherpkg", SHA256: shaV3More},
				},
			},
		},
	})
	if _, err := cat.Refresh(ctx); err != nil {
		t.Fatalf("refresh 1.2.0: %v", err)
	}
	rec, err = mgr.Get(ctx, "community.plutotv.ether")
	if err != nil {
		t.Fatalf("get after 1.2.0 refresh: %v", err)
	}
	if rec.Version != "1.1.0" || rec.UpdateVersion != "1.2.0" {
		t.Fatalf("expected update paused at 1.1.0 with pending 1.2.0, got version=%s updateVersion=%s", rec.Version, rec.UpdateVersion)
	}
	// Applying without accepting added permissions must fail.
	if _, err := mgr.ApplyUpdate(ctx, "community.plutotv.ether", false); err == nil {
		t.Fatalf("expected ApplyUpdate without acceptAdded to fail")
	}
	// Applying with acceptAdded=true succeeds.
	updated, err := mgr.ApplyUpdate(ctx, "community.plutotv.ether", true)
	if err != nil {
		t.Fatalf("ApplyUpdate with acceptAdded=true: %v", err)
	}
	if updated.Version != "1.2.0" {
		t.Fatalf("expected version 1.2.0 after ApplyUpdate, got %s", updated.Version)
	}

	// 7. Revoking the entry disables the installed plugin, records the reason, and emits a console notice.
	revokeReason := "Revoked due to upstream credential harvesting."
	publishIndex(Index{
		Schema:      1,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Entries: []Entry{
			{
				ID:      "community.plutotv.ether",
				Name:    "Pluto TV",
				Summary: "Free live television channels.",
				Kind:    "livetv_source",
				Revoked: true,
				Reason:  revokeReason,
				Versions: []Version{
					{Version: "1.2.0", URL: srv.URL + "/pluto-1.2.0.etherpkg", SHA256: shaV3More},
				},
			},
		},
	})
	if _, err := cat.Refresh(ctx); err != nil {
		t.Fatalf("refresh revoked: %v", err)
	}
	rec, err = mgr.Get(ctx, "community.plutotv.ether")
	if err != nil {
		t.Fatalf("get after revoke: %v", err)
	}
	if rec.Enabled {
		t.Fatalf("expected revoked plugin to be disabled")
	}

	detailResp, err := http.Get(apiSrv.URL + "/community.plutotv.ether")
	if err != nil {
		t.Fatalf("GET detail: %v", err)
	}
	var detail plugins.Detail
	if err := json.NewDecoder(detailResp.Body).Decode(&detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	_ = detailResp.Body.Close()
	if detail.Health != "disabled" || !strings.Contains(detail.HealthDetail.Reason, revokeReason) {
		t.Fatalf("expected disabled health with revoke reason, got health=%q reason=%q", detail.Health, detail.HealthDetail.Reason)
	}

	noticesMu.Lock()
	gotNotices := append([]string(nil), notices...)
	noticesMu.Unlock()
	foundRevokeNotice := false
	for _, msg := range gotNotices {
		if strings.Contains(msg, "Pluto TV") && strings.Contains(msg, revokeReason) {
			foundRevokeNotice = true
		}
	}
	if !foundRevokeNotice {
		t.Fatalf("expected revoked notice with reason, got %+v", gotNotices)
	}

	// Re-enabling a revoked plugin must be refused with ErrRevoked.
	if err := mgr.SetEnabled(ctx, "community.plutotv.ether", true); !errors.Is(err, plugins.ErrRevoked) {
		t.Fatalf("expected ErrRevoked when re-enabling revoked plugin, got %v", err)
	}
}

func TestCacheRequiresCurrentSigningKey(t *testing.T) {
	db := store.OpenTest(t)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"schema":1,"entries":[]}`)
	cache := cacheStore{db: db}
	if err := cache.save(context.Background(), Index{Schema: 1}, raw, SignDetached(raw, priv), time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := cache.load(context.Background(), pub); !ok {
		t.Fatal("valid cached index rejected")
	}
	other, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := cache.load(context.Background(), other); ok {
		t.Fatal("cached index trusted under a different key")
	}
	if _, err := db.Exec(`UPDATE plugins_catalogue_index SET raw_json = '{"schema":1,"entries":[] }'`); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := cache.load(context.Background(), pub); ok {
		t.Fatal("tampered cached index trusted")
	}
}
