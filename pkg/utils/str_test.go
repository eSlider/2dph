package utils

import (
	"strings"
	"testing"
)

func TestSnippetShortIsUnchanged(t *testing.T) {
	s := "abc"
	if got := Snippet(s, 100); got != s {
		t.Fatalf("Snippet = %q", got)
	}
}

func TestSnippetTruncates(t *testing.T) {
	s := strings.Repeat("x", 500)
	got := Snippet(s, 300)
	if len(got) != 303 { // 300 bytes + 3-byte ellipsis
		t.Fatalf("len = %d, want 303", len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("Snippet = %q, want trailing ellipsis", got)
	}
}

func TestOrDefault(t *testing.T) {
	if got := Or("", "def"); got != "def" {
		t.Fatalf("Or empty = %q", got)
	}
	if got := Or("val", "def"); got != "val" {
		t.Fatalf("Or set = %q", got)
	}
}
