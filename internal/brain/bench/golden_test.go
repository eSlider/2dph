package bench

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGoldenSetCommitted pins the issue #202 requirement: a committed
// golden-set of ~50 queries covering the named topics, ru+en.
func TestGoldenSetCommitted(t *testing.T) {
	g, err := LoadGolden(filepath.Join("..", "testdata", "golden-set.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Queries) < 45 {
		t.Fatalf("golden-set has %d queries, want >= 45", len(g.Queries))
	}
	byTopic := map[string]int{}
	byLang := map[string]int{}
	frags := 0
	for _, q := range g.Queries {
		byTopic[q.Topic]++
		byLang[q.Lang]++
		if q.Fragment != "" {
			frags++
		}
	}
	for _, topic := range []string{"facts", "mail", "docs", "git", "ssh"} {
		if byTopic[topic] < 4 {
			t.Errorf("topic %q covered by %d queries, want >= 4", topic, byTopic[topic])
		}
	}
	if byLang["ru"] < 20 || byLang["en"] < 5 {
		t.Errorf("lang mix: ru=%d en=%d, want ru>=20 en>=5", byLang["ru"], byLang["en"])
	}
	// The CI gate needs a fragment on (almost) every query: recall@5 >= 0.95
	// must be reachable even if a couple of queries regress.
	if frags < len(g.Queries)*95/100 {
		t.Errorf("fragments on %d/%d queries, want >= 95%% (CI recall gate)", frags, len(g.Queries))
	}
}

func TestLoadGoldenValidation(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	if _, err := LoadGolden(write("missing.json", "{")); err == nil {
		t.Error("malformed json: want error")
	}
	if _, err := LoadGolden(write("empty.json", `{"version":1,"queries":[]}`)); err == nil {
		t.Error("empty queries: want error")
	}
	if _, err := LoadGolden(write("dup.json",
		`{"version":1,"queries":[
			{"q":"a","topic":"docs","lang":"ru"},
			{"q":"a","topic":"docs","lang":"ru"}]}`)); err == nil {
		t.Error("duplicate query: want error")
	}
	if _, err := LoadGolden(write("topic.json",
		`{"version":1,"queries":[{"q":"a","topic":"nosuch","lang":"ru"}]}`)); err == nil {
		t.Error("unknown topic: want error")
	}
	if _, err := LoadGolden(write("lang.json",
		`{"version":1,"queries":[{"q":"a","topic":"docs","lang":"de"}]}`)); err == nil {
		t.Error("unknown lang: want error")
	}
	if _, err := LoadGolden(write("blankfrag.json",
		`{"version":1,"queries":[{"q":"a","topic":"docs","lang":"ru","fragment":"  "}]}`)); err == nil {
		t.Error("blank fragment: want error")
	}
}

func TestGoldenFragmentCount(t *testing.T) {
	g := &GoldenSet{Version: 1, Queries: []GoldenEntry{
		{Query: "a", Topic: "docs", Lang: "ru", Fragment: "x"},
		{Query: "b", Topic: "docs", Lang: "ru"},
	}}
	if n := g.FragmentCount(); n != 1 {
		t.Fatalf("FragmentCount=%d, want 1", n)
	}
}
