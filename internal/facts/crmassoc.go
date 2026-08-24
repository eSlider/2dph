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
// surrounding punctuation. Short tokens (it, of, ag) never drive a match.
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
		if alpha >= 4 {
			out = append(out, strings.ToLower(w))
		}
	}
	return out
}
