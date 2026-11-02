package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestSubtitlePickEnforcedAndSingleUse(t *testing.T) {
	e := newTestEnv(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e.m.now = func() time.Time { return now }
	req := Request{Method: "POST", Path: "/ether/subtitles/download", Body: json.RawMessage(`{"id":"sub","lang":"en","format":"srt"}`), Headers: map[string]string{"X-Ether-Title": "title"}}
	if resp, err := e.m.Call(context.Background(), "plugin", req); !errors.Is(err, ErrPermissionDenied) || resp.Status != 403 {
		t.Fatalf("missing pick: %+v %v", resp, err)
	}
	token, err := e.m.MintSubtitlePick("plugin", "title", "sub")
	if err != nil {
		t.Fatal(err)
	}
	if !e.m.consumeSubtitlePick(token, "plugin", "title", "sub") {
		t.Fatal("valid token rejected")
	}
	if e.m.consumeSubtitlePick(token, "plugin", "title", "sub") {
		t.Fatal("token reused")
	}
	token, _ = e.m.MintSubtitlePick("plugin", "title", "sub")
	now = now.Add(time.Minute)
	if e.m.consumeSubtitlePick(token, "plugin", "title", "sub") {
		t.Fatal("expired token accepted")
	}
	token, _ = e.m.MintSubtitlePick("plugin", "title", "sub")
	if e.m.consumeSubtitlePick(token, "plugin", "other", "sub") {
		t.Fatal("wrong title accepted")
	}
	if e.m.consumeSubtitlePick(token, "plugin", "title", "sub") {
		t.Fatal("mismatched token reused")
	}
}
