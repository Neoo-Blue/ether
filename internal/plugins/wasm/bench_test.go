package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
)

func benchEngine(b *testing.B) *Engine {
	b.Helper()
	e := NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { _ = e.Close() })
	return e
}

const benchJS = `
globalThis.handle = function(req) { return { status: 200, body: "ok:" + req.path }; };
globalThis.rank_key = function(c, cfg) {
  let s = 0;
  if (c.resolution >= 2160) s += 100; else if (c.resolution >= 1080) s += 50;
  if (c.cached) s += 10;
  if (c.hdr && c.hdr.indexOf("DV") >= 0) s += 5;
  return s + (c.size % 97);
};
`

func benchCandidates(prefix string, n int) []Candidate {
	cands := make([]Candidate, n)
	for i := range cands {
		cands[i] = Candidate{ReleaseID: fmt.Sprintf("%s-%d", prefix, i), Facts: json.RawMessage(fmt.Sprintf(
			`{"releaseId":"r%d","name":"Movie.2024.%dp.WEB-DL-GRP","size":%d,"resolution":%d,"hdr":["DV"],"cached":%t,"verified":true}`,
			i, []int{1080, 2160}[i%2], 8_000_000_000+i*1_000_003, []int{1080, 2160}[i%2], i%3 == 0))}
	}
	return cands
}

// BenchmarkColdStartJS measures load (compile cache warm, new QuickJS
// instance, source eval) plus the first served request.
func BenchmarkColdStartJS(b *testing.B) {
	e := benchEngine(b)
	for i := 0; i < b.N; i++ {
		p, err := e.LoadJS("bench.cold", "1.0.0", benchJS, nil, Limits{})
		if err != nil {
			b.Fatal(err)
		}
		if _, err := p.Call(context.Background(), Request{Method: "GET", Path: "/cold"}); err != nil {
			b.Fatal(err)
		}
		p.Close()
	}
}

func BenchmarkWarmHandleJS(b *testing.B) {
	e := benchEngine(b)
	p, err := e.LoadJS("bench.warm", "1.0.0", benchJS, nil, Limits{})
	if err != nil {
		b.Fatal(err)
	}
	defer p.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := p.Call(context.Background(), Request{Method: "GET", Path: "/warm"}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRankBatch500JS(b *testing.B) {
	e := benchEngine(b)
	p, err := e.LoadJS("bench.rank", "1.0.0", benchJS, nil, Limits{})
	if err != nil {
		b.Fatal(err)
	}
	defer p.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scores, out, err := p.RankBatch(context.Background(), benchCandidates(fmt.Sprintf("i%d", i), 500), nil)
		if err != nil {
			b.Fatal(err)
		}
		if out.SlowCandidates > 0 || len(scores) != 500 {
			b.Fatalf("slow=%d scores=%d", out.SlowCandidates, len(scores))
		}
	}
}

func BenchmarkRankBatch500Raw(b *testing.B) {
	e := benchEngine(b)
	mod, err := os.ReadFile("testdata/rawplugin.wasm")
	if err != nil {
		b.Fatal(err)
	}
	p, err := e.LoadWASM("bench.raw", "1.0.0", mod, nil, Limits{})
	if err != nil {
		b.Fatal(err)
	}
	defer p.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scores, out, err := p.RankBatch(context.Background(), benchCandidates(fmt.Sprintf("i%d", i), 500), nil)
		if err != nil {
			b.Fatal(err)
		}
		if out.SlowCandidates > 0 || len(scores) != 500 {
			b.Fatalf("slow=%d scores=%d", out.SlowCandidates, len(scores))
		}
	}
}

func BenchmarkCompileTS(b *testing.B) {
	files := map[string]string{"index.ts": `
export function handle(req: { path: string }): { status: number; body: string } {
  return { status: 200, body: "hi " + req.path };
}`}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		js, _, diags := Compile(files, "index.ts")
		if len(js) == 0 || len(diags) != 0 {
			b.Fatalf("diags=%v", diags)
		}
	}
}
