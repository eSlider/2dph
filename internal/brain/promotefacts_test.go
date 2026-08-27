//go:build cgo && system_ladybug

package brain

import (
	"fmt"
	"path/filepath"
	"testing"
)

func toString(v any) string { return fmt.Sprint(v) }

func TestSourceCount(t *testing.T) {
	cases := []struct {
		src  string
		want int
	}{
		{"docker ps x compose:compose.yaml", 2},
		{"ssh config x docs(README.md, PLAN.md)", 2},
		{"a.md x b.md x c.md", 3},
		{"single-file.md", 1},
		{"docs/a x b.md", 2}, // " x " is the documented separator
		{"a.md x a.md", 1},   // duplicate source is not independent
		{"", 0},
		{"   ", 0},
	}
	for _, c := range cases {
		if got := SourceCount(c.src); got != c.want {
			t.Errorf("SourceCount(%q) = %d, want %d", c.src, got, c.want)
		}
	}
}

func TestPromoteEligible(t *testing.T) {
	leafs := []LeafInput{
		{Text: "two-source confirmed", Source: "docker ps x compose:compose.yaml", Root: "info", Confidence: "confirmed", How: "kb/index"},
		{Text: "single-source confirmed", Source: "docs/runbook.md", Root: "info", Confidence: "confirmed", How: "kb/index"},
		{Text: "two-source hypothesis", Source: "a x b", Root: "info", Confidence: "hypothesis", How: "kb/index"},
		{Text: "already a fact", Source: "docker ps x compose.yaml", Root: "facts", Confidence: "confirmed", How: "facts/extract"},
	}
	got := PromoteEligible(leafs)
	if len(got) != 1 {
		t.Fatalf("PromoteEligible returned %d leafs, want 1 (only the 2-source confirmed info)", len(got))
	}
	if got[0].Text != "two-source confirmed" {
		t.Errorf("promoted %q, want the 2-source confirmed leaf", got[0].Text)
	}
	if got[0].Root != "facts" {
		t.Errorf("promoted leaf root = %q, want facts", got[0].Root)
	}
	if got[0].How != "facts/promote" {
		t.Errorf("promoted leaf how = %q, want facts/promote", got[0].How)
	}
}

// seedPromoteFixture creates a temp DB with one 2-source confirmed info leaf,
// one 1-source confirmed info leaf and one 2-source hypothesis leaf.
func seedPromoteFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "kb.lbug")
	db, conn, err := OpenWritable(dbpath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer conn.Close()
	if err := InitSchema(conn); err != nil {
		t.Fatal(err)
	}
	if _, err := AddLeafs(conn, []LeafInput{
		{Text: "promotable two-source fact token-2src", Source: "docker ps x compose:compose.yaml", Root: "info", Confidence: "confirmed", How: "kb/index"},
		{Text: "single-source stays info token-1src", Source: "docs/runbook.md", Root: "info", Confidence: "confirmed", How: "kb/index"},
		{Text: "hypothesis stays info token-hyp", Source: "a x b", Root: "info", Confidence: "hypothesis", How: "kb/index"},
	}); err != nil {
		t.Fatal(err)
	}
	return dbpath
}

