package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenAPIIncludesCorePaths(t *testing.T) {
	doc := OpenAPI()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	paths, _ := doc["paths"].(map[string]any)
	for _, p := range []string{"/search", "/get", "/stats", "/audit"} {
		if _, ok := paths[p]; !ok {
			t.Fatalf("openapi missing path %s (%s)", p, raw)
		}
	}
}

func TestMCPToolsMatchOpenAPIPaths(t *testing.T) {
	paths, _ := OpenAPI()["paths"].(map[string]any)
	tools := MCPTools()
	if len(tools) == 0 {
		t.Fatal("no MCP tools")
	}
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
		path := "/" + tool.Name
		if _, ok := paths[path]; !ok {
			t.Fatalf("MCP tool %s has no OpenAPI path %s", tool.Name, path)
		}
	}
	for _, need := range []string{"search", "get", "audit"} {
		if !names[need] {
			t.Fatalf("MCP tools missing %s: %v", need, names)
		}
	}
	for _, no := range []string{"stats", "ingest"} {
		if names[no] {
			t.Fatalf("MCP tool %s should not be exposed (keep surface minimal): %v", no, names)
		}
	}
}

func TestOpenAPIHTTP(t *testing.T) {
	h := NewServer(&fakeSearcher{}, 1)
	code, body := get(t, h, "/openapi.json")
	if code != http.StatusOK {
		t.Fatalf("code = %d body=%s", code, body)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("not json: %v", err)
	}
	if doc["openapi"] == nil {
		t.Fatalf("missing openapi version: %s", body)
	}
}

func TestMCPToolsListAndCall(t *testing.T) {
	h := NewServer(&fakeSearcher{}, 1)
	code, body := postJSON(t, h, "/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if code != http.StatusOK {
		t.Fatalf("list code = %d body=%s", code, body)
	}
	if !strings.Contains(string(body), `"search"`) {
		t.Fatalf("tools/list missing search: %s", body)
	}
	code, body = postJSON(t, h, "/mcp", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"search","arguments":{"q":"matrix","n":3}}}`)
	if code != http.StatusOK {
		t.Fatalf("call code = %d body=%s", code, body)
	}
	if !strings.Contains(string(body), "matrix") {
		t.Fatalf("search call body %s", body)
	}
}

func TestSkillMarkdownMatchesCommittedFile(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("..", "..", "skills", "brain", "references", "tools.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := SkillMarkdown()
	if got != string(want) {
		t.Fatalf("skills/brain/references/tools.md stale; regenerate from SkillMarkdown()\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func postJSON(t *testing.T, h http.Handler, path, raw string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}
