package extras

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/tmdb"
)

// fakePATH swaps PATH for a temp dir holding the named dummy executables,
// so the default exec.LookPath resolves exactly those tools.
func fakePATH(t *testing.T, names ...string) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		if runtime.GOOS == "windows" {
			name += ".exe" // exec.LookPath only finds PATHEXT extensions there
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

func TestDefaultResolverFromPath(t *testing.T) {
	fakePATH(t, "yt-dlp")
	vs := fakeVideos{[]tmdb.Video{{Site: "YouTube", Key: "trlOffEN001", Type: "Trailer", Official: true}}}
	s := New(Options{Videos: vs, Config: func() config.Extras {
		return config.Extras{Enabled: true}
	}})
	if !s.Enabled() || s.DisabledReason() != "" {
		t.Fatalf("yt-dlp on PATH should enable trailers: enabled=%v reason=%q", s.Enabled(), s.DisabledReason())
	}
	got, err := s.List(context.Background(), movie(), "en")
	if err != nil || len(got) != 1 {
		t.Fatalf("list with default resolver: %v %v", got, err)
	}
}

func TestDefaultResolverMissingFromPath(t *testing.T) {
	fakePATH(t)
	s := New(Options{Config: func() config.Extras {
		return config.Extras{Enabled: true}
	}})
	if s.Enabled() {
		t.Fatal("no yt-dlp on PATH and no configured command should stay off")
	}
	if !strings.Contains(s.DisabledReason(), "yt-dlp") {
		t.Fatalf("reason should name yt-dlp: %q", s.DisabledReason())
	}
}

func TestResolveUsesDefaultResolver(t *testing.T) {
	fakePATH(t, "yt-dlp")
	var ranName string
	var ranArgs []string
	s := New(Options{
		Config: func() config.Extras { return config.Extras{Enabled: true} },
		Runner: func(_ context.Context, name string, args []string) ([]byte, error) {
			ranName, ranArgs = name, args
			return []byte("https://cdn.example/v.mp4\n"), nil
		},
	})
	r, err := s.Resolve(context.Background(), Extra{Source: SourceCommand, Site: "YouTube", SiteKey: "trlOffEN001"})
	if err != nil {
		t.Fatal(err)
	}
	if r.URL != "https://cdn.example/v.mp4" || ranName != "yt-dlp" {
		t.Fatalf("resolved=%+v ran=%q", r, ranName)
	}
	if len(ranArgs) == 0 || ranArgs[len(ranArgs)-1] != "https://www.youtube.com/watch?v=trlOffEN001" {
		t.Fatalf("default args not used: %v", ranArgs)
	}
}

func TestConfiguredCommandBeatsDefault(t *testing.T) {
	fakePATH(t, "yt-dlp", "my-resolver")
	var ranName string
	s := New(Options{
		Config: enabledCfg("my-resolver"),
		Runner: func(_ context.Context, name string, _ []string) ([]byte, error) {
			ranName = name
			return []byte("https://cdn.example/v.mp4\n"), nil
		},
	})
	if _, err := s.Resolve(context.Background(), Extra{Source: SourceCommand, Site: "YouTube", SiteKey: "trlOffEN001"}); err != nil {
		t.Fatal(err)
	}
	if ranName != "my-resolver" {
		t.Fatalf("configured command should win, ran %q", ranName)
	}
}
