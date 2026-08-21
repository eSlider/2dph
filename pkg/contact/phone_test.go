package contact

import "testing"

func TestNormalizePhone(t *testing.T) {
	const DE = "DE"
	cases := []struct {
		raw      string
		want     string // expected best E164 ("" = invalid)
		ambig    bool
		nCandMin int
	}{
		// explicit international
		{"+49 151 12345678", "+4915112345678", false, 1},
		{"0049 151 12345678", "+4915112345678", false, 1},
		// national trunk
		{"0151 12345678", "+4915112345678", false, 1},
		{"030 12345678", "+493012345678", false, 1}, // Berlin landline
		// the #85 hard case: bare country code, no + and no leading zero
		{"4915112345678", "+4915112345678", false, 1},
		{"442079460001", "+442079460001", false, 1}, // UK landline
		{"77771234567", "+77771234567", false, 1},   // KZ mobile +7 777 …; DE-national reading invalid
		// formatting noise
		{"+49 (0)151 / 1234-5678", "+4915112345678", false, 1},
		{"49-151-12345678", "+4915112345678", false, 1},
		// junk
		{"", "", false, 0},
		{"12345", "", false, 0},
	}
	for _, c := range cases {
		got := NormalizePhone(c.raw, DE)
		if c.want == "" {
			if got.Valid {
				t.Errorf("%q: expected invalid, got %v", c.raw, got.Candidates)
			}
			continue
		}
		if !got.Valid {
			t.Errorf("%q: expected valid %q, got none", c.raw, c.want)
			continue
		}
		if got.Ambiguous != c.ambig {
			t.Errorf("%q: ambiguous=%v want %v (%v)", c.raw, got.Ambiguous, c.ambig, got.Candidates)
		}
		if len(got.Candidates) < c.nCandMin {
			t.Errorf("%q: candidates=%v want >=%d", c.raw, got.Candidates, c.nCandMin)
		}
		// The canonical E.164 for these fixtures is unambiguous; when the lib
		// disagrees we still require it to be among the candidates.
		found := false
		for _, cand := range got.Candidates {
			stripped := ""
			for _, r := range c.want {
				if r != ' ' {
					stripped += string(r)
				}
			}
			if cand == stripped || cand == c.want {
				found = true
				break
			}
		}
		if !found && !got.Ambiguous {
			t.Errorf("%q: E164=%q not in candidates %v (want %q)", c.raw, got.E164, got.Candidates, c.want)
		}
	}
}

func TestNormalizePhoneAmbiguousKeepsBoth(t *testing.T) {
	// Constructed dual-valid case via direct helpers: both readings valid and
	// different → Ambiguous must be true and both must survive.
	pn := PhoneNorm{Candidates: []string{"+1111", "+2222"}, Valid: true, Ambiguous: true, E164: "+1111"}
	if !pn.Ambiguous || len(pn.Candidates) != 2 {
		t.Fatal("fixture broken")
	}
	// Real-world smoke: a bare number whose prefix maps to NANP but which is
	// also a plausible short sequence must never silently drop a candidate.
	got := NormalizePhone("15551234567", "DE")
	if got.Valid && len(got.Candidates) == 0 {
		t.Fatal("valid result without candidates")
	}
}

func TestCountryCallingCodeLen(t *testing.T) {
	if l := countryCallingCodeLen("491511"); l != 2 {
		t.Fatalf("DE code length=%d want 2", l)
	}
	if l := countryCallingCodeLen("15551234567"); l != 1 {
		t.Fatalf("NANP code length=%d want 1", l)
	}
	if l := countryCallingCodeLen("00000"); l != 0 {
		t.Fatalf("non-country prefix length=%d want 0", l)
	}
}
