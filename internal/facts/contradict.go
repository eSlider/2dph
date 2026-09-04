// Package facts is cgo-free evidence rules: D16 contradictions
// (Adjudicate/CheckFactRow) and L-9.3 formal URL checks
// (CheckFormal/CanonicalURL, identity/contradiction/excluded_middle).
package facts

import "strconv"

const (
	ConfConfirmed  = "confirmed"
	ConfHypothesis = "hypothesis"

	RuleUnresolved        = "unresolved"
	RuleTemporalFreshness = "temporal_freshness"
	RuleAuthorityPairing  = "authority_pairing"
	RuleTwoSource         = "two_source"
	RuleSingleSource      = "single_source"

	KindRuntime   = "runtime"
	KindConfig    = "config"
	KindNarrative = "narrative"
)

// Source is one independent pointer on a yes or no side.
type Source struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	When  string `json:"when,omitempty"`
	Stale bool   `json:"stale,omitempty"`
}

// Claim is one assertion with yes/no evidence lists.
type Claim struct {
	Text string   `json:"text"`
	Yes  []Source `json:"yes"`
	No   []Source `json:"no"`
}

// Result is audit output. Confirmed=false means `(not confirmed)`.
type Result struct {
	Text       string `json:"text"`
	Confidence string `json:"confidence"`
	Confirmed  bool   `json:"confirmed"`
	Rule       string `json:"rule"`
	Winner     string `json:"winner,omitempty"`
	YesN       int    `json:"yes"`
	NoN        int    `json:"no"`
}

func independent(ss []Source) int {
	seen := map[string]struct{}{}
	for i, s := range ss {
		id := s.ID
		if id == "" {
			id = s.Kind + "#" + strconv.Itoa(i)
		}
		seen[id] = struct{}{}
	}
	return len(seen)
}

func freshN(ss []Source) int {
	n := 0
	for _, s := range ss {
		if !s.Stale {
			n++
		}
	}
	return n
}

func strongN(ss []Source) int {
	n := 0
	for _, s := range ss {
		if s.Kind == KindRuntime || s.Kind == KindConfig {
			n++
		}
	}
	return n
}

func out(c Claim, conf, rule, winner string) Result {
	return Result{
		Text:       c.Text,
		Confidence: conf,
		Confirmed:  conf == ConfConfirmed,
		Rule:       rule,
		Winner:     winner,
		YesN:       independent(c.Yes),
		NoN:        independent(c.No),
	}
}

// Adjudicate applies D16: ≥2 yes vs ≥2 no stays hypothesis until a rule fires.
// Order: temporal_freshness, then authority_pairing (A/B beats narrative C).
func Adjudicate(c Claim) Result {
	yesN := independent(c.Yes)
	noN := independent(c.No)
	if yesN < 2 || noN < 2 {
		if yesN >= 2 {
			return out(c, ConfConfirmed, RuleTwoSource, "yes")
		}
		if noN >= 2 {
			return out(c, ConfConfirmed, RuleTwoSource, "no")
		}
		return out(c, ConfHypothesis, RuleSingleSource, "")
	}
	yf, nf := freshN(c.Yes), freshN(c.No)
	if yf >= 2 && nf < 2 {
		return out(c, ConfConfirmed, RuleTemporalFreshness, "yes")
	}
	if nf >= 2 && yf < 2 {
		return out(c, ConfConfirmed, RuleTemporalFreshness, "no")
	}
	ys, ns := strongN(c.Yes), strongN(c.No)
	if ys >= 2 && ns < 2 {
		return out(c, ConfConfirmed, RuleAuthorityPairing, "yes")
	}
	if ns >= 2 && ys < 2 {
		return out(c, ConfConfirmed, RuleAuthorityPairing, "no")
	}
	return out(c, ConfHypothesis, RuleUnresolved, "")
}
