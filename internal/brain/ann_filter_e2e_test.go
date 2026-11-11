//go:build cgo && system_ladybug

// Filter-aware retrieval e2e gate (issue #3) on a synthetic fixture DB: a
// corpus of n info leafs that dominate BM25 and the vector neighborhood plus a
// single facts leaf. The old pipeline (fetch limit*3 candidates unfiltered,
// apply root/repo/as-of afterwards) returns zero facts on these queries — the
// measured live defect (43,133 leafs, 39 facts) — while the filtered retrieval
// finds the fact at n=5. All offline: deterministic embeddings, no model.
package brain

import (
	"fmt"
	"math/rand"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eSlider/2dph/internal/brain/ann"
	"github.com/eSlider/2dph/internal/brain/rank"
)

// rareRootQuery is the broad query the info leafs dominate.
const rareRootQuery = "matrix server docker host"

// rareRootFact is the single fact leaf: short text (low BM25), a vector in the
// other cluster (far from a query aimed at the info cluster), a source that
// carries the repo filter, and a dated D24 interval.
const rareRootFact = "Матрикс: matrix — правило хоста docker, факт из двух источников."

// rareRootLeafs returns n info leafs (dense query terms, embeddings in cluster
// A) plus the single facts leaf (cluster B).
func rareRootLeafs(n int) []LeafInput {
	const dim = EmbedDim
	info := make([]float32, dim)
	info[0] = 1
	leafs := make([]LeafInput, 0, n+1)
	rng := rand.New(rand.NewSource(3))
	for i := 0; i < n; i++ {
		emb := make([]float64, dim)
		for j := range emb {
			emb[j] = float64(info[j])
		}
		for j := 0; j < dim; j += 8 {
			emb[j] += (rng.Float64() - 0.5) * 0.01
		}
		leafs = append(leafs, LeafInput{
			Text: fmt.Sprintf("%s — info notes %d: %s %s %s %s.",
				rareRootQuery, i, rareRootQuery, rareRootQuery, rareRootQuery, rareRootQuery),
			Source:    fmt.Sprintf("fixture-info-%d.md", i),
			Root:      "info",
			Type:      "reference",
			How:       "ann-filter-e2e",
			Embedding: emb,
		})
	}
	fact := make([]float64, dim)
	fact[1] = 1
	leafs = append(leafs, LeafInput{
		Text:       rareRootFact,
		Source:     "repo-alpha/docs/AGENTS.md x 2dph/AGENTS.md",
		Root:       "facts",
		Type:       "fact",
		Confidence: "confirmed",
		ValidFrom:  "2025-01-01",
		ValidTo:    "2025-12-31",
		How:        "ann-filter-e2e",
		Embedding:  fact,
	})
	return leafs
}

// queryNearInfoCluster returns a query embedding close to the info cluster, so
// the facts leaf is outside the unfiltered vector top-k.
func queryNearInfoCluster() []float64 {
	q := make([]float64, EmbedDim)
	q[0] = 1
	q[8] = 0.01
	return q
}

// hasText reports whether any hit carries the fragment (case-insensitive).
func hasText(hits []Hit, fragment string) bool {
	frag := strings.ToLower(fragment)
	for _, h := range hits {
		if strings.Contains(strings.ToLower(h.Text), frag) {
			return true
		}
	}
	return false
}

// stubQueryEmbed points the query embedding at the info cluster (no model).
func stubQueryEmbed(t *testing.T, emb []float64) {
	t.Helper()
	prev := embedQueryFn
	embedQueryFn = func(string) ([]float64, error) { return emb, nil }
	t.Cleanup(func() { embedQueryFn = prev })
}

// TestFilteredRetrievalFindsRareRoot is the acceptance gate of issue #3: with
// n=5 the old pipeline loses the fact, the filtered one returns it.
func TestFilteredRetrievalFindsRareRoot(t *testing.T) {
	const nInfo = 120
	leafs := rareRootLeafs(nInfo)
	dbpath := annFixtureDBLeafs(t, leafs)
	openFixtureRead(t, dbpath)
	setAnnCfg(t, false, "") // vector path: exact scan, no index in this test
	stubQueryEmbed(t, queryNearInfoCluster())

	// Old behaviour, exactly as searchHits did before the fix: an unfiltered
	// limit*3 window per retriever, root applied afterwards.
	oldFTS, err := queryFTS(rareRootQuery, 5*3, rank.Filter{})
	if err != nil {
		t.Fatalf("old fts: %v", err)
	}
	if got := rank.FilterHits(oldFTS, rank.Filter{Root: "facts"}); len(got) != 0 {
		t.Fatalf("fixture must reproduce the defect: old FTS window already holds %d facts", len(got))
	}
	oldVec, err := queryVector(queryNearInfoCluster(), 5*3, rank.Filter{})
	if err != nil {
		t.Fatalf("old vec: %v", err)
	}
	if got := rank.FilterHits(oldVec, rank.Filter{Root: "facts"}); len(got) != 0 {
		t.Fatalf("fixture must reproduce the defect: old vector window already holds %d facts", len(got))
	}

	// New behaviour: the filter is applied before the top-k cut.
	hits, err := searchHits(rareRootQuery, "facts", "", 5, "", false, false)
	if err != nil {
		t.Fatalf("searchHits: %v", err)
	}
	if !hasText(hits, "matrix") {
		t.Fatalf("root=facts returned no fact for %q: %v", rareRootQuery, hits)
	}
	for _, h := range hits {
		if h.Root != "facts" {
			t.Fatalf("filter leak: %s has root %q", h.ID, h.Root)
		}
	}
}

