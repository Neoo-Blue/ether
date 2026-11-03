package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/plextv"
)

func TestCacheSnapshotExplainsReleasesAndOwnerPin(t *testing.T) {
	a := setupTestApp(t, true)
	if err := a.users.SetOwner(context.Background(), plextv.Account{ID: 42, Username: "owner"}, "test-owner"); err != nil {
		t.Fatal(err)
	}
	if err := a.cache.Put("release", 0, []byte("cached")); err != nil {
		t.Fatal(err)
	}
	a.cache.RecordProvenance(core.CacheProvenance{ReleaseID: "release", File: "Film.mkv", Source: "Release.Name", Title: "Film", Size: 100, Cause: core.CacheCausePlaying})

	call := func(method, path, body, csrf string) *httptest.ResponseRecorder {
		var reader *strings.Reader
		if body == "" {
			reader = strings.NewReader("")
		} else {
			reader = strings.NewReader(body)
		}
		r := httptest.NewRequest(method, "http://localhost"+path, reader)
		r.RemoteAddr = "127.0.0.1:1000"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Plex-Token", "test-owner")
		if csrf != "" {
			r.Header.Set("X-Ether-Request", csrf)
		}
		w := httptest.NewRecorder()
		a.handler.ServeHTTP(w, r)
		return w
	}
	if w := call("POST", "/ether/api/cache/pin", `{"releaseId":"release","pinned":true}`, "1"); w.Code != 204 {
		t.Fatalf("pin %d %s", w.Code, w.Body)
	}
	w := call("GET", "/ether/api/cache/", "", "")
	if w.Code != 200 {
		t.Fatalf("snapshot %d %s", w.Code, w.Body)
	}
	var snapshot struct {
		Releases []struct {
			ReleaseID string `json:"ReleaseID"`
			Reason    *struct {
				Causes    []core.CacheCause `json:"causes"`
				Retention struct {
					Kind string   `json:"kind"`
					Pins []string `json:"pins"`
				} `json:"retention"`
				File   string `json:"file"`
				Source string `json:"source"`
			} `json:"reason"`
		} `json:"releases"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, r := range snapshot.Releases {
		if r.ReleaseID != "release" {
			continue
		}
		found = true
		if r.Reason == nil || r.Reason.File != "Film.mkv" || r.Reason.Source != "Release.Name" {
			t.Fatalf("reason = %+v", r.Reason)
		}
		codes := map[string]bool{}
		for _, c := range r.Reason.Causes {
			codes[c.Code] = true
		}
		if !codes[core.CacheCausePlaying] || !codes[core.CacheCauseOwnerPin] {
			t.Fatalf("causes = %+v", r.Reason.Causes)
		}
		if r.Reason.Retention.Kind != "pinned" || len(r.Reason.Retention.Pins) == 0 {
			t.Fatalf("retention = %+v", r.Reason.Retention)
		}
	}
	if !found {
		t.Fatal("release missing from snapshot")
	}
	for _, key := range []string{`"Username"`, `"UserID"`, `"username"`, `"userId"`, `"accountId"`} {
		if strings.Contains(w.Body.String(), key) {
			t.Fatalf("snapshot exposes %s", key)
		}
	}
}