func TestPromoteFactsPromotesAndIdempotent(t *testing.T) {
	dbpath := seedPromoteFixture(t)
	db, conn, err := OpenWritable(dbpath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer conn.Close()

	countByRoot := func() map[string]int {
		t.Helper()
		out := map[string]int{}
		res, err := conn.Query("MATCH (l:Leaf) RETURN l.root, count(*)")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Close()
		for res.HasNext() {
			row, err := res.Next()
			if err != nil {
				t.Fatal(err)
			}
			vals, err := row.GetAsSlice()
			if err != nil || len(vals) < 2 {
				continue
			}
			out[toString(vals[0])] = int(asInt(vals[1]))
		}
		return out
	}

	before := countByRoot()
	if before["info"] != 3 {
		t.Fatalf("seed info count = %d, want 3", before["info"])
	}

	n, err := PromoteFacts(conn)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("PromoteFacts promoted %d, want 1", n)
	}

	after := countByRoot()
	if after["facts"] != 1 {
		t.Errorf("facts count after promote = %d, want 1", after["facts"])
	}
	if after["info"] != 2 {
		t.Errorf("info count after promote = %d, want 2 (single-source + hypothesis stay)", after["info"])
	}

	// The promoted leaf must have flipped root to facts and how to facts/promote.
	res, err := conn.Query("MATCH (l:Leaf {text:'promotable two-source fact token-2src'}) RETURN l.root, l.how")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Close()
	if !res.HasNext() {
		t.Fatal("promoted leaf not found")
	}
	row, _ := res.Next()
	vals, _ := row.GetAsSlice()
	if toString(vals[0]) != "facts" || toString(vals[1]) != "facts/promote" {
		t.Errorf("promoted leaf root/how = %q/%q, want facts/facts/promote", toString(vals[0]), toString(vals[1]))
	}

	// Idempotency: a second run promotes nothing and creates no duplicates.
	n2, err := PromoteFacts(conn)
	if err != nil {
		t.Fatal(err)
	}
	if n2 != 0 {
		t.Errorf("second PromoteFacts promoted %d, want 0 (idempotent)", n2)
	}
	after2 := countByRoot()
	if after2["facts"] != 1 || after2["info"] != 2 {
		t.Errorf("after re-run counts facts=%d info=%d, want 1/2 (no dupes)", after2["facts"], after2["info"])
	}

	// Audit-shaped grouping (same query as the HTTP /audit handler): the
	// promoted leaf is (facts, confirmed).
	audit := map[string]int{}
	ares, err := conn.Query("MATCH (l:Leaf) RETURN l.root, l.confidence, count(*)")
	if err != nil {
		t.Fatal(err)
	}
	defer ares.Close()
	for ares.HasNext() {
		row, _ := ares.Next()
		vals, _ := row.GetAsSlice()
		audit[toString(vals[0])+"|"+toString(vals[1])] = int(asInt(vals[2]))
	}
	if audit["facts|confirmed"] != 1 {
		t.Errorf("audit facts|confirmed = %d, want 1", audit["facts|confirmed"])
	}
	if audit["info|confirmed"] != 1 {
		t.Errorf("audit info|confirmed = %d, want 1", audit["info|confirmed"])
	}
	if audit["info|hypothesis"] != 1 {
		t.Errorf("audit info|hypothesis = %d, want 1", audit["info|hypothesis"])
	}
}

// TestPromoteFactsReadPathDistinguishes verifies that after promotion the read
// paths (stats by_root, leaf query with root filter, audit-style grouping) see
// the promoted leaf as facts and the single-source one as info.
func TestPromoteFactsReadPathDistinguishes(t *testing.T) {
	dbpath := seedPromoteFixture(t)

	// Write the promotion.
	db, conn, err := OpenWritable(dbpath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PromoteFacts(conn); err != nil {
		db.Close()
		conn.Close()
		t.Fatal(err)
	}
	conn.Close()
	db.Close()

	// Serve-read path on the same file.
	prev := dbPathFn
	dbPathFn = func() string { return dbpath }
	t.Cleanup(func() { dbPathFn = prev })
	if err := openBrain(); err != nil {
		t.Fatal(err)
	}
	defer closeBrain()

	s, err := leafStats()
	if err != nil {
		t.Fatal(err)
	}
	byRoot := s["by_root"].(map[string]int)
	if byRoot["facts"] != 1 || byRoot["info"] != 2 {
		t.Errorf("stats by_root facts=%d info=%d, want 1/2", byRoot["facts"], byRoot["info"])
	}

	// queryLeafs with root filter must only see facts on the facts root.
	facts, err := queryLeafs("facts", "", "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || facts[0]["text"] != "promotable two-source fact token-2src" {
		t.Errorf("queryLeafs(facts) = %v, want the promoted 2-source leaf only", facts)
	}
	info, err := queryLeafs("info", "", "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(info) != 2 {
		t.Errorf("queryLeafs(info) = %d leafs, want 2", len(info))
	}

}