// TestFilteredRetrievalRepoAndAsOf — repo (source substring) and as-of (D24)
// are eligible-aware too: the fact is found while in the interval and is
// honestly absent outside it, with no ineligible hit slipping through.
func TestFilteredRetrievalRepoAndAsOf(t *testing.T) {
	dbpath := annFixtureDBLeafs(t, rareRootLeafs(80))
	openFixtureRead(t, dbpath)
	setAnnCfg(t, false, "")
	stubQueryEmbed(t, queryNearInfoCluster())

	hits, err := searchHits(rareRootQuery, "", "repo-alpha", 5, "", false, false)
	if err != nil {
		t.Fatalf("repo search: %v", err)
	}
	if !hasText(hits, "matrix") {
		t.Fatalf("repo=repo-alpha lost the fact: %v", hits)
	}
	for _, h := range hits {
		if !strings.Contains(h.Source, "repo-alpha") {
			t.Fatalf("repo filter leak: %s source %q", h.ID, h.Source)
		}
	}

	if hits, err = searchHits(rareRootQuery, "", "no-such-repo", 5, "", false, false); err != nil {
		t.Fatalf("missing repo search: %v", err)
	} else if len(hits) != 0 {
		t.Fatalf("repo=no-such-repo returned %d hits, want 0", len(hits))
	}

	inWindow, err := searchHits(rareRootQuery, "facts", "", 5, "2025-06-01", false, false)
	if err != nil {
		t.Fatalf("as-of search: %v", err)
	}
	if !hasText(inWindow, "matrix") {
		t.Fatalf("as-of inside the interval lost the fact: %v", inWindow)
	}
	outWindow, err := searchHits(rareRootQuery, "facts", "", 5, "2026-06-01", false, false)
	if err != nil {
		t.Fatalf("as-of search (outside): %v", err)
	}
	if hasText(outWindow, "matrix") {
		t.Fatalf("as-of outside the interval still returned the fact: %v", outWindow)
	}
}

// TestFilteredScanRanksEligibleBeforeCut — the exact scan (no ANN index) must
// apply the filter before the top-k cut; the fact vector is the farthest from
// the query, so an unfiltered scan cannot see it.
func TestFilteredScanRanksEligibleBeforeCut(t *testing.T) {
	dbpath := annFixtureDBLeafs(t, rareRootLeafs(60))
	openFixtureRead(t, dbpath)
	setAnnCfg(t, false, "")

	unfiltered, err := queryVector(queryNearInfoCluster(), 5, rank.Filter{})
	if err != nil {
		t.Fatalf("unfiltered scan: %v", err)
	}
	if hasText(unfiltered, "matrix — правило") {
		t.Fatal("fixture sanity: the fact must not be in the unfiltered vector top-5")
	}
	filtered, err := queryVector(queryNearInfoCluster(), 5, rank.Filter{Root: "facts"})
	if err != nil {
		t.Fatalf("filtered scan: %v", err)
	}
	if !hasText(filtered, "matrix — правило") {
		t.Fatalf("filtered scan lost the far fact: %v", filtered)
	}
}

