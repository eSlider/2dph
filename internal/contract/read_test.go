package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

// Read-contract (P-9.4, docs/brain/read-contract.md): нормативный формат
// ответов search/get/stats/audit. Тесты cgo-free, фикстуры inline.

const (
	fixtureSearch = `{
		"contract_version": "1.0",
		"query": "matrix federation",
		"root_filter": "",
		"as_of": "2026-01-01",
		"count": 2,
		"results": [
			{
				"id": "4b3dfcae6099f20a",
				"text": "leaf text",
				"root": "facts",
				"confidence": "confirmed",
				"score": 0.9,
				"snippet": "leaf te",
				"valid_from": "2025-01-01",
				"valid_to": "2026-12-31",
				"hops": [{"id": "f1", "label": "File", "name": "a.md", "depth": 1}]
			},
			{
				"id": "9f20a4909f7b16a4",
				"text": "info leaf",
				"root": "info",
				"score": 0.7
			}
		],
		"web": {
			"status": "ok",
			"note": "second source",
			"results": [{"rank": 1, "title": "t", "url": "https://example.com", "snippet": "s", "engine": "searxng"}]
		}
	}`
	fixtureGet    = `{"contract_version":"1.0","id":"4b3dfcae6099f20a","root":"info","confidence":"confirmed","source":"mail","type":"mail","text":"full body"}`
	fixtureGetMin = `{"contract_version":"1.0","id":"4b3dfcae6099f20a","root":"facts","confidence":"confirmed","source":"docs","type":"fact","snippet":"clip"}`
	fixtureStats  = `{"contract_version":"1.0","total":105210,"by_root":{"facts":21,"info":105189},"db":"/var/kb.lbug","model":"minishlab/potion-multilingual-128M"}`
	fixtureAudit  = `{"contract_version":"1.0","status":"ok","by_confidence":[{"root":"facts","confidence":"confirmed","count":21},{"root":"info","confidence":"confirmed","count":105189}]}`
)

func mustErr(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err.Error(), want)
	}
}

func TestValidateSearchResponseValid(t *testing.T) {
	got, err := ValidateSearchResponse([]byte(fixtureSearch))
	if err != nil {
		t.Fatalf("valid search rejected: %v", err)
	}
	if got.ContractVersion != ReadContractVersion {
		t.Fatalf("version = %q, want %q", got.ContractVersion, ReadContractVersion)
	}
	if got.Count != 2 || len(got.Results) != 2 {
		t.Fatalf("count/results mismatch: %d / %d", got.Count, len(got.Results))
	}
	if got.Results[0].Root != "facts" || got.Results[0].Confidence != "confirmed" {
		t.Fatalf("hit[0] root/confidence: %q / %q", got.Results[0].Root, got.Results[0].Confidence)
	}
	if got.Web == nil || got.Web.Status != "ok" || len(got.Web.Results) != 1 {
		t.Fatalf("web block: %+v", got.Web)
	}
	if len(got.Results[0].Hops) != 1 || got.Results[0].Hops[0].Name != "a.md" {
		t.Fatalf("hops: %+v", got.Results[0].Hops)
	}
}

func TestValidateSearchResponseRequiredFields(t *testing.T) {
	cases := []struct {
		name string
		edit func(*map[string]any)
		want string
	}{
		{"missing contract_version", func(m *map[string]any) { delete(*m, "contract_version") }, "contract_version"},
		{"incompatible version", func(m *map[string]any) { (*m)["contract_version"] = "2.0" }, "version"},
		{"missing query", func(m *map[string]any) { delete(*m, "query") }, "query"},
		{"missing root_filter", func(m *map[string]any) { delete(*m, "root_filter") }, "root_filter"},
		{"negative count", func(m *map[string]any) { (*m)["count"] = -1 }, "count"},
		{"results null", func(m *map[string]any) { (*m)["results"] = nil }, "results"},
		{"as_of not a day", func(m *map[string]any) { (*m)["as_of"] = "2026-13-40" }, "as_of"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m map[string]any
			if err := json.Unmarshal([]byte(fixtureSearch), &m); err != nil {
				t.Fatal(err)
			}
			tc.edit(&m)
			body, _ := json.Marshal(m)
			_, err := ValidateSearchResponse(body)
			mustErr(t, err, tc.want)
		})
	}
}

func TestValidateSearchResponseHits(t *testing.T) {
	cases := []struct {
		name string
		edit func(*map[string]any)
		want string
	}{
		{"hit missing id", func(m *map[string]any) { delete((*m)["results"].([]any)[0].(map[string]any), "id") }, "results[0].id"},
		{"hit bad root", func(m *map[string]any) { (*m)["results"].([]any)[0].(map[string]any)["root"] = "other" }, "root"},
		{"hit bad valid_from", func(m *map[string]any) { (*m)["results"].([]any)[0].(map[string]any)["valid_from"] = "01-01-2025" }, "valid_from"},
		{"hop missing name", func(m *map[string]any) {
			delete((*m)["results"].([]any)[0].(map[string]any)["hops"].([]any)[0].(map[string]any), "name")
		}, "hops[0].name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m map[string]any
			if err := json.Unmarshal([]byte(fixtureSearch), &m); err != nil {
				t.Fatal(err)
			}
			tc.edit(&m)
			body, _ := json.Marshal(m)
			_, err := ValidateSearchResponse(body)
			mustErr(t, err, tc.want)
		})
	}
}

