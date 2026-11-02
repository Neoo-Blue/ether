package dublanguage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/plugins/studio"
	"github.com/braedonsaunders/ether/internal/plugins/wasm"
	"github.com/braedonsaunders/ether/internal/resolver"
	"github.com/braedonsaunders/ether/internal/store"
)

func files(t *testing.T) (plugins.Manifest, []byte, []byte) {
	t.Helper()
	raw, e := os.ReadFile("manifest.json")
	if e != nil {
		t.Fatal(e)
	}
	m, e := plugins.ParseManifest(raw)
	if e != nil {
		t.Fatal(e)
	}
	js, e := os.ReadFile("dist/plugin.js")
	if e != nil {
		t.Fatal(e)
	}
	return m, raw, js
}
func TestHostedRanking500(t *testing.T) {
	m, _, js := files(t)
	eng := wasm.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer eng.Close()
	rt, err := plugins.HostedRuntimeFactory(eng)(plugins.Installed{ID: m.ID, Manifest: m, Files: func(p string) ([]byte, bool) { return js, p == "dist/plugin.js" }})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	hot := rt.(plugins.HotRuntime)
	c := make([]plugins.CandidateFacts, 500)
	for i := range c {
		c[i] = plugins.CandidateFacts{ReleaseID: fmt.Sprint(i), Name: "fixture", Languages: []string{"de"}}
		if i%2 == 0 {
			c[i].Languages = []string{"fr"}
		}
	}
	keys, out, err := hot.RankBatch(context.Background(), c, json.RawMessage(`{"value":["fr","de"]}`))
	if err != nil {
		t.Fatalf("rank: %+v %v", out, err)
	}
	for i, k := range keys {
		want := int64(1)
		if i%2 == 0 {
			want = 2
		}
		if k != want {
			t.Fatalf("key %d = %d", i, k)
		}
	}
}
func TestStudioReplayChangesPick(t *testing.T) {
	_, raw, js := files(t)
	db := store.OpenTest(t)
	eng := wasm.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer eng.Close()
	runner := studio.NewRunner(studio.ReplayDeps{DB: db, Engine: eng})
	defer runner.Close()
	tr := resolver.Trace{TitleName: "Fixture", Profile: "default", At: time.Now(), Winner: "a", Candidates: []resolver.CandidateTrace{{ReleaseID: "a", Name: "Film.1080p.WEB-DL", Score: 100, Height: 1080}, {ReleaseID: "b", Name: "Film.FRENCH.DUB.1080p.WEB-DL", Score: 90, Height: 1080}}}
	encoded, _ := json.Marshal(tr)
	_, err := db.Exec(`INSERT INTO resolver_history(id,resolved_at,rating_key,title_name,profile,release_id,release_name,indexer,size,error,failover,pinned,candidates,trace) VALUES (1,1,1,'Fixture','default','a','Film.1080p.WEB-DL','test',1,'',0,0,2,?)`, string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	pos := 0
	rep, err := runner.Replay(context.Background(), studio.ReplayRequest{DevInstance: &studio.DevInstance{Manifest: raw, Files: map[string]string{"dist/plugin.js": string(js)}}, Kind: studio.ReplayRanking, Params: studio.ReplayParams{StepKey: "dub_language", Position: &pos, Config: json.RawMessage(`{"value":["fr"]}`)}})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Ranking == nil || rep.Ranking.ChangedCount != 1 {
		t.Fatalf("replay: %+v", rep.Ranking)
	}
}
