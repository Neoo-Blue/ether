package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdIndexSignAndVerify(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pubB64 := base64.StdEncoding.EncodeToString(pub)
	privB64 := base64.StdEncoding.EncodeToString(priv)

	tmp := t.TempDir()
	pluginsDir := filepath.Join(tmp, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yamlEntry := `id: community.plutotv.ether
name: Pluto TV
summary: Free live television channels.
kind: livetv_source
category: Live TV
capabilities:
  - livetv_channels
permissions:
  network:
    - api.pluto.tv
versions:
  - version: 1.0.0
    url: https://catalogue.example.invalid/pluto-1.0.0.etherpkg
    sha256: ` + strings.Repeat("a", 64) + `
`
	if err := os.WriteFile(filepath.Join(pluginsDir, "community.plutotv.ether.yaml"), []byte(yamlEntry), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	outIndex := filepath.Join(tmp, "index.json")
	if err := cmdIndex([]string{"-dir", pluginsDir, "-out", outIndex, "-key", privB64}); err != nil {
		t.Fatalf("cmdIndex sign from dir: %v", err)
	}

	if _, err := os.Stat(outIndex); err != nil {
		t.Fatalf("expected index.json written: %v", err)
	}
	if _, err := os.Stat(outIndex + ".sig"); err != nil {
		t.Fatalf("expected index.json.sig written: %v", err)
	}

	// Verify succeeds with matching public key.
	if err := cmdIndex([]string{"-verify", "-in", outIndex, "-pub", pubB64}); err != nil {
		t.Fatalf("cmdIndex verify: %v", err)
	}

	// Tampering with index.json causes verify to fail.
	raw, _ := os.ReadFile(outIndex)
	_ = os.WriteFile(outIndex, append(raw, ' '), 0o644)
	if err := cmdIndex([]string{"-verify", "-in", outIndex, "-pub", pubB64}); err == nil {
		t.Fatalf("expected cmdIndex verify to fail on tampered index.json")
	}
}

func TestCmdIndexCheckWithoutSigningKey(t *testing.T) {
	t.Setenv("ETHER_CATALOGUE_PRIVATE_KEY", "")
	t.Setenv("ETHER_UPDATE_PRIVATE_KEY", "")
	dir := t.TempDir()
	index := filepath.Join(dir, "index.json")
	if err := cmdIndex([]string{"-check", "-dir", dir, "-out", index}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"entries": []`) {
		t.Fatalf("expected empty array, got %s", data)
	}
	if _, err := os.Stat(index + ".sig"); !os.IsNotExist(err) {
		t.Fatalf("unexpected signature: %v", err)
	}
}
