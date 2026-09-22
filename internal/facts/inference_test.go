package facts

import (
	"path/filepath"
	"testing"

	"github.com/eSlider/2dph/internal/rules"
)

func premise(id, subject, attr string, neg bool, conf string) Premise {
	return Premise{ID: id, Subject: subject, Attr: attr, Neg: neg, Conf: conf}
}

func TestDeriveModusPonens(t *testing.T) {
	// site is up ⇒ endpoint answers; site is up.
	premises := []Premise{premise("FACT-1", "https://x.test", "site-up", false, ConfConfirmed)}
	imps := []Implication{{ID: "FACT-2", Subject: "https://x.test", FromAttr: "site-up", ToAttr: "endpoint-answers"}}
	ds := Derive(premises, imps)
	if len(ds) != 1 {
		t.Fatalf("got %d derivations, want 1: %+v", len(ds), ds)
	}
	if ds[0].Rule != RuleModusPonens || ds[0].Atom.Attr != "endpoint-answers" || ds[0].Atom.Neg {
		t.Fatalf("wrong derivation: %+v", ds[0])
	}
	if len(ds[0].FactIDs) != 2 || ds[0].FactIDs[0] != "FACT-2" || ds[0].FactIDs[1] != "FACT-1" {
		t.Fatalf("wrong evidence: %v", ds[0].FactIDs)
	}
}

func TestDeriveModusTollens(t *testing.T) {
	// site is up ⇒ endpoint answers; endpoint does NOT answer ⇒ site is not up.
	premises := []Premise{premise("FACT-3", "https://x.test", "endpoint-answers", true, ConfConfirmed)}
	imps := []Implication{{ID: "FACT-2", Subject: "https://x.test", FromAttr: "site-up", ToAttr: "endpoint-answers"}}
	ds := Derive(premises, imps)
	if len(ds) != 1 {
		t.Fatalf("got %d derivations, want 1: %+v", len(ds), ds)
	}
	if ds[0].Rule != RuleModusTollens || ds[0].Atom.Attr != "site-up" || !ds[0].Atom.Neg {
		t.Fatalf("wrong derivation: %+v", ds[0])
	}
}

func TestDeriveIgnoresHypothesis(t *testing.T) {
	// A hypothesis is not asserted, so it cannot license a conclusion.
	premises := []Premise{premise("OPEN-1", "s", "a", false, ConfHypothesis)}
	imps := []Implication{{ID: "FACT-2", Subject: "s", FromAttr: "a", ToAttr: "b"}}
	if ds := Derive(premises, imps); len(ds) != 0 {
		t.Fatalf("hypothesis licensed a derivation: %+v", ds)
	}
}

func TestDeriveNoPonensWithoutPremise(t *testing.T) {
	imps := []Implication{{ID: "FACT-2", Subject: "s", FromAttr: "a", ToAttr: "b"}}
	if ds := Derive(nil, imps); len(ds) != 0 {
		t.Fatalf("derivation without a premise: %+v", ds)
	}
}

func TestCheckInferenceConflict(t *testing.T) {
	// a ⇒ b, a, and ¬b: the set cannot all be true.
	premises := []Premise{
		premise("FACT-1", "s", "a", false, ConfConfirmed),
		premise("FACT-4", "s", "b", true, ConfConfirmed),
	}
	imps := []Implication{{ID: "FACT-2", Subject: "s", FromAttr: "a", ToAttr: "b"}}
	ps := CheckInference(premises, imps)
	if len(ps) != 1 {
		t.Fatalf("got %d problems, want 1: %+v", len(ps), ps)
	}
	p := ps[0]
	if p.Verdict != VerdictReject || p.Rule != RuleModusPonens || p.URL != "s" {
		t.Fatalf("wrong problem: %+v", p)
	}
	want := []string{"FACT-1", "FACT-2", "FACT-4"}
	if len(p.FactIDs) != 3 || p.FactIDs[0] != want[0] || p.FactIDs[1] != want[1] || p.FactIDs[2] != want[2] {
		t.Fatalf("evidence = %v, want %v", p.FactIDs, want)
	}
}

func TestCheckInferenceTollensConflicts(t *testing.T) {
	// a ⇒ b, ¬b, and a: one inconsistent set. Both rules detect it; Derive
	// proves tollens is exercised, CheckInference reports it once (ponens).
	premises := []Premise{
		premise("FACT-5", "s", "b", true, ConfConfirmed),
		premise("FACT-6", "s", "a", false, ConfConfirmed),
	}
	imps := []Implication{{ID: "FACT-2", Subject: "s", FromAttr: "a", ToAttr: "b"}}
	ds := Derive(premises, imps)
	if len(ds) != 2 {
		t.Fatalf("want ponens+tollens derivations, got %+v", ds)
	}
	ps := CheckInference(premises, imps)
	if len(ps) != 1 || ps[0].Verdict != VerdictReject {
		t.Fatalf("wrong inference conflict: %+v", ps)
	}
}

func TestCheckInferenceClean(t *testing.T) {
	// a ⇒ b and a: derives b, no opposite asserted → no problem (use Derive).
	premises := []Premise{premise("FACT-1", "s", "a", false, ConfConfirmed)}
	imps := []Implication{{ID: "FACT-2", Subject: "s", FromAttr: "a", ToAttr: "b"}}
	if ps := CheckInference(premises, imps); len(ps) != 0 {
		t.Fatalf("clean set flagged: %+v", ps)
	}
	if ds := Derive(premises, imps); len(ds) != 1 {
		t.Fatalf("clean set should derive one conclusion: %+v", ds)
	}
}

// TestInferenceRuleIDsInCatalog guards the citation contract: the rule ids
// this package emits must exist in the committed 6.042J catalog.
func TestInferenceRuleIDsInCatalog(t *testing.T) {
	dir := filepath.Join("..", "..", "etc", "rules", "6-042")
	rs, err := rules.Load(dir)
	if err != nil {
		t.Fatalf("Load(%s): %v", dir, err)
	}
	have := map[string]bool{}
	for _, r := range rs {
		have[r.ID] = true
	}
	for _, id := range []string{RuleModusPonens, RuleModusTollens} {
		if !have[id] {
			t.Fatalf("cited rule %q is not in the catalog %s", id, dir)
		}
	}
}
