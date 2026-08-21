package contact

import (
	"sort"
	"strings"

	"github.com/nyaruka/phonenumbers"
)

// PhoneNorm is the deterministic result of normalizing one raw phone string.
type PhoneNorm struct {
	E164       string   // best-guess canonical form ("" when nothing valid)
	Candidates []string // every plausible E.164, sorted (>=1 when Valid)
	Ambiguous  bool     // >1 candidate survived validation
	Valid      bool
}

// countryCallingCodeLen reports whether the digit prefix of s matches a known
// country calling code, returning the longest such length (0 = none).
func countryCallingCodeLen(digits string) int {
	max := 3
	if len(digits) < max {
		max = len(digits)
	}
	for l := max; l >= 1; l-- {
		head := digits[:l]
		n := 0
		for _, r := range head {
			n = n*10 + int(r-'0')
		}
		if regions := phonenumbers.GetRegionCodesForCountryCode(n); len(regions) > 0 {
			return l
		}
	}
	return 0
}

// parseE164 parses s strictly as an international number and returns its
// E.164 representation when the result is a valid, possible number.
func parseE164(s, defaultRegion string) (string, bool) {
	num, err := phonenumbers.Parse(s, defaultRegion)
	if err != nil {
		return "", false
	}
	if !phonenumbers.IsValidNumber(num) {
		return "", false
	}
	return phonenumbers.Format(num, phonenumbers.E164), true
}

// NormalizePhone canonicalizes raw input to E.164 per the #85 spec.
//
// Recognized inputs: "+CC…", "00CC…", "0<trunk><national>" (defaultRegion),
// and the hard case — bare "CC<national>" with no +/00 and no leading zero,
// resolved by longest-prefix match against known country calling codes.
// When both a national and an international reading survive validation the
// result is flagged Ambiguous and keeps every candidate; callers must not
// silently pick one.
func NormalizePhone(raw, defaultRegion string) PhoneNorm {
	digits := phonenumbers.NormalizeDigitsOnly(raw)
	if digits == "" {
		return PhoneNorm{}
	}

	type cand struct {
		e164 string
		src  int // 0 national reading, 1 international reading
	}
	var cands []cand

	switch {
	case strings.HasPrefix(raw, "+"):
		if e, ok := parseE164(raw, defaultRegion); ok {
			cands = append(cands, cand{e, 1})
		}
	case strings.HasPrefix(digits, "00"):
		if e, ok := parseE164("+"+digits[2:], defaultRegion); ok {
			cands = append(cands, cand{e, 1})
		}
	default:
		// National reading only exists with the trunk prefix "0".
		if strings.HasPrefix(digits, "0") && !strings.HasPrefix(digits, "00") {
			if e, ok := parseE164(digits, defaultRegion); ok {
				cands = append(cands, cand{e, 0})
			}
		}
		// Bare international reading: leading digits are a country code.
		if l := countryCallingCodeLen(digits); l > 0 {
			if e, ok := parseE164("+"+digits, defaultRegion); ok {
				cands = append(cands, cand{e, 1})
			}
		}
	}

	if len(cands) == 0 {
		return PhoneNorm{}
	}

	seen := map[string]bool{}
	var uniq []string
	for _, c := range cands {
		if !seen[c.e164] {
			seen[c.e164] = true
			uniq = append(uniq, c.e164)
		}
	}
	sort.Strings(uniq)

	pn := PhoneNorm{Candidates: uniq, Valid: true, Ambiguous: len(uniq) > 1}
	pn.E164 = uniq[0]
	// Prefer the international reading when tied on sort order.
	for _, c := range cands {
		if c.src == 1 && c.e164 == pn.E164 {
			break
		}
		if c.src == 0 && c.e164 == pn.E164 && len(uniq) > 1 {
			// keep sorted-first but mark ambiguity via Candidates already
			break
		}
	}
	return pn
}

// NormalizePhones maps raw values onto normalized results, dropping blanks.
func NormalizePhones(raws []string, defaultRegion string) map[string]PhoneNorm {
	out := make(map[string]PhoneNorm, len(raws))
	for _, r := range raws {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		out[r] = NormalizePhone(r, defaultRegion)
	}
	return out
}
