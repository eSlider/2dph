package facts

import (
	"fmt"
	"sort"
)

// ProjectContacts mirrors the ooCRM graph.json projects_contacts entry: a
// project title plus the list of companies associated with it. The Companies
// list is the OnlyOffice-side evidence for company->project associations; when
// empty the graph producer did not populate it (a data gap, reported, never a
// fact).
type ProjectContacts struct {
	Title     string   `json:"title"`
	Companies []string `json:"companies"`
}

// CRMAssoc is the ooCRM graph.json shape consumed by the CRM association
// merger. CompaniesWithPersons is the person->company evidence, ProjectsContacts
// the company->project evidence.
type CRMAssoc struct {
	CompaniesWithPersons map[string][]string        `json:"companies_with_persons"`
	ProjectsContacts     map[string]ProjectContacts `json:"projects_contacts"`
}

// CRMAssocFacts applies the detective two-source rule to CRM associations
// (person->company, company->project): an association is PROVEN only when the
// company appears in BOTH the corpus orgs AND the ooCRM graph. Every other
// combination is a reported mismatch, never a fact. Deterministic output order.
//
// Shared by prove-crm (brain writer) so the association rule lives in exactly
// one place (repo rule #10).
func CRMAssocFacts(orgs map[string]Org, g CRMAssoc) (proven, mismatches []string) {
	// ---- person -> company -------------------------------------------------
	crmCompanies := make([]string, 0, len(g.CompaniesWithPersons))
	for c := range g.CompaniesWithPersons {
		crmCompanies = append(crmCompanies, c)
	}
	sort.Strings(crmCompanies)

	ids := make([]string, 0, len(orgs))
	for id := range orgs {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	matchedCorpus := map[string]bool{}
	for _, id := range ids {
		o := orgs[id]
		label := o.Label
		if label == "" {
			label = id
		}
		key, ok := MatchCompanyName(label, crmCompanies)
		if !ok {
			mismatches = append(mismatches, fmt.Sprintf("corpus org '%s' (%s) not found in CRM", id, o.Label))
			continue
		}
		persons := g.CompaniesWithPersons[key]
		if len(persons) == 0 {
			mismatches = append(mismatches, fmt.Sprintf("corpus org '%s' (%s) has no CRM persons", id, o.Label))
			continue
		}
		matchedCorpus[key] = true
		for _, p := range persons {
			proven = append(proven, fmt.Sprintf("%s is associated with %s (role: %s, %s)",
				p, o.Label, def(o.Kind, "?"), o.Period))
		}
	}

	// CRM companies with persons that no corpus org matches: one-sided, CRM-only.
	for _, c := range crmCompanies {
		if matchedCorpus[c] {
			continue
		}
		if len(g.CompaniesWithPersons[c]) > 0 {
			mismatches = append(mismatches, fmt.Sprintf("CRM company '%s' has persons but no corpus org", c))
		}
	}

	// ---- company -> project ------------------------------------------------
	corpusLabels := make([]string, 0, len(orgs))
	for _, id := range ids {
		label := orgs[id].Label
		if label == "" {
			label = id
		}
		corpusLabels = append(corpusLabels, label)
	}

	titles := make([]string, 0, len(g.ProjectsContacts))
	for t := range g.ProjectsContacts {
		titles = append(titles, t)
	}
	sort.Strings(titles)

	for _, t := range titles {
		p := g.ProjectsContacts[t]
		if len(p.Companies) == 0 {
			mismatches = append(mismatches, fmt.Sprintf(
				"CRM project '%s' has no company association (projects_contacts companies empty)", t))
			continue
		}
		for _, c := range p.Companies {
			if _, ok := MatchCompanyName(c, corpusLabels); ok {
				proven = append(proven, fmt.Sprintf("%s is associated with project %s", c, t))
			} else {
				mismatches = append(mismatches, fmt.Sprintf(
					"CRM project '%s' company '%s' not found in corpus", t, c))
			}
		}
	}

	return proven, mismatches
}

func def(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
