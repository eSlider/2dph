// Unit tests for the pure ranking/filtering stage (no db, no model, offline).
package main

import "testing"

func h(id, root, source string) Hit {
	return Hit{ID: id, Text: id, Root: root, Source: source}
}

func ids(hits []Hit) []string {
	out := make([]string, len(hits))
	for i, hit := range hits {
		out[i] = hit.ID
	}
	return out
}

func eq(t *testing.T, got []Hit, want ...string) {
	t.Helper()
	g := ids(got)
	if len(g) != len(want) {
		t.Fatalf("got %v, want %v", g, want)
	}
	for i := range want {
		if g[i] != want[i] {
			t.Fatalf("got %v, want %v", g, want)
		}
	}
}

// A facts leaf that ranks below the limit in the unfiltered list must still
// be returned for --root facts. Filtering after truncation loses it.
func TestRankAndFilterFiltersBeforeLimit(t *testing.T) {
	fts := []Hit{
		h("i1", "info", "docs/a.md"),
		h("i2", "info", "docs/b.md"),
		h("i3", "info", "docs/c.md"),
		h("f1", "facts", "docker ps x compose"),
	}
	eq(t, rankAndFilter(fts, nil, "facts", "", 2), "f1")
}

func TestRankAndFilterRepoFiltersBeforeLimit(t *testing.T) {
	fts := []Hit{
		h("a", "info", "eSlider/2dph:README.md"),
		h("b", "info", "eSlider/2dph:PLAN.md"),
		h("c", "info", "eSlider/ops:compose.yaml"),
	}
	eq(t, rankAndFilter(fts, nil, "", "ops", 2), "c")
}

func TestRankAndFilterTruncatesToLimit(t *testing.T) {
	fts := []Hit{h("a", "info", "x"), h("b", "info", "x"), h("c", "info", "x")}
	eq(t, rankAndFilter(fts, nil, "", "", 2), "a", "b")
}

func TestRankAndFilterLimitZeroKeepsAll(t *testing.T) {
	fts := []Hit{h("a", "info", "x"), h("b", "info", "x")}
	eq(t, rankAndFilter(fts, nil, "", "", 0), "a", "b")
}

// A leaf found by both retrievers outranks one found by a single retriever,
// even when the single-retriever hit sits at rank 1 of its list.
func TestHybridFusesBothRetrievers(t *testing.T) {
	fts := []Hit{h("only-fts", "info", "x"), h("both", "info", "x")}
	vec := []Hit{h("only-vec", "info", "x"), h("both", "info", "x")}
	eq(t, hybrid(fts, vec, 0), "both", "only-fts", "only-vec")
}

// Equal RRF scores must not depend on Go's randomized map iteration.
func TestHybridTiesAreDeterministic(t *testing.T) {
	fts := []Hit{h("b", "info", "x"), h("a", "info", "x")}
	first := ids(hybrid(fts, nil, 0))
	for i := 0; i < 50; i++ {
		got := ids(hybrid(fts, nil, 0))
		for j := range first {
			if got[j] != first[j] {
				t.Fatalf("unstable order: %v then %v", first, got)
			}
		}
	}
}

// Vector hits keep the similarity score when FTS contributed none.
func TestHybridKeepsVectorScoreForSharedHit(t *testing.T) {
	fts := []Hit{{ID: "x", Root: "info", Score: 0}}
	vec := []Hit{{ID: "x", Root: "info", Score: 0.87}}
	got := hybrid(fts, vec, 0)
	if len(got) != 1 || got[0].Score != 0.87 {
		t.Fatalf("got %+v, want score 0.87", got)
	}
}

// Regression guard for the FTS statement: BM25 scores rank best-first.
func TestFTSQueryOrdersByScoreDescending(t *testing.T) {
	if !contains(ftsStmt, "ORDER BY score DESC") {
		t.Fatalf("FTS query must order by score DESC, got:\n%s", ftsStmt)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
