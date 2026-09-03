package mailconv

// Тесты SplitPersonName: косметика N-1.4 #271 — завершающая скобочная
// декорация « (WhereGroup)»/« (FOSSGIS e.V.)» не должна утекать в
// given/family (пилот N-1.3 #270: firstName «Astrid Emde (FOSSGIS»,
// lastName «e.V.)»). cgo-free, синтетика.

import "testing"

func TestSplitPersonNameStripsTrailingOrgDecoration(t *testing.T) {
	cases := []struct {
		name, email           string
		wantGiven, wantFamily string
	}{
		{"Astrid Emde (FOSSGIS e.V.)", "astrid.emde@fossgis.de", "Astrid", "Emde"},
		{"U. Rothstein (WhereGroup)", "uli.rothstein@wheregroup.com", "U.", "Rothstein"},
		{"Bob Builder (ACME GmbH)", "bob@example.com", "Bob", "Builder"},
		{"Arash Rashid Pour (WhereGroup)", "arash@example.com", "Arash Rashid", "Pour"},
		{"Paul Schmidt (WhereGroup)", "paul@example.com", "Paul", "Schmidt"},
	}
	for _, c := range cases {
		g, f := SplitPersonName(c.name, c.email)
		if g != c.wantGiven || f != c.wantFamily {
			t.Errorf("SplitPersonName(%q) = %q/%q, want %q/%q", c.name, g, f, c.wantGiven, c.wantFamily)
		}
	}
}

func TestSplitPersonNameWithoutDecorationUntouched(t *testing.T) {
	cases := []struct {
		name, email           string
		wantGiven, wantFamily string
	}{
		{"Bob Builder", "bob@example.com", "Bob", "Builder"},
		{"WhereGroup Wartung", "wartung@wheregroup.com", "WhereGroup", "Wartung"},
		{"Astrid Emde", "astrid.emde@wheregroup.com", "Astrid", "Emde"},
		{"Astrid Emde (FOSSGIS", "astrid@example.com", "Astrid Emde", "(FOSSGIS"}, // несбалансированная скобка — не декорация
	}
	for _, c := range cases {
		g, f := SplitPersonName(c.name, c.email)
		if g != c.wantGiven || f != c.wantFamily {
			t.Errorf("SplitPersonName(%q) = %q/%q, want %q/%q", c.name, g, f, c.wantGiven, c.wantFamily)
		}
	}
}

func TestSplitPersonNameAllParensFallsBackToLocalPart(t *testing.T) {
	// имя целиком в скобках — декорация без имени; fallback на локальную часть
	g, f := SplitPersonName("(WhereGroup)", "info@wheregroup.com")
	if g != "Info" || f != "" {
		t.Errorf("SplitPersonName((WhereGroup)) = %q/%q, want Info/\"\"", g, f)
	}
}

func TestSplitPersonNameNestedDecoration(t *testing.T) {
	g, f := SplitPersonName("Astrid Emde (FOSSGIS (e.V.))", "a@example.com")
	if g != "Astrid" || f != "Emde" {
		t.Errorf("nested decoration = %q/%q, want Astrid/Emde", g, f)
	}
}
