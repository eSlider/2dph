package facts

import "testing"

// TestMatchCompanyNameSignificantToken pins the canonical corpus x CRM company
// matcher shared by prove-crm and fulfill-assoc: a CRM display name matches an
// org label when its normalized form contains a significant (>=4 alpha chars)
// token of the label. Deterministic, case-insensitive.
func TestMatchCompanyNameSignificantToken(t *testing.T) {
	names := []string{"GRID GmbH", "WhereGroup", "Dyvenia"}
	if got, ok := MatchCompanyName("WhereGroup AG", names); !ok || got != "WhereGroup" {
		t.Fatalf("WhereGroup match = %q, %v; want WhereGroup, true", got, ok)
	}
	// produktor label must not collide with any of the listed companies.
	if got, ok := MatchCompanyName("ProProdukt SL / produktor.io", names); ok {
		t.Fatalf("produktor must not match any company, got %q", got)
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
	names := []string{"produktor.io GmbH"}
	if got, ok := MatchCompanyName("ProProdukt SL / produktor.io", names); !ok || got != "produktor.io GmbH" {
		t.Fatalf("slash label match = %q, %v; want produktor.io GmbH, true", got, ok)
	}
}

// TestMatchCompanyNameGenericTokensNeverMatch guards #55's false positive: a
// generic legal/role word ("client", "gmbh", "ag", "systems", "platform", …)
// in a corpus label must never drive a company match. Otherwise the corpus org
// "Markets Platform (client)" matched the CRM company "Client not named yet".
func TestMatchCompanyNameGenericTokensNeverMatch(t *testing.T) {
	if got, ok := MatchCompanyName("Markets Platform (client)", []string{"Client not named yet", "Capitole", "talabat"}); ok {
		t.Fatalf("generic 'client' token matched CRM company %q", got)
	}
	if _, ok := MatchCompanyName("IT GmbH", []string{"GmbH Services"}); ok {
		t.Fatal("generic 'gmbh' token must not match")
	}
	if _, ok := MatchCompanyName("GRID GmbH / GRIDFACTOR", []string{"GmbH Factor"}); ok {
		t.Fatal("generic 'gmbh' token must not match")
	}
}
