package facts

import (
	"fmt"
	"sort"
	"strings"
)

// P3 (epic #324): implication-inference fragment of the 6.042J rule catalog.
//
// The Vinogradov checks in formal.go reason over atomic claims (URL, attr, ±).
// This file adds the one premise shape they cannot express: a conditional
// A → B. From conditionals plus atomic premises the audit derives conclusions
// (modus ponens / modus tollens) and flags a claim set that is jointly
// inconsistent.
//
// Scope is deliberately narrow. De Morgan and quantifier-negation rules from
// the catalog have no premise shape in the claim model (there are no
// connectives or quantifiers), so they stay executable:false (retrieval only)
// until such a shape exists — wiring them now would be dead code. The rule ids
// cited here must exist in etc/rules/6-042/logic.yml (guarded by a test).

// Rule ids cited from the 6.042J catalog (logic.yml). Keep in sync.
const (
	RuleModusPonens  = "logic.inference.modus-ponens"
	RuleModusTollens = "logic.inference.modus-tollens"
)

// Atom — a signed proposition over a subject: the subject has Attr (Neg=false)
// or its negation (Neg=true). Mirrors URLFact's (Attr, Neg).
type Atom struct {
	Subject string
	Attr    string
	Neg     bool
}

// key — identity of an atom for indexing/dedup.
func (a Atom) key() string {
	return a.Subject + "\x00" + a.Attr + "\x00" + negBit(a.Neg)
}

func negBit(neg bool) string {
	if neg {
		return "1"
	}
	return "0"
}

// Premise — an atomic claim with its evidence id and confirmation level.
// Only Confirmed premises take part in a derivation (a hypothesis is not
// asserted, so it cannot license a conclusion).
type Premise struct {
	ID      string
	Subject string
	Attr    string
	Neg     bool
	Conf    string
}

func (p Premise) atom() Atom { return Atom{Subject: p.Subject, Attr: p.Attr, Neg: p.Neg} }

func (p Premise) confirmed() bool { return p.Conf == "" || p.Conf == ConfConfirmed }

// Implication — a conditional claim on one subject: FromAttr → ToAttr, where
// each side is signed (FromNeg/ToNeg). ID is the evidence id.
type Implication struct {
	ID       string
	Subject  string
	FromAttr string
	FromNeg  bool
	ToAttr   string
	ToNeg    bool
}

// Derivation — a conclusion derived from an implication plus a premise.
// Rule names the catalog rule that licensed it; FactIDs are the evidence
// (implication first, then premise).
type Derivation struct {
	Rule    string
	Atom    Atom
	FactIDs []string
}

// Derive computes the implication closure over confirmed premises using modus
// ponens and modus tollens. Deterministic: implications and premises are
// indexed and walked in sorted id order, conclusions deduped by atom.
func Derive(premises []Premise, implications []Implication) []Derivation {
	have := map[string]bool{} // confirmed premise atoms
	for _, p := range premises {
		if p.confirmed() {
			have[p.atom().key()] = true
		}
	}

	imps := append([]Implication(nil), implications...)
	sort.SliceStable(imps, func(i, j int) bool { return imps[i].ID < imps[j].ID })

	seen := map[string]bool{}
	var out []Derivation
	add := func(rule string, a Atom, ids ...string) {
		k := rule + "\x00" + a.key()
		if seen[k] {
			return
		}
		seen[k] = true
		ids = append([]string(nil), ids...)
		out = append(out, Derivation{Rule: rule, Atom: a, FactIDs: ids})
	}

	for _, im := range imps {
		from := Atom{Subject: im.Subject, Attr: im.FromAttr, Neg: im.FromNeg}
		to := Atom{Subject: im.Subject, Attr: im.ToAttr, Neg: im.ToNeg}

		// Modus ponens: From → To holds and From is asserted ⇒ To.
		if have[from.key()] {
			add(RuleModusPonens, to, im.ID, premiseID(premises, from))
		}
		// Modus tollens: From → To holds and ¬To is asserted ⇒ ¬From.
		notTo := Atom{Subject: to.Subject, Attr: to.Attr, Neg: !to.Neg}
		if have[notTo.key()] {
			notFrom := Atom{Subject: from.Subject, Attr: from.Attr, Neg: !from.Neg}
			add(RuleModusTollens, notFrom, im.ID, premiseID(premises, notTo))
		}
	}
	return out
}

// premiseID returns the evidence id of the confirmed premise asserting a.
func premiseID(premises []Premise, a Atom) string {
	for _, p := range premises {
		if p.confirmed() && p.atom().key() == a.key() {
			return p.ID
		}
	}
	return ""
}

// CheckInference runs Derive and reports a claim set that is jointly
// inconsistent: a derived conclusion whose opposite is a confirmed premise.
// Such a set is a real contradiction (premises + conditional cannot all hold),
// so the verdict is reject. derivations that add a new, non-conflicting
// conclusion are not problems — callers use Derive for those.
func CheckInference(premises []Premise, implications []Implication) []Problem {
	have := map[string]Premise{}
	for _, p := range premises {
		if p.confirmed() {
			have[p.atom().key()] = p
		}
	}
	// One inconsistency is one report: an implication whose premises conflict
	// is flagged once (Derive lists modus ponens before modus tollens).
	seen := map[string]bool{}
	var ps []Problem
	for _, d := range Derive(premises, implications) {
		impID := ""
		if len(d.FactIDs) > 0 {
			impID = d.FactIDs[0]
		}
		if seen[impID] {
			continue
		}
		opp := Atom{Subject: d.Atom.Subject, Attr: d.Atom.Attr, Neg: !d.Atom.Neg}
		p, ok := have[opp.key()]
		if !ok {
			continue
		}
		seen[impID] = true
		ids := append([]string(nil), d.FactIDs...)
		ids = append(ids, p.ID)
		sort.Strings(ids)
		ps = append(ps, Problem{
			Rule:    d.Rule,
			URL:     d.Atom.Subject,
			FactIDs: ids,
			Verdict: VerdictReject,
			Message: fmt.Sprintf(
				"inference: %s: из посылок выводится %s, но подтверждено противоположное (%s) — набор посылок противоречив",
				ruleName(d.Rule), atomString(d.Atom), p.ID),
		})
	}
	sortProblems(ps)
	return ps
}

// ruleName — короткое человекочитаемое имя правила каталога (для сообщений).
func ruleName(rule string) string {
	if i := strings.LastIndex(rule, "."); i >= 0 {
		return rule[i+1:]
	}
	return rule
}

// atomString — «attr» / «¬attr» для сообщений.
func atomString(a Atom) string {
	if a.Neg {
		return "¬" + a.Attr
	}
	return a.Attr
}
