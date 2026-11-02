package plugins

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type fakeHot struct{ *fakeRuntime }

func (fakeHot) RankBatch(context.Context, []CandidateFacts, json.RawMessage) ([]int64, HotOutcome, error) {
	return nil, HotOutcome{Status: "ok"}, nil
}
func (fakeHot) CondMatch(context.Context, CandidateFacts, string, json.RawMessage) (bool, HotOutcome, error) {
	return false, HotOutcome{Status: "ok"}, nil
}
func (fakeHot) HasRankKey() bool    { return true }
func (fakeHot) HasCondMatch() bool  { return false }
func (fakeHot) Budgets() HotBudgets { return HotBudgets{} }

func TestHotReadyNeverBlocksAndWarmsInBackground(t *testing.T) {
	env := newTestEnv(t)
	release := make(chan struct{})
	env.m.RegisterRuntime("hosted", func(inst Installed) (Runtime, error) {
		<-release // a slow load: compiling, reading artifacts
		return fakeHot{env.fake}, nil
	})
	rec := env.installHello(t, "1.0.0")

	got := make(chan bool, 1)
	go func() {
		_, ok := env.m.HotReady(rec.ID)
		got <- ok
	}()
	select {
	case ok := <-got:
		if ok {
			t.Fatal("runtime cannot be ready before it has loaded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("HotReady blocked on a runtime load")
	}

	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if hot, ok := env.m.HotReady(rec.ID); ok {
			if !hot.HasRankKey() {
				t.Fatal("warmed runtime lost its capabilities")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("runtime never finished warming")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := env.m.SetShadow(context.Background(), rec.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, ok := env.m.HotReady(rec.ID); ok {
		t.Fatal("a plugin in shadow mode must not change real picks")
	}
	if err := env.m.SetShadow(context.Background(), rec.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := env.m.HotReady(rec.ID); !ok {
		t.Fatal("leaving shadow mode restores the plugin")
	}

	if err := env.m.SetEnabled(context.Background(), rec.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := env.m.HotReady(rec.ID); ok {
		t.Fatal("a disabled plugin must not serve the hot path")
	}
}

func TestPlainStremioManifestUsesTheRemoteRuntime(t *testing.T) {
	env := newTestEnv(t)
	plain := Record{ID: "community.stremio.addon", Manifest: Manifest{ID: "community.stremio.addon", Version: "1.0.0", Name: "Add-on"}}
	if got := plain.Manifest.RuntimeKind(); got != "remote" {
		t.Fatalf("runtime kind = %q, want remote", got)
	}
	rt, err := env.m.runtimeFor(context.Background(), plain)
	if err != nil || rt == nil {
		t.Fatalf("a plain Stremio add-on must open on the remote runtime: %v", err)
	}
	if env.fake.inst.ID != plain.ID {
		t.Fatalf("remote factory opened %q", env.fake.inst.ID)
	}
}
