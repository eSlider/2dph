package rank

import (
	"strings"
	"testing"
)

func TestShouldEscalateWhenNoFacts(t *testing.T) {
	if !ShouldEscalate(nil, "") {
		t.Fatal("empty local graph must escalate")
	}
	if !ShouldEscalate([]Hit{h("i", "info", "docs/a.md")}, "") {
		t.Fatal("info-only must escalate (not confirmed)")
	}
}

func TestShouldEscalateWhenHypothesisFacts(t *testing.T) {
	hyp := Hit{ID: "c", Root: "facts", Confidence: "hypothesis", Source: "a x b vs c x d"}
	if !ShouldEscalate([]Hit{hyp}, "") {
		t.Fatal("hypothesis facts are (not confirmed); escalate")
	}
	if ConfirmedFact(hyp) {
		t.Fatal("hypothesis is not confirmed")
	}
}

func TestShouldNotEscalateWhenFactsConfirm(t *testing.T) {
	hits := []Hit{h("f", "facts", "docker ps x compose"), h("i", "info", "docs/a.md")}
	if ShouldEscalate(hits, "") {
		t.Fatal("facts hit is already confirmed; do not mix web")
	}
}

func TestShouldNotEscalateWhenRootFilterSet(t *testing.T) {
	if ShouldEscalate(nil, "facts") {
		t.Fatal("--root facts must stay local")
	}
	if ShouldEscalate([]Hit{h("i", "info", "x")}, "info") {
		t.Fatal("--root info must stay local")
	}
}

func TestDeduceCallsWebOnlyWhenEscalating(t *testing.T) {
	called := 0
	web := func(q string) SecondSource {
		called++
		if q != "LadybugDB" {
			t.Fatalf("query = %q", q)
		}
		return SecondSource{Status: "ok", Results: []SecondSourceHit{{Title: "t", URL: "http://example.com"}}}
	}
	got := Deduce([]Hit{h("i", "info", "x")}, "LadybugDB", "", false, web)
	if called != 1 || got == nil || got.Status != "ok" {
		t.Fatalf("got %+v called=%d", got, called)
	}
}

func TestDeduceNilWhenFactsOrNoWeb(t *testing.T) {
	web := func(string) SecondSource {
		t.Fatal("web must not run")
		return SecondSource{}
	}
	if Deduce([]Hit{h("f", "facts", "x")}, "q", "", false, web) != nil {
		t.Fatal("facts")
	}
	if Deduce([]Hit{h("i", "info", "x")}, "q", "", true, web) != nil {
		t.Fatal("--no-web")
	}
	if Deduce(nil, "q", "facts", false, web) != nil {
		t.Fatal("--root facts")
	}
	if Deduce(nil, "q", "", false, nil) != nil {
		t.Fatal("nil web fn")
	}
}

func TestParseNoWeb(t *testing.T) {
	opt, err := ParseArgs([]string{"query", "--no-web", "--json"})
	if err != nil || !opt.NoWeb || !opt.JSONOut || opt.Query != "query" {
		t.Fatalf("got %+v err=%v", opt, err)
	}
}

func TestUsageNamesNoWeb(t *testing.T) {
	if !strings.Contains(Usage, "--no-web") {
		t.Fatalf("usage must name --no-web, got:\n%s", Usage)
	}
}
