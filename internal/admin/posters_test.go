package admin

import (
	"encoding/json"
	"github.com/braedonsaunders/ether/internal/config"
	"strings"
	"testing"
)

func TestPosterSecrets(t *testing.T) {
	c := config.Defaults(t.TempDir())
	c.Posters = config.Posters{Enabled: true, Source: "custom", APIKey: "private-key", URLTemplate: "http://lan/{type}/{tmdb_id}?secret=private-template"}
	redacted := RedactConfig(c)
	raw, _ := json.Marshal(redacted)
	if strings.Contains(string(raw), "private-") {
		t.Fatal("secret exposed")
	}
	restored, errs := UnredactConfig(redacted, c)
	if len(errs) > 0 || restored.Posters != c.Posters {
		t.Fatal(errs)
	}
	redacted.Posters.URLTemplate = "http://other/{type}/{tmdb_id}?key={api_key}"
	if _, errs = UnredactConfig(redacted, c); len(errs) == 0 {
		t.Fatal("forwarded retained key to new template")
	}
}

func TestXRDBSecretRedaction(t *testing.T) {
	c := config.Defaults(t.TempDir())
	c.Posters = config.Posters{Enabled: true, Source: "xrdb", BaseURL: "https://xrdb.example", Profile: "home", APIKey: "private-key"}
	r := RedactConfig(c)
	data, _ := json.Marshal(r)
	if strings.Contains(string(data), "private-key") {
		t.Fatal("key exposed")
	}
	restored, errs := UnredactConfig(r, c)
	if len(errs) != 0 || restored.Posters != c.Posters {
		t.Fatal(errs)
	}
	r.Posters.BaseURL = "https://other.example"
	if _, errs := UnredactConfig(r, c); len(errs) == 0 {
		t.Fatal("retained key sent to changed instance")
	}
}

func TestIndependentPosterAccountSecrets(t *testing.T) {
	c := config.Defaults(t.TempDir())
	c.Posters = config.Posters{Version: 1, Source: "tmdb"}
	c.Posters.Accounts.RPDB = config.PosterAccount{Enabled: true, APIKey: "rpdb-secret"}
	c.Posters.Accounts.Custom = config.PosterAccount{Enabled: true, APIKey: "custom-secret", URLTemplate: "https://example.com/{type}/{tmdb_id}?key={api_key}"}
	redacted := RedactConfig(c)
	if redacted.Posters.Accounts.RPDB.APIKey != RedactedSecret || redacted.Posters.Accounts.Custom.URLTemplate != RedactedSecret {
		t.Fatal("account secret leaked")
	}
	redacted.Posters.Source = "custom"
	restored, errs := UnredactConfig(redacted, c)
	if len(errs) > 0 || restored.Posters.Accounts != c.Posters.Accounts {
		t.Fatal("selecting a source lost another account's credentials", errs)
	}
}
