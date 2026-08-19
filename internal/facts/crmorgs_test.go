package facts

import "testing"

// Mirrors the former bin/tools/test_crm_facts.py CorpusOrgsTest cases.
const corpusOrgsFixture = `schema: 2
meta:
  title: x
orgs:
- id: produktor
  label: ProProdukt SL / produktor.io
  kind: own
  period: 2006–present
  website: https://produktor.io
- id: dyvenia
  label: Dyvenia
  kind: employer
  period: 2023–2025
clients:
- name: One
- name: Two
timeline:
- start: 2001
`

func TestCorpusOrgsParsesLabelKindPeriod(t *testing.T) {
	orgs := CorpusOrgs(corpusOrgsFixture)
	if got := orgs["produktor"].Label; got != "ProProdukt SL / produktor.io" {
		t.Errorf("produktor label = %q", got)
	}
	if got := orgs["produktor"].Kind; got != "own" {
		t.Errorf("produktor kind = %q", got)
	}
	if got := orgs["dyvenia"].Kind; got != "employer" {
		t.Errorf("dyvenia kind = %q", got)
	}
	if got := orgs["produktor"].Period; got != "2006–present" {
		t.Errorf("produktor period = %q", got)
	}
	if got := orgs["produktor"].Website; got != "https://produktor.io" {
		t.Errorf("produktor website = %q", got)
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