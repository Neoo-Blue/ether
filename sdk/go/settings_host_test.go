package etherplugin

import (
	"context"
	"encoding/json"
	"testing"
)

type currentSettingsHost struct {
	*MockHost
	word string
}

func (h *currentSettingsHost) SettingsGet(context.Context) (map[string]any, error) {
	return map[string]any{"word": h.word}, nil
}
func TestCurrentHostSettingsOnEveryRequest(t *testing.T) {
	h := &currentSettingsHost{MockHost: NewMockHost(), word: "one"}
	p := New(Definition{Manifest: Manifest{ID: "test.settings", Name: "Settings", Version: "1", Ether: &EtherManifestBlock{Settings: &SettingsSchema{Fields: []SettingField{{Key: "word", Kind: "text"}}}}}, Host: h, Routes: Routes{Test: func(ctx *Context) (TestResponse, error) {
		return TestResponse{OK: true, Message: ctx.Settings["word"].(string)}, nil
	}}})
	for _, want := range []string{"one", "two"} {
		h.word = want
		r := p.Dispatch(context.Background(), RawRequest{Method: "POST", Path: "/ether/test"})
		var body TestResponse
		if err := json.Unmarshal(r.Body, &body); err != nil || body.Message != want {
			t.Fatalf("response %s: %s %v", want, r.Body, err)
		}
	}
}
