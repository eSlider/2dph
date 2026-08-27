package facts

import (
	"strings"
	"testing"
)

// crmAssocFixture is synthetic (no PII): two corpus orgs and a small CRM graph.
// orgs: ACME Ltd (employer, has persons in CRM) + Globex GmbH (client, no CRM).
// CRM:  ACME Ltd -> [Alice Example]; Solo Corp -> [Bob Example] (CRM-only);
//
//	projects: Rocket->ACME (both sources), Moon (no company), Zen->Unknown (no corpus org).
func crmAssocFixture() (map[string]Org, CRMAssoc) {
	orgs := map[string]Org{
		"acme":   {Label: "ACME Ltd", Kind: "employer", Period: "2020–2024"},
		"globex": {Label: "Globex GmbH", Kind: "client", Period: "2021–2023"},
	}
	g := CRMAssoc{
		CompaniesWithPersons: map[string][]string{
			"ACME Ltd":  {"Alice Example"},
			"Solo Corp": {"Bob Example"},
		},
		ProjectsContacts: map[string]ProjectContacts{
			"DE | ACME Ltd | Rocket": {Title: "DE | ACME Ltd | Rocket", Companies: []string{"ACME Ltd"}},
			"DE | ACME Ltd | Moon":   {Title: "DE | ACME Ltd | Moon", Companies: []string{}},
			"DE | Unknown Co | Zen":  {Title: "DE | Unknown Co | Zen", Companies: []string{"Unknown Co"}},
		},
	}
	return orgs, g
}

// TestCRMAssocFactsTwoSourceRule is the core guarantee of #55: an association
// (person->company, company->project) is PROVEN only when both sources agree
// (corpus org x CRM graph). Every other combination is a reported mismatch,
// never a fact.
func TestCRMAssocFactsTwoSourceRule(t *testing.T) {
	orgs, g := crmAssocFixture()
	proven, _ := CRMAssocFacts(orgs, g)

	// person->company: ACME present in corpus orgs AND has CRM persons -> proven.
	if !contains(proven, "Alice Example is associated with ACME Ltd (role: employer, 2020–2024)") {
		t.Fatalf("two-source person->company not proven; proven=%q", proven)
	}
	// company->project: Rocket present in CRM AND its company matches a corpus org -> proven.
	if !contains(proven, "ACME Ltd is associated with project DE | ACME Ltd | Rocket") {
		t.Fatalf("two-source company->project not proven; proven=%q", proven)
	}

	// Single-source facts must never appear as proven.
	for _, p := range proven {
		if strings.Contains(p, "Globex") {
			t.Fatalf("one-sided corpus org leaked into proven facts: %q", p)
		}
		if strings.Contains(p, "Solo Corp") || strings.Contains(p, "Bob Example") {
			t.Fatalf("CRM-only company leaked into proven facts: %q", p)
		}
		if strings.Contains(p, "Moon") || strings.Contains(p, "Unknown Co") {
			t.Fatalf("one-sided project leaked into proven facts: %q", p)
		}
	}
}

// TestCRMAssocFactsMismatchPaths pins each one-sided case to a precise
// mismatch report (never a fact).
func TestCRMAssocFactsMismatchPaths(t *testing.T) {
	orgs, g := crmAssocFixture()
	proven, mism := CRMAssocFacts(orgs, g)
	if len(proven) != 2 {
		t.Fatalf("want exactly 2 proven facts, got %q", proven)
	}

	want := []string{
		"corpus org 'globex' (Globex GmbH) not found in CRM",
		"CRM company 'Solo Corp' has persons but no corpus org",
		"CRM project 'DE | ACME Ltd | Moon' has no company association (projects_contacts companies empty)",
		"CRM project 'DE | Unknown Co | Zen' company 'Unknown Co' not found in corpus",
	}
	for _, w := range want {
		if !contains(mism, w) {
			t.Errorf("missing mismatch %q; mism=%q", w, mism)
		}
	}
	if got := countMism(mism); got != len(want) {
		t.Errorf("want %d distinct mismatches, got %d: %q", len(want), got, mism)
	}
}

// TestCRMAssocFactsGenericLabelTokenNotAMatch guards the false positive that
// motivated the fix: the corpus org "Markets Platform (client)" used to match
// the CRM company "Client not named yet" through the generic word "client".
func TestCRMAssocFactsGenericLabelTokenNotAMatch(t *testing.T) {
	orgs := map[string]Org{
		"markets": {Label: "Markets Platform (client)", Kind: "client", Period: "2024–2025"},
	}
	g := CRMAssoc{
		CompaniesWithPersons: map[string][]string{
			"Client not named yet": {"Divy Bhatti"},
		},
	}
	proven, mism := CRMAssocFacts(orgs, g)
	if len(proven) != 0 {
		t.Fatalf("generic 'client' token must not prove an association: %q", proven)
	}
	if !contains(mism, "corpus org 'markets' (Markets Platform (client)) not found in CRM") {
		t.Fatalf("one-sided corpus org must be reported as mismatch: %q", mism)
	}
	if !contains(mism, "CRM company 'Client not named yet' has persons but no corpus org") {
		t.Fatalf("one-sided CRM company must be reported as mismatch: %q", mism)
	}
}

// TestCRMAssocFactsEmptyInput: no orgs / no graph -> nothing proven, no crash.
func TestCRMAssocFactsEmptyInput(t *testing.T) {
	if proven, mism := CRMAssocFacts(nil, CRMAssoc{}); len(proven) != 0 || len(mism) != 0 {
		t.Fatalf("empty input must be empty: proven=%q mism=%q", proven, mism)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func countMism(s []string) int {
	seen := map[string]struct{}{}
	for _, x := range s {
		seen[x] = struct{}{}
	}
	return len(seen)
}
