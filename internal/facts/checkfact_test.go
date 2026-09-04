package facts

import "testing"

func TestCheckFactRowConfirmed(t *testing.T) {
	problems := CheckFactRow("l1", "crm.md x contract.md", "contracts/a.md", "test", ConfConfirmed)
	if len(problems) != 0 {
		t.Fatalf("clean confirmed fact must pass: %v", problems)
	}
}

func TestCheckFactRowConfirmedSingleSource(t *testing.T) {
	problems := CheckFactRow("l1", "crm.md", "crm/a.md", "test", ConfConfirmed)
	if len(problems) != 1 || problems[0] != "l1: needs 2-source evidence in source, got 'crm.md'" {
		t.Fatalf("got %v", problems)
	}
}

func TestCheckFactRowConfirmedVsContradiction(t *testing.T) {
	problems := CheckFactRow("l1", "a x b vs c x d", "x/y.md", "test", ConfConfirmed)
	found := false
	for _, p := range problems {
		if p == "l1: confirmed fact cannot keep a vs-contradiction" {
			found = true
		}
	}
	if !found {
		t.Fatalf("got %v", problems)
	}
}

func TestCheckFactRowHypothesisShape(t *testing.T) {
	problems := CheckFactRow("l2", "a x b vs c x d", "x/y.md", "test", ConfHypothesis)
	if len(problems) != 0 {
		t.Fatalf("well-formed contradiction must pass: %v", problems)
	}
}

func TestCheckFactRowHypothesisMalformed(t *testing.T) {
	problems := CheckFactRow("l2", "a only", "x/y.md", "test", ConfHypothesis)
	if len(problems) != 1 {
		t.Fatalf("got %v", problems)
	}
}

func TestCheckFactRowMissingLocHow(t *testing.T) {
	problems := CheckFactRow("l3", "a x b", "", "", ConfConfirmed)
	want := []string{
		"l3: missing loc (evidence pointer)",
		"l3: missing how",
	}
	if len(problems) != 2 || problems[0] != want[0] || problems[1] != want[1] {
		t.Fatalf("got %v", problems)
	}
}

func TestCheckFactRowUnknownConfidence(t *testing.T) {
	problems := CheckFactRow("l4", "a x b", "x/y.md", "test", "guessed")
	if len(problems) != 1 || problems[0] != "l4: unknown confidence 'guessed'" {
		t.Fatalf("got %v", problems)
	}
}

func TestCheckFactRowPartial(t *testing.T) {
	if problems := CheckFactRow("l5", "solo", "x/y.md", "test", ConfPartial); len(problems) != 0 {
		t.Fatalf("partial must not fail: %v", problems)
	}
}

func TestParseSourceField(t *testing.T) {
	yes, no := ParseSourceField("a x b vs c x d")
	if yes != "a x b" || no != "c x d" {
		t.Fatalf("got %q %q", yes, no)
	}
	yes, no = ParseSourceField("single x source")
	if yes != "single x source" || no != "" {
		t.Fatalf("got %q %q", yes, no)
	}
}