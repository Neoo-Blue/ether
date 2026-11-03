package adapters_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/plugins/adapters"
	"github.com/braedonsaunders/ether/internal/subtitles"
)

func TestSubtitleSearchPickAndLifecycle(t *testing.T) {
	h := newLiveTVTestHarness(t)
	svc := subtitles.New(subtitles.Options{CacheDir: t.TempDir()})
	adapters.AttachSubtitles(h.mgr, svc)
	man := map[string]any{"id": "community.subtitle.test", "version": "1.0.0", "name": "Test subtitles", "ether": map[string]any{"protocol": 1, "kind": "integration", "summary": "Subtitle provider for testing", "runtime": map[string]any{"kind": "hosted", "entry": "dist/plugin.js"}, "capabilities": []string{"subtitles"}, "contributes": map[string]any{"integration": map[string]any{"category": "media", "adds": []string{"Subtitles"}, "connect": "none"}, "subtitles": map[string]any{"languages": []string{"en"}, "formats": []string{"srt"}}}}}
	raw, _ := json.Marshal(man)
	ctx := context.Background()
	preview, err := h.mgr.PreviewInstall(ctx, plugins.Source{Kind: "studio", Manifest: raw, Files: map[string][]byte{"dist/plugin.js": []byte("// fixture")}})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := h.mgr.Install(ctx, preview, plugins.InstallOptions{AcceptAddedPermissions: true})
	if err != nil {
		t.Fatal(err)
	}
	h.rt.routes["/subtitles/"] = func(req plugins.Request) (plugins.Response, error) {
		if req.Headers["X-Ether-Pick"] != "" {
			t.Error("listing had pick")
		}
		return plugins.Response{Status: 200, Body: json.RawMessage(`{"subtitles":[{"id":"sub1","lang":"en","format":"srt","name":"episode.srt"}]}`)}, nil
	}
	h.rt.routes["/ether/subtitles/download"] = func(req plugins.Request) (plugins.Response, error) {
		if req.Headers["X-Ether-Pick"] == "" {
			t.Error("download has no pick")
		}
		return plugins.Response{Status: 200, Body: json.RawMessage(`{"format":"srt","encoding":"utf8","data":"1\n00:00:01,000 --> 00:00:02,000\nHello\n"}`)}, nil
	}
	title := &core.Title{Key: core.MovieKey(99), Type: core.TypeMovie, IDs: core.IDs{IMDB: "tt1234567"}}
	h.rt.resetCalls()
	candidates, err := svc.SearchOnDemand(ctx, title, "en", "")
	if err != nil || len(candidates) != 1 {
		t.Fatalf("search: %+v %v", candidates, err)
	}
	for _, p := range h.rt.paths() {
		if strings.Contains(p, "download") {
			t.Fatal("downloaded during search")
		}
	}
	if _, err := svc.DownloadOnDemand(ctx, title, candidates[0].FileID); err != nil {
		t.Fatal(err)
	}
	if err := h.mgr.SetEnabled(ctx, rec.ID, false); err != nil {
		t.Fatal(err)
	}
	candidates, err = svc.SearchOnDemand(ctx, title, "en", "")
	if err != nil || len(candidates) != 0 {
		t.Fatalf("disabled provider: %+v %v", candidates, err)
	}
}
