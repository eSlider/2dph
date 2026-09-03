package facts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// sotFixturePath is the synthetic SoT used by append tests (testdata/audit-sot.yml).
func sotFixturePath(t *testing.T) string {
	t.Helper()
	return filepath.Join("testdata", "audit-sot.yml")
}

// copyFixture copies the synthetic SoT into a temp dir and returns its path,
// so append tests never touch the committed fixture.
func copyFixture(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile(sotFixturePath(t))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dst := filepath.Join(t.TempDir(), "source-of-truth.yml")
	if err := os.WriteFile(dst, src, 0o644); err != nil {
		t.Fatalf("write temp SoT: %v", err)
	}
	return dst
}

func card() AuditCard {
	return AuditCard{
		Date:      "2026-09-04",
		Claim:     "второй прогон: demo2 подтверждён вторым источником",
		Premises:  []string{"FACT-9002", "FACT-9001"},
		Inference: InferenceDeduction,
		Gaps:      []string{"OPEN-9001"},
		Counter:   "none",
		Verdict:   VerdictWeaken,
	}
}

func TestVerdictOnlyAcceptRejectWeaken(t *testing.T) {
	for _, v := range []Verdict{VerdictAccept, VerdictReject, VerdictWeaken} {
		if !v.Valid() {
			t.Fatalf("Verdict %q must be valid", v)
		}
	}
	for _, v := range []Verdict{"", "confirm", "ACCEPT", "accept ", "pass"} {
		if v.Valid() {
			t.Fatalf("Verdict %q must be invalid", v)
		}
	}
}

func TestInferenceEnum(t *testing.T) {
	for _, i := range []Inference{InferenceDeduction, InferenceInduction, InferenceAnalogy, InferenceOther} {
		if !i.Valid() {
			t.Fatalf("Inference %q must be valid", i)
		}
	}
	for _, i := range []Inference{"", "guess", "Deduction"} {
		if i.Valid() {
			t.Fatalf("Inference %q must be invalid", i)
		}
	}
}

func TestNextAuditID(t *testing.T) {
	tests := []struct {
		ids  []string
		want string
	}{
		{nil, "AUD-0001"},
		{[]string{}, "AUD-0001"},
		{[]string{"AUD-0001"}, "AUD-0002"},
		{[]string{"AUD-0002", "AUD-0001"}, "AUD-0003"},
		{[]string{"AUD-0009"}, "AUD-0010"},
		{[]string{"AUD-0042", "AUD-0007"}, "AUD-0043"},
		{[]string{"AUD-0999"}, "AUD-1000"},
		{[]string{"FOO-1", "not-an-audit"}, "AUD-0001"},
	}
	for _, tc := range tests {
		if got := NextAuditID(tc.ids); got != tc.want {
			t.Errorf("NextAuditID(%v) = %q, want %q", tc.ids, got, tc.want)
		}
	}
}

func TestValidateAuditCard(t *testing.T) {
	base := card()
	if ps := ValidateAuditCard(base); len(ps) != 0 {
		t.Fatalf("valid card must pass, got problems: %v", ps)
	}

	noClaim := base
	noClaim.Claim = ""
	noPremises := base
	noPremises.Premises = nil
	noInference := base
	noInference.Inference = "guess"
	noCounter := base
	noCounter.Counter = ""
	noVerdict := base
	noVerdict.Verdict = "confirm"
	noDate := base
	noDate.Date = ""
	emptyPremise := base
	emptyPremise.Premises = []string{"FACT-9001", "  "}

	for name, c := range map[string]AuditCard{
		"claim":         noClaim,
		"premises":      noPremises,
		"inference":     noInference,
		"counter":       noCounter,
		"verdict":       noVerdict,
		"date":          noDate,
		"empty premise": emptyPremise,
	} {
		if ps := ValidateAuditCard(c); len(ps) == 0 {
			t.Errorf("%s: must fail validation", name)
		}
	}
}

// auditCount reads a written SoT and returns the audits[] length and the last
// card's id, decoding through the yaml node tree (comments survive that path).
func auditTail(t *testing.T, path string) (int, string, string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written SoT: %v", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("written SoT is not valid yaml: %v", err)
	}
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "audits" {
			continue
		}
		seq := root.Content[i+1]
		last := seq.Content[len(seq.Content)-1]
		var id, verdict string
		for j := 0; j+1 < len(last.Content); j += 2 {
			switch last.Content[j].Value {
			case "id":
				id = last.Content[j+1].Value
			case "verdict":
				verdict = last.Content[j+1].Value
			}
		}
		return len(seq.Content), id, verdict
	}
	t.Fatalf("audits section missing in written SoT")
	return 0, "", ""
}

