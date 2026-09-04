package facts

import "testing"

// TestMatchCompanyNameSignificantToken pins the canonical org x CRM company
// matcher: a CRM display name matches an org label when its normalized form
// contains a significant (>=4 alpha chars) token of the label. Deterministic,
// case-insensitive. Companies are synthetic fixtures.
func TestMatchCompanyNameSignificantToken(t *testing.T) {
	names := []string{"Northwind GmbH", "Globex", "Initech"}
	if got, ok := MatchCompanyName("Globex AG", names); !ok || got != "Globex" {
		t.Fatalf("Globex match = %q, %v; want Globex, true", got, ok)
	}
	// own-org label must not collide with any of the listed companies.
	if got, ok := MatchCompanyName("Mercury SL / mercury.example", names); ok {
		t.Fatalf("mercury must not match any company, got %q", got)
	}
}

// TestMatchCompanyNameIgnoresShortTokens guarantees that a 2-3 char token
// (e.g. "IT", "of") never triggers a false association.
func TestMatchCompanyNameIgnoresShortTokens(t *testing.T) {
	if _, ok := MatchCompanyName("IT GmbH", []string{"IT Services"}); ok {
		t.Fatal("2-char 'it' token must not match")
	}
	if _, ok := MatchCompanyName("", []string{"acme ltd"}); ok {
		t.Fatal("empty label must not match")
	}
	if _, ok := MatchCompanyName("ACME GmbH", nil); ok {
		t.Fatal("no companies must not match")
	}
}

// TestMatchCompanyNameAkaAndLabel covers the "Label A / Label B" convention:
// each slash-separated label is a candidate for the significant-token scan.
func TestMatchCompanyNameAkaAndLabel(t *testing.T) {
	names := []string{"mercury.example GmbH"}
	if got, ok := MatchCompanyName("Mercury SL / mercury.example", names); !ok || got != "mercury.example GmbH" {
		t.Fatalf("slash label match = %q, %v; want mercury.example GmbH, true", got, ok)
	}
}

// TestMatchCompanyNameGenericTokensNeverMatch guards #55's false positive: a
// generic legal/role word ("client", "gmbh", "ag", "systems", "platform", …)
// in a corpus label must never drive a company match. Otherwise the corpus org
// "Data Platform (client)" would match the CRM company "Client not named yet".
func TestMatchCompanyNameGenericTokensNeverMatch(t *testing.T) {
	if got, ok := MatchCompanyName("Data Platform (client)", []string{"Client not named yet", "Omnicorp", "Betacorp"}); ok {
		t.Fatalf("generic 'client' token matched CRM company %q", got)
	}
	if _, ok := MatchCompanyName("IT GmbH", []string{"GmbH Services"}); ok {
		t.Fatal("generic 'gmbh' token must not match")
	}
	if _, ok := MatchCompanyName("NIMBUS GmbH / NIMBUSLABS", []string{"GmbH Labs"}); ok {
		t.Fatal("generic 'gmbh' token must not match")
	}
}
