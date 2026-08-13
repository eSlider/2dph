// Unit tests for ranking/filtering and CLI parsing (no db, no model, offline).
package rank

import (
	"strings"
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
	eq(t, RankAndFilter(fts, nil, "facts", "", 2), "f1")
}

func TestRankAndFilterRepoFiltersBeforeLimit(t *testing.T) {
	fts := []Hit{
		h("a", "info", "eSlider/2dph:README.md"),
		h("b", "info", "eSlider/2dph:PLAN.md"),
		h("c", "info", "eSlider/ops:compose.yaml"),
	}
	eq(t, RankAndFilter(fts, nil, "", "ops", 2), "c")
}

func TestRankAndFilterTruncatesToLimit(t *testing.T) {
	fts := []Hit{h("a", "info", "x"), h("b", "info", "x"), h("c", "info", "x")}
	eq(t, RankAndFilter(fts, nil, "", "", 2), "a", "b")
}

func TestRankAndFilterLimitZeroKeepsAll(t *testing.T) {
	fts := []Hit{h("a", "info", "x"), h("b", "info", "x")}
	eq(t, RankAndFilter(fts, nil, "", "", 0), "a", "b")
}

func TestHybridFusesBothRetrievers(t *testing.T) {
	fts := []Hit{h("only-fts", "info", "x"), h("both", "info", "x")}
	vec := []Hit{h("only-vec", "info", "x"), h("both", "info", "x")}
	eq(t, Hybrid(fts, vec, 0), "both", "only-fts", "only-vec")
}

func TestHybridTiesAreDeterministic(t *testing.T) {
	fts := []Hit{h("b", "info", "x"), h("a", "info", "x")}
	first := ids(Hybrid(fts, nil, 0))
	for i := 0; i < 50; i++ {
		got := ids(Hybrid(fts, nil, 0))
		for j := range first {
			if got[j] != first[j] {
				t.Fatalf("unstable order: %v then %v", first, got)
			}
		}
	}
}

func TestHybridKeepsVectorScoreForSharedHit(t *testing.T) {
	fts := []Hit{{ID: "x", Root: "info", Score: 0}}
	vec := []Hit{{ID: "x", Root: "info", Score: 0.87}}
	got := Hybrid(fts, vec, 0)
	if len(got) != 1 || got[0].Score != 0.87 {
		t.Fatalf("got %+v, want score 0.87", got)
	}
}

// The old parser dropped unknown flags and appended their arguments to the
// query, so `search "q" --hop 1` searched for "q 1". --hop is not implemented
// here (needs File edges); it must still fail closed instead of changing q.
func TestParseHopIsNotSwallowedIntoTheQuery(t *testing.T) {
	_, err := ParseArgs([]string{"what runs on arc-2", "--hop", "1"})
	if err == nil {
		t.Fatal("expected --hop to error (not implemented), not be swallowed")
	}
	if !strings.Contains(err.Error(), "--hop") {
		t.Fatalf("error should name --hop, got %v", err)
	}
}

func TestParseRejectsUnknownFlags(t *testing.T) {
	if _, err := ParseArgs([]string{"query", "--nope"}); err == nil {
		t.Fatal("unknown flag accepted")
	}
}

func TestParseRejectsBadValues(t *testing.T) {
	for _, args := range [][]string{
		{"q", "-n", "zero"},
		{"q", "-n", "0"},
		{"q", "--root", "nonsense"},
		{"q", "--hop"},
		{"--json"},
	} {
		if _, err := ParseArgs(args); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
}

func TestParseDefaults(t *testing.T) {
	opt, err := ParseArgs([]string{"two", "words", "--json"})
	if err != nil || opt.Query != "two words" || opt.Limit != 20 || !opt.JSONOut {
		t.Fatalf("got %+v err=%v", opt, err)
	}
}

func TestListModelNeedsNoQuery(t *testing.T) {
	if _, err := ParseArgs([]string{"--list-model"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFTSQueryOrdersByScoreDescending(t *testing.T) {
	if !strings.Contains(FTSStmt, "ORDER BY score DESC") {
		t.Fatalf("FTS query must order by score DESC, got:\n%s", FTSStmt)
	}
}
