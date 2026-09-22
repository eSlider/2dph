package rules

import (
	"strings"
	"testing"
)

// marker builds a pdftotext page footer as it appears in the real book.
func marker(page string) string {
	return "\f                     “mcs” — 2015/5/18 — 1:43 — page " + page + " — #99"
}

func TestSplitChapters(t *testing.T) {
	text := strings.Join([]string{
		marker("ii"), // front matter (TOC) — must be ignored
		"",
		"Contents",
		"",
		"     1   What is a Proof?       5", // TOC line: not after our body start
		"",
		marker("3"),
		"",
		"",
		"1   What is a Proof?",
		"",
		"1.1   Propositions",
		"            body of chapter one",
		"",
		marker("4"),
		"",
		"4   Chapter 1 What is a Proof?", // running head — ignored (contains "Chapter")
		"",
		"            more chapter one",
		"",
		marker("27"),
		"",
		"2   The Well Ordering Principle",
		"",
		"2.1   Well Ordering Proofs",
		"            body of chapter two",
		"",
		marker("41"),
		"",
		"3   Logical Formulas",
		"",
		"3.1   Propositions from Propositions",
		"            body of chapter three",
	}, "\n")

	chs := SplitChapters(text)
	if len(chs) != 3 {
		t.Fatalf("got %d chapters, want 3", len(chs))
	}
	want := []struct {
		n     int
		title string
	}{
		{1, "What is a Proof?"},
		{2, "The Well Ordering Principle"},
		{3, "Logical Formulas"},
	}
	for i, w := range want {
		if chs[i].Number != w.n || chs[i].Title != w.title {
			t.Fatalf("chapter %d = %d %q, want %d %q", i, chs[i].Number, chs[i].Title, w.n, w.title)
		}
	}
	if !strings.Contains(chs[0].Body, "body of chapter one") || strings.Contains(chs[0].Body, "body of chapter two") {
		t.Fatalf("chapter 1 body boundaries wrong")
	}
	if !strings.Contains(chs[2].Body, "body of chapter three") {
		t.Fatalf("chapter 3 body missing")
	}
}

func TestSplitChaptersRejectsOutOfOrder(t *testing.T) {
	// A «3   Something» right after the first body page must NOT start the
	// catalog: the splitter expects chapter 1 first.
	text := strings.Join([]string{
		marker("3"),
		"",
		"3   Logical Formulas",
		"",
		"body",
	}, "\n")
	if chs := SplitChapters(text); len(chs) != 0 {
		t.Fatalf("got %d chapters, want 0", len(chs))
	}
}

func TestSplitChaptersEmpty(t *testing.T) {
	if chs := SplitChapters(""); len(chs) != 0 {
		t.Fatalf("empty text produced %d chapters", len(chs))
	}
}
