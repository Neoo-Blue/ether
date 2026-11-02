package notify

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/store"
)

func upgradedSealer(t *testing.T) *config.Store {
	t.Helper()
	s, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpgradeCredentials(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func rawChannel(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	var raw string
	if err := db.QueryRow(`SELECT config FROM notify_channels WHERE id=?`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func smtpChannel() Channel {
	return Channel{ID: "mail", Name: "Mail", Kind: "smtp", Enabled: true, Events: []string{"test"},
		Secrets:  map[string]string{"endpoint": "smtp.test:587", "username": "smtp-user-value", "password": "smtp-password-value"},
		Settings: map[string]string{"from": "ether@example.test", "to": "owner@example.test"}}
}

func withEndpoint(c Channel, id, endpoint string) Channel {
	c.ID = id
	c.Secrets["endpoint"] = endpoint
	return c
}

func TestChannelSecretsSealedAtRest(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := New(db, Options{Sealer: upgradedSealer(t)})
	hook := Channel{ID: "hook", Name: "Hook", Kind: "webhook", Enabled: true, Events: []string{"test"}, Secrets: map[string]string{"endpoint": "https://hooks.test/secret-hook-path", "token": "hook-token-value"}}
	push := Channel{ID: "push", Name: "Push", Kind: "webpush", Enabled: true, Events: []string{"test"}}
	push.SetSubscription(&PushSubscription{Endpoint: "https://push.test/secret-subscription", Keys: PushKeys{Auth: "AAAAAAAAAAAAAAAAAAAAAA", P256dh: validP256dh(t)}})
	for _, c := range []Channel{smtpChannel(), hook, push} {
		if err := s.Save(ctx, c); err != nil {
			t.Fatalf("save %s: %v", c.ID, err)
		}
	}
	for _, id := range []string{"mail", "hook", "push"} {
		raw := rawChannel(t, db, id)
		for _, secret := range []string{"smtp-user-value", "smtp-password-value", "secret-hook-path", "hook-token-value", "secret-subscription", "AAAAAAAAAAAAAAAAAAAAAA"} {
			if strings.Contains(raw, secret) {
				t.Fatalf("channel %s stores %q in plaintext", id, secret)
			}
		}
	}
	cs, err := s.channels(ctx)
	if err != nil || len(cs) != 3 {
		t.Fatalf("channels: %v", err)
	}
	byID := map[string]Channel{}
	for _, c := range cs {
		byID[c.ID] = c
	}
	pushSub, err := byID["push"].Subscription()
	if err != nil {
		t.Fatal(err)
	}
	if byID["mail"].Secret("password") != "smtp-password-value" || byID["mail"].Secret("username") != "smtp-user-value" || byID["hook"].Secret("endpoint") != "https://hooks.test/secret-hook-path" || pushSub.Endpoint != "https://push.test/secret-subscription" {
		t.Fatalf("round trip: %+v", byID)
	}
	// The VAPID private key is sealed, bound to its public key.
	private, public, err := s.pushKeys(ctx)
	if err != nil || private == "" {
		t.Fatal(err)
	}
	var stored string
	if err := db.QueryRow(`SELECT private_key FROM notify_push_keys WHERE id='vapid'`).Scan(&stored); err != nil || strings.Contains(stored, private) || !config.IsSealed(stored) {
		t.Fatalf("push key at rest: %v", err)
	}
	s2 := New(db, Options{Sealer: s.opts.Sealer})
	if p2, pub2, err := s2.pushKeys(ctx); err != nil || p2 != private || pub2 != public {
		t.Fatal("push identity did not reopen")
	}
}

func TestChannelPlaceholderNotCarriedToNewEndpoint(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := New(db, Options{Sealer: upgradedSealer(t)})
	if err := s.Save(ctx, smtpChannel()); err != nil {
		t.Fatal(err)
	}
	same := smtpChannel()
	same.Secrets["password"], same.Name = Redacted, "Renamed"
	if err := s.Save(ctx, same); err != nil {
		t.Fatalf("placeholder with the same server: %v", err)
	}
	moved := smtpChannel()
	moved.Secrets["password"], moved.Secrets["endpoint"] = Redacted, "evil.test:587"
	if err := s.Save(ctx, moved); !errors.Is(err, errSecretNeedsDestination) {
		t.Fatalf("placeholder carried to another server: %v", err)
	}
	cs, _ := s.channels(ctx)
	if cs[0].Secret("endpoint") != "smtp.test:587" || cs[0].Secret("password") != "smtp-password-value" || cs[0].Name != "Renamed" {
		t.Fatalf("stored channel: %+v", cs[0])
	}
}

func TestChannelMigrationAndTampering(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Rows written before sealing.
	legacy := New(db, Options{})
	for _, c := range []Channel{smtpChannel(), withEndpoint(smtpChannel(), "mail2", "other.test:587")} {
		if err := legacy.Save(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := legacy.pushKeys(ctx); err != nil {
		t.Fatal(err)
	}
	s := New(db, Options{Sealer: upgradedSealer(t)})
	n, err := s.MigrateSecrets(ctx)
	if err != nil || n != 3 {
		t.Fatalf("migration: %d %v", n, err)
	}
	if strings.Contains(rawChannel(t, db, "mail"), "smtp-password-value") {
		t.Fatal("legacy row still plaintext")
	}
	if n, err := s.MigrateSecrets(ctx); err != nil || n != 0 {
		t.Fatalf("rerun: %d %v", n, err)
	}
	// A sealed password moved to a channel with another server fails
	// closed, and the migration leaves the row as it is.
	mail := rawChannel(t, db, "mail")
	c, _, err := s.decodeChannel(mail)
	if err != nil {
		t.Fatal(err)
	}
	sealed, _ := s.sealChannel(c)
	other, _ := s.sealChannel(withEndpoint(smtpChannel(), "mail2", "other.test:587"))
	other.Secrets["password"] = sealed.Secrets["password"]
	tampered, _ := jsonString(other)
	if _, err := db.Exec(`UPDATE notify_channels SET config=? WHERE id='mail2'`, tampered); err != nil {
		t.Fatal(err)
	}
	if _, err := s.channels(ctx); err == nil {
		t.Fatal("moved password opened")
	}
	if _, err := s.MigrateSecrets(ctx); err == nil {
		t.Fatal("migration hid an unreadable row")
	}
	if rawChannel(t, db, "mail2") != tampered {
		t.Fatal("unreadable row rewritten")
	}
	// Without a key, sealed rows are an error rather than garbage.
	if _, err := New(db, Options{}).channels(ctx); !errors.Is(err, config.ErrSealedWithoutKey) {
		t.Fatalf("nil sealer: %v", err)
	}
}

func validP256dh(t *testing.T) string {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
}

func jsonString(c Channel) (string, error) {
	b, err := json.Marshal(c)
	return string(b), err
}
