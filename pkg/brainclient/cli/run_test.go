package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// CLI bin/brain/client.go — сервисный клиент read-контракта (P-9.5):
// подкоманды search/get/stats/audit против httptest-фикстур (без сети,
// без kb.lbug). Логика гейта покрыта в pkg/brainclient; здесь — проводка
// флагов, выходов и формы вывода.

const (
	cliSearchMixed = `{"contract_version":"1.0","query":"where is cs-lexicon","root_filter":"","count":4,"results":[
		{"id":"f-ok","text":"lexicon is under /ops/docs","root":"facts","confidence":"confirmed","score":0.91},
		{"id":"f-hyp","text":"claim a x b vs c x d","root":"facts","confidence":"hypothesis","score":0.8},
		{"id":"f-par","text":"one source","root":"facts","confidence":"partial","score":0.7},
		{"id":"i-note","text":"narrative note","root":"info","confidence":"confirmed","score":0.6}]}`
	cliGet   = `{"contract_version":"1.0","id":"f-ok","root":"facts","confidence":"confirmed","source":"facts","type":"fact","text":"full body"}`
	cliStats = `{"contract_version":"1.0","total":105220,"by_root":{"facts":21,"info":105199},"db":"/var/kb.lbug"}`
	cliAudit = `{"contract_version":"1.0","status":"ok","by_confidence":[
		{"root":"facts","confidence":"confirmed","count":21},
		{"root":"facts","confidence":"hypothesis","count":2},
		{"root":"info","confidence":"confirmed","count":105199}]}`
)

func newCLIServer(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search":
			w.Write([]byte(cliSearchMixed))
		case "/get":
			w.Write([]byte(cliGet))
		case "/stats":
			w.Write([]byte(cliStats))
		case "/audit":
			w.Write([]byte(cliAudit))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

func runCLI(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(args, &out, &errb)
	return code, out.String() + errb.String()
}

func TestCLISearchRootFactsGatedJSON(t *testing.T) {
	ts := newCLIServer(t)
	code, out := runCLI(t, "search", "where is cs-lexicon", "--root", "facts", "--json", "--base", ts.URL)
	if code != 0 {
		t.Fatalf("exit %d, out: %s", code, out)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if m["root_filter"] != "facts" {
		t.Errorf("root_filter = %v, want facts", m["root_filter"])
	}
	confirmed := m["confirmed"].([]any)
	if len(confirmed) != 1 {
		t.Fatalf("confirmed = %d hits, want 1: %s", len(confirmed), out)
	}
	nf := m["not_confirmed"].([]any)
	if len(nf) != 3 {
		t.Fatalf("not_confirmed = %d, want 3: %s", len(nf), out)
	}
	first := nf[0].(map[string]any)
	if first["reason"] == nil {
		t.Errorf("not_confirmed hit must carry reason: %s", out)
	}
}

func TestCLISearchAllRootsContractShape(t *testing.T) {
	ts := newCLIServer(t)
	code, out := runCLI(t, "search", "where is cs-lexicon", "--json", "--base", ts.URL)
	if code != 0 {
		t.Fatalf("exit %d, out: %s", code, out)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if _, ok := m["results"]; !ok {
		t.Errorf("all-roots search must keep the contract shape (results): %s", out)
	}
	if _, ok := m["not_confirmed"]; ok {
		t.Errorf("all-roots search must not invent not_confirmed: %s", out)
	}
}

func TestCLISearchRootFactsHumanMarksNotConfirmed(t *testing.T) {
	ts := newCLIServer(t)
	code, out := runCLI(t, "search", "where is cs-lexicon", "--root", "facts", "--base", ts.URL)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, "(not confirmed)") {
		t.Errorf("human facts output must mark rejected hits (not confirmed):\n%s", out)
	}
	if !strings.Contains(out, "f-ok") {
		t.Errorf("human facts output must list the confirmed fact:\n%s", out)
	}
}

func TestCLIGetStatsAudit(t *testing.T) {
	ts := newCLIServer(t)
	if code, out := runCLI(t, "get", "f-ok", "--body", "--json", "--base", ts.URL); code != 0 {
		t.Fatalf("get exit %d: %s", code, out)
	} else if !strings.Contains(out, `"text": "full body"`) {
		t.Errorf("get --body must include text:\n%s", out)
	}
	if code, out := runCLI(t, "stats", "--json", "--base", ts.URL); code != 0 {
		t.Fatalf("stats exit %d: %s", code, out)
	}
	// audit с гипотезой на facts-корне: гейт должен предупредить (exit 1),
	// а не выдать гистограмму за чистый facts-аудит — и в human, и в --json.
	if code, out := runCLI(t, "audit", "--base", ts.URL); code != 1 {
		t.Fatalf("audit with hypothesis on facts root must exit 1 (gate), got %d:\n%s", code, out)
	} else if !strings.Contains(out, "not confirmed") && !strings.Contains(out, "hypothesis") {
		t.Errorf("audit gate warning must explain the problem:\n%s", out)
	}
	if code, out := runCLI(t, "audit", "--json", "--base", ts.URL); code != 1 {
		t.Fatalf("audit --json with hypothesis on facts root must exit 1 (gate), got %d:\n%s", code, out)
	} else if !strings.Contains(out, `"not_confirmed_facts": 2`) {
		t.Errorf("audit --json must carry the facts_gate verdict:\n%s", out)
	}
}

func TestCLIUsageErrors(t *testing.T) {
	if code, out := runCLI(t); code == 0 {
		t.Fatalf("no command must fail usage, out: %s", out)
	}
	if code, out := runCLI(t, "get"); code != 2 {
		t.Fatalf("get without id must exit 2, got %d: %s", code, out)
	}
	if code, out := runCLI(t, "search", "--base", "http://127.0.0.1:1"); code != 2 {
		t.Fatalf("search without query must exit 2, got %d: %s", code, out)
	}
	if code, out := runCLI(t, "bogus"); code != 2 {
		t.Fatalf("unknown command must exit 2, got %d: %s", code, out)
	}
}

func TestCLIServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
	}))
	defer ts.Close()
	if code, out := runCLI(t, "stats", "--json", "--base", ts.URL); code != 1 {
		t.Fatalf("server error must exit 1, got %d: %s", code, out)
	}
}
