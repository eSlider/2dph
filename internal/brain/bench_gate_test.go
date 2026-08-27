//go:build cgo && system_ladybug

// Offline CI gate for the search benchmark harness (issue #202): builds a
// synthetic fixture DB (no model, no network), runs a mini golden-set through
// the real FTS query path via the bench Runner, and asserts the recall@5 gate
// (>= 0.95) that bin/brain/bench.go enforces in production.
package brain

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eSlider/2dph/internal/brain/bench"
)

// benchFixtureLeafs returns synthetic leafs (Alice/Bob, no PII): each text
// carries one golden fragment so the paired query recalls via FTS.
func benchFixtureLeafs(queries []bench.GoldenEntry) []LeafInput {
	leafs := make([]LeafInput, 0, len(queries))
	for i, q := range queries {
		emb := make([]float64, EmbedDim)
		emb[i%EmbedDim] = 0.5 // deterministic, model-free
		leafs = append(leafs, LeafInput{
			Text:   fmt.Sprintf("Fixture %d: %s. %s — synthetic Alice/Bob data.", i+1, q.Query, q.Fragment),
			Source: "bench-fixture.md",
			Root:   "info",
			Type:   "reference",
			How:    "bench-gate-test",
			Embedding: emb,
		})
	}
	return leafs
}

// miniGolden picks a representative slice of the committed golden-set.
func miniGolden(t *testing.T, n int) []bench.GoldenEntry {
	t.Helper()
	full, err := bench.LoadGolden(filepath.Join("testdata", "golden-set.json"))
	if err != nil {
		t.Fatal(err)
	}
	if n > len(full.Queries) {
		n = len(full.Queries)
	}
	// spread over topics: step so ru/en and all topics appear
	step := len(full.Queries) / n
	if step < 1 {
		step = 1
	}
	out := make([]bench.GoldenEntry, 0, n)
	for i := 0; i < len(full.Queries) && len(out) < n; i += step {
		out = append(out, full.Queries[i])
	}
	return out
}

func TestBenchGateRecallOffline(t *testing.T) {
	queries := miniGolden(t, 12)
	golden := &bench.GoldenSet{Version: 1, Queries: queries}

	dir := t.TempDir()
	dbpath := filepath.Join(dir, "kb.lbug")
	db, conn, err := OpenWritable(dbpath)
	if err != nil {
		t.Fatal(err)
	}
	if err := InitSchema(conn); err != nil {
		t.Fatal(err)
	}
	if _, err := AddLeafs(conn, benchFixtureLeafs(queries)); err != nil {
		t.Fatal(err)
	}
	if err := EnsureIndexes(conn); err != nil {
		t.Fatal(err)
	}
	db.Close()
	conn.Close()

	// Point the read path at the fixture, then open the read handle.
	prev := dbPathFn
	dbPathFn = func() string { return dbpath }
	defer func() { dbPathFn = prev }()
	if err := openBrain(); err != nil {
		t.Fatalf("openBrain fixture: %v", err)
	}
	defer closeBrain()

	// The bench searcher over the real FTS query path (eval.go pattern:
	// fragment recall needs no embedding).
	searcher := ftsBenchSearcher{}
	runner := &bench.Runner{Golden: golden, Searcher: searcher, Workers: 2, Limit: 5}
	rep, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Recall.Total != len(queries) {
		t.Errorf("gate queries=%d, want %d", rep.Recall.Total, len(queries))
	}
	if rep.Recall.Score < bench.DefaultMinRecall5 {
		t.Errorf("recall@5=%.3f < %.2f: bench gate would FAIL", rep.Recall.Score, bench.DefaultMinRecall5)
	}
	if rep.Failed != 0 {
		t.Errorf("failed=%d, want 0", rep.Failed)
	}
}

// ftsBenchSearcher adapts queryFTS to the bench Searcher interface.
type ftsBenchSearcher struct{}

func (ftsBenchSearcher) Search(_ context.Context, q string, limit int) ([]bench.Hit, error) {
	hits, err := queryFTS(q, limit)
	if err != nil {
		return nil, err
	}
	out := make([]bench.Hit, 0, len(hits))
	for _, h := range hits {
		out = append(out, bench.Hit{ID: h.ID, Text: h.Text, Root: h.Root})
	}
	return out, nil
}

func (ftsBenchSearcher) Close() error { return nil }

func (ftsBenchSearcher) Name() string { return "fts-fixture" }

var _ = strings.ToLower // keep strings imported if the fixture set shrinks
