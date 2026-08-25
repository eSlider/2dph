package facts

import "strings"

// MatchCompanyName returns the first CRM company display name whose
// normalized form contains a significant token (>=4 alpha chars) of the org
// label. Deterministic — the canonical corpus x CRM company matcher, shared by
// prove-crm (brain writer) and fulfill-assoc (oo CLI emitter) so the
// association rule lives in exactly one place (repo rule #10).
func MatchCompanyName(label string, companyNames []string) (string, bool) {
	toks := significantTokens(label)
	if len(toks) == 0 {
		return "", false
	}
	for _, name := range companyNames {
		nl := strings.ToLower(strings.TrimSpace(name))
		for _, t := range toks {
			if strings.Contains(nl, t) {
				return name, true
			}
		}
	}
	return "", false
}

// significantTokens extracts lowercased words of >=4 alpha chars, trimmed of
// surrounding punctuation. Short tokens (it, of, ag) and generic legal/role
// words (client, gmbh, ag, systems, platform, …) never drive a company match:
// otherwise the corpus label "Markets Platform (client)" would match the CRM
// company "Client not named yet" through the word "client" (#55).
func significantTokens(s string) []string {
	var out []string
	for _, w := range strings.Fields(s) {
		w = strings.Trim(w, "()/.,'\"")
		alpha := 0
		for _, r := range w {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				alpha++
			}
		}
		if alpha < 4 {
			continue
		}
		lw := strings.ToLower(w)
		if _, stop := companyStopwords[lw]; stop {
			continue
		}
		out = append(out, lw)
	}
	return out
}

// companyStopwords are generic legal/role tokens that must never be the basis
// of a company-name association. A real company match is driven by its proper
// name, not by a suffix or role annotation.
var companyStopwords = map[string]struct{}{
	"client": {}, "customer": {}, "employer": {}, "company": {},
	"gmbh": {}, "ag": {}, "ltd": {}, "limited": {}, "llc": {}, "inc": {},
	"group": {}, "platform": {}, "systems": {},
}
