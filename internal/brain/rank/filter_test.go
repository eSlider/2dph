package rank

import (
	"errors"
	"fmt"
	"testing"
)

// hit builds one candidate with the metadata the eligibility predicate reads.
func hit(id, root, source, from, to string) Hit {
	return Hit{ID: id, Root: root, Source: source, ValidFrom: from, ValidTo: to}
}

// TestFilterEligibleMatrix pins the eligibility semantics shared by the ANN
// index, the FTS query and the final ranking (issue #3): root is exact, repo
// is a source substring, as-of is the D24 interval, and an empty filter keeps
// everything.
func TestFilterEligibleMatrix(t *testing.T) {
	row := func(root, source, from, to string) bool {
		return Filter{}.Eligible(root, source, from, to)
	}
	if !row("facts", "docs/a.md x docs/b.md", "", "") {
		t.Fatal("empty filter must keep every row")
	}

	facts := Filter{Root: "facts"}
	if !facts.Eligible("facts", "docs/a.md", "", "") {
		t.Fatal("facts row must be eligible for root=facts")
	}
	if facts.Eligible("info", "docs/a.md", "", "") {
		t.Fatal("info row must not be eligible for root=facts")
	}

	repo := Filter{Repo: "office-assistant"}
	if !repo.Eligible("info", "office-assistant/docs/a.md x office-assistant/AGENTS.md", "", "") {
		t.Fatal("repo substring hit must be eligible")
	}
	if repo.Eligible("info", "docs/a.md x docs/b.md", "", "") {
		t.Fatal("repo miss must not be eligible")
	}

	asOf := Filter{AsOf: "2025-06-01"}
	if !asOf.Eligible("info", "s", "2025-01-01", "2025-12-31") {
		t.Fatal("interval covering as_of must be eligible")
	}
	if asOf.Eligible("info", "s", "2025-07-01", "") {
		t.Fatal("interval starting after as_of must not be eligible")
	}
	if asOf.Eligible("info", "s", "", "2025-05-31") {
		t.Fatal("interval ending before as_of must not be eligible")
	}
	if !asOf.Eligible("info", "s", "", "") {
		t.Fatal("empty interval (legacy leaf) must stay eligible (D24)")
	}

	all := Filter{Root: "facts", Repo: "office-assistant", AsOf: "2025-06-01"}
	if !all.Eligible("facts", "office-assistant/x.md", "2025-01-01", "") {
		t.Fatal("row passing every filter must be eligible")
	}
	if all.Eligible("info", "office-assistant/x.md", "2025-01-01", "") {
		t.Fatal("one failing dimension must reject the row")
	}
}

func TestFilterEmpty(t *testing.T) {
	if !(Filter{}).Empty() {
		t.Fatal("zero Filter must be Empty")
	}
	for _, f := range []Filter{{Root: "facts"}, {Repo: "2dph"}, {AsOf: "2025-01-01"}} {
		if f.Empty() {
			t.Fatalf("%+v must not be Empty", f)
		}
	}
}

// TestFilterHitsKeepsOrderAndLegacyRows — the post-fusion filter must behave
// exactly like the old per-dimension filters (same rows, same order).
func TestFilterHitsKeepsOrderAndLegacyRows(t *testing.T) {
	hits := []Hit{
		hit("i1", "info", "docs/a.md", "", ""),
		hit("f1", "facts", "2dph/AGENTS.md", "", ""),
		hit("i2", "info", "docs/b.md", "2024-01-01", "2024-12-31"),
		hit("f2", "facts", "2dph/AGENTS.md", "2025-01-01", ""),
	}
	got := FilterHits(hits, Filter{Root: "facts"})
	if len(got) != 2 || got[0].ID != "f1" || got[1].ID != "f2" {
		t.Fatalf("root filter = %v, want [f1 f2]", ids(got))
	}
	got = FilterHits(hits, Filter{AsOf: "2025-06-01"})
	if fmt.Sprint(ids(got)) != "[i1 f1 f2]" {
		t.Fatalf("as-of filter = %v, want [i1 f1 f2] (legacy + open interval)", ids(got))
	}
	if got := FilterHits(hits, Filter{}); len(got) != len(hits) {
		t.Fatalf("empty filter dropped rows: %d of %d", len(got), len(hits))
	}
}

