package reasoner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is the OpenAI-compatible base URL used when none is configured.
const DefaultBaseURL = "http://127.0.0.1:11435/v1"

// HF IDs named in docs. No Qwen3.6-9B exists.
const (
	HFQwen35_9B   = "Qwen/Qwen3.5-9B"
	HFQwen36_27B  = "Qwen/Qwen3.6-27B"
	HFBonsai27B   = "prism-ml/Bonsai-27B-gguf"
	OllamaRAM     = "qwen3.5:9b"
	OllamaQuality = "MichelRosselli/bonsai-27b:Q1_0"
)

// ToolCall is the bake-off outcome: one requested tool invocation.
type ToolCall struct {
	Name      string
	Arguments string
}

// Result is the outcome of one bake-off prompt.
type Result struct {
	Model      string `json:"model"`
	OK         bool   `json:"ok"`
	ToolName   string `json:"tool_name,omitempty"`
	XMLLeak    bool   `json:"xml_leak"`
	Err        string `json:"error,omitempty"`
	LatencyMS  int64  `json:"latency_ms"`
	RSSMB      int    `json:"rss_mb,omitempty"`
	Device     string `json:"device"`
	WantedTool string `json:"wanted_tool"`
}

type Prompt struct {
	Name string
	Want string
	User string
}

var BakePrompts = []Prompt{
	{
		Name: "search-before-claim",
		Want: "search",
		User: "Use tools. Search the 2dph brain for LadybugDB before you answer. Call search.",
	},
	{
		Name: "get-leaf",
		Want: "get",
		User: "Use tools. Fetch leaf id leaf-demo with get. Do not invent the body.",
	},
	{
		Name: "audit-index",
		Want: "audit",
		User: "Use tools. Call audit on the brain index health.",
	},
}

const bakeSystemPrompt = "You are PicoClaw talking to 2dph MCP. Always call a tool before a factual claim. search then get then audit."

// Tool is an OpenAI-style function tool schema sent in the chat request.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Parameters  ToolSchema `json:"parameters"`
}

// ToolSchema is a minimal JSON-schema object for tool parameters.
type ToolSchema struct {
	Type        string                `json:"type"`
	Description string                `json:"description,omitempty"`
	Properties  map[string]ToolSchema `json:"properties,omitempty"`
	Required    []string              `json:"required,omitempty"`
}

// MCPTools returns the OpenAI function tools for the bake-off MCP ops.
func MCPTools() []Tool {
	return []Tool{
		openaiTool("search", "deduction search (facts → info → web)", ToolSchema{
			Type: "object",
			Properties: map[string]ToolSchema{
				"q": {Type: "string", Description: "search query"},
				"n": {Type: "integer"},
			},
			Required: []string{"q"},
		}),
		openaiTool("get", "read one leaf by id", ToolSchema{
			Type: "object",
			Properties: map[string]ToolSchema{
				"id":   {Type: "string"},
				"body": {Type: "boolean"},
			},
			Required: []string{"id"},
		}),
		openaiTool("audit", "facts confidence histogram", ToolSchema{Type: "object"}),
	}
}

func openaiTool(name, desc string, schema ToolSchema) Tool {
	return Tool{
		Type: "function",
		Function: ToolFunction{
			Name:        name,
			Description: desc,
			Parameters:  schema,
		},
	}
}

// Config holds reasoner client settings. Zero values fall back to defaults
// inside New; BaseURL defaults to DefaultBaseURL and Timeout to 10m.
type Config struct {
	BaseURL    string
	Model      string
	Device     string
	ToolChoice string // "", "required", "auto", "none"; "" behaves as "required"
	Timeout    time.Duration
	HTTP       *http.Client
}

// Client is a concurrency-safe OpenAI-compatible reasoner client.
type Client struct {
	hc  *http.Client
	cfg Config
}

// New builds a reasoner client. A nil cfg is valid; BaseURL/Timeout default in.
// The optional cfg.HTTP transport is reused when provided.
func New(cfg *Config) *Client {
	var c Config
	if cfg != nil {
		c = *cfg
	}
	if strings.TrimSpace(c.BaseURL) == "" {
		c.BaseURL = DefaultBaseURL
	}
	if c.Timeout == 0 {
		c.Timeout = 10 * time.Minute
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: c.Timeout}
	}
	return &Client{hc: hc, cfg: c}
}

