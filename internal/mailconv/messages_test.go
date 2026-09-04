package mailconv

// Тесты SplitPersonName: косметика N-1.4 #271 — завершающая скобочная
// декорация « (ExampleCorp)»/« (ExampleOrg e.V.)» не должна утекать в
// given/family (пилот N-1.3 #270: firstName «Jane Doe (ExampleOrg»,
// lastName «e.V.)»). cgo-free, синтетика.

import "testing"

func TestSplitPersonNameStripsTrailingOrgDecoration(t *testing.T) {
	cases := []struct {
		name, email           string
		wantGiven, wantFamily string
	}{
		{"Jane Doe (ExampleOrg e.V.)", "jane.doe@example.org", "Jane", "Doe"},
		{"J. Rivera (ExampleCorp)", "j.rivera@example.com", "J.", "Rivera"},
		{"Bob Builder (ACME GmbH)", "bob@example.com", "Bob", "Builder"},
		{"Mary Jane Watson (ExampleCorp)", "mary@example.com", "Mary Jane", "Watson"},
		{"Sam Rivera (ExampleCorp)", "sam@example.com", "Sam", "Rivera"},
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
		{"ExampleCorp Wartung", "wartung@example.com", "ExampleCorp", "Wartung"},
		{"Jane Doe", "jane.doe@example.com", "Jane", "Doe"},
		{"Jane Doe (ExampleOrg", "jane@example.com", "Jane Doe", "(ExampleOrg"}, // несбалансированная скобка — не декорация
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
	g, f := SplitPersonName("(ExampleCorp)", "info@example.com")
	if g != "Info" || f != "" {
		t.Errorf("SplitPersonName((ExampleCorp)) = %q/%q, want Info/\"\"", g, f)
	}
}

func TestSplitPersonNameNestedDecoration(t *testing.T) {
	g, f := SplitPersonName("Jane Doe (ExampleOrg (e.V.))", "a@example.com")
	if g != "Jane" || f != "Doe" {
		t.Errorf("nested decoration = %q/%q, want Jane/Doe", g, f)
	}
}
