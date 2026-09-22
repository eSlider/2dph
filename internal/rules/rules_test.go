package rules

import (
	"path/filepath"
	"testing"
)

func TestValidateOK(t *testing.T) {
	rs := []Rule{
		{ID: "logic.inference.modus-ponens", Domain: DomainProofs, Name: "Modus ponens", Form: "P, P → Q  ⊢  Q", Source: "6.042J §3.4", Executable: true},
		{ID: "number.euler.totient", Domain: DomainNumber, Name: "Euler's theorem", Form: "gcd(a, n) = 1  ⊢  a^φ(n) ≡ 1 (mod n)", Source: "6.042J §8.10"},
	}
	if err := Validate(rs); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRejects(t *testing.T) {
	base := Rule{ID: "proof.method.direct", Domain: DomainProofs, Name: "Direct proof", Form: "P  ⊢  Q", Source: "6.042J §1.5"}
	cases := []struct {
		name   string
		mutate func(*Rule)
	}{
		{"no id", func(r *Rule) { r.ID = "" }},
		{"bad id", func(r *Rule) { r.ID = "Proof Method" }},
		{"no domain", func(r *Rule) { r.Domain = "" }},
		{"unknown domain", func(r *Rule) { r.Domain = "astrology" }},
		{"no name", func(r *Rule) { r.Name = "" }},
		{"no form", func(r *Rule) { r.Form = "" }},
		{"no source", func(r *Rule) { r.Source = "" }},
		{"exec without turnstile", func(r *Rule) { r.Executable = true; r.Form = "just so" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.mutate(&r)
			if err := Validate([]Rule{r}); err == nil {
				t.Fatalf("Validate accepted invalid rule %+v", r)
			}
		})
	}
}

func TestValidateDuplicateID(t *testing.T) {
	r := Rule{ID: "logic.inference.modus-ponens", Domain: DomainProofs, Name: "Modus ponens", Form: "P, P → Q  ⊢  Q", Source: "6.042J §3.4"}
	if err := Validate([]Rule{r, r}); err == nil {
		t.Fatal("Validate accepted duplicate ids")
	}
}

func TestLoadFixture(t *testing.T) {
	rs, err := Load(filepath.Join("testdata", "catalog"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(rs) != 2 {
		t.Fatalf("got %d rules, want 2 (index.yml must be skipped)", len(rs))
	}
	// sorted by id
	if rs[0].ID != "number.mod.cancellation" || rs[1].ID != "proof.method.contrapositive" {
		t.Fatalf("not sorted by id: %q, %q", rs[0].ID, rs[1].ID)
	}
	exec := Executable(rs)
	if len(exec) != 1 || exec[0].ID != "proof.method.contrapositive" {
		t.Fatalf("Executable filter wrong: %+v", exec)
	}
}

func TestLoadIndex(t *testing.T) {
	idx, err := LoadIndex(filepath.Join("testdata", "catalog"))
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}
	if idx.Pages <= 0 || idx.License == "" || idx.SHA256 == "" {
		t.Fatalf("index incomplete: %+v", idx)
	}
}

func TestLoadMissingIndex(t *testing.T) {
	if _, err := LoadIndex(t.TempDir()); err == nil {
		t.Fatal("LoadIndex accepted a dir without index.yml")
	}
}

// TestRepoCatalog guards the committed core catalog: it must load, validate,
// and stay above the P2 floor (epic #324).
func TestRepoCatalog(t *testing.T) {
	dir := filepath.Join("..", "..", "etc", "rules", "6-042")
	rs, err := Load(dir)
	if err != nil {
		t.Fatalf("Load(%s): %v", dir, err)
	}
	if len(rs) < 35 {
		t.Fatalf("core catalog has %d rules, want >= 35", len(rs))
	}
	exec := Executable(rs)
	if len(exec) < 25 {
		t.Fatalf("core catalog has %d executable rules, want >= 25", len(exec))
	}
	byDomain := map[Domain]int{}
	for _, r := range rs {
		byDomain[r.Domain]++
	}
	for _, d := range []Domain{DomainProofs, DomainLogic, DomainInduction} {
		if byDomain[d] == 0 {
			t.Fatalf("core catalog has no rules for domain %q", d)
		}
	}
	if _, err := LoadIndex(dir); err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}
}
