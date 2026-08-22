package reasoner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(&Config{BaseURL: srv.URL + "/v1", Model: "stub", Device: "cpu", HTTP: srv.Client()})
}

func chatResponseToolCall() string {
	return `{"model":"stub","choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"search","arguments":"{\"q\":\"LadybugDB\"}"}}]}}]}`
}

func TestChatCompletionsRoundTrip(t *testing.T) {
	var gotBody map[string]any
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path=%s", r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &gotBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(chatResponseToolCall()))
	}))

	choice := "required"
	resp, err := c.Chat(context.Background(), ChatRequest{
		Model: "stub",
		Messages: []ChatMessage{
			{Role: "system", Content: "behave"},
			{Role: "user", Content: "use tools"},
		},
		Tools:      MCPTools(),
		ToolChoice: &choice,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Model != "stub" || len(resp.Choices) != 1 {
		t.Fatalf("resp=%+v", resp)
	}
	tc, content, err := extractTool(resp)
	if err != nil {
		t.Fatal(err)
	}
	if tc.Name != "search" || !strings.Contains(tc.Arguments, "LadybugDB") {
		t.Fatalf("tc=%+v content=%q", tc, content)
	}
	if gotBody["model"] != "stub" {
		t.Fatalf("body model=%v", gotBody["model"])
	}
	if _, ok := gotBody["tools"]; !ok {
		t.Fatal("tools missing from request body")
	}
	if gotBody["tool_choice"] != "required" {
		t.Fatalf("tool_choice=%v", gotBody["tool_choice"])
	}
	if gotBody["stream"] != nil && gotBody["stream"] != false {
		t.Fatalf("stream should be off: %v", gotBody["stream"])
	}
}

func TestChatToolCallContentAndNoCall(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"plain answer"}}]}`))
	}))
	resp, err := c.Chat(context.Background(), ChatRequest{Model: "stub", Messages: []ChatMessage{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, content, err := extractTool(resp); err == nil || content != "plain answer" {
		t.Fatalf("content=%q err=%v", content, err)
	}
}

func TestChatHTTPErrorStatus(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"model not loaded"}`, http.StatusBadGateway)
	}))
	_, err := c.Chat(context.Background(), ChatRequest{Model: "stub"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "model not loaded") {
		t.Fatalf("err=%v", err)
	}
}

func TestChatHonorsContextCancel(t *testing.T) {
	done := make(chan struct{})
	defer close(done)
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-done
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Chat(ctx, ChatRequest{Model: "stub"}); err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestDoStreamCollectsNDJSON(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = io.WriteString(w, `{"a":1}`+"\n"+`{"a":2}`+"\n")
	}))
	var got []int
	err := c.doStream(context.Background(), http.MethodPost, c.chatURL(), ChatRequest{Model: "stub"}, func(raw json.RawMessage) error {
		var v struct {
			A int `json:"a"`
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		got = append(got, v.A)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("got=%v", got)
	}
}

func TestFetchPSTyped(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"stub","size":6500000000,"size_vram":0}]}`))
	}))
	mems, err := c.FetchPS(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 1 || mems[0].VRAMMB != 0 || mems[0].SizeMB < 6000 {
		t.Fatalf("mems=%+v", mems)
	}
}

func TestLoadEnvReadsReasonerConfig(t *testing.T) {
	t.Setenv("REASONER_BASE_URL", "http://cfg.test:9999/v1")
	t.Setenv("REASONER_MODEL", "cfg-model")
	t.Setenv("REASONER_DEVICE", "gpu")
	cfg := LoadEnv()
	if cfg.BaseURL != "http://cfg.test:9999/v1" || cfg.Model != "cfg-model" || cfg.Device != "gpu" {
		t.Fatalf("cfg=%+v", cfg)
	}
}

func TestNewDefaultsTimeoutAndURL(t *testing.T) {
	c := New(nil)
	if c.cfg.BaseURL != DefaultBaseURL {
		t.Fatalf("base=%q", c.cfg.BaseURL)
	}
	if c.cfg.Timeout != 10*time.Minute {
		t.Fatalf("timeout=%v", c.cfg.Timeout)
	}
	if c.hc == nil {
		t.Fatal("http client nil")
	}
}
