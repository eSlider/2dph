//go:build cgo && system_ladybug

// A/B gate for the ANN candidate (issue #204): bench --inproc (baseline =
// linear scan) vs --candidate inproc-ann (same process, same DB, ANN vector
// path) on a synthetic fixture. Mirrors the production A/B the PO gates on:
// candidate recall@5 vs baseline >= 0.95 and the latency ratio gate.
package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/eSlider/2dph/internal/brain/ann"
	"github.com/eSlider/2dph/internal/brain/bench"
)

// annBenchLeafs: n leafs whose texts carry golden fragments (FTS recall, the
// bench_gate_test pattern) and whose embeddings cluster by the same topic
// (ANN vector recall). All synthetic, model-free, deterministic.
func annBenchLeafs(n int, queries []bench.GoldenEntry) []LeafInput {
	rng := rand.New(rand.NewSource(204))
	const dim = EmbedDim
	centroids := make([][]float64, len(queries))
	for c := range centroids {
		v := make([]float64, dim)
		for i := range v {
			v[i] = rng.Float64()*2 - 1
		}
		norm := 0.0
		for _, f := range v {
			norm += f * f
		}
		for i := range v {
			v[i] /= sqrt64(norm)
		}
		centroids[c] = v
	}
	leafs := make([]LeafInput, n)
	for i := range leafs {
		c := i % len(queries)
		emb := make([]float64, dim)
		copy(emb, centroids[c])
		for j := 0; j < dim; j += 8 {
			emb[j] += (rng.Float64() - 0.5) * 0.02
		}
		leafs[i] = LeafInput{
			Text:      fmt.Sprintf("ANN bench fixture %d: %s. %s — synthetic Alice/Bob data.", i+1, queries[c].Query, queries[c].Fragment),
			Source:    "ann-bench-fixture.md",
			Root:      "info",
			Type:      "reference",
			How:       "ann-bench-gate-test",
			Embedding: emb,
		}
	}
	return leafs
}

func TestBenchANNInprocCandidateGate(t *testing.T) {
	queries := miniGolden(t, 12)
	// Model-free: stub the query embed so the gate runs on CI runners with no
	// model in the HF cache (write-path step precedes the HF-cache warm step).
	// A fixed deterministic vector is enough — recall comes from FTS (leaf
	// texts carry the query fragments); ANN parity is asserted by the vector
	// path over the fixture index, not by real embeddings.
	prevEmbed := embedQueryFn
	embedQueryFn = func(_ string) ([]float64, error) {
		vec := make([]float64, EmbedDim)
		vec[0] = 0.5
		return vec, nil
	}
	defer func() { embedQueryFn = prevEmbed }()
	const perCluster = 50
	n := len(queries) * perCluster
	dbpath := annFixtureDBLeafs(t, annBenchLeafs(n, queries))
	openFixtureRead(t, dbpath)

	rows, err := extractRows(0)
	if err != nil {
		t.Fatalf("extractRows: %v", err)
	}
	if len(rows) != n {
		t.Fatalf("extracted %d rows, want %d", len(rows), n)
	}
	idxPath := filepath.Join(filepath.Dir(dbpath), "vector.ann")
	idx, err := ann.Open(idxPath, ann.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Build(rows); err != nil {
		t.Fatal(err)
	}
	if err := idx.SaveTo(idxPath); err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	setANNIndexPath(t, idxPath)

	// Wire the two in-process openers exactly like MainBench: baseline
	// forces the linear scan, candidate forces ANN; both reuse the already
	// open read handle (no double-open).
	prevCand := bench.CandInprocOpener
	bench.CandInprocOpener = func(_ context.Context, _ string) (bench.Searcher, error) {
		setAnnMode(annForceOn)
		return &inprocSearcher{}, nil
	}
	defer func() { bench.CandInprocOpener = prevCand }()
	baselineOpener := func(_ context.Context, _ string) (bench.Searcher, error) {
		if !brainOpen() {
			if err := openBrain(); err != nil {
				return nil, err
			}
		}
		setAnnMode(annForceOff)
		return &inprocSearcher{}, nil
	}

	golden := &bench.GoldenSet{Version: 1, Queries: queries}
	goldenPath := filepath.Join(t.TempDir(), "golden.json")
	raw, err := json.Marshal(golden)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goldenPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	code := bench.Main([]string{
		"--inproc", "--db", dbpath,
		"--candidate", "inproc-ann",
		"--golden", goldenPath,
		"--limit", "5",
	}, baselineOpener)
	setAnnMode(annAuto)
	if code != 0 {
		t.Fatalf("bench A/B gate exited %d (0 = pass)", code)
	}
}
