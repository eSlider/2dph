package bench

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeSearcher returns deterministic hits per query, optionally sleeping.
type fakeSearcher struct {
	mu       sync.Mutex
	byQ      map[string][]Hit
	delay    time.Duration
	searches int
}

func (f *fakeSearcher) Search(_ context.Context, q string, limit int) ([]Hit, error) {
	f.mu.Lock()
	f.searches++
	f.mu.Unlock()
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	hits := f.byQ[q]
	if limit < len(hits) {
		hits = hits[:limit]
	}
	return hits, nil
}

func (f *fakeSearcher) Close() error { return nil }

func (f *fakeSearcher) Name() string { return "fake" }

func goldenWith(queries ...GoldenEntry) *GoldenSet {
	return &GoldenSet{Version: 1, Queries: queries}
}

func TestRunnerBaseline(t *testing.T) {
	fs := &fakeSearcher{byQ: map[string][]Hit{
		"alpha": {{ID: "a1", Text: "BM25 is here"}},
		"beta":  {{ID: "b1", Text: "no fragment"}},
	}}
	g := goldenWith(
		GoldenEntry{Query: "alpha", Topic: "docs", Lang: "en", Fragment: "bm25"},
		GoldenEntry{Query: "beta", Topic: "docs", Lang: "en", Fragment: "missing"},
	)
	r := &Runner{Golden: g, Searcher: fs, Workers: 1, Limit: 5}
	rep, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Queries != 2 || rep.Failed != 0 {
		t.Fatalf("Queries=%d Failed=%d", rep.Queries, rep.Failed)
	}
	if rep.Latency.N != 2 {
		t.Errorf("latency N=%d, want 2", rep.Latency.N)
	}
	if rep.Recall.Total != 2 || rep.Recall.Recalled != 1 {
		t.Errorf("recall = %+v, want 1/2", rep.Recall)
	}
	if len(rep.PerQuery) != 2 || rep.PerQuery[0].Entry.Query != "alpha" {
		t.Errorf("per-query order broken: %+v", rep.PerQuery)
	}
}

func TestRunnerCollectsErrors(t *testing.T) {
	fail := &failSearcher{err: context.DeadlineExceeded}
	g := goldenWith(GoldenEntry{Query: "q", Topic: "docs", Lang: "en", Fragment: "x"})
	r := &Runner{Golden: g, Searcher: fail, Workers: 1, Limit: 5}
	rep, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Failed != 1 {
		t.Errorf("Failed=%d, want 1 (per-query error must not kill the run)", rep.Failed)
	}
	if rep.Latency.N != 0 {
		t.Errorf("latency N=%d, want 0 (failed query contributes no sample)", rep.Latency.N)
	}
	if rep.Recall.Score != 0 {
		t.Errorf("recall must be 0 when the only query failed")
	}
}

type failSearcher struct {
	err error
}

func (f *failSearcher) Search(context.Context, string, int) ([]Hit, error) {
	return nil, f.err
}
func (f *failSearcher) Close() error { return nil }
func (f *failSearcher) Name() string { return "fail" }

func TestRunnerWorkersBounded(t *testing.T) {
	fs := &fakeSearcher{byQ: map[string][]Hit{"q": {{ID: "1", Text: "x"}}}, delay: 20 * time.Millisecond}
	queries := make([]GoldenEntry, 0, 10)
	for i := 0; i < 10; i++ {
		queries = append(queries, GoldenEntry{Query: "q", Topic: "docs", Lang: "en"})
	}
	g := goldenWith(queries...)
	start := time.Now()
	r := &Runner{Golden: g, Searcher: fs, Workers: 4, Limit: 5}
	rep, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if rep.Latency.N != 10 {
		t.Errorf("N=%d, want 10", rep.Latency.N)
	}
	// 10 × 20ms with 4 workers ≈ 60ms; sequential would be 200ms. Allow slack.
	if elapsed > 150*time.Millisecond {
		t.Errorf("4 workers took %v, want ~60ms (bounded pool)", elapsed)
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.searches != 10 {
		t.Errorf("searches=%d, want 10", fs.searches)
	}
}

func TestRunnerNoGolden(t *testing.T) {
	r := &Runner{Searcher: &fakeSearcher{byQ: map[string][]Hit{}}}
	if _, err := r.Run(context.Background()); err == nil {
		t.Error("empty golden must error")
	}
}