type Report struct {
	Model        string   `json:"model"`
	HF           string   `json:"hf_id"`
	Device       string   `json:"device"`
	ToolCallOK   int      `json:"tool_call_ok"`
	ToolCallN    int      `json:"tool_call_n"`
	XMLLeak      int      `json:"xml_leak"`
	RSSMB        int      `json:"rss_mb"`
	VRAMMB       int      `json:"vram_mb"`
	LatencyP50MS float64  `json:"latency_p50_ms,omitempty"`
	LatencyP95MS float64  `json:"latency_p95_ms,omitempty"`
	Prompts      []Result `json:"prompts"`
}

func HFFor(model string) string {
	switch model {
	case OllamaRAM:
		return HFQwen35_9B
	case OllamaQuality:
		return HFBonsai27B
	default:
		if strings.Contains(model, "qwen3.6") || strings.Contains(model, "Qwen3.6") {
			return HFQwen36_27B
		}
		return ""
	}
}

func Origin(base string) string {
	s := strings.TrimRight(base, "/")
	return strings.TrimSuffix(s, "/v1")
}

func (c *Client) chatURL() string { return strings.TrimRight(c.cfg.BaseURL, "/") + "/chat/completions" }
func (c *Client) psURL() string   { return Origin(c.cfg.BaseURL) + "/api/ps" }

// doJSON performs a JSON request and decodes the response into dest.
func (c *Client) doJSON(ctx context.Context, method, url string, body, dest any) error {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("reasoner: marshal request: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return fmt.Errorf("reasoner: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("reasoner: send request: %w", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("reasoner: %s %s: http %d: %s", method, url, res.StatusCode, snippet(string(raw), 300))
	}
	if dest != nil {
		if err := json.Unmarshal(raw, dest); err != nil {
			return fmt.Errorf("reasoner: decode response: %w", err)
		}
	}
	return nil
}

// doStream reads newline-delimited JSON, calling onItem for each non-empty line.
func (c *Client) doStream(ctx context.Context, method, url string, body any, onItem func(json.RawMessage) error) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("reasoner: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("reasoner: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	res, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("reasoner: send request: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		raw, _ := io.ReadAll(res.Body)
		return fmt.Errorf("reasoner: %s %s: http %d: %s", method, url, res.StatusCode, snippet(string(raw), 300))
	}
	scanner := bufio.NewScanner(res.Body)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := onItem(append([]byte(nil), line...)); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reasoner: read stream: %w", err)
	}
	return nil
}

// --- chat completions -------------------------------------------------------

type ChatRequest struct {
	Model      string        `json:"model"`
	Messages   []ChatMessage `json:"messages"`
	Tools      []Tool        `json:"tools,omitempty"`
	ToolChoice *string       `json:"tool_choice,omitempty"`
	Stream     bool          `json:"stream,omitempty"`
}

type ChatMessage struct {
	Role      string         `json:"role"`
	Content   string         `json:"content,omitempty"`
	ToolCalls []ChatToolCall `json:"tool_calls,omitempty"`
}

type ChatToolCall struct {
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function ChatToolCallFunc `json:"function"`
}

type ChatToolCallFunc struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type ChatResponse struct {
	Model   string       `json:"model"`
	Choices []ChatChoice `json:"choices"`
}

type ChatChoice struct {
	Index   int         `json:"index"`
	Message ChatMessage `json:"message"`
}

// Chat posts a chat completion request. Streaming (req.Stream) collects
// NDJSON chunks into a single response; otherwise the JSON body is decoded.
func (c *Client) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	if req.Model == "" {
		req.Model = c.cfg.Model
	}
	var resp ChatResponse
	if req.Stream {
		err := c.doStream(ctx, http.MethodPost, c.chatURL(), req, func(raw json.RawMessage) error {
			var chunk ChatResponse
			if err := json.Unmarshal(raw, &chunk); err != nil {
				return fmt.Errorf("reasoner: decode chat chunk: %w", err)
			}
			resp.Choices = append(resp.Choices, chunk.Choices...)
			return nil
		})
		return &resp, err
	}
	if err := c.doJSON(ctx, http.MethodPost, c.chatURL(), req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// extractTool pulls the first tool call and message content off a response.
func extractTool(resp *ChatResponse) (ToolCall, string, error) {
	if resp == nil || len(resp.Choices) == 0 {
		return ToolCall{}, "", fmt.Errorf("reasoner: empty response")
	}
	msg := resp.Choices[0].Message
	if n := len(msg.ToolCalls); n > 0 {
		fn := msg.ToolCalls[0].Function
		return ToolCall{Name: fn.Name, Arguments: rawArgs(fn.Arguments)}, msg.Content, nil
	}
	return ToolCall{}, msg.Content, fmt.Errorf("reasoner: no tool_calls")
}

// ParseToolResponse extracts a tool call from a raw chat-completions body.
func ParseToolResponse(raw []byte) (ToolCall, string, error) {
	var resp ChatResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ToolCall{}, "", err
	}
	return extractTool(&resp)
}

