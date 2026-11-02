package config

// Plugins configures the signed plugin catalogue (SPEC WS12). Ether ships
// no third-party catalogue URL; the owner sets IndexURL and PublicKey when
// connecting a catalogue they trust.
type Plugins struct {
	// IndexURL is the URL of index.json (with a sibling index.json.sig).
	// Empty disables automatic catalogue fetches.
	IndexURL string `json:"indexUrl,omitempty"`
	// PublicKey is the base64-encoded ed25519 public key that signs index.json.
	PublicKey string `json:"publicKey,omitempty"`
}
