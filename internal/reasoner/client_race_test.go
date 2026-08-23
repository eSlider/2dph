package reasoner

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestClientConcurrentRunSafe drives the reasoner Client (used by
// bin/reasoner/bench.go) from many goroutines at once — the "bench concurrent
// runs" path. The Client is documented as concurrency-safe: it holds only an
// *http.Client and an immutable Config, and Run keeps all accounting in
// call-local state. Under -race this must be clean even when several bench runs
// share one client against the same backend.
func TestClientConcurrentRunSafe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/ps" {
			_, _ = w.Write([]byte(`{"models":[{"name":"stub","size":6500000000,"size_vram":0}]}`))
			return
		}
		// Echo the tool each prompt wants so all three bake-off steps pass.
		tc := echoTool(r)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{
					"tool_calls": []any{map[string]any{
						"function": map[string]any{"name": tc, "arguments": "{}"},
					}},
				},
			}},
		})
	}))
	defer srv.Close()

	c := New(&Config{BaseURL: srv.URL + "/v1", Model: "stub", Device: "cpu", HTTP: srv.Client()})
	var wg sync.WaitGroup
	for w := 0; w < 12; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rep := c.Run(t.Context())
			if rep.ToolCallN != len(BakePrompts) {
				t.Errorf("tool_call_n=%d, want %d", rep.ToolCallN, len(BakePrompts))
				return
			}
			if rep.ToolCallOK != len(BakePrompts) {
				t.Errorf("tool_call_ok=%d, want %d", rep.ToolCallOK, len(BakePrompts))
				return
			}
			if rep.RSSMB == 0 {
				t.Errorf("rss not sampled")
				return
			}
		}()
	}
	wg.Wait()
}

// TestClientSharedClientConcurrent proves a single Client instance is safe to
// reuse from concurrent callers over a shared transport (each RunPrompt gets
// its own request and result; nothing is mutated on the receiver).
func TestClientSharedClientConcurrent(t *testing.T) {
	var hits int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		_ = payload
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"tool_calls":[{"function":{"name":"search","arguments":"{}"}}]}}]}`))
	}))
	defer srv.Close()

	c := New(&Config{BaseURL: srv.URL + "/v1", Model: "stub", Device: "cpu", HTTP: srv.Client()})
	var wg sync.WaitGroup
	for w := 0; w < 16; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				r := c.RunPrompt(t.Context(), BakePrompts[0])
				if !r.OK {
					t.Errorf("prompt=%+v", r)
					return
				}
			}
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if hits != 16*10 {
		t.Errorf("hits=%d, want %d", hits, 16*10)
	}
}

// echoTool reads the last user message from a chat request and returns the
// tool the prompt asks for. Each bake-off prompt names its wanted tool, so we
// return whichever of the three tool names appears in the message.
func echoTool(r *http.Request) string {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(body, &req)
	content := ""
	for _, m := range req.Messages {
		if m.Role == "user" {
			content = m.Content
		}
	}
	for _, name := range []string{"audit", "search", "get"} {
		if strings.Contains(content, name) {
			return name
		}
	}
	return "search"
}