func rawArgs(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

func XMLLeak(content string) bool {
	s := strings.ToLower(content)
	return strings.Contains(s, "<tool_call>") ||
		strings.Contains(s, "<function=") ||
		strings.Contains(s, "<parameter")
}

func (c *Client) bakeRequest(user string) ChatRequest {
	choice := c.cfg.ToolChoice
	if choice == "" {
		choice = "required"
	}
	return ChatRequest{
		Model: c.cfg.Model,
		Messages: []ChatMessage{
			{Role: "system", Content: bakeSystemPrompt},
			{Role: "user", Content: user},
		},
		Tools:      MCPTools(),
		ToolChoice: &choice,
	}
}

// RunPrompt runs one bake-off prompt against the client.
func (c *Client) RunPrompt(ctx context.Context, p Prompt) Result {
	start := time.Now()
	resp, err := c.Chat(ctx, c.bakeRequest(p.User))
	out := Result{
		Model:      c.cfg.Model,
		WantedTool: p.Want,
		LatencyMS:  time.Since(start).Milliseconds(),
		Device:     c.cfg.Device,
	}
	if err != nil {
		out.Err = err.Error()
		return out
	}
	tc, content, err := extractTool(resp)
	out.XMLLeak = XMLLeak(content)
	if err != nil {
		out.Err = err.Error()
		if content != "" && out.XMLLeak {
			out.Err = "xml tool call instead of openai tool_calls"
		}
		return out
	}
	out.ToolName = tc.Name
	out.OK = tc.Name == p.Want
	if !out.OK {
		out.Err = "wanted " + p.Want + " got " + tc.Name
	}
	return out
}

// --- /api/ps ----------------------------------------------------------------

type ProcMem struct {
	Name   string
	SizeMB int
	VRAMMB int
}

type psEnvelope struct {
	Models []psModel `json:"models"`
}

type psModel struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	SizeVRAM int64  `json:"size_vram"`
}

func ParsePS(raw []byte) []ProcMem {
	var wrap psEnvelope
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil
	}
	return psFromEnvelope(wrap)
}

func psFromEnvelope(wrap psEnvelope) []ProcMem {
	out := make([]ProcMem, 0, len(wrap.Models))
	for _, m := range wrap.Models {
		out = append(out, ProcMem{
			Name:   m.Name,
			SizeMB: int(m.Size / (1024 * 1024)),
			VRAMMB: int(m.SizeVRAM / (1024 * 1024)),
		})
	}
	return out
}

// FetchPS returns the models currently loaded in memory.
func (c *Client) FetchPS(ctx context.Context) ([]ProcMem, error) {
	var wrap psEnvelope
	if err := c.doJSON(ctx, http.MethodGet, c.psURL(), nil, &wrap); err != nil {
		return nil, err
	}
	return psFromEnvelope(wrap), nil
}

// Run executes the CPU tool-call bake-off.
func (c *Client) Run(ctx context.Context) Report {
	cfg := c.cfg
	device := cfg.Device
	if device == "" {
		device = "cpu"
	}
	rep := Report{
		Model:  cfg.Model,
		HF:     HFFor(cfg.Model),
		Device: device,
	}
	for _, p := range BakePrompts {
		r := c.RunPrompt(ctx, p)
		rep.Prompts = append(rep.Prompts, r)
		rep.ToolCallN++
		if r.OK {
			rep.ToolCallOK++
		}
		if r.XMLLeak {
			rep.XMLLeak++
		}
	}
	if mems, err := c.FetchPS(ctx); err == nil && len(mems) > 0 {
		rep.RSSMB = mems[0].SizeMB
		rep.VRAMMB = mems[0].VRAMMB
		if device == "cpu" && mems[0].VRAMMB > 0 {
			rep.Device = "gpu"
		}
		for i := range rep.Prompts {
			rep.Prompts[i].RSSMB = rep.RSSMB
		}
	}
	return rep
}

func snippet(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
