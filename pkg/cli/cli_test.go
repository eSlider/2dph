package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/integrii/flaggy"
)

func TestParseBoolAndIntAnyPosition(t *testing.T) {
	p := New("t")
	jsonOut := false
	n := 20
	q := ""
	p.Bool(&jsonOut, "", "json", "JSON")
	p.Int(&n, "n", "n", "limit")
	p.AddPositionalValue(&q, "query", 1, false, "q")
	if err := Parse(p, []string{"two", "words", "--json", "-n", "5"}); err != nil {
		t.Fatal(err)
	}
	got := Query(q, p.TrailingArguments)
	if got != "two words" || !jsonOut || n != 5 {
		t.Fatalf("q=%q json=%v n=%d", got, jsonOut, n)
	}
}

func TestParseUnknownFlagIsError(t *testing.T) {
	p := New("t")
	jsonOut := false
	p.Bool(&jsonOut, "", "json", "JSON")
	if err := Parse(p, []string{"--nope"}); err == nil {
		t.Fatal("unknown flag accepted")
	}
}

func TestParseHelpIsErrHelp(t *testing.T) {
	p := New("t")
	jsonOut := false
	p.Bool(&jsonOut, "", "json", "JSON")
	err := Parse(p, []string{"--help"})
	if !errors.Is(err, ErrHelp) {
		t.Fatalf("got %v", err)
	}
}

func TestParseMissingFlagValueIsError(t *testing.T) {
	p := New("t")
	n := 0
	p.Int(&n, "", "hop", "hop")
	if err := Parse(p, []string{"--hop"}); err == nil {
		t.Fatal("expected missing value error")
	}
}

func TestBashScriptNamesShebangPath(t *testing.T) {
	out := BashScript([]Tool{{
		Path: "bin/brain/search.go",
		Name: "brain-search",
		New:  newSearchLike,
	}})
	if !strings.Contains(out, "--json") || !strings.Contains(out, "--hop") {
		t.Fatalf("flags missing:\n%s", out)
	}
	if !strings.Contains(out, "complete -F") || !strings.Contains(out, "bin/brain/search.go") {
		t.Fatalf("shebang complete missing:\n%s", out)
	}
}

func newSearchLike() *flaggy.Parser {
	p := New("brain-search")
	jsonOut := false
	hop := 0
	p.Bool(&jsonOut, "", "json", "JSON")
	p.Int(&hop, "", "hop", "graph hop")
	return p
}
