package main

import (
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/braedonsaunders/ether/internal/plugins/catalogue"
)

// cmdIndex compiles (when -dir is passed), validates, signs, or verifies a
// plugin catalogue index (index.json + index.json.sig).
//
// Usage:
//
//	ether-release-sign index -in index.json [-dir plugins/] [-out index.json] [-sig index.json.sig] -key <b64|file>
//	ether-release-sign index -verify -in index.json [-sig index.json.sig] -pub <b64|file>
func cmdIndex(args []string) error {
	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	in := fs.String("in", "index.json", "input index.json path")
	out := fs.String("out", "", "output index.json path (default: -in)")
	dir := fs.String("dir", "", "optional directory of per-plugin YAML files to compile into index.json")
	sigPath := fs.String("sig", "", "detached signature path (default: <out>.sig)")
	keySpec := fs.String("key", "", "ed25519 private key (base64, hex, or file; or $ETHER_CATALOGUE_PRIVATE_KEY / $ETHER_UPDATE_PRIVATE_KEY)")
	pubSpec := fs.String("pub", "", "ed25519 public key for -verify (base64, hex, or file; or $ETHER_CATALOGUE_PUBLIC_KEY / $ETHER_UPDATE_PUBLIC_KEY)")
	verify := fs.Bool("verify", false, "verify an existing index.json and detached signature")
	if err := fs.Parse(args); err != nil {
		return err
	}

	target := *out
	if target == "" {
		target = *in
	}
	if target == "" {
		target = "index.json"
	}
	sigOut := *sigPath
	if sigOut == "" {
		sigOut = target + ".sig"
	}

	if *verify {
		pub, err := loadCataloguePublicKey(*pubSpec)
		if err != nil {
			return err
		}
		rawIndex, err := os.ReadFile(*in)
		if err != nil {
			return err
		}
		rawSig, err := os.ReadFile(sigOut)
		if err != nil {
			return err
		}
		idx, err := catalogue.ParseAndVerify(rawIndex, rawSig, pub)
		if err != nil {
			return err
		}
		fmt.Printf("ok: %d catalogue entries verified (%s)\n", len(idx.Entries), *in)
		return nil
	}

	var rawIndex []byte
	if *dir != "" {
		idx, err := catalogue.LoadEntriesDir(*dir)
		if err != nil {
			return err
		}
		b, err := json.MarshalIndent(idx, "", "  ")
		if err != nil {
			return err
		}
		rawIndex = append(b, '\n')
		if err := os.WriteFile(target, rawIndex, 0o644); err != nil {
			return err
		}
	} else {
		b, err := os.ReadFile(*in)
		if err != nil {
			return err
		}
		if _, err := catalogue.ParseIndex(b); err != nil {
			return err
		}
		rawIndex = b
		if target != *in {
			if err := os.WriteFile(target, rawIndex, 0o644); err != nil {
				return err
			}
		}
	}

	// If no key is provided and -dir was used, allow unsigned compilation when -key is omitted?
	// Wait: `ether-release-sign index` signs when a key is present, or requires a key unless -unsigned is set.
	priv, err := loadCataloguePrivateKey(*keySpec)
	if err != nil {
		return err
	}
	sig := catalogue.SignDetached(rawIndex, priv)
	return os.WriteFile(sigOut, []byte(sig+"\n"), 0o644)
}

func loadCataloguePrivateKey(spec string) (ed25519.PrivateKey, error) {
	if spec == "" {
		spec = os.Getenv("ETHER_CATALOGUE_PRIVATE_KEY")
	}
	if spec == "" {
		spec = os.Getenv("ETHER_UPDATE_PRIVATE_KEY")
	}
	if spec == "" {
		return nil, fmt.Errorf("no private key: pass -key or set ETHER_CATALOGUE_PRIVATE_KEY")
	}
	if _, err := os.Stat(spec); err == nil {
		b, err := os.ReadFile(spec)
		if err != nil {
			return nil, err
		}
		s := strings.TrimSpace(string(b))
		for _, line := range strings.Split(s, "\n") {
			line = strings.TrimSpace(line)
			if v, ok := strings.CutPrefix(line, "ETHER_CATALOGUE_PRIVATE_KEY="); ok {
				s = v
				break
			}
			if v, ok := strings.CutPrefix(line, "ETHER_UPDATE_PRIVATE_KEY="); ok {
				s = v
				break
			}
		}
		spec = strings.TrimSpace(s)
	}
	return catalogue.ParsePrivateKey(spec)
}

func loadCataloguePublicKey(spec string) (ed25519.PublicKey, error) {
	if spec == "" {
		spec = os.Getenv("ETHER_CATALOGUE_PUBLIC_KEY")
	}
	if spec == "" {
		spec = os.Getenv("ETHER_UPDATE_PUBLIC_KEY")
	}
	if spec == "" {
		return nil, fmt.Errorf("no public key: pass -pub or set ETHER_CATALOGUE_PUBLIC_KEY")
	}
	if _, err := os.Stat(spec); err == nil {
		b, err := os.ReadFile(spec)
		if err != nil {
			return nil, err
		}
		s := strings.TrimSpace(string(b))
		for _, line := range strings.Split(s, "\n") {
			line = strings.TrimSpace(line)
			if v, ok := strings.CutPrefix(line, "ETHER_CATALOGUE_PUBLIC_KEY="); ok {
				s = v
				break
			}
			if v, ok := strings.CutPrefix(line, "ETHER_UPDATE_PUBLIC_KEY="); ok {
				s = v
				break
			}
		}
		spec = strings.TrimSpace(s)
	}
	return catalogue.ParsePublicKey(spec)
}
