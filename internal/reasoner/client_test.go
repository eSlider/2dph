package reasoner

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eSlider/2dph/pkg/httpapi"
)

func TestHFIdsAreRealAndNoQwen36Nine(t *testing.T) {
	if HFQwen35_9B != "Qwen/Qwen3.5-9B" {
		t.Fatalf("9B id = %s", HFQwen35_9B)
	}
	if HFQwen36_27B != "Qwen/Qwen3.6-27B" {
		t.Fatalf("27B id = %s", HFQwen36_27B)
	}
	if HFBonsai27B != "prism-ml/Bonsai-27B-gguf" {
		t.Fatalf("bonsai id = %s", HFBonsai27B)
	}
}

func TestParseToolResponseOpenAI(t *testing.T) {
	raw := []byte(`{"choices":[{"message":{"tool_calls":[{"function":{"name":"search","arguments":"{\"q\":\"LadybugDB\"}"}}]}}]}`)
	tc, _, err := ParseToolResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if tc.Name != "search" {
		t.Fatalf("name=%s", tc.Name)
	}
}

func TestParseToolResponseXMLIsFailure(t *testing.T) {
	raw := []byte(`{"choices":[{"message":{"content":"<tool_call>search</tool_call>"}}]}`)
	_, content, err := ParseToolResponse(raw)
	if err == nil {
		t.Fatal("expected no tool_calls")
	}
	if !XMLLeak(content) {
		t.Fatal("xml leak not detected")
	}
}

func TestBakePromptsWantMCPTools(t *testing.T) {
	if len(BakePrompts) != 3 {
		t.Fatalf("prompts=%d", len(BakePrompts))
	}
	names := map[string]bool{}
	for _, p := range BakePrompts {
		names[p.Want] = true
	}
	for _, n := range []string{"search", "get", "audit"} {
		if !names[n] {
			t.Fatalf("missing want %s", n)
		}
	}
}

func TestParseToolResponseObjectArgs(t *testing.T) {
	raw := []byte(`{"choices":[{"message":{"tool_calls":[{"function":{"name":"get","arguments":{"id":"leaf-demo"}}}]}}]}`)
	tc, _, err := ParseToolResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if tc.Name != "get" {
		t.Fatalf("name=%s", tc.Name)
	}
	if !strings.Contains(tc.Arguments, "leaf-demo") {
		t.Fatalf("args=%s", tc.Arguments)
	}
}

func TestParsePSCPUNotVRAM(t *testing.T) {
	raw := []byte(`{"models":[{"name":"qwen3.5:9b","size":6900000000,"size_vram":0}]}`)
	got := ParsePS(raw)
	if len(got) != 1 {
		t.Fatalf("n=%d", len(got))
	}
	if got[0].VRAMMB != 0 {
		t.Fatalf("vram=%d", got[0].VRAMMB)
	}
	if got[0].SizeMB < 6000 {
		t.Fatalf("rss=%d", got[0].SizeMB)
	}
}

func TestMCPToolsArePicoClawSubset(t *testing.T) {
	mcp := map[string]bool{}
	for _, op := range httpapi.Ops {
		if op.MCP {
			mcp[op.ID] = true
		}
	}
	seen := map[string]bool{}
	for _, tool := range MCPTools() {
		fn, _ := tool["function"].(map[string]any)
		name, _ := fn["name"].(string)
		if !mcp[name] {
			t.Fatalf("%s is not an MCP op", name)
		}
		seen[name] = true
	}
	for _, n := range []string{"search", "get", "audit"} {
		if !seen[n] {
			t.Fatalf("bake-off missing %s", n)
		}
	}
}

func TestChatToolsHitsOpenAIPath(t *testing.T) {
	var gotTools bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path=%s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		if _, ok := payload["tools"]; ok {
			gotTools = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"tool_calls":[{"function":{"name":"search","arguments":"{\"q\":\"LadybugDB\"}"}}]}}]}`))
	}))
	defer srv.Close()
	c := Client{BaseURL: srv.URL + "/v1", Model: "stub", HTTP: srv.Client(), Device: "cpu"}
	r := RunPrompt(c, BakePrompts[0])
	if !gotTools {
		t.Fatal("tools not sent")
	}
	if !r.OK {
		t.Fatalf("result=%+v", r)
	}
}

func TestRunSamplesPSAfterPrompts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/ps" {
			_, _ = w.Write([]byte(`{"models":[{"name":"stub","size":6500000000,"size_vram":0}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"tool_calls":[{"function":{"name":"search","arguments":"{}"}}]}}]}`))
	}))
	defer srv.Close()
	c := Client{BaseURL: srv.URL + "/v1", Model: "stub", HTTP: srv.Client(), Device: "cpu"}
	if mems := c.FetchPS(); len(mems) != 1 || mems[0].VRAMMB != 0 {
		t.Fatalf("%+v", mems)
	}
	if mems := c.FetchPS(); mems[0].SizeMB < 6000 {
		t.Fatalf("rss=%d", mems[0].SizeMB)
	}
}

func TestHFForKnownOllamaTags(t *testing.T) {
	if HFFor(OllamaRAM) != HFQwen35_9B {
		t.Fatal(HFFor(OllamaRAM))
	}
	if HFFor(OllamaQuality) != HFBonsai27B {
		t.Fatal(HFFor(OllamaQuality))
	}
}
