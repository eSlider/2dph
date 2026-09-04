package facts

import "testing"

func src(id, kind string, stale bool) Source {
	return Source{ID: id, Kind: kind, Stale: stale}
}

func TestTwoVsTwoStaysHypothesis(t *testing.T) {
	c := Claim{
		Text: "svc listens on 443",
		Yes: []Source{
			src("docker-ps", KindRuntime, false),
			src("compose", KindConfig, false),
		},
		No: []Source{
			src("docker-ps-old", KindRuntime, false),
			src("compose-old", KindConfig, false),
		},
	}
	r := Adjudicate(c)
	if r.Confirmed || r.Confidence != ConfHypothesis || r.Rule != RuleUnresolved {
		t.Fatalf("2v2 must stay (not confirmed): %+v", r)
	}
	if r.Winner != "" {
		t.Fatalf("unresolved must not name a winner: %+v", r)
	}
}

func TestTemporalFreshnessResolvesStaleSide(t *testing.T) {
	c := Claim{
		Text: "svc listens on 443",
		Yes: []Source{
			src("docker-ps", KindRuntime, false),
			src("compose", KindConfig, false),
		},
		No: []Source{
			src("old-readme", KindNarrative, true),
			src("old-wiki", KindNarrative, true),
		},
	}
	r := Adjudicate(c)
	if !r.Confirmed || r.Rule != RuleTemporalFreshness || r.Winner != "yes" {
		t.Fatalf("fresh yes vs stale no: %+v", r)
	}
}

func TestAuthorityPairingBeatsNarrative(t *testing.T) {
	c := Claim{
		Text: "svc listens on 443",
		Yes: []Source{
			src("docker-ps", KindRuntime, false),
			src("compose", KindConfig, false),
		},
		No: []Source{
			src("readme", KindNarrative, false),
			src("wiki", KindNarrative, false),
		},
	}
	r := Adjudicate(c)
	if !r.Confirmed || r.Rule != RuleAuthorityPairing || r.Winner != "yes" {
		t.Fatalf("A×B vs C×C: %+v", r)
	}
}

func TestTwoSourceYesIsConfirmed(t *testing.T) {
	c := Claim{
		Text: "edge-1 runs the mail server",
		Yes: []Source{
			src("compose", KindConfig, false),
			src("docker-ps", KindRuntime, false),
		},
	}
	r := Adjudicate(c)
	if !r.Confirmed || r.Rule != RuleTwoSource || r.Winner != "yes" {
		t.Fatalf("%+v", r)
	}
}

func TestSingleSourceIsHypothesis(t *testing.T) {
	c := Claim{Text: "maybe", Yes: []Source{src("readme", KindNarrative, false)}}
	r := Adjudicate(c)
	if r.Confirmed || r.Rule != RuleSingleSource {
		t.Fatalf("%+v", r)
	}
}