// TestEscalateCandidatesSingleRung — a retriever that pre-filters answers at
// the first budget (limit*3) and is never asked twice.
func TestEscalateCandidatesSingleRung(t *testing.T) {
	var calls []int
	got, err := EscalateCandidates(5, func(n int) ([]Hit, bool, error) {
		calls = append(calls, n)
		out := make([]Hit, n)
		for i := range out {
			out[i] = hit(fmt.Sprintf("h%d", i), "facts", "s", "", "")
		}
		return out, false, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 15 {
		t.Fatalf("got %d hits, want the full limit*3 window (15)", len(got))
	}
	if fmt.Sprint(calls) != "[15]" {
		t.Fatalf("calls = %v, want one rung [15]", calls)
	}
}

// TestEscalateCandidatesDoublesWhenEligibleAreSparse — the as-of case: each
// full window yields only a fifth as eligible rows, so the ladder must double
// until limit eligible are collected.
func TestEscalateCandidatesDoublesWhenEligibleAreSparse(t *testing.T) {
	var calls []int
	got, err := EscalateCandidates(5, func(n int) ([]Hit, bool, error) {
		calls = append(calls, n)
		eligible := n / 5
		out := make([]Hit, eligible)
		for i := range out {
			out[i] = hit(fmt.Sprintf("h%d", i), "facts", "s", "", "")
		}
		return out, true, nil // a full window was available, more rows exist
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 6 {
		t.Fatalf("got %d eligible, want 6 (n=30/5)", len(got))
	}
	if fmt.Sprint(calls) != "[15 30]" {
		t.Fatalf("calls = %v, want [15 30]", calls)
	}
}

// TestEscalateCandidatesStopsWhenRetrieverIsExhausted — more=false means the
// retriever returned a short window: no bigger budget can find new rows.
func TestEscalateCandidatesStopsWhenRetrieverIsExhausted(t *testing.T) {
	var calls []int
	got, err := EscalateCandidates(20, func(n int) ([]Hit, bool, error) {
		calls = append(calls, n)
		return []Hit{hit("f1", "facts", "s", "", "")}, false, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "f1" {
		t.Fatalf("got %v, want the single eligible hit", ids(got))
	}
	if len(calls) != 1 {
		t.Fatalf("calls = %v, want one rung (exhausted)", calls)
	}
}

// TestEscalateCandidatesCap — a retriever that keeps claiming more rows is
// asked up to CandidateCap and then left alone (no unbounded fetch).
func TestEscalateCandidatesCap(t *testing.T) {
	var calls []int
	got, err := EscalateCandidates(1, func(n int) ([]Hit, bool, error) {
		calls = append(calls, n)
		return nil, true, nil // full window, nothing eligible
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want none", ids(got))
	}
	if last := calls[len(calls)-1]; last != CandidateCap {
		t.Fatalf("last budget = %d, want the cap %d (calls %v)", last, CandidateCap, calls)
	}
	for i := 1; i < len(calls); i++ {
		if calls[i] != calls[i-1]*2 && calls[i] != CandidateCap {
			t.Fatalf("budget must double: %v", calls)
		}
	}
}

func TestEscalateCandidatesErrorAndNoOp(t *testing.T) {
	want := errors.New("fetch failed")
	if _, err := EscalateCandidates(5, func(int) ([]Hit, bool, error) {
		return nil, false, want
	}); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	called := false
	got, err := EscalateCandidates(0, func(int) ([]Hit, bool, error) {
		called = true
		return nil, false, nil
	})
	if err != nil || got != nil || called {
		t.Fatalf("limit<=0 must be a no-op, got %v %v called=%v", got, err, called)
	}
}

// TestEvalFilteredQuestionsAreWellFormed pins the filtered gate table (issue
// #3): every case carries a filter, a fragment and a known root, and the
// unfiltered gate stays untouched at three cases.
func TestEvalFilteredQuestionsAreWellFormed(t *testing.T) {
	if len(EvalQuestions) != 3 {
		t.Fatalf("unfiltered gate changed: %d questions, want 3", len(EvalQuestions))
	}
	for _, q := range EvalQuestions {
		if q.Root != "" || q.Repo != "" || q.AsOf != "" {
			t.Fatalf("unfiltered gate question %q must stay unfiltered", q.Query)
		}
	}
	if len(EvalFilteredQuestions) == 0 {
		t.Fatal("no filtered eval questions")
	}
	rooted, repoed, dated := 0, 0, 0
	for _, q := range EvalFilteredQuestions {
		if q.Query == "" || q.Fragment == "" {
			t.Fatalf("filtered case needs query and fragment: %+v", q)
		}
		if q.Filter().Empty() {
			t.Fatalf("filtered case %q has no filter", q.Query)
		}
		if q.Root != "" && q.Root != "facts" && q.Root != "info" {
			t.Fatalf("case %q: root %q is not facts|info", q.Query, q.Root)
		}
		if q.Root != "" {
			rooted++
		}
		if q.Repo != "" {
			repoed++
		}
		if q.AsOf != "" {
			dated++
			if len(q.AsOf) != 10 || q.AsOf[4] != '-' || q.AsOf[7] != '-' {
				t.Fatalf("case %q: as_of %q is not YYYY-MM-DD", q.Query, q.AsOf)
			}
		}
	}
	if rooted == 0 || repoed == 0 || dated == 0 {
		t.Fatalf("gate must cover root=%d repo=%d as_of=%d", rooted, repoed, dated)
	}
}