func TestValidateSearchResponseWeb(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal([]byte(fixtureSearch), &m); err != nil {
		t.Fatal(err)
	}
	w := m["web"].(map[string]any)
	delete(w, "status")
	body, _ := json.Marshal(m)
	_, err := ValidateSearchResponse(body)
	mustErr(t, err, "web.status")
}

func TestValidateGetResponse(t *testing.T) {
	if _, err := ValidateGetResponse([]byte(fixtureGet)); err != nil {
		t.Fatalf("valid get rejected: %v", err)
	}
	if _, err := ValidateGetResponse([]byte(fixtureGetMin)); err != nil {
		t.Fatalf("valid get (snippet) rejected: %v", err)
	}
	cases := []struct {
		name string
		edit func(*map[string]any)
		want string
	}{
		{"missing version", func(m *map[string]any) { delete(*m, "contract_version") }, "contract_version"},
		{"missing id", func(m *map[string]any) { delete(*m, "id") }, "id"},
		{"missing source", func(m *map[string]any) { delete(*m, "source") }, "source"},
		{"missing type", func(m *map[string]any) { delete(*m, "type") }, "type"},
		{"missing confidence", func(m *map[string]any) { delete(*m, "confidence") }, "confidence"},
		{"bad root", func(m *map[string]any) { (*m)["root"] = "evidence" }, "root"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m map[string]any
			if err := json.Unmarshal([]byte(fixtureGet), &m); err != nil {
				t.Fatal(err)
			}
			tc.edit(&m)
			body, _ := json.Marshal(m)
			_, err := ValidateGetResponse(body)
			mustErr(t, err, tc.want)
		})
	}
}

func TestValidateStatsResponse(t *testing.T) {
	got, err := ValidateStatsResponse([]byte(fixtureStats))
	if err != nil {
		t.Fatalf("valid stats rejected: %v", err)
	}
	if got.Total != 105210 || got.ByRoot["facts"] != 21 || got.ByRoot["info"] != 105189 {
		t.Fatalf("stats totals: %+v", got)
	}
	cases := []struct {
		name string
		edit func(*map[string]any)
		want string
	}{
		{"missing version", func(m *map[string]any) { delete(*m, "contract_version") }, "contract_version"},
		{"missing by_root", func(m *map[string]any) { delete(*m, "by_root") }, "by_root"},
		{"missing db", func(m *map[string]any) { delete(*m, "db") }, "db"},
		{"negative total", func(m *map[string]any) { (*m)["total"] = -1 }, "total"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m map[string]any
			if err := json.Unmarshal([]byte(fixtureStats), &m); err != nil {
				t.Fatal(err)
			}
			tc.edit(&m)
			body, _ := json.Marshal(m)
			_, err := ValidateStatsResponse(body)
			mustErr(t, err, tc.want)
		})
	}
}

func TestValidateAuditResponse(t *testing.T) {
	got, err := ValidateAuditResponse([]byte(fixtureAudit))
	if err != nil {
		t.Fatalf("valid audit rejected: %v", err)
	}
	if got.Status != "ok" || len(got.ByConfidence) != 2 {
		t.Fatalf("audit: %+v", got)
	}
	cases := []struct {
		name string
		edit func(*map[string]any)
		want string
	}{
		{"missing version", func(m *map[string]any) { delete(*m, "contract_version") }, "contract_version"},
		{"bad status", func(m *map[string]any) { (*m)["status"] = "fail" }, "status"},
		{"missing by_confidence", func(m *map[string]any) { delete(*m, "by_confidence") }, "by_confidence"},
		{"row bad root", func(m *map[string]any) { (*m)["by_confidence"].([]any)[0].(map[string]any)["root"] = "other" }, "root"},
		{"row missing confidence", func(m *map[string]any) { delete((*m)["by_confidence"].([]any)[0].(map[string]any), "confidence") }, "confidence"},
		{"row negative count", func(m *map[string]any) { (*m)["by_confidence"].([]any)[0].(map[string]any)["count"] = -3 }, "count"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m map[string]any
			if err := json.Unmarshal([]byte(fixtureAudit), &m); err != nil {
				t.Fatal(err)
			}
			tc.edit(&m)
			body, _ := json.Marshal(m)
			_, err := ValidateAuditResponse(body)
			mustErr(t, err, tc.want)
		})
	}
}

func TestCompatibleVersion(t *testing.T) {
	for _, v := range []string{"1.0", "1.1", "1.2.3", "1.0.0"} {
		if !CompatibleVersion(v) {
			t.Errorf("version %q should be compatible with %q", v, ReadContractVersion)
		}
	}
	for _, v := range []string{"", "0.9", "2.0", "1", "one.two", "v1.0"} {
		if CompatibleVersion(v) {
			t.Errorf("version %q should be incompatible with %q", v, ReadContractVersion)
		}
	}
}

func TestValidDay(t *testing.T) {
	for _, d := range []string{"", "2026-01-01", "2025-12-31"} {
		if !ValidDay(d) {
			t.Errorf("day %q should be valid", d)
		}
	}
	for _, d := range []string{"2026-13-01", "01-01-2025", "20260101", "2026-1-1"} {
		if ValidDay(d) {
			t.Errorf("day %q should be invalid", d)
		}
	}
}
