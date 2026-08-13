// Unit tests for the pure ranking/filtering stage (no db, no model, offline).
package main

import (
	"errors"
	"testing"
)

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

// --- flag parsing ---

// The bug this replaced: --hop was dropped as an unknown flag and its
// argument "1" was appended to the query, so the search silently answered a
// different question.
func TestParseHopIsNotSwallowedIntoTheQuery(t *testing.T) {
	opt, err := parseArgs([]string{"what runs on arc-2", "--hop", "1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opt.query != "what runs on arc-2" {
		t.Fatalf("query = %q, want %q", opt.query, "what runs on arc-2")
	}
	if opt.hops != 1 {
		t.Fatalf("hops = %d, want 1", opt.hops)
	}
}

func TestParseRejectsUnknownFlags(t *testing.T) {
	if _, err := parseArgs([]string{"query", "--nope"}); err == nil {
		t.Fatal("unknown flag accepted")
	}
}

func TestParseRejectsBadValues(t *testing.T) {
	for _, args := range [][]string{
		{"q", "-n", "zero"},
		{"q", "-n", "0"},
		{"q", "--hop", "-1"},
		{"q", "--root", "nonsense"},
		{"q", "--hop"},
		{"--json"},
	} {
		if _, err := parseArgs(args); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
}

func TestParseDefaults(t *testing.T) {
	opt, err := parseArgs([]string{"two", "words", "--json"})
	if err != nil || opt.query != "two words" || opt.limit != 20 || opt.hops != 0 || !opt.jsonOut {
		t.Fatalf("got %+v err=%v", opt, err)
	}
}

func TestListModelNeedsNoQuery(t *testing.T) {
	if _, err := parseArgs([]string{"--list-model"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- graph walk ---

func fakeGraph(edges map[string][]string) func([]string) ([]Hit, error) {
	return func(ids []string) ([]Hit, error) {
		var out []Hit
		for _, id := range ids {
			for _, n := range edges[id] {
				out = append(out, h(n, "info", "x"))
			}
		}
		return out, nil
	}
}

func TestExpandHopsAddsNeighboursTaggedWithDepth(t *testing.T) {
	got, err := expandHops([]Hit{h("a", "info", "x")}, 1, 0,
		fakeGraph(map[string][]string{"a": {"b", "c"}}))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, got, "a", "b", "c")
	if got[1].Hop != 1 || got[2].Hop != 1 {
		t.Fatalf("neighbours not tagged with depth: %+v", got)
	}
}

func TestExpandHopsWalksFurtherOnHigherN(t *testing.T) {
	edges := map[string][]string{"a": {"b"}, "b": {"c"}, "c": {"d"}}
	one, _ := expandHops([]Hit{h("a", "info", "x")}, 1, 0, fakeGraph(edges))
	eq(t, one, "a", "b")
	two, _ := expandHops([]Hit{h("a", "info", "x")}, 2, 0, fakeGraph(edges))
	eq(t, two, "a", "b", "c")
	if two[2].Hop != 2 {
		t.Fatalf("depth not counted per round: %+v", two[2])
	}
}

func TestExpandHopsNeverRevisitsALeaf(t *testing.T) {
	// a<->b would loop forever without the seen set.
	got, _ := expandHops([]Hit{h("a", "info", "x")}, 5, 0,
		fakeGraph(map[string][]string{"a": {"b"}, "b": {"a"}}))
	eq(t, got, "a", "b")
}

func TestExpandHopsIsANoOpWithoutHops(t *testing.T) {
	seed := []Hit{h("a", "info", "x")}
	got, _ := expandHops(seed, 0, 0, fakeGraph(map[string][]string{"a": {"b"}}))
	eq(t, got, "a")
}

func TestExpandHopsCapsEachRound(t *testing.T) {
	got, _ := expandHops([]Hit{h("a", "info", "x")}, 1, 2,
		fakeGraph(map[string][]string{"a": {"b", "c", "d", "e"}}))
	eq(t, got, "a", "b", "c")
}

func TestExpandHopsReturnsWhatItHasOnError(t *testing.T) {
	boom := func([]string) ([]Hit, error) { return nil, errors.New("db gone") }
	got, err := expandHops([]Hit{h("a", "info", "x")}, 1, 0, boom)
	if err == nil {
		t.Fatal("error swallowed")
	}
	eq(t, got, "a")
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