// TestFilteredANNIndexCarriesMetadata — the built index (extractRows → build)
// carries root/source/interval, so the ANN path filters without a graph lookup
// and survives a snapshot round-trip.
func TestFilteredANNIndexCarriesMetadata(t *testing.T) {
	dbpath := annFixtureDBLeafs(t, rareRootLeafs(100))
	openFixtureRead(t, dbpath)
	rows, err := extractRows(0)
	if err != nil {
		t.Fatalf("extractRows: %v", err)
	}
	facts := 0
	for _, r := range rows {
		if r.Root == "facts" {
			facts++
			if r.Source == "" || r.ValidFrom != "2025-01-01" || r.ValidTo != "2025-12-31" {
				t.Fatalf("extractRows lost filter metadata: %+v", r)
			}
		}
	}
	if facts != 1 {
		t.Fatalf("extracted %d facts rows, want 1", facts)
	}
	idxPath := filepath.Join(filepath.Dir(dbpath), "vector.ann")
	idx := ann.New(ann.Params{RngSeed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Build(rows); err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := idx.SaveTo(idxPath); err != nil {
		t.Fatalf("save: %v", err)
	}
	idx.Close()
	setANNIndexPath(t, idxPath)
	setAnnCfg(t, true, idxPath)

	hits, err := queryVector(queryNearInfoCluster(), 5, rank.Filter{Root: "facts"})
	if err != nil {
		t.Fatalf("filtered ANN query: %v", err)
	}
	if len(hits) != 1 || hits[0].Root != "facts" {
		t.Fatalf("filtered ANN query = %v, want the one fact", hits)
	}
}

// TestFilteredEvalGate — the gate of bin/brain/eval.go on the fixture: the
// filtered cases of the issue (root/repo/as_of before the cut) pass, a missing
// corpus slice is skipped instead of failing CI, and a wrong expectation still
// fails (the gate is not vacuous).
func TestFilteredEvalGate(t *testing.T) {
	dbpath := annFixtureDBLeafs(t, rareRootLeafs(80))
	openFixtureRead(t, dbpath)
	setAnnCfg(t, false, "")
	stubQueryEmbed(t, queryNearInfoCluster())

	prev := rank.EvalFilteredQuestions
	t.Cleanup(func() { rank.EvalFilteredQuestions = prev })

	// Facts slice exists and the queries hit it → gate passes.
	rank.EvalFilteredQuestions = []rank.EvalQuestion{
		{Query: rareRootQuery, Fragment: "matrix", Root: "facts"},
		{Query: rareRootQuery, Fragment: "matrix", Repo: "repo-alpha"},
		{Query: rareRootQuery, Fragment: "matrix", Root: "facts", AsOf: "2025-06-01"},
	}
	recalled, total, _, _ := evalFilteredGate()
	if total != 3 || recalled != 3 {
		t.Fatalf("filtered gate = %d/%d, want 3/3", recalled, total)
	}

	// A slice the corpus lacks is skipped, not failed.
	rank.EvalFilteredQuestions = []rank.EvalQuestion{
		{Query: rareRootQuery, Fragment: "matrix", Root: "hypothesis-only"},
		{Query: rareRootQuery, Fragment: "matrix", Repo: "no-such-repo"},
	}
	if recalled, total, _, _ = evalFilteredGate(); total != 0 {
		t.Fatalf("absent slices must be skipped, gate counted %d cases", total)
	}

	// A wrong fragment fails: the gate measures the real retrieval.
	rank.EvalFilteredQuestions = []rank.EvalQuestion{
		{Query: rareRootQuery, Fragment: "no-such-fragment", Root: "facts"},
	}
	if recalled, total, _, _ = evalFilteredGate(); total != 1 || recalled != 0 {
		t.Fatalf("wrong fragment must fail the case, gate = %d/%d", recalled, total)
	}
}

// TestFilteredANNStaleIndexFallsBackToScan — the index does not know the fact
// (built before it existed). An empty filtered ANN result must not become an
// empty answer: the exact scan proves existence.
func TestFilteredANNStaleIndexFallsBackToScan(t *testing.T) {
	dbpath := annFixtureDBLeafs(t, rareRootLeafs(100))
	openFixtureRead(t, dbpath)
	rows, err := extractRows(0)
	if err != nil {
		t.Fatalf("extractRows: %v", err)
	}
	fresh := rows[:0]
	for _, r := range rows {
		if r.Root != "facts" {
			fresh = append(fresh, r) // simulate a stale index: fact not indexed
		}
	}
	idxPath := filepath.Join(filepath.Dir(dbpath), "vector.ann")
	idx := ann.New(ann.Params{RngSeed: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Build(fresh); err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := idx.SaveTo(idxPath); err != nil {
		t.Fatalf("save: %v", err)
	}
	idx.Close()
	setANNIndexPath(t, idxPath)
	setAnnCfg(t, true, idxPath)

	hits, err := queryVector(queryNearInfoCluster(), 5, rank.Filter{Root: "facts"})
	if err != nil {
		t.Fatalf("filtered ANN query: %v", err)
	}
	if len(hits) != 1 || hits[0].Root != "facts" {
		t.Fatalf("stale filtered index must fall back to the exact scan, got %v", hits)
	}
}
