package facts

import (
	"strings"
	"testing"
)

// src is a compact Source literal for tests: kind:id[:when][:stale]
func mustSrc(t *testing.T, lit string) Source {
	t.Helper()
	parts := strings.Split(lit, ":")
	if len(parts) < 2 {
		t.Fatalf("bad source literal %q", lit)
	}
	s := Source{Kind: parts[0], ID: parts[1]}
	if len(parts) > 2 && parts[2] != "" {
		s.When = parts[2]
	}
	for _, p := range parts {
		if p == "stale" {
			s.Stale = true
		}
	}
	return s
}

func claim(text string, yesLits []string, noLits ...string) Claim {
	c := Claim{Text: text}
	for _, l := range yesLits {
		c.Yes = append(c.Yes, Source{Kind: field(l, 0), ID: field(l, 1)})
	}
	for _, l := range noLits {
		c.No = append(c.No, Source{Kind: field(l, 0), ID: field(l, 1)})
	}
	return c
}

func field(lit string, i int) string {
	parts := strings.SplitN(lit, ":", 2)
	if i >= len(parts) {
		return ""
	}
	return parts[i]
}

// TestAdjudicateFactNotFactMatrix is the core guarantee: every verdict class
// the system can emit has a pinned fixture, so regressions in adjudication
// semantics fail loudly.
func TestAdjudicateFactNotFactMatrix(t *testing.T) {
	t.Run("fact_two_independent_yes", func(t *testing.T) {
		r := Adjudicate(claim("X works",
			[]string{"runtime:probe-a", "config:manifest-b"}))
		if !r.Confirmed || r.Winner != "yes" || r.Rule != RuleTwoSource {
			t.Fatalf("want confirmed/yes/two_source, got %+v", r)
		}
	})
	t.Run("not_fact_two_independent_no", func(t *testing.T) {
		r := Adjudicate(claim("X broken",
			[]string{"narrative:blog-1", "runtime:probe-a"},
			"runtime:probe-c", "config:manifest-d"))
		if !r.Confirmed || r.Winner != "no" {
			t.Fatalf("want confirmed no, got %+v", r)
		}
	})
	t.Run("hypothesis_single_source_is_not_confirmed", func(t *testing.T) {
		r := Adjudicate(claim("X maybe", []string{"narrative:post-1"}))
		if r.Confirmed {
			t.Fatalf("single source must stay unconfirmed: %+v", r)
		}
		if r.Confidence != ConfHypothesis {
			t.Fatalf("want hypothesis, got %q", r.Confidence)
		}
	})
	t.Run("contradiction_fresh_beats_stale", func(t *testing.T) {
		y := []Source{
			mustSrc(t, "runtime:probe-a:2026-08"),
			mustSrc(t, "runtime:probe-b:2026-07"),
		}
		n := []Source{
			mustSrc(t, "narrative:old-doc:2020-01:stale"),
			mustSrc(t, "narrative:older-doc:2019-01:stale"),
		}
		r := Adjudicate(Claim{Text: "current state", Yes: y, No: n})
		if !r.Confirmed || r.Winner != "yes" || r.Rule != RuleTemporalFreshness {
			t.Fatalf("fresh evidence must win: %+v", r)
		}
	})
	t.Run("contradiction_authority_beats_narrative", func(t *testing.T) {
		y := []Source{
			mustSrc(t, "config:deploy-yaml:x"),
			mustSrc(t, "runtime:live-probe:y"),
		}
		n := []Source{
			mustSrc(t, "narrative:deck:a"),
			mustSrc(t, "narrative:talk:b"),
		}
		r := Adjudicate(Claim{Text: "stack truth", Yes: y, No: n})
		if !r.Confirmed || r.Winner != "yes" || r.Rule != RuleAuthorityPairing {
			t.Fatalf("authority pairing must win: %+v", r)
		}
	})
	t.Run("unresolved_tie_stays_hypothesis", func(t *testing.T) {
		y := []Source{mustSrc(t, "narrative:a:1"), mustSrc(t, "narrative:b:1")}
		n := []Source{mustSrc(t, "narrative:c:2"), mustSrc(t, "narrative:d:2")}
		r := Adjudicate(Claim{Text: "tie", Yes: y, No: n})
		if r.Confirmed || r.Rule != RuleUnresolved {
			t.Fatalf("tie must not be confirmed: %+v", r)
		}
	})
	t.Run("duplicate_ids_do_not_inflate_independence", func(t *testing.T) {
		y := []Source{
			mustSrc(t, "runtime:same"),
			mustSrc(t, "config:same"), // same ID → 1 independent source
		}
		r := Adjudicate(Claim{Text: "dup", Yes: y})
		if r.Confirmed || r.YesN != 1 {
			t.Fatalf("same-id sources count once: %+v", r)
		}
	})
	t.Run("empty_claim_hypothesis", func(t *testing.T) {
		r := Adjudicate(Claim{Text: "nothing"})
		if r.Confirmed || r.Rule != RuleSingleSource {
			t.Fatalf("empty claim must be hypothesis: %+v", r)
		}
	})
}

// TestCheckFactRowInvariants pins the facts-file lexicon: a leaf marked
// confirmed must never hide a vs-contradiction, and every leaf needs loc+how
// pointers. These are the guarantees later agents rely on when trusting facts.
func TestCheckFactRowInvariants(t *testing.T) {
	ok := CheckFactRow("l1", "crm x mesh", "oo://p/1", "prove-crm", ConfConfirmed)
	if len(ok) != 0 {
		t.Fatalf("clean confirmed row flagged: %v", ok)
	}
	if bad := CheckFactRow("l2", "crm x mesh vs doc x doc2", "loc", "how", ConfConfirmed); len(bad) == 0 {
		t.Fatal("confirmed with vs-contradiction must be flagged")
	}
	if bad := CheckFactRow("l3", "only-one", "", "", ConfConfirmed); len(bad) < 3 {
		t.Fatalf("missing loc/how/single-source must all flag: %v", bad)
	}
	if bad := CheckFactRow("l4", "a x b vs c x d", "loc", "how", ConfHypothesis); len(bad) != 0 {
		t.Fatalf("well-formed hypothesis contradiction flagged: %v", bad)
	}
	if bad := CheckFactRow("l5", "x", "loc", "how", "weird"); len(bad) == 0 || !strings.Contains(bad[0], "unknown confidence") {
		t.Fatalf("unknown confidence must flag: %v", bad)
	}
	if _, no := ParseSourceField("a x b"); no != "" {
		t.Fatalf("no vs marker must yield empty no-side")
	}
}
