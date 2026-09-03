package brainclient

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eSlider/2dph/internal/contract"
)

// Гейт facts (P-9.5): клиент никогда не выдаёт не подтверждённый лист как
// подтверждённый факт. Семантика — docs/brain/read-contract.md: подтверждён
// только root=facts с confidence confirmed (пустое = legacy → confirmed);
// info, hypothesis/partial/estimated/inferred и противоречия (2v2) → (not
// confirmed). Тесты cgo-free, фикстуры inline.

func TestGate(t *testing.T) {
	cases := []struct {
		root, confidence string
		want             bool
	}{
		// Подтверждённые факты.
		{"facts", "confirmed", true},
		{"facts", "", true}, // legacy-лист: пустой confidence = confirmed (read-contract.md)
		// Всё остальное — (not confirmed).
		{"facts", "hypothesis", false}, // 2v2-противоречие, D16: hypothesis до аудита
		{"facts", "partial", false},
		{"facts", "estimated", false},
		{"facts", "inferred", false},
		{"info", "confirmed", false}, // info — нарратив, никогда не факт
		{"info", "", false},
		{"", "", false},
		{"", "confirmed", false},
	}
	for _, tc := range cases {
		if got := Gate(tc.root, tc.confidence); got != tc.want {
			t.Errorf("Gate(%q, %q) = %v, want %v", tc.root, tc.confidence, got, tc.want)
		}
	}
}

func TestGateReason(t *testing.T) {
	// Подтверждённый факт причины не имеет.
	if r := GateReason("facts", "confirmed"); r != "" {
		t.Fatalf("confirmed fact must have empty reason, got %q", r)
	}
	cases := []struct {
		root, confidence string
		wantSub          string
	}{
		{"info", "confirmed", "root=info"},
		{"facts", "hypothesis", "hypothesis"},
		{"facts", "partial", "partial"},
		{"facts", "estimated", "estimated"},
	}
	for _, tc := range cases {
		r := GateReason(tc.root, tc.confidence)
		if r == "" {
			t.Errorf("GateReason(%q, %q) must not be empty", tc.root, tc.confidence)
			continue
		}
		if !strings.HasPrefix(r, "(not confirmed)") {
			t.Errorf("GateReason(%q, %q) = %q, want prefix (not confirmed)", tc.root, tc.confidence, r)
		}
		if !strings.Contains(r, tc.wantSub) {
			t.Errorf("GateReason(%q, %q) = %q, want substring %q", tc.root, tc.confidence, r, tc.wantSub)
		}
	}
}

// searchFixture — ответ сервера на search с root=facts: confirmed-факт,
// гипотеза (2v2-противоречие на facts-корне), partial и info-хит. Клиент
// обязан отделить confirmed от остального и не выдать гипотезу как факт.
const searchFixture = `{
	"contract_version": "1.0",
	"query": "where is cs-lexicon",
	"root_filter": "facts",
	"count": 4,
	"results": [
		{"id": "f-ok",  "text": "lexicon is under /ops/docs", "root": "facts", "confidence": "confirmed", "score": 0.91},
		{"id": "f-hyp", "text": "claim a x b vs c x d",       "root": "facts", "confidence": "hypothesis", "score": 0.80},
		{"id": "f-par", "text": "one source only",            "root": "facts", "confidence": "partial", "score": 0.70},
		{"id": "i-note","text": "narrative note",             "root": "info",  "confidence": "confirmed", "score": 0.60}
	]
}`

func TestGateSearchResponse(t *testing.T) {
	resp, err := contract.ValidateSearchResponse([]byte(searchFixture))
	if err != nil {
		t.Fatalf("fixture must be contract-compliant: %v", err)
	}
	facts := GateSearch(&resp)
	if len(facts.Confirmed) != 1 {
		t.Fatalf("Confirmed = %d hits, want 1 (only f-ok)", len(facts.Confirmed))
	}
	if facts.Confirmed[0].ID != "f-ok" {
		t.Fatalf("Confirmed[0] = %q, want f-ok", facts.Confirmed[0].ID)
	}
	if len(facts.NotConfirmed) != 3 {
		t.Fatalf("NotConfirmed = %d hits, want 3", len(facts.NotConfirmed))
	}
	// Гипотеза (2v2-противоречие) обязана уйти в (not confirmed), а не в факты.
	reasons := map[string]string{}
	for _, nf := range facts.NotConfirmed {
		reasons[nf.ID] = nf.Reason
		if Gate(nf.Root, nf.Confidence) {
			t.Errorf("NotConfirmed hit %s passes the gate", nf.ID)
		}
	}
	for _, id := range []string{"f-hyp", "f-par", "i-note"} {
		if reasons[id] == "" {
			t.Errorf("hit %s must be in NotConfirmed with a reason", id)
		}
	}
	if !strings.Contains(reasons["f-hyp"], "hypothesis") {
		t.Errorf("f-hyp reason %q must mention hypothesis (contradiction)", reasons["f-hyp"])
	}
}

func TestGateSearchEmptyHits(t *testing.T) {
	resp := &contract.SearchResponse{ContractVersion: contract.ReadContractVersion}
	facts := GateSearch(resp)
	if len(facts.Confirmed) != 0 || len(facts.NotConfirmed) != 0 {
		t.Fatalf("empty response must gate to empty, got %d/%d", len(facts.Confirmed), len(facts.NotConfirmed))
	}
	// confirmed обязан быть [] в JSON, а не null (клиентская схема).
	raw, err := json.Marshal(facts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"confirmed":null`) {
		t.Fatalf("confirmed must serialize as [], got: %s", raw)
	}
	if !strings.Contains(string(raw), `"confirmed":[]`) {
		t.Fatalf("confirmed must serialize as [], got: %s", raw)
	}
}

const auditFixture = `{
	"contract_version": "1.0",
	"status": "ok",
	"by_confidence": [
		{"root": "facts", "confidence": "confirmed", "count": 21},
		{"root": "facts", "confidence": "hypothesis", "count": 2},
		{"root": "info",  "confidence": "confirmed", "count": 105199}
	]
}`

func TestGateAudit(t *testing.T) {
	resp, err := contract.ValidateAuditResponse([]byte(auditFixture))
	if err != nil {
		t.Fatalf("fixture must be contract-compliant: %v", err)
	}
	g := GateAudit(&resp)
	if g.ConfirmedFacts != 21 {
		t.Errorf("ConfirmedFacts = %d, want 21", g.ConfirmedFacts)
	}
	if g.NotConfirmedFacts != 2 {
		t.Errorf("NotConfirmedFacts = %d, want 2 (hypothesis rows on facts root)", g.NotConfirmedFacts)
	}
	if g.TotalFacts != 23 {
		t.Errorf("TotalFacts = %d, want 23", g.TotalFacts)
	}
	if g.OK() {
		t.Error("gate must flag hypothesis rows on the facts root")
	}
	// Только confirmed на facts-корне — гейт чист.
	clean := contract.AuditResponse{
		ContractVersion: contract.ReadContractVersion,
		Status:          "ok",
		ByConfidence:    []contract.AuditRow{{Root: "facts", Confidence: "confirmed", Count: 21}},
	}
	if g := GateAudit(&clean); !g.OK() {
		t.Errorf("clean audit must pass the gate: %+v", g)
	}
}