func TestAppendAuditCardAddsToAudits(t *testing.T) {
	path := copyFixture(t)
	got, err := AppendAuditCard(path, card())
	if err != nil {
		t.Fatalf("AppendAuditCard: %v", err)
	}
	if got.ID != "AUD-0002" {
		t.Fatalf("appended id = %q, want AUD-0002", got.ID)
	}
	if n, id, verdict := auditTail(t, path); n != 2 || id != "AUD-0002" || verdict != "weaken" {
		t.Fatalf("after append: audits=%d last=%s verdict=%s, want 2/AUD-0002/weaken", n, id, verdict)
	}
}

func TestAppendAuditCardTypedRoundTrip(t *testing.T) {
	// Карточка со спецсимволами (двоеточие, кавычки, кириллица) должна
	// пережить yaml-запись и читаться обратно в типизированную структуру.
	path := copyFixture(t)
	c := card()
	c.Claim = "Аудит: demo2 подтверждён — «второй» источник #2 (fixture)"
	c.Premises = []string{"FACT-9002 (source: docker ps x compose.yml)", "FACT-9001"}
	c.Inference = InferenceInduction
	if _, err := AppendAuditCard(path, c); err != nil {
		t.Fatalf("AppendAuditCard: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Audits []AuditCard `yaml:"audits"`
	}
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("typed decode of written SoT: %v", err)
	}
	if len(got.Audits) != 2 {
		t.Fatalf("audits = %d, want 2", len(got.Audits))
	}
	back := got.Audits[1]
	if back.ID != "AUD-0002" || back.Claim != c.Claim || back.Verdict != c.Verdict ||
		back.Inference != c.Inference || back.Counter != "none" || back.Date != "2026-09-04" {
		t.Fatalf("typed round-trip mismatch:\n%+v", back)
	}
	if len(back.Premises) != 2 || back.Premises[0] != c.Premises[0] {
		t.Fatalf("premises round-trip = %v", back.Premises)
	}
}

func TestAppendAuditCardPreservesCommentsAndOtherSections(t *testing.T) {
	path := copyFixture(t)
	if _, err := AppendAuditCard(path, card()); err != nil {
		t.Fatalf("AppendAuditCard: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	for _, probe := range []string{
		"# source-of-truth.yml", // шапка-комментарий
		"# fixture leaf demo",   // inline-комментарий факта
		"FACT-9001",             // прежние секции не тронуты
		"OPEN-9001",
		"phase: draft",
	} {
		if !strings.Contains(out, probe) {
			t.Errorf("written SoT lost %q", probe)
		}
	}
}

func TestAppendAuditCardDeterministic(t *testing.T) {
	run := func() []byte {
		path := copyFixture(t)
		if _, err := AppendAuditCard(path, card()); err != nil {
			t.Fatalf("AppendAuditCard: %v", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	a, b := run(), run()
	if string(a) != string(b) {
		t.Fatal("same card + same SoT must produce byte-identical output")
	}
}

func TestAppendAuditCardSequentialIDs(t *testing.T) {
	path := copyFixture(t)
	c1, err := AppendAuditCard(path, card())
	if err != nil {
		t.Fatal(err)
	}
	if c1.ID != "AUD-0002" {
		t.Fatalf("first append id = %s, want AUD-0002", c1.ID)
	}
	c2 := card()
	c2.Claim = "третий прогон"
	c2.Verdict = VerdictAccept
	c2.Premises = []string{"FACT-9001"}
	got, err := AppendAuditCard(path, c2)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "AUD-0003" {
		t.Fatalf("second append id = %s, want AUD-0003", got.ID)
	}
	if n, _, _ := auditTail(t, path); n != 3 {
		t.Fatalf("audits = %d, want 3", n)
	}
}

func TestAppendAuditCardMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such.yml")
	if _, err := AppendAuditCard(path, card()); err == nil {
		t.Fatal("missing SoT must error")
	} else if !strings.Contains(err.Error(), "no-such.yml") {
		t.Fatalf("error must name the path, got %v", err)
	}
}

func TestAppendAuditCardInvalidVerdictLeavesFileUntouched(t *testing.T) {
	path := copyFixture(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	c := card()
	c.Verdict = "confirm"
	if _, err := AppendAuditCard(path, c); err == nil {
		t.Fatal("invalid verdict must be rejected")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("failed append must not modify the SoT")
	}
}

func TestAppendAuditCardNoAuditsSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty-sot.yml")
	src := "# minimal SoT without audits\nphase: draft\nfacts: []\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := AppendAuditCard(path, card()); err == nil {
		t.Fatal("SoT without audits[] must error")
	}
}
