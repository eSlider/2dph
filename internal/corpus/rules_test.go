package corpus

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eSlider/2dph/internal/contract"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRulesStream(t *testing.T) {
	dir := t.TempDir()
	cat := filepath.Join(dir, "6-042")
	writeFile(t, filepath.Join(cat, "index.yml"), "source: x\nlicense: y\nsha256: z\npages: 1\n")
	writeFile(t, filepath.Join(cat, "logic.yml"), `rules:
  - id: logic.inference.modus-ponens
    domain: logic
    name: Modus ponens
    form: "P ; P → Q  ⊢  Q"
    statement: From P and P implies Q conclude Q.
    source: "6.042J §3.4"
    executable: true
  - id: number.euler.theorem
    domain: number-theory
    name: Euler's theorem
    form: "gcd(a, n) = 1  ⊢  a^φ(n) ≡ 1 (mod n)"
    statement: A unit modulo n raised to phi(n) is one.
    source: "6.042J §8.10"
    executable: false
`)
	// not a catalog (no index.yml) — must be skipped
	writeFile(t, filepath.Join(dir, "loose", "notes.yml"), "rules: []\n")

	var got []contract.Leaf
	err := (Rules{Dir: dir}).Stream(context.Background(), func(l contract.Leaf) error {
		got = append(got, l)
		return nil
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d leafs, want 2: %+v", len(got), got)
	}
	byID := map[string]contract.Leaf{}
	for _, l := range got {
		byID[l.ExternalID] = l
		if l.Source != "rules" || l.Kind != "reference" || l.Root != "info" {
			t.Fatalf("bad leaf meta: %+v", l)
		}
		if err := l.Validate(); err != nil {
			t.Fatalf("leaf invalid: %v", err)
		}
	}
	l, ok := byID["6-042/logic.inference.modus-ponens"]
	if !ok {
		t.Fatalf("missing leaf for modus-ponens: %v", byID)
	}
	if !strings.Contains(l.Text, "executable (audit engine)") || !strings.Contains(l.Text, "P → Q") {
		t.Fatalf("leaf text not self-contained: %q", l.Text)
	}
	if !strings.Contains(byID["6-042/number.euler.theorem"].Text, "reference (retrieval)") {
		t.Fatalf("non-executable rule not marked reference")
	}
}

func TestRulesStreamMissingDir(t *testing.T) {
	if err := (Rules{Dir: filepath.Join(t.TempDir(), "nope")}).Stream(context.Background(), func(contract.Leaf) error {
		t.Fatal("emitted a leaf for a missing dir")
		return nil
	}); err != nil {
		t.Fatalf("missing dir should be empty, not an error: %v", err)
	}
}

// TestRulesRepoCatalog guards the real wiring: the committed etc/rules catalog
// must stream as reference leafs (epic #324 P5).
func TestRulesRepoCatalog(t *testing.T) {
	var n, exec int
	err := (Rules{Dir: filepath.Join("..", "..", "etc", "rules")}).Stream(context.Background(), func(l contract.Leaf) error {
		n++
		if strings.Contains(l.Text, "executable (audit engine)") {
			exec++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Stream(etc/rules): %v", err)
	}
	if n < 150 {
		t.Fatalf("repo catalog streamed %d leafs, want >= 150", n)
	}
	if exec == 0 {
		t.Fatalf("no executable rules surfaced in the corpus stream")
	}
}
