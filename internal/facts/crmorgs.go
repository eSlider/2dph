package facts

import (
	"regexp"
	"strings"
)

// Org mirrors the fields kept from the CV knowledge-mesh orgs block.
type Org struct {
	Label   string
	Kind    string
	Period  string
	Website string
}

// CorpusOrgs parses the orgs block of the CV knowledge-mesh YAML into
// id -> Org, stopping at the first sibling top-level key (clients, timeline,
// tech_weights, nodes, edges). Ported from the former tools/crmfacts
// corpus_orgs so the parser stays covered by unit tests.
func CorpusOrgs(raw string) map[string]Org {
	re := regexp.MustCompile(`(?ms)^orgs:\n(.*?)\n^(?:clients|timeline|tech_weights|nodes|edges):`)
	m := re.FindStringSubmatch(raw)
	if m == nil {
		return nil
	}
	orgs := map[string]Org{}
	cur := ""
	idRe := regexp.MustCompile(`^\s*- id:\s*(\S+)`)
	fRe := regexp.MustCompile(`^\s+(\w+):\s*(.*)$`)
	for _, line := range strings.Split(m[1], "\n") {
		if lm := idRe.FindStringSubmatch(line); lm != nil {
			cur = lm[1]
			orgs[cur] = Org{}
			continue
		}
		if fm := fRe.FindStringSubmatch(line); fm != nil && cur != "" {
			o := orgs[cur]
			switch fm[1] {
			case "label":
				o.Label = strings.TrimSpace(fm[2])
			case "kind":
				o.Kind = strings.TrimSpace(fm[2])
			case "period":
				o.Period = strings.TrimSpace(fm[2])
			case "website":
				o.Website = strings.TrimSpace(fm[2])
			}
			orgs[cur] = o
		}
	}
	return orgs
}