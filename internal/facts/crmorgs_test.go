package facts

import "testing"

// Synthetic org fixture: parser coverage, no real employers.
const corpusOrgsFixture = `schema: 2
meta:
  title: x
orgs:
- id: mercury
  label: Mercury SL / mercury.example
  kind: own
  period: 2015–present
  website: https://mercury.example
- id: northwind
  label: Northwind GmbH
  kind: employer
  period: 2020–2023
clients:
- name: One
- name: Two
timeline:
- start: 2001
`

func TestCorpusOrgsParsesLabelKindPeriod(t *testing.T) {
	orgs := CorpusOrgs(corpusOrgsFixture)
	if got := orgs["mercury"].Label; got != "Mercury SL / mercury.example" {
		t.Errorf("mercury label = %q", got)
	}
	if got := orgs["mercury"].Kind; got != "own" {
		t.Errorf("mercury kind = %q", got)
	}
	if got := orgs["northwind"].Kind; got != "employer" {
		t.Errorf("northwind kind = %q", got)
	}
	if got := orgs["mercury"].Period; got != "2015–present" {
		t.Errorf("mercury period = %q", got)
	}
	if got := orgs["mercury"].Website; got != "https://mercury.example" {
		t.Errorf("mercury website = %q", got)
	}
}

func TestCorpusOrgsDoesNotLeakSiblingKeys(t *testing.T) {
	orgs := CorpusOrgs(corpusOrgsFixture)
	for _, k := range []string{"One", "Two", "timeline"} {
		if _, ok := orgs[k]; ok {
			t.Errorf("sibling key %q leaked into orgs", k)
		}
	}
}

func TestCorpusOrgsMissingBlock(t *testing.T) {
	if orgs := CorpusOrgs("nodes:\n- id: x\n"); len(orgs) != 0 {
		t.Errorf("expected no orgs, got %v", orgs)
	}
}
